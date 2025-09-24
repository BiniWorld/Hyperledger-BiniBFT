/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	binibftconsensus "github.com/hyperledger/fabric/orderer/consensus/binibft/consensus"
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
	app ApplicationDelivery
}

func (a *FabricApplicationAdapter) Deliver(proposal binibftconsensus.Proposal) error {
	// Convert BiniBFT proposal to Fabric proposal
	fabricProposal := BiniBFTProposal{
		Payload:              proposal.Payload,
		Header:               proposal.Header,
		Metadata:             proposal.Metadata,
		VerificationSequence: proposal.VerificationSequence,
	}

	// For now, deliver with empty signatures - in production this would include proper signatures
	var signatures []BiniBFTSignature
	a.app.Deliver(fabricProposal, signatures)
	return nil
}

// FabricAssemblerAdapter adapts Fabric's assembler to BiniBFT's Assembler interface
type FabricAssemblerAdapter struct {
	assembler *Assembler
}

func (a *FabricAssemblerAdapter) AssembleProposal(metadata []byte, requests [][]byte) binibftconsensus.Proposal {
	// Create a simple proposal from requests
	// In production, this would use the actual assembler logic
	var payload []byte
	for _, req := range requests {
		payload = append(payload, req...)
	}

	return binibftconsensus.Proposal{
		Payload:              payload,
		Header:               []byte("fabric-header"),
		Metadata:             metadata,
		VerificationSequence: 1, // Default sequence
	}
}

// FabricSignerAdapter adapts Fabric's signer to BiniBFT's Signer interface
type FabricSignerAdapter struct {
	signer *Signer
}

func (s *FabricSignerAdapter) SignProposal(proposal binibftconsensus.Proposal, data []byte) *binibftconsensus.Signature {
	sig, err := s.signer.SignerSerializer.Sign(data)
	if err != nil {
		return nil
	}

	return &binibftconsensus.Signature{
		ID:    s.signer.ID,
		Value: sig,
		Msg:   data,
	}
}

func (s *FabricSignerAdapter) Sign(msg []byte) []byte {
	sig, _ := s.signer.SignerSerializer.Sign(msg)
	return sig
}

// FabricRequestInspectorAdapter adapts Fabric's request inspection to BiniBFT's RequestInspector interface
type FabricRequestInspectorAdapter struct{}

func (r *FabricRequestInspectorAdapter) RequestID(req []byte) binibftconsensus.RequestInfo {
	// Simple request ID generation - in production this would be more sophisticated
	return binibftconsensus.RequestInfo{
		ClientID: "fabric-client",
		ID:       fmt.Sprintf("req-%d", len(req)),
	}
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
