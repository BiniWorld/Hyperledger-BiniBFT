/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	cb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/msp"
	"github.com/hyperledger/fabric/common/crypto"
	binibftconsensus "github.com/hyperledger/fabric/orderer/consensus/binibft/consensus"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
)

// FabricNetworkAdapter adapts Fabric's communication to BiniBFT's NetworkInterface
type FabricNetworkAdapter struct {
	comm   *EgressComm
	logger *flogging.FabricLogger
}

func (n *FabricNetworkAdapter) Send(nodeID binibftconsensus.NodeID, message binibftconsensus.Message) error {
	// Convert BiniBFT message to Fabric BiniBFT message
	fabricMsg := BiniBFTMessage{
		Type:      MessageType(message.Type),
		From:      NodeID(message.From),
		To:        NodeID(message.To),
		ShardID:   ShardID(message.ShardID),
		Timestamp: message.Timestamp,
	}

	// Marshal the payload
	payloadBytes, err := json.Marshal(message.Payload)
	if err != nil {
		return err
	}
	fabricMsg.Payload = payloadBytes

	return n.comm.Send(NodeID(nodeID), fabricMsg)
}

func (n *FabricNetworkAdapter) Broadcast(nodeIDs []binibftconsensus.NodeID, message binibftconsensus.Message) error {
	for _, nodeID := range nodeIDs {
		if err := n.Send(nodeID, message); err != nil {
			return err
		}
	}
	return nil
}

func (n *FabricNetworkAdapter) RegisterHandler(handler binibftconsensus.MessageHandler) {
	// Create a bridge handler that converts between Fabric and BiniBFT message formats
	bridgeHandler := &FabricMessageHandler{
		consensusHandler: handler,
		logger:           n.logger,
	}
	n.comm.RegisterHandler(bridgeHandler)
}

func (n *FabricNetworkAdapter) SendTransaction(targetID binibftconsensus.NodeID, request []byte) error {
	// For transaction forwarding, use the same Send mechanism
	msg := binibftconsensus.Message{
		Type:      binibftconsensus.MsgRequest,
		From:      binibftconsensus.NodeID("fabric"),
		To:        targetID,
		Timestamp: time.Now(),
		Payload:   request,
	}
	return n.Send(targetID, msg)
}

// FabricLoggerAdapter adapts Fabric's logger to BiniBFT's Logger interface
type FabricLoggerAdapter struct {
	logger *flogging.FabricLogger
}

func (l *FabricLoggerAdapter) Info(msg string, fields ...any) {
	l.logger.Infof(msg, fields...)
}

func (l *FabricLoggerAdapter) Error(msg string, fields ...any) {
	l.logger.Errorf(msg, fields...)
}

func (l *FabricLoggerAdapter) Debug(msg string, fields ...any) {
	l.logger.Debugf(msg, fields...)
}

// FabricApplicationAdapter adapts Fabric's application delivery
type FabricApplicationAdapter struct {
	app    ApplicationDelivery
	logger *flogging.FabricLogger
}

func (a *FabricApplicationAdapter) Deliver(proposal binibftconsensus.Proposal, signatures []binibftconsensus.Signature) error {
	// Convert BiniBFT proposal to Fabric proposal
	fabricProposal := BiniBFTProposal{
		Payload:              proposal.Payload,
		Header:               proposal.Header,
		Metadata:             proposal.Metadata,
		VerificationSequence: proposal.VerificationSequence,
	}

	// Convert BiniBFT signatures to Fabric signatures
	fabricSignatures := a.convertSignatures(signatures, fabricProposal)

	// Log the delivery attempt
	if a.logger != nil {
		a.logger.Infof("FabricApplicationAdapter: Delivering proposal with %d signatures", len(fabricSignatures))
	}

	reconfig := a.app.Deliver(fabricProposal, fabricSignatures)

	// Log the result
	if a.logger != nil {
		a.logger.Infof("FabricApplicationAdapter: Delivery completed, reconfig: %+v", reconfig)
	}

	return nil
}

// convertSignatures converts BiniBFT consensus signatures to Fabric signatures
func (a *FabricApplicationAdapter) convertSignatures(consensusSignatures []binibftconsensus.Signature, proposal BiniBFTProposal) []BiniBFTSignature {
	var fabricSignatures []BiniBFTSignature

	// Convert proposal to block to get block header for signature creation
	block, err := ProposalToBlock(proposal)
	if err != nil {
		if a.logger != nil {
			a.logger.Errorf("Failed to convert proposal to block for signature conversion: %v", err)
		}
		return fabricSignatures
	}

	for _, consensusSig := range consensusSignatures {
		// Create proper signature structure (same as SmartBFT)
		nonce := a.randomNonceOrPanic()

		sig := &Signature{
			BlockHeader:      protoutil.BlockHeaderBytes(block.Header),
			IdentifierHeader: protoutil.MarshalOrPanic(a.newIdentifierHeader(nonce, consensusSig.ID)),
			OrdererBlockMetadata: protoutil.MarshalOrPanic(&cb.OrdererBlockMetadata{
				LastConfig:        &cb.LastConfig{Index: a.getLastConfigBlockNum(block)},
				ConsenterMetadata: proposal.Metadata,
			}),
		}

		fabricSignatures = append(fabricSignatures, BiniBFTSignature{
			ID:    consensusSig.ID,
			Value: consensusSig.Value, // Use the actual signature from consensus
			Msg:   sig.Marshal(),      // Marshal the signature structure
		})
	}

	return fabricSignatures
}

// Helper methods (similar to SmartBFT signer)
func (a *FabricApplicationAdapter) randomNonceOrPanic() []byte {
	nonce, err := crypto.GetRandomNonce()
	if err != nil {
		panic(err)
	}
	return nonce
}

func (a *FabricApplicationAdapter) newIdentifierHeader(nonce []byte, nodeID uint64) *cb.IdentifierHeader {
	return &cb.IdentifierHeader{
		Identifier: uint32(nodeID), // Use the actual node ID from consensus
		Nonce:      nonce,
	}
}

func (a *FabricApplicationAdapter) getLastConfigBlockNum(block *cb.Block) uint64 {
	// Extract the last config block number from block metadata
	// This follows the same approach as SmartBFT
	if block.Metadata != nil && len(block.Metadata.Metadata) > int(cb.BlockMetadataIndex_SIGNATURES) {
		signaturesMetadata := block.Metadata.Metadata[cb.BlockMetadataIndex_SIGNATURES]
		if len(signaturesMetadata) > 0 {
			metadata := &cb.Metadata{}
			if err := proto.Unmarshal(signaturesMetadata, metadata); err == nil {
				if len(metadata.Value) > 0 {
					ordererBlockMetadata := &cb.OrdererBlockMetadata{}
					if err := proto.Unmarshal(metadata.Value, ordererBlockMetadata); err == nil {
						if ordererBlockMetadata.LastConfig != nil {
							return ordererBlockMetadata.LastConfig.Index
						}
					}
				}
			}
		}
	}

	// If we can't extract from metadata, check if this is a config block
	if protoutil.IsConfigBlock(block) {
		return block.Header.Number
	}

	// Default to 0 if we can't determine the last config block
	return 0
}

// FabricAssemblerAdapter adapts Fabric's assembler to BiniBFT's Assembler interface
type FabricAssemblerAdapter struct {
	assembler *Assembler
}

func (a *FabricAssemblerAdapter) AssembleProposal(metadata []byte, requests [][]byte) binibftconsensus.Proposal {
	// Use the Fabric assembler to create a BiniBFTProposal
	fabricProposal := a.assembler.AssembleProposal(metadata, requests)

	// Convert BiniBFTProposal to binibftconsensus.Proposal
	return binibftconsensus.Proposal{
		Payload:              fabricProposal.Payload,
		Header:               fabricProposal.Header,
		Metadata:             fabricProposal.Metadata,
		VerificationSequence: fabricProposal.VerificationSequence,
	}
}

// FabricSignerAdapter adapts Fabric's signer to BiniBFT's Signer interface
type FabricSignerAdapter struct {
	signer *Signer
}

func (s *FabricSignerAdapter) SignProposal(proposal binibftconsensus.Proposal, data []byte) *binibftconsensus.Signature {
	// Convert consensus proposal to BiniBFT proposal for block creation
	fabricProposal := BiniBFTProposal{
		Payload:              proposal.Payload,
		Header:               proposal.Header,
		Metadata:             proposal.Metadata,
		VerificationSequence: proposal.VerificationSequence,
	}

	// Convert proposal to block (same approach as SmartBFT)
	block, err := ProposalToBlock(fabricProposal)
	if err != nil {
		// Log error but don't panic, return nil instead
		return nil
	}

	// Create proper signature structure (same as SmartBFT)
	nonce := s.randomNonceOrPanic()

	sig := &Signature{
		BlockHeader:      protoutil.BlockHeaderBytes(block.Header),
		IdentifierHeader: protoutil.MarshalOrPanic(s.newIdentifierHeaderOrPanic(nonce)),
		OrdererBlockMetadata: protoutil.MarshalOrPanic(&cb.OrdererBlockMetadata{
			LastConfig:        &cb.LastConfig{Index: s.getLastConfigBlockNum(block)},
			ConsenterMetadata: proposal.Metadata,
		}),
	}

	// Sign the signature structure
	signature, err := s.signer.SignerSerializer.Sign(sig.AsBytes())
	if err != nil {
		return nil
	}

	return &binibftconsensus.Signature{
		ID:    s.signer.ID,
		Value: signature,
		Msg:   sig.Marshal(),
	}
}

// Helper methods (same as SmartBFT)
func (s *FabricSignerAdapter) randomNonceOrPanic() []byte {
	nonce, err := crypto.GetRandomNonce()
	if err != nil {
		panic(err)
	}
	return nonce
}

func (s *FabricSignerAdapter) newIdentifierHeaderOrPanic(nonce []byte) *cb.IdentifierHeader {
	return &cb.IdentifierHeader{
		Identifier: uint32(s.signer.ID),
		Nonce:      nonce,
	}
}

func (s *FabricSignerAdapter) getLastConfigBlockNum(block *cb.Block) uint64 {
	// Extract the last config block number from block metadata
	// This follows the same approach as SmartBFT
	if block.Metadata != nil && len(block.Metadata.Metadata) > int(cb.BlockMetadataIndex_SIGNATURES) {
		signaturesMetadata := block.Metadata.Metadata[cb.BlockMetadataIndex_SIGNATURES]
		if len(signaturesMetadata) > 0 {
			metadata := &cb.Metadata{}
			if err := proto.Unmarshal(signaturesMetadata, metadata); err == nil {
				if len(metadata.Value) > 0 {
					ordererBlockMetadata := &cb.OrdererBlockMetadata{}
					if err := proto.Unmarshal(metadata.Value, ordererBlockMetadata); err == nil {
						if ordererBlockMetadata.LastConfig != nil {
							return ordererBlockMetadata.LastConfig.Index
						}
					}
				}
			}
		}
	}

	// If we can't extract from metadata, check if this is a config block
	if protoutil.IsConfigBlock(block) {
		return block.Header.Number
	}

	// Default to 0 if we can't determine the last config block
	return 0
}

func (s *FabricSignerAdapter) Sign(msg []byte) []byte {
	sig, _ := s.signer.SignerSerializer.Sign(msg)
	return sig
}

// FabricRequestInspectorAdapter adapts Fabric's request inspection to BiniBFT's RequestInspector interface
type FabricRequestInspectorAdapter struct {
	ValidateIdentityStructure func(identity *msp.SerializedIdentity) error
}

// request struct to hold unwrapped request data (same as SmartBFT)
type request struct {
	sigHdr   *cb.SignatureHeader
	envelope *cb.Envelope
	chHdr    *cb.ChannelHeader
}

// RequestID unwraps the request info from the raw request (identical to SmartBFT)
func (r *FabricRequestInspectorAdapter) RequestID(rawReq []byte) binibftconsensus.RequestInfo {
	req, err := r.unwrapReq(rawReq)
	if err != nil {
		return binibftconsensus.RequestInfo{}
	}

	if req.chHdr.Type == int32(cb.HeaderType_CONFIG) {
		configEnvelope := &cb.ConfigEnvelope{}
		_, err = protoutil.UnmarshalEnvelopeOfType(req.envelope, cb.HeaderType_CONFIG, configEnvelope)
		if err != nil {
			return binibftconsensus.RequestInfo{}
		}

		reqInfo, err := r.requestIDFromEnvelope(configEnvelope.LastUpdate)
		if err != nil {
			return binibftconsensus.RequestInfo{}
		}

		return reqInfo
	}

	reqInfo, err := r.requestIDFromSigHeader(req.sigHdr)
	if err != nil {
		return binibftconsensus.RequestInfo{}
	}
	return reqInfo
}

// requestIDFromSigHeader extracts request info from signature header (identical to SmartBFT)
func (r *FabricRequestInspectorAdapter) requestIDFromSigHeader(sigHdr *cb.SignatureHeader) (binibftconsensus.RequestInfo, error) {
	sID := &msp.SerializedIdentity{}
	if err := proto.Unmarshal(sigHdr.Creator, sID); err != nil {
		return binibftconsensus.RequestInfo{}, errors.Wrap(err, "identity isn't an MSP Identity")
	}

	if r.ValidateIdentityStructure != nil {
		if err := r.ValidateIdentityStructure(sID); err != nil {
			return binibftconsensus.RequestInfo{}, err
		}
	}

	var preimage []byte
	preimage = append(preimage, sigHdr.Nonce...)
	preimage = append(preimage, sigHdr.Creator...)
	txID := sha256.Sum256(preimage)
	clientID := sha256.Sum256(sigHdr.Creator)
	return binibftconsensus.RequestInfo{
		ID:       hex.EncodeToString(txID[:]),
		ClientID: hex.EncodeToString(clientID[:]),
	}, nil
}

// requestIDFromEnvelope extracts request info from envelope (identical to SmartBFT)
func (r *FabricRequestInspectorAdapter) requestIDFromEnvelope(envelope *cb.Envelope) (binibftconsensus.RequestInfo, error) {
	if envelope == nil {
		return binibftconsensus.RequestInfo{}, errors.New("proto: Marshal called with nil")
	}
	data, err := proto.Marshal(envelope)
	if err != nil {
		return binibftconsensus.RequestInfo{}, err
	}

	req, err := r.unwrapReqFromEnvelope(envelope)
	if err != nil {
		return binibftconsensus.RequestInfo{}, err
	}

	txID := sha256.Sum256(data)
	clientID := sha256.Sum256(req.sigHdr.Creator)
	return binibftconsensus.RequestInfo{
		ID:       hex.EncodeToString(txID[:]),
		ClientID: hex.EncodeToString(clientID[:]),
	}, nil
}

// unwrapReq unwraps request from raw bytes (identical to SmartBFT)
func (r *FabricRequestInspectorAdapter) unwrapReq(req []byte) (*request, error) {
	envelope, err := protoutil.UnmarshalEnvelope(req)
	if err != nil {
		return nil, err
	}

	return r.unwrapReqFromEnvelope(envelope)
}

// unwrapReqFromEnvelope unwraps request from envelope (identical to SmartBFT)
func (r *FabricRequestInspectorAdapter) unwrapReqFromEnvelope(envelope *cb.Envelope) (*request, error) {
	payload := &cb.Payload{}
	if err := proto.Unmarshal(envelope.Payload, payload); err != nil {
		return nil, errors.Wrap(err, "failed unmarshalling payload")
	}

	if payload.Header == nil {
		return nil, errors.Errorf("no header in payload")
	}

	sigHdr := &cb.SignatureHeader{}
	if err := proto.Unmarshal(payload.Header.SignatureHeader, sigHdr); err != nil {
		return nil, err
	}

	if len(payload.Header.ChannelHeader) == 0 {
		return nil, errors.New("no channel header in payload")
	}

	chdr, err := protoutil.UnmarshalChannelHeader(payload.Header.ChannelHeader)
	if err != nil {
		return nil, errors.WithMessage(err, "error unmarshalling channel header")
	}

	return &request{
		chHdr:    chdr,
		sigHdr:   sigHdr,
		envelope: envelope,
	}, nil
}

// FabricMessageHandler bridges between Fabric's MessageHandler and BiniBFT's MessageHandler
type FabricMessageHandler struct {
	consensusHandler binibftconsensus.MessageHandler
	logger           *flogging.FabricLogger
}

func (h *FabricMessageHandler) HandleMessage(from NodeID, message BiniBFTMessage) error {
	// Convert Fabric BiniBFT message to consensus message
	consensusMsg := binibftconsensus.Message{
		Type:      binibftconsensus.MessageType(message.Type),
		From:      binibftconsensus.NodeID(from),
		To:        binibftconsensus.NodeID(message.To),
		ShardID:   binibftconsensus.ShardID(message.ShardID),
		Timestamp: message.Timestamp,
		Payload:   message.Payload,
	}

	// Route to the consensus handler
	return h.consensusHandler.HandleMessage(binibftconsensus.NodeID(from), consensusMsg)
}
