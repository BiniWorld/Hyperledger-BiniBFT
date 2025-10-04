/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"crypto/sha256"
	"encoding/asn1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	sync "sync"
	"time"

	"github.com/hyperledger/fabric-lib-go/bccsp"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	cb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric/orderer/common/cluster"
	"github.com/hyperledger/fabric/orderer/common/localconfig"
	"github.com/hyperledger/fabric/orderer/consensus"
)

// WALConfig consensus specific configuration parameters from orderer.yaml
type WALConfig struct {
	WALDir            string // WAL data of <my-channel> is stored in WALDir/binibft/<my-channel> (separate from SmartBFT)
	SnapDir           string // Snapshots of <my-channel> are stored in SnapDir/binibft/<my-channel>
	EvictionSuspicion string // Duration threshold that the node samples in order to suspect its eviction from the channel.
}

// NodeRole defines the role of a node in the hierarchical consensus
type NodeRole int

const (
	RolePrimaryLeader NodeRole = iota
	RoleShardLeader
	RoleShardFollower
)

func (r NodeRole) String() string {
	switch r {
	case RolePrimaryLeader:
		return "PrimaryLeader"
	case RoleShardLeader:
		return "ShardLeader"
	case RoleShardFollower:
		return "ShardFollower"
	default:
		return "Unknown"
	}
}

// NodeID represents a unique identifier for a node
type NodeID string

// ShardID represents a unique identifier for a shard
type ShardID uint32

// BiniBFTProposal represents a consensus proposal in BiniBFT
// This structure is identical to SmartBFT's types.Proposal for compatibility
type BiniBFTProposal struct {
	Payload              []byte
	Header               []byte
	Metadata             []byte
	VerificationSequence int64 // int64 for asn1 marshaling
}

func (p BiniBFTProposal) Digest() string {
	rawBytes, err := asn1.Marshal(BiniBFTProposal{
		VerificationSequence: p.VerificationSequence,
		Metadata:             p.Metadata,
		Payload:              p.Payload,
		Header:               p.Header,
	})
	if err != nil {
		panic(fmt.Sprintf("failed marshaling proposal: %v", err))
	}
	return computeDigest(rawBytes)
}

// BiniBFTVote represents a vote on a proposal
type BiniBFTVote struct {
	ProposalID string
	NodeID     NodeID
	ShardID    ShardID
	Approve    bool
	Signature  []byte
}

// BiniBFTDecision represents a consensus decision
type BiniBFTDecision struct {
	ProposalID string
	Committed  bool
	ShardVotes map[ShardID][]BiniBFTVote
	Timestamp  time.Time
	BatchID    string
}

// BiniBFTConfig holds the configuration for the BiniBFT consensus instance
type BiniBFTConfig struct {
	NodeID                  NodeID
	RequestBatchMaxCount    uint64
	RequestBatchMaxBytes    uint64
	RequestBatchMaxInterval time.Duration
	ShardID                 ShardID
	Role                    NodeRole
	ShardNodes              map[ShardID][]NodeID
	PrimaryLeader           NodeID
	ShardLeaders            map[ShardID]NodeID
	MajorityRequired        int
	Timeout                 time.Duration
	ViewChangeTimeout       time.Duration
	MaxFaultyNodes          int
	SigningKey              []byte
	VerificationKeys        map[NodeID][]byte
	ShardMajorityThreshold  float64
	CrossShardThreshold     float64
	BatchSize               int
	MaxBatchDelay           time.Duration
}

// BiniBFTMessage represents a BiniBFT consensus message
type BiniBFTMessage struct {
	Type      MessageType
	From      NodeID
	To        NodeID
	ShardID   ShardID
	Timestamp time.Time
	Payload   []byte
}

func (m *BiniBFTMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func (m *BiniBFTMessage) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// MessageType defines the type of BiniBFT consensus message
type MessageType int

const (
	MsgProposal MessageType = iota
	MsgVote
	MsgShardDecision
	MsgHeartbeat
	MsgViewChange
	MsgPrepare
	MsgCommitPhase
	MsgNewView
	MsgBatchProposal
	MsgRequest
	MsgPrePrep
	MsgPreparePhase
	MsgCommitRequest
	MsgCrossShardRequest
	MsgShardAck
	MsgFinalizedBlock
	MsgIntraShardVote
	MsgIntraShardVoteResponse
)

// SignerSerializer interface for signing and serializing
type SignerSerializer interface {
	Sign(message []byte) ([]byte, error)
	Serialize() ([]byte, error)
}

// ConfigBlockValidator interface
type ConfigBlockValidator interface {
	ValidateConfig(env any) error
}

// // RuntimeConfig holds runtime configuration for BiniBFT
// type RuntimeConfig struct {
// 	logger          any
// 	id              uint64
// 	Nodes           []uint64
// 	RemoteNodes     []cluster.RemoteNode
// 	BFTConfig       any
// 	LastBlock       any
// 	LastConfigBlock any
// 	consenters      []*cb.Consenter
// }

// func (rtc RuntimeConfig) BlockCommitted(block *cb.Block, bccsp bccsp.BCCSP) (RuntimeConfig, error) {
// 	if block == nil {
// 		return rtc, fmt.Errorf("cannot commit a nil block")
// 	}

// 	newRTC := rtc
// 	newRTC.LastBlock = block

// 	// If it's a config block, extract new consenters and update runtime state.
// 	if protoutil.IsConfigBlock(block) {
// 		newRTC.LastConfigBlock = block

// 		// Extract the config envelope from the block
// 		configEnv, err := protoutil.ExtractEnvelope(block, 0)
// 		if err != nil {
// 			return rtc, fmt.Errorf("failed to extract config envelope from block %d: %w", block.Header.Number, err)
// 		}

// 		configEnvelope, err := protoutil.UnmarshalEnvelopeOfType(configEnv, cb.HeaderType_CONFIG)
// 		if err != nil {
// 			return rtc, fmt.Errorf("failed to unmarshal config envelope: %w", err)
// 		}

// 		// The payload contains the ConfigEnvelope
// 		payload, err := protoutil.UnmarshalPayload(configEnvelope.Payload)
// 		if err != nil {
// 			return rtc, fmt.Errorf("failed to unmarshal payload: %w", err)
// 		}

// 		configEnvProto, err := protoutil.UnmarshalConfigEnvelope(payload.Data)
// 		if err != nil {
// 			return rtc, fmt.Errorf("failed to unmarshal ConfigEnvelope: %w", err)
// 		}

// 		// Extract Orderer group consenters from the config
// 		consenters, err := extractOrdererConsenters(configEnvProto)
// 		if err != nil {
// 			return rtc, fmt.Errorf("failed to extract consenters: %w", err)
// 		}
// 		newRTC.consenters = consenters

// 		// Optionally update Nodes and RemoteNodes if BiniBFT uses them
// 		newRTC.Nodes = extractNodeIDsFromConsenters(consenters)
// 		newRTC.RemoteNodes = buildClusterRemoteNodes(consenters)
// 	}

// 	return newRTC, nil
// }

// // extractOrdererConsenters reads the consenters from a ConfigEnvelope.
// func extractOrdererConsenters(env *cb.ConfigEnvelope) ([]*orderer.Consenter, error) {
// 	ordererGroup := env.Config.ChannelGroup.Groups["Orderer"]
// 	if ordererGroup == nil {
// 		return nil, fmt.Errorf("orderer group not found in config")
// 	}

// 	consenterSetVal := ordererGroup.Values["Consenters"]
// 	if consenterSetVal == nil {
// 		return nil, fmt.Errorf("no Consenters found in orderer config")
// 	}

// 	consenterSet := &orderer.ConsenterSet{}
// 	if err := protoutil.Unmarshal(consenterSetVal.Value, consenterSet); err != nil {
// 		return nil, fmt.Errorf("failed to unmarshal ConsenterSet: %w", err)
// 	}

// 	return consenterSet.Consenters, nil
// }

// // extractNodeIDsFromConsenters converts consenters into node IDs for internal use.
// func extractNodeIDsFromConsenters(consenters []*orderer.Consenter) []uint64 {
// 	nodeIDs := make([]uint64, 0, len(consenters))
// 	for _, c := range consenters {
// 		nodeIDs = append(nodeIDs, uint64(c.GetId())) // cast uint32 → uint64
// 	}
// 	return nodeIDs
// }

// // buildClusterRemoteNodes builds cluster.RemoteNode list from consenters.
// func buildClusterRemoteNodes(consenters []*orderer.Consenter) []cluster.RemoteNode {
// 	remoteNodes := make([]cluster.RemoteNode, 0, len(consenters))
// 	for _, c := range consenters {
// 		remoteNodes = append(remoteNodes, cluster.RemoteNode{
// 			Address:       fmt.Sprintf("%s:%d", c.Host, c.Port),
// 			ServerTLSCert: c.ServerTlsCert,
// 			ClientTLSCert: c.ClientTlsCert,
// 		})
// 	}
// 	return remoteNodes
// }

// Configuration represents the BiniBFT configuration
type Configuration struct {
	SelfID                    uint64
	RequestBatchMaxCount      uint64
	RequestBatchMaxBytes      uint64
	RequestBatchMaxInterval   time.Duration
	IncomingMessageBufferSize uint64
	RequestPoolSize           uint64
	RequestForwardTimeout     time.Duration
	RequestComplainTimeout    time.Duration
	RequestAutoRemoveTimeout  time.Duration
	ViewChangeResendInterval  time.Duration
	ViewChangeTimeout         time.Duration
	LeaderHeartbeatTimeout    time.Duration
	LeaderHeartbeatCount      uint64
	CollectTimeout            time.Duration
	SyncOnStart               bool
	SpeedUpViewChange         bool
	LeaderRotation            bool
	DecisionsPerLeader        uint64
	RequestMaxBytes           uint64
	RequestPoolSubmitTimeout  time.Duration
}

// ConfigFromMetadataOptions creates a Configuration from metadata options
func ConfigFromMetadataOptions(selfID uint64, options *BiniBFTOptions) (*Configuration, error) {
	config := &Configuration{
		SelfID:                    selfID,
		RequestBatchMaxCount:      options.RequestBatchMaxCount,
		RequestBatchMaxBytes:      options.RequestBatchMaxBytes,
		RequestBatchMaxInterval:   parseDuration(options.RequestBatchMaxInterval),
		IncomingMessageBufferSize: options.IncomingMessageBufferSize,
		RequestPoolSize:           options.RequestPoolSize,
		RequestForwardTimeout:     parseDuration(options.RequestForwardTimeout),
		RequestComplainTimeout:    parseDuration(options.RequestComplainTimeout),
		RequestAutoRemoveTimeout:  parseDuration(options.RequestAutoRemoveTimeout),
		ViewChangeResendInterval:  parseDuration(options.ViewChangeResendInterval),
		ViewChangeTimeout:         parseDuration(options.ViewChangeTimeout),
		LeaderHeartbeatTimeout:    parseDuration(options.LeaderHeartbeatTimeout),
		LeaderHeartbeatCount:      options.LeaderHeartbeatCount,
		CollectTimeout:            parseDuration(options.CollectTimeout),
		SyncOnStart:               options.SyncOnStart,
		SpeedUpViewChange:         options.SpeedUpViewChange,
		LeaderRotation:            options.LeaderRotation,
		DecisionsPerLeader:        options.DecisionsPerLeader,
		RequestMaxBytes:           options.RequestMaxBytes,
		RequestPoolSubmitTimeout:  parseDuration(options.RequestPoolSubmitTimeout),
	}
	return config, nil
}

// parseDuration safely parses duration strings
func parseDuration(durationStr string) time.Duration {
	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return 0
	}
	return duration
}

// computeDigest computes a digest for the given data using SHA256
func computeDigest(data []byte) string {
	h := sha256.New()
	h.Write(data)
	digest := h.Sum(nil)
	return hex.EncodeToString(digest)
}

// BiniBFTSyncResponse represents the response from a synchronization operation
type BiniBFTSyncResponse struct {
	Latest   BiniBFTDecision
	Reconfig BiniBFTReconfigSync
}

// BiniBFTReconfigSync represents reconfiguration information during sync
type BiniBFTReconfigSync struct {
	InReplicatedDecisions bool
	CurrentNodes          []uint64
	CurrentConfig         *Configuration
}

// SyncBuffer is a buffer for synchronizing blocks during BFT sync
type SyncBuffer struct {
	blocks   map[uint64]*cb.Block
	capacity uint
	stopped  bool
	mutex    sync.Mutex
	cond     *sync.Cond
}

// NewSyncBuffer creates a new sync buffer with the given capacity
func NewSyncBuffer(capacity uint) *SyncBuffer {
	sb := &SyncBuffer{
		blocks:   make(map[uint64]*cb.Block),
		capacity: capacity,
	}
	sb.cond = sync.NewCond(&sb.mutex)
	return sb
}

// PullBlock pulls a block from the buffer at the given sequence number
func (sb *SyncBuffer) PullBlock(seq uint64) *cb.Block {
	sb.mutex.Lock()
	defer sb.mutex.Unlock()

	for {
		if sb.stopped {
			return nil
		}
		if block, exists := sb.blocks[seq]; exists {
			delete(sb.blocks, seq)
			return block
		}
		sb.cond.Wait()
	}
}

// HandleBlock handles a received block by adding it to the buffer
func (sb *SyncBuffer) HandleBlock(channelID string, block *cb.Block) error {
	sb.mutex.Lock()
	defer sb.mutex.Unlock()

	if sb.stopped {
		return fmt.Errorf("sync buffer is stopped")
	}

	if uint(len(sb.blocks)) >= sb.capacity {
		return fmt.Errorf("sync buffer is full")
	}

	sb.blocks[block.Header.Number] = block
	sb.cond.Broadcast()
	return nil
}

// Stop stops the sync buffer
func (sb *SyncBuffer) Stop() {
	sb.mutex.Lock()
	defer sb.mutex.Unlock()

	sb.stopped = true
	sb.cond.Broadcast()
}

// BiniBFTSynchronizerInterface interface for synchronizing with other nodes
type BiniBFTSynchronizerInterface interface {
	Sync() BiniBFTSyncResponse
}

// BiniBFTSimpleSynchronizer is a simple synchronizer that doesn't use BFT delivery
type BiniBFTSimpleSynchronizer struct {
	lastReconfig       BiniBFTReconfig
	selfID             uint64
	LatestConfig       func() (*Configuration, []uint64)
	BlockToDecision    func(*cb.Block) *BiniBFTDecision
	OnCommit           func(*cb.Block) BiniBFTReconfig
	Support            consensus.ConsenterSupport
	CryptoProvider     bccsp.BCCSP
	ClusterDialer      *cluster.PredicateDialer
	LocalConfigCluster localconfig.Cluster
	BlockPullerFactory BlockPullerFactory
	Logger             *flogging.FabricLogger
}

func (s *BiniBFTSimpleSynchronizer) Sync() BiniBFTSyncResponse {
	s.Logger.Debug("BiniBFT Simple Sync initiated")
	// Simple synchronizer just returns current state from ledger
	block := s.Support.Block(s.Support.Height() - 1)
	config, nodes := s.LatestConfig()
	return BiniBFTSyncResponse{
		Latest: *s.BlockToDecision(block),
		Reconfig: BiniBFTReconfigSync{
			InReplicatedDecisions: false,
			CurrentNodes:          nodes,
			CurrentConfig:         config,
		},
	}
}

// BiniBFTMetadata represents metadata for BiniBFT consensus
type BiniBFTMetadata struct {
	ViewId         uint64 `protobuf:"varint,1,opt,name=view_id,json=viewId,proto3" json:"view_id,omitempty"`
	LatestSequence uint64 `protobuf:"varint,2,opt,name=latest_sequence,json=latestSequence,proto3" json:"latest_sequence,omitempty"`
}

// GetViewId returns the view ID
func (m *BiniBFTMetadata) GetViewId() uint64 {
	if m != nil {
		return m.ViewId
	}
	return 0
}

// GetLatestSequence returns the latest sequence
func (m *BiniBFTMetadata) GetLatestSequence() uint64 {
	if m != nil {
		return m.LatestSequence
	}
	return 0
}
