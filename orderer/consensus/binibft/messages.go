/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"time"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
)

// RequestInfo represents request information
type RequestInfo struct {
	ClientID string
	ID       string
}

// PrePrepMessage for pre-preparation phase
type PrePrepMessage struct {
	Proposal  BiniBFTProposal
	View      uint64
	Sequence  uint64
	Digest    string
	NodeID    NodeID
	ShardID   ShardID
	Signature []byte
}

// PreparePhaseMessage for prepare phase
type PreparePhaseMessage struct {
	Proposal  BiniBFTProposal
	View      uint64
	Sequence  uint64
	Digest    string
	NodeID    NodeID
	ShardID   ShardID
	Signature []byte
}

// CommitRequestMessage for commit phase
type CommitRequestMessage struct {
	Proposal  BiniBFTProposal
	View      uint64
	Sequence  uint64
	Digest    string
	NodeID    NodeID
	ShardID   ShardID
	Signature []byte
}

// ShardAckMessage for shard acknowledgment
type ShardAckMessage struct {
	Sequence     uint64
	ShardID      ShardID
	NodeID       NodeID
	Acknowledged bool
	Phase        string // "preprep", "prepare", "commit"
	Timestamp    time.Time
}

// IntraShardVoteMessage for requesting votes within a shard
type IntraShardVoteMessage struct {
	Sequence  uint64
	Proposal  BiniBFTProposal
	Phase     string // "preprep", "prepare", "commit"
	ShardID   ShardID
	NodeID    NodeID
	Digest    string
	Signature []byte
	Timestamp time.Time
}

// IntraShardVoteResponse for responding to intra-shard vote requests
type IntraShardVoteResponse struct {
	Sequence  uint64
	Phase     string
	ShardID   ShardID
	NodeID    NodeID
	Vote      bool // true for approve, false for reject
	Signature []byte
	Timestamp time.Time
}

// RequestMessage contains client request data
type RequestMessage struct {
	Request *BiniBFTRequest
}

// NetworkInterface defines network operations for BiniBFT
type NetworkInterface interface {
	Send(nodeID NodeID, message BiniBFTMessage) error
	Broadcast(nodeIDs []NodeID, message BiniBFTMessage) error
}

// Logger interface for BiniBFT logging
type Logger interface {
	Info(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Debug(msg string, fields ...interface{})
}

// BiniBFTSigner interface for BiniBFT signing
type BiniBFTSigner interface {
	Sign(msg []byte) []byte
}

// NetworkAdapter adapts Fabric's communication to BiniBFT's NetworkInterface
type NetworkAdapter struct {
	comm   *EgressComm
	logger *flogging.FabricLogger
}

func (n *NetworkAdapter) Send(nodeID NodeID, message BiniBFTMessage) error {
	return n.comm.Send(nodeID, message)
}

func (n *NetworkAdapter) Broadcast(nodeIDs []NodeID, message BiniBFTMessage) error {
	for _, nodeID := range nodeIDs {
		if err := n.comm.Send(nodeID, message); err != nil {
			return err
		}
	}
	return nil
}

// LoggerAdapter adapts Fabric's logger to BiniBFT's Logger interface
type LoggerAdapter struct {
	logger *flogging.FabricLogger
}

func (l *LoggerAdapter) Info(msg string, fields ...interface{}) {
	l.logger.Infof(msg, fields...)
}

func (l *LoggerAdapter) Error(msg string, fields ...interface{}) {
	l.logger.Errorf(msg, fields...)
}

func (l *LoggerAdapter) Debug(msg string, fields ...interface{}) {
	l.logger.Debugf(msg, fields...)
}

// SignerAdapter adapts Fabric's signer to BiniBFT's BiniBFTSigner interface
type SignerAdapter struct {
	signer *Signer
}

func (s *SignerAdapter) Sign(msg []byte) []byte {
	sig, _ := s.signer.SignerSerializer.Sign(msg)
	return sig
}

// ApplicationAdapter adapts Fabric's application delivery
type ApplicationAdapter struct {
	app ApplicationDelivery
}

func (a *ApplicationAdapter) Deliver(proposal BiniBFTProposal, signatures []BiniBFTSignature) BiniBFTReconfig {
	return a.app.Deliver(proposal, signatures)
}
