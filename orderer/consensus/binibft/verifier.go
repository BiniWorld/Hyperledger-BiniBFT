/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	cb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/msp"
	"github.com/hyperledger/fabric/common/policies"
	"github.com/hyperledger/fabric/common/util"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"go.uber.org/zap/zapcore"
	"google.golang.org/protobuf/proto"
)

//go:generate mockery -dir . -name Sequencer -case underscore -output mocks

// Sequencer returns sequences
type Sequencer interface {
	Sequence() uint64
}

//go:generate mockery -dir . -name ConsenterVerifier -case underscore -output mocks

// ConsenterVerifier is used to determine whether a signature from one of the consenters is valid
type ConsenterVerifier interface {
	// Evaluate takes a set of SignedData and evaluates whether this set of signatures satisfies the policy
	Evaluate(signatureSet []*protoutil.SignedData) error
}

//go:generate mockery -dir . -name AccessController -case underscore -output mocks

// AccessController is used to determine if a signature of a certain client is valid
type AccessController interface {
	// Evaluate takes a set of SignedData and evaluates whether this set of signatures satisfies the policy
	Evaluate(signatureSet []*protoutil.SignedData) error
}

type requestVerifier func(req []byte, isolated bool) (BiniBFTRequestInfo, error)

// BiniBFTRequestInfo represents request information for BiniBFT
type BiniBFTRequestInfo struct {
	ID       string
	ClientID string
}

// RequestInspector inspects incoming requests and validates serialized identity
type RequestInspector struct {
	ValidateIdentityStructure func(identity *msp.SerializedIdentity) error
	Logger                    *flogging.FabricLogger
}

// RequestID unwraps the request info from the raw request
func (ri *RequestInspector) RequestID(rawReq []byte) BiniBFTRequestInfo {
	req, err := ri.unwrapReq(rawReq)
	if err != nil {
		ri.Logger.Warnf("Failed to unwrap request: %v", err)
		return BiniBFTRequestInfo{}
	}

	reqInfo, err := ri.requestIDFromSigHeader(req.sigHdr)
	if err != nil {
		ri.Logger.Warnf("Failed to extract request ID from signature header: %v", err)
		return BiniBFTRequestInfo{}
	}

	return reqInfo
}

func (ri *RequestInspector) isEmpty(req BiniBFTRequestInfo) bool {
	if len(req.ID) == 0 && len(req.ClientID) == 0 {
		return true
	}
	return false
}

func (ri *RequestInspector) requestIDFromSigHeader(sigHdr *cb.SignatureHeader) (BiniBFTRequestInfo, error) {
	sID := &msp.SerializedIdentity{}
	if err := proto.Unmarshal(sigHdr.Creator, sID); err != nil {
		return BiniBFTRequestInfo{}, errors.Wrap(err, "identity isn't an MSP Identity")
	}

	if ri.ValidateIdentityStructure != nil {
		if err := ri.ValidateIdentityStructure(sID); err != nil {
			return BiniBFTRequestInfo{}, err
		}
	}

	return BiniBFTRequestInfo{
		ID:       hex.EncodeToString(sigHdr.Nonce),
		ClientID: sID.Mspid,
	}, nil
}

type biniBFTRequest struct {
	chHdr    *cb.ChannelHeader
	sigHdr   *cb.SignatureHeader
	envelope *cb.Envelope
}

func (ri *RequestInspector) unwrapReq(req []byte) (*biniBFTRequest, error) {
	envelope, err := protoutil.UnmarshalEnvelope(req)
	if err != nil {
		return nil, err
	}
	return ri.unwrapReqFromEnvelop(envelope)
}

func (ri *RequestInspector) unwrapReqFromEnvelop(envelope *cb.Envelope) (*biniBFTRequest, error) {
	payload := &cb.Payload{}
	if err := proto.Unmarshal(envelope.Payload, payload); err != nil {
		return nil, errors.Wrap(err, "failed unmarshaling payload")
	}

	chdr := &cb.ChannelHeader{}
	if err := proto.Unmarshal(payload.Header.ChannelHeader, chdr); err != nil {
		return nil, errors.Wrap(err, "failed unmarshaling channel header")
	}

	shdr := &cb.SignatureHeader{}
	if err := proto.Unmarshal(payload.Header.SignatureHeader, shdr); err != nil {
		return nil, errors.Wrap(err, "failed unmarshaling signature header")
	}

	return &biniBFTRequest{
		chHdr:    chdr,
		sigHdr:   shdr,
		envelope: envelope,
	}, nil
}

// Verifier verifies proposals and signatures
type Verifier struct {
	Channel               string
	RuntimeConfig         *atomic.Value
	ReqInspector          *RequestInspector
	ConsenterVerifier     ConsenterVerifier
	AccessController      AccessController
	VerificationSequencer Sequencer
	Ledger                Ledger
	Logger                *flogging.FabricLogger
	ConfigValidator       ConfigValidator
}

// Ledger interface for accessing ledger operations
type Ledger interface {
	Height() uint64
	Block(number uint64) *cb.Block
}

// ConfigValidator interface for validating configuration
type ConfigValidator interface {
	ValidateConfig(env any) error
}

// AuxiliaryData unmarshals and returns auxiliary data from signature
func (v *Verifier) AuxiliaryData(msg []byte) []byte {
	sig := &Signature{}
	if err := sig.Unmarshal(msg); err != nil {
		v.Logger.Warnf("Failed unmarshalling signature message %s: %v", hex.EncodeToString(msg), err)
	}
	return nil
}

// VerifyProposal verifies proposal and returns []BiniBFTRequestInfo
func (v *Verifier) VerifyProposal(proposal BiniBFTProposal) ([]BiniBFTRequestInfo, error) {
	block, err := ProposalToBlock(proposal)
	if err != nil {
		return nil, err
	}

	rtc := v.RuntimeConfig.Load().(RuntimeConfig)
	if err := verifyHashChain(block, rtc.LastCommittedBlockHash); err != nil {
		return nil, err
	}

	requests, err := v.verifyBlockDataAndMetadata(block, proposal.Metadata)
	if err != nil {
		return nil, err
	}

	verificationSeq := v.VerificationSequence()
	if verificationSeq != uint64(proposal.VerificationSequence) {
		return nil, errors.Errorf("expected verification sequence %d, but proposal has %d", verificationSeq, proposal.VerificationSequence)
	}

	return requests, nil
}

// RequestsFromProposal converts proposal to []BiniBFTRequestInfo
func (v *Verifier) RequestsFromProposal(proposal BiniBFTProposal) []BiniBFTRequestInfo {
	block, err := ProposalToBlock(proposal)
	if err != nil {
		return []BiniBFTRequestInfo{}
	}

	if block.Data == nil {
		return []BiniBFTRequestInfo{}
	}

	var res []BiniBFTRequestInfo
	for _, txn := range block.Data.Data {
		req := v.ReqInspector.RequestID(txn)
		res = append(res, req)
	}

	return res
}

// VerifySignature verifies signature
func (v *Verifier) VerifySignature(signature BiniBFTSignature) error {
	id2Identity := v.RuntimeConfig.Load().(RuntimeConfig).ID2Identities
	identity, exists := id2Identity[signature.ID]
	if !exists {
		return errors.Errorf("node with id of %d doesn't exist", signature.ID)
	}

	return v.ConsenterVerifier.Evaluate([]*protoutil.SignedData{
		{Identity: identity, Data: signature.Msg, Signature: signature.Value},
	})
}

// VerifyRequest verifies raw request
func (v *Verifier) VerifyRequest(rawRequest []byte) (BiniBFTRequestInfo, error) {
	return v.verifyRequest(rawRequest, false)
}

func (v *Verifier) verifyRequest(rawRequest []byte, noConfigAllowed bool) (BiniBFTRequestInfo, error) {
	req, err := v.ReqInspector.unwrapReq(rawRequest)
	if err != nil {
		return BiniBFTRequestInfo{}, err
	}

	err = v.AccessController.Evaluate([]*protoutil.SignedData{
		{Identity: req.sigHdr.Creator, Data: req.envelope.Payload, Signature: req.envelope.Signature},
	})

	if err != nil {
		return BiniBFTRequestInfo{}, errors.Wrap(err, "access denied")
	}

	if noConfigAllowed && req.chHdr.Type != int32(cb.HeaderType_ENDORSER_TRANSACTION) {
		return BiniBFTRequestInfo{}, errors.Errorf("only endorser transactions can be sent with other transactions")
	}

	if req.chHdr.ChannelId != v.Channel {
		return BiniBFTRequestInfo{}, errors.Errorf("request is for channel %s but expected channel %s", req.chHdr.ChannelId, v.Channel)
	}

	switch req.chHdr.Type {
	case int32(cb.HeaderType_CONFIG):
	case int32(cb.HeaderType_ORDERER_TRANSACTION):
		return BiniBFTRequestInfo{}, fmt.Errorf("orderer transactions are not supported in v3")
	case int32(cb.HeaderType_ENDORSER_TRANSACTION):
	default:
		return BiniBFTRequestInfo{}, errors.Errorf("transaction of type %s is not allowed to be included in blocks", cb.HeaderType_name[req.chHdr.Type])
	}

	if req.chHdr.Type == int32(cb.HeaderType_CONFIG) {
		err = v.ConfigValidator.ValidateConfig(req.envelope)
		if err != nil {
			v.Logger.Errorf("Error verifying config update: %v", err)
			return BiniBFTRequestInfo{}, err
		}

		reqID := v.ReqInspector.RequestID(rawRequest)
		if v.ReqInspector.isEmpty(reqID) {
			return BiniBFTRequestInfo{}, errors.Errorf("request id is empty")
		}

		return reqID, nil
	}

	return v.ReqInspector.requestIDFromSigHeader(req.sigHdr)
}

// VerifyConsenterSig verifies consenter signature
func (v *Verifier) VerifyConsenterSig(signature BiniBFTSignature, prop BiniBFTProposal) ([]byte, error) {
	id2Identity := v.RuntimeConfig.Load().(RuntimeConfig).ID2Identities

	identity, exists := id2Identity[signature.ID]
	if !exists {
		return nil, errors.Errorf("node with id of %d doesn't exist", signature.ID)
	}

	sig := &Signature{}
	if err := sig.Unmarshal(signature.Msg); err != nil {
		v.Logger.Errorf("Failed unmarshaling signature from %d: %v", signature.ID, err)
		v.Logger.Errorf("Offending signature Msg: %s", base64.StdEncoding.EncodeToString(signature.Msg))
		v.Logger.Errorf("Offending signature Value: %s", base64.StdEncoding.EncodeToString(signature.Value))
		return nil, errors.Wrap(err, "malformed signature format")
	}

	if err := v.verifySignatureIsBoundToProposal(sig, signature.ID, prop); err != nil {
		return nil, err
	}

	expectedMsgToBeSigned := util.ConcatenateBytes(sig.OrdererBlockMetadata, sig.IdentifierHeader, sig.BlockHeader, nil)
	signedData := &protoutil.SignedData{
		Signature: signature.Value,
		Data:      expectedMsgToBeSigned,
		Identity:  identity,
	}

	return nil, v.ConsenterVerifier.Evaluate([]*protoutil.SignedData{signedData})
}

// VerificationSequence returns verification sequence
func (v *Verifier) VerificationSequence() uint64 {
	return v.VerificationSequencer.Sequence()
}

func verifyHashChain(block *cb.Block, prevHeaderHash string) error {
	thisHdrHashOfPrevHdr := hex.EncodeToString(block.Header.PreviousHash)
	if prevHeaderHash != thisHdrHashOfPrevHdr {
		return errors.Errorf("previous header hash is %s but expected %s", thisHdrHashOfPrevHdr, prevHeaderHash)
	}

	dataHash, err := protoutil.BlockDataHash(block.Data)
	if err != nil {
		return err
	}
	dataHashString := hex.EncodeToString(block.Header.DataHash)

	actualHashOfData := hex.EncodeToString(dataHash)
	if dataHashString != actualHashOfData {
		return errors.Errorf("data hash is %s but expected %s", dataHashString, actualHashOfData)
	}
	return nil
}

func (v *Verifier) verifyBlockDataAndMetadata(block *cb.Block, metadata []byte) ([]BiniBFTRequestInfo, error) {
	if block.Data == nil || len(block.Data.Data) == 0 {
		return nil, errors.New("empty block data")
	}

	if block.Metadata == nil || len(block.Metadata.Metadata) < len(cb.BlockMetadataIndex_name) {
		return nil, errors.New("block metadata is either missing or contains too few entries")
	}

	signatureMetadata, err := protoutil.GetMetadataFromBlock(block, cb.BlockMetadataIndex_SIGNATURES)
	if err != nil {
		return nil, err
	}
	ordererMetadataFromSignature := &cb.OrdererBlockMetadata{}
	if err := proto.Unmarshal(signatureMetadata.Value, ordererMetadataFromSignature); err != nil {
		return nil, errors.Wrap(err, "failed unmarshaling OrdererBlockMetadata")
	}

	// Ensure the view metadata in the block signature and in the proposal are the same
	if !bytes.Equal(ordererMetadataFromSignature.ConsenterMetadata, metadata) {
		return nil, errors.Errorf("expected metadata in block to match proposal metadata")
	}

	rtc := v.RuntimeConfig.Load().(RuntimeConfig)
	lastConfig := rtc.LastConfigBlock.Header.Number

	if protoutil.IsConfigBlock(block) {
		lastConfig = block.Header.Number
	}

	// Verify last config
	if ordererMetadataFromSignature.LastConfig == nil {
		return nil, errors.Errorf("last config is nil")
	}

	if ordererMetadataFromSignature.LastConfig.Index != lastConfig {
		return nil, errors.Errorf("last config in block orderer metadata points to %d but our persisted last config is %d", ordererMetadataFromSignature.LastConfig.Index, lastConfig)
	}

	rawLastConfig, err := protoutil.GetMetadataFromBlock(block, cb.BlockMetadataIndex_LAST_CONFIG)
	if err != nil {
		return nil, err
	}
	lastConf := &cb.LastConfig{}
	if err := proto.Unmarshal(rawLastConfig.Value, lastConf); err != nil {
		return nil, err
	}
	if lastConf.Index != lastConfig {
		return nil, errors.Errorf("last config in block metadata points to %d but our persisted last config is %d", ordererMetadataFromSignature.LastConfig.Index, lastConfig)
	}

	return validateTransactions(block.Data.Data, v.verifyRequest)
}

func validateTransactions(blockData [][]byte, verifyReq requestVerifier) ([]BiniBFTRequestInfo, error) {
	var validationFinished sync.WaitGroup
	validationFinished.Add(len(blockData))

	type txnValidation struct {
		indexInBlock  int
		extractedInfo BiniBFTRequestInfo
		validationErr error
	}

	noConfigAllowed := len(blockData) > 1

	validations := make(chan txnValidation, len(blockData))
	for i, payload := range blockData {
		go func(indexInBlock int, payload []byte) {
			defer validationFinished.Done()
			reqInfo, err := verifyReq(payload, noConfigAllowed)
			validations <- txnValidation{
				indexInBlock:  indexInBlock,
				extractedInfo: reqInfo,
				validationErr: err,
			}
		}(i, payload)
	}

	validationFinished.Wait()
	close(validations)

	indexToRequestInfo := make(map[int]BiniBFTRequestInfo)
	for validationResult := range validations {
		indexToRequestInfo[validationResult.indexInBlock] = validationResult.extractedInfo
		if validationResult.validationErr != nil {
			return nil, validationResult.validationErr
		}
	}

	var res []BiniBFTRequestInfo
	for indexInBlock := range blockData {
		res = append(res, indexToRequestInfo[indexInBlock])
	}

	return res, nil
}

func (v *Verifier) verifySignatureIsBoundToProposal(sig *Signature, identityID uint64, prop BiniBFTProposal) error {
	// We verify the following fields:
	// ConsenterMetadata    []byte
	// SignatureHeader      []byte
	// BlockHeader          []byte
	// OrdererBlockMetadata []byte

	// Ensure block header is equal
	if !bytes.Equal(prop.Header, sig.BlockHeader) {
		v.Logger.Errorf("Expected block header %s but got %s", base64.StdEncoding.EncodeToString(prop.Header),
			base64.StdEncoding.EncodeToString(sig.BlockHeader))
		return errors.Errorf("mismatched block header")
	}

	// Ensure signature header matches the identity
	sigHdr := &cb.IdentifierHeader{}
	if err := proto.Unmarshal(sig.IdentifierHeader, sigHdr); err != nil {
		return errors.Wrap(err, "malformed signature header")
	}
	if identityID != uint64(sigHdr.Identifier) {
		v.Logger.Warnf("Expected identity %d but got %d", identityID,
			sigHdr.Identifier)
		return errors.Errorf("identity in signature header does not match expected identity")
	}

	// Ensure orderer block metadata's consenter MD matches the proposal
	ordererMD := &cb.OrdererBlockMetadata{}
	if err := proto.Unmarshal(sig.OrdererBlockMetadata, ordererMD); err != nil {
		return errors.Wrap(err, "malformed orderer metadata in signature")
	}

	if !bytes.Equal(ordererMD.ConsenterMetadata, prop.Metadata) {
		v.Logger.Warnf("Expected consenter metadata %s but got %s in proposal",
			base64.StdEncoding.EncodeToString(ordererMD.ConsenterMetadata), base64.StdEncoding.EncodeToString(prop.Metadata))
		return errors.Errorf("consenter metadata in OrdererBlockMetadata doesn't match proposal")
	}

	block, err := ProposalToBlock(prop)
	if err != nil {
		v.Logger.Warnf("got malformed proposal: %v", err)
		return err
	}

	// Ensure Metadata slice is of the right size
	if len(block.Metadata.Metadata) != len(cb.BlockMetadataIndex_name) {
		return errors.Errorf("block metadata is of size %d but should be of size %d",
			len(block.Metadata.Metadata), len(cb.BlockMetadataIndex_name))
	}

	signatureMetadata := &cb.Metadata{}
	if err := proto.Unmarshal(block.Metadata.Metadata[cb.BlockMetadataIndex_SIGNATURES], signatureMetadata); err != nil {
		return errors.Wrap(err, "malformed signature metadata")
	}

	ordererMDFromBlock := &cb.OrdererBlockMetadata{}
	if err := proto.Unmarshal(signatureMetadata.Value, ordererMDFromBlock); err != nil {
		return errors.Wrap(err, "malformed orderer metadata in block")
	}

	// Ensure the block's OrdererBlockMetadata matches the signature.
	if !proto.Equal(ordererMDFromBlock, ordererMD) {
		return errors.Errorf("signature's OrdererBlockMetadata and OrdererBlockMetadata extracted from block do not match")
	}

	return nil
}

type consenterVerifier struct {
	logger        *flogging.FabricLogger
	channel       string
	policyManager policies.Manager
}

// Evaluate evaluates signed data and returns no error if signature is valid and satisfies the policy
func (cv *consenterVerifier) Evaluate(signatureSet []*protoutil.SignedData) error {
	policy, ok := cv.policyManager.GetPolicy(policies.ChannelOrdererWriters)
	if !ok {
		cv.logger.Errorf("[%s] Error: could not find policy %s in policy manager %v", cv.channel, policies.ChannelOrdererWriters, cv.policyManager)
		return errors.Errorf("could not find policy %s", policies.ChannelOrdererWriters)
	}

	if cv.logger.IsEnabledFor(zapcore.DebugLevel) {
		cv.logger.Debugf("== Evaluating %T Policy %s ==", policy, policies.ChannelOrdererWriters)
		defer cv.logger.Debugf("== Done Evaluating %T Policy %s", policy, policies.ChannelOrdererWriters)
	}

	return policy.EvaluateSignedData(signatureSet)
}
