package consensus

import "time"

// MessageType defines the type of consensus message
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
)

// Message represents a consensus message
type Message struct {
	Type      MessageType
	From      NodeID
	To        NodeID
	ShardID   ShardID
	Timestamp time.Time
	Payload   interface{}
}

// ProposalMessage contains proposal data
type ProposalMessage struct {
	Proposal *Proposal
}

// VoteMessage contains vote data
type VoteMessage struct {
	Vote *Vote
}

// ShardDecisionMessage contains shard-level decision
type ShardDecisionMessage struct {
	ShardID   ShardID
	Decision  bool
	Votes     []Vote
	Signature []byte
	BatchID   string // For batch decisions
}

// CommitMessage signals final commitment
type CommitMessage struct {
	ProposalID     string
	ShardDecisions map[ShardID]*ShardDecisionMessage
	FinalDecision  bool
	RequestData    *CommitRequestData // Include request data for finalization
}

// CommitRequestData contains request data for finalization
type CommitRequestData struct {
	ID        string
	Data      []byte
	ClientID  string
	Timestamp time.Time
	ShardID   ShardID
}

// HeartbeatMessage for liveness detection
type HeartbeatMessage struct {
	NodeID    NodeID
	ShardID   ShardID
	Timestamp time.Time
}

// BatchProposalMessage contains a batch of proposals
type BatchProposalMessage struct {
	Batch *ProposalBatch
}

// RequestMessage contains client request data
type RequestMessage struct {
	Request *Request
}

// PrePrepMessage for pre-preparation phase
type PrePrepMessage struct {
	RequestID       string
	View            uint64
	Sequence        uint64
	Digest          []byte
	NodeID          NodeID
	ShardID         ShardID
	Signature       []byte
	BatchRequestIDs []string // For batched requests
}

// PrepMessage for preparation phase
type PrepMessage struct {
	RequestID string
	View      uint64
	Sequence  uint64
	Digest    []byte
	NodeID    NodeID
	ShardID   ShardID
	Signature []byte
}

// PreparePhaseMessage for prepare phase
type PreparePhaseMessage struct {
	RequestID string
	View      uint64
	Sequence  uint64
	Digest    []byte
	NodeID    NodeID
	ShardID   ShardID
	Signature []byte
}

// CommitRequestMessage for commit phase
type CommitRequestMessage struct {
	RequestID string
	View      uint64
	Sequence  uint64
	Digest    []byte
	NodeID    NodeID
	ShardID   ShardID
	Signature []byte
}

// CrossShardRequestMessage for cross-shard request coordination
type CrossShardRequestMessage struct {
	Request     *Request
	OriginShard ShardID
	TargetShard ShardID
	Timestamp   time.Time
}

// ShardAckMessage for shard acknowledgment
type ShardAckMessage struct {
	RequestID    string
	ShardID      ShardID
	NodeID       NodeID
	Acknowledged bool
	Phase        string // "preprep", "prepare", "commit"
	Timestamp    time.Time
}
