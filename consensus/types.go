package consensus

import (
	"context"
	"fmt"
	"sync"
	"time"
)

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

// Proposal represents a consensus proposal
type Proposal struct {
	ID        string
	Data      []byte
	Timestamp time.Time
	ShardID   ShardID
	Proposer  NodeID
}

// Vote represents a vote on a proposal
type Vote struct {
	ProposalID string
	NodeID     NodeID
	ShardID    ShardID
	Approve    bool
	Signature  []byte
}

// Decision represents a consensus decision
type Decision struct {
	ProposalID string
	Committed  bool
	ShardVotes map[ShardID][]Vote
	Timestamp  time.Time
	BatchID    string // For batch decisions
}

// Transaction represents a transaction within a block
type Transaction struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Amount    uint64    `json:"amount"`
	Data      []byte    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
	Signature []byte    `json:"signature"`
}

// Block represents a block containing transactions
type Block struct {
	ID           string        `json:"id"`
	PreviousHash string        `json:"previous_hash"`
	Hash         string        `json:"hash"`
	Height       uint64        `json:"height"`
	Timestamp    time.Time     `json:"timestamp"`
	Transactions []Transaction `json:"transactions"`
	ProposalID   string        `json:"proposal_id"`
	Proposer     NodeID        `json:"proposer"`
	Signature    []byte        `json:"signature"`
}

// Config holds the configuration for the consensus instance
type Config struct {
	NodeID            NodeID
	ShardID           ShardID
	Role              NodeRole
	ShardNodes        map[ShardID][]NodeID
	PrimaryLeader     NodeID
	ShardLeaders      map[ShardID]NodeID
	MajorityRequired  int
	Timeout           time.Duration
	ViewChangeTimeout time.Duration
	Network           NetworkInterface
	Logger            Logger
	// Byzantine fault tolerance settings
	MaxFaultyNodes   int
	SigningKey       []byte
	VerificationKeys map[NodeID][]byte
	// Hierarchical sharding settings
	ShardMajorityThreshold float64       // Percentage of nodes required for shard consensus (0.5-1.0)
	CrossShardThreshold    float64       // Percentage of shards required for global consensus (0.5-1.0)
	BatchSize              int           // Number of proposals to batch together
	MaxBatchDelay          time.Duration // Maximum time to wait before processing a batch
	// Storage
	Storage BlockStorage // Storage for blocks and transactions
}

// ConsensusInterface defines the main consensus operations
type ConsensusInterface interface {
	Start(ctx context.Context) error
	Stop() error
	Propose(proposal *Proposal) error
	GetStatus() Status
}

// NetworkInterface defines network operations
type NetworkInterface interface {
	Send(nodeID NodeID, message Message) error
	Broadcast(nodeIDs []NodeID, message Message) error
	RegisterHandler(handler MessageHandler)
}

// MessageHandler handles incoming messages
type MessageHandler interface {
	HandleMessage(from NodeID, message Message) error
}

// Logger interface for logging
type Logger interface {
	Info(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Debug(msg string, fields ...interface{})
}

// Status represents the current status of the consensus
type Status struct {
	Role         NodeRole
	ShardID      ShardID
	IsActive     bool
	LastDecision time.Time
	CurrentView  uint64
	IsPrimary    bool
}

// ViewChange represents a view change request
type ViewChange struct {
	NewView   uint64
	NodeID    NodeID
	ShardID   ShardID
	Reason    string
	Timestamp time.Time
	Signature []byte
}

// RequestPhase represents the phase of request processing
type RequestPhase int

const (
	PhasePrePrep RequestPhase = iota
	PhasePrepare
	PhaseCommit
)

func (p RequestPhase) String() string {
	switch p {
	case PhasePrePrep:
		return "PrePrep"
	case PhasePrepare:
		return "Prepare"
	case PhaseCommit:
		return "Commit"
	default:
		return "Unknown"
	}
}

// Request represents a client request in the request pool
type Request struct {
	ID        string
	Data      []byte
	ClientID  string
	Timestamp time.Time
	Phase     RequestPhase
	ShardID   ShardID
}

// RequestPoolInterface defines the interface for request pools
type RequestPoolInterface interface {
	AddRequest(request *Request) error
	GetRequest(requestID string) (*Request, bool)
	RemoveRequest(requestID string) error
	UpdateRequestPhase(requestID string, phase RequestPhase)
	Size() int
	Close()
	GetRequestsByPhase(phase RequestPhase) []*Request
}

// RequestPool manages pending requests with enhanced features
type RequestPool struct {
	requests map[string]*Request
	mu       sync.RWMutex
	maxSize  int
	timeouts map[string]*time.Timer
	logger   Logger
	closed   bool
}

// RequestPoolOptions for configuring the request pool
type RequestPoolOptions struct {
	MaxSize int
	Logger  Logger
}

// NewRequestPool creates a new request pool
func NewRequestPool() *RequestPool {
	return NewRequestPoolWithOptions(RequestPoolOptions{
		MaxSize: 10000, // Default max size
	})
}

// NewRequestPoolWithOptions creates a new request pool with options
func NewRequestPoolWithOptions(opts RequestPoolOptions) *RequestPool {
	if opts.MaxSize <= 0 {
		opts.MaxSize = 10000
	}

	return &RequestPool{
		requests: make(map[string]*Request),
		maxSize:  opts.MaxSize,
		timeouts: make(map[string]*time.Timer),
		logger:   opts.Logger,
		closed:   false,
	}
}

// AddRequest adds a request to the pool
func (rp *RequestPool) AddRequest(request *Request) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	if rp.closed {
		return fmt.Errorf("request pool is closed")
	}

	if len(rp.requests) >= rp.maxSize {
		return fmt.Errorf("request pool is full (max: %d)", rp.maxSize)
	}

	if _, exists := rp.requests[request.ID]; exists {
		return fmt.Errorf("request %s already exists", request.ID)
	}

	rp.requests[request.ID] = request

	// Set a timeout for the request (optional)
	if rp.logger != nil {
		rp.logger.Debug("Added request to pool", "requestID", request.ID, "phase", request.Phase.String())
	}

	return nil
}

// GetRequest retrieves a request from the pool
func (rp *RequestPool) GetRequest(requestID string) (*Request, bool) {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	req, exists := rp.requests[requestID]
	return req, exists
}

// UpdateRequestPhase updates the phase of a request
func (rp *RequestPool) UpdateRequestPhase(requestID string, phase RequestPhase) {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	if req, exists := rp.requests[requestID]; exists {
		oldPhase := req.Phase
		req.Phase = phase
		if rp.logger != nil {
			rp.logger.Debug("Updated request phase", "requestID", requestID, "oldPhase", oldPhase.String(), "newPhase", phase.String())
		}
	}
}

// RemoveRequest removes a request from the pool
func (rp *RequestPool) RemoveRequest(requestID string) error {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	if _, exists := rp.requests[requestID]; !exists {
		return fmt.Errorf("request %s not found", requestID)
	}

	delete(rp.requests, requestID)

	// Clean up timeout if exists
	if timer, exists := rp.timeouts[requestID]; exists {
		timer.Stop()
		delete(rp.timeouts, requestID)
	}

	if rp.logger != nil {
		rp.logger.Debug("Removed request from pool", "requestID", requestID)
	}

	return nil
}

// Size returns the number of requests in the pool
func (rp *RequestPool) Size() int {
	rp.mu.RLock()
	defer rp.mu.RUnlock()
	return len(rp.requests)
}

// Close closes the request pool
func (rp *RequestPool) Close() {
	rp.mu.Lock()
	defer rp.mu.Unlock()

	rp.closed = true

	// Stop all timers
	for _, timer := range rp.timeouts {
		timer.Stop()
	}

	// Clear all data
	rp.requests = make(map[string]*Request)
	rp.timeouts = make(map[string]*time.Timer)

	if rp.logger != nil {
		rp.logger.Debug("Request pool closed")
	}
}

// GetRequestsByPhase returns all requests in a specific phase
func (rp *RequestPool) GetRequestsByPhase(phase RequestPhase) []*Request {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	var requests []*Request
	for _, req := range rp.requests {
		if req.Phase == phase {
			requests = append(requests, req)
		}
	}
	return requests
}

// PrepareMessage represents a prepare phase message
type PrepareMessage struct {
	View       uint64
	Sequence   uint64
	ProposalID string
	Digest     []byte
	NodeID     NodeID
	Signature  []byte
}

// CommitPhaseMessage represents a commit phase message
type CommitPhaseMessage struct {
	View       uint64
	Sequence   uint64
	ProposalID string
	Digest     []byte
	NodeID     NodeID
	Signature  []byte
}
