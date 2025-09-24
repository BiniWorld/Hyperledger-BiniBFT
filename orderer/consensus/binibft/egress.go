/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"fmt"
	"sync/atomic"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	cb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
)

// EgressComm handles outgoing communication for BiniBFT, similar to SmartBFT
type EgressComm struct {
	Channel       string
	RPC           RPC
	Logger        *flogging.FabricLogger
	RuntimeConfig *atomic.Value

	// Message handler for incoming messages
	messageHandler MessageHandler
}

// RPC interface for sending consensus and submit messages
type RPC interface {
	SendConsensus(dest uint64, msg *ab.ConsensusRequest) error
	SendSubmit(destination uint64, request *ab.SubmitRequest, report func(error)) error
}

// MessageHandler interface for handling incoming messages
type MessageHandler interface {
	HandleMessage(from NodeID, message BiniBFTMessage) error
}

// Send sends a BiniBFT message to a specific node
func (e *EgressComm) Send(nodeID NodeID, message BiniBFTMessage) error {
	e.Logger.Debugf("Sending BiniBFT message to node %s on channel %s", nodeID, e.Channel)

	// Convert BiniBFT message to cluster consensus message
	msgBytes, err := message.Marshal()
	if err != nil {
		e.Logger.Errorf("Failed to marshal BiniBFT message: %v", err)
		return err
	}

	consensusMsg := &ab.ConsensusRequest{
		Payload: msgBytes,
	}

	// Parse nodeID to uint64
	var targetID uint64
	if _, err := fmt.Sscanf(string(nodeID), "%d", &targetID); err != nil {
		e.Logger.Errorf("Failed to parse node ID %s: %v", nodeID, err)
		return err
	}

	// Send via RPC
	if err := e.RPC.SendConsensus(targetID, consensusMsg); err != nil {
		e.Logger.Warnf("Failed sending BiniBFT message to %d: %v", targetID, err)
		return err
	}

	return nil
}

// Broadcast sends a BiniBFT message to multiple nodes
func (e *EgressComm) Broadcast(nodeIDs []NodeID, message BiniBFTMessage) error {
	for _, nodeID := range nodeIDs {
		if err := e.Send(nodeID, message); err != nil {
			e.Logger.Errorf("Failed to broadcast to node %s: %v", nodeID, err)
		}
	}
	return nil
}

// SendTransaction sends a transaction to a specific node
func (e *EgressComm) SendTransaction(targetID NodeID, request []byte) error {
	e.Logger.Debugf("Sending transaction to node %s", targetID)

	// Parse nodeID to uint64
	var destID uint64
	if _, err := fmt.Sscanf(string(targetID), "%d", &destID); err != nil {
		e.Logger.Errorf("Failed to parse node ID %s: %v", targetID, err)
		return err
	}

	// Create submit request
	submitReq := &ab.SubmitRequest{
		Payload: &cb.Envelope{
			Payload: request,
		},
	}

	report := func(err error) {
		if err != nil {
			e.Logger.Warnf("Failed sending transaction to %d: %v", destID, err)
		}
	}

	return e.RPC.SendSubmit(destID, submitReq, report)
}

// RegisterHandler registers a message handler for incoming messages
func (e *EgressComm) RegisterHandler(handler MessageHandler) {
	e.messageHandler = handler
	e.Logger.Debugf("Registered message handler for channel %s", e.Channel)
}

// HandleIncomingMessage processes incoming messages and routes them to the registered handler
func (e *EgressComm) HandleIncomingMessage(from NodeID, message BiniBFTMessage) error {
	if e.messageHandler == nil {
		e.Logger.Warnf("No message handler registered for channel %s", e.Channel)
		return fmt.Errorf("no message handler registered")
	}

	return e.messageHandler.HandleMessage(from, message)
}

// Nodes returns the list of nodes from runtime config
func (e *EgressComm) Nodes() []uint64 {
	if e.RuntimeConfig == nil {
		return nil
	}

	rtc := e.RuntimeConfig.Load().(RuntimeConfig)
	var nodes []uint64
	nodes = append(nodes, rtc.Nodes...)
	return nodes
}
