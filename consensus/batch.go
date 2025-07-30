package consensus

import (
	"fmt"
	"sync"
	"time"
)

// ProposalBatch represents a batch of proposals
type ProposalBatch struct {
	ID        string
	Proposals []*Proposal
	ShardID   ShardID
	NodeID    NodeID
	Timestamp time.Time
}

// BatchManager manages batching of proposals
type BatchManager struct {
	batchSize    int
	maxDelay     time.Duration
	nodeID       NodeID
	shardID      ShardID
	proposals    []*Proposal
	mu           sync.Mutex
	batchCh      chan *ProposalBatch
	timer        *time.Timer
	timerRunning bool
}

// NewBatchManager creates a new batch manager
func NewBatchManager(batchSize int, maxDelay time.Duration, nodeID NodeID, shardID ShardID) *BatchManager {
	return &BatchManager{
		batchSize: batchSize,
		maxDelay:  maxDelay,
		nodeID:    nodeID,
		shardID:   shardID,
		proposals: make([]*Proposal, 0, batchSize),
		batchCh:   make(chan *ProposalBatch, 10),
	}
}

// AddProposal adds a proposal to the batch
func (bm *BatchManager) AddProposal(proposal *Proposal) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Add proposal to batch
	bm.proposals = append(bm.proposals, proposal)

	// Start timer if not already running
	if !bm.timerRunning {
		bm.timerRunning = true
		bm.timer = time.AfterFunc(bm.maxDelay, bm.processBatch)
	}

	// Process batch immediately if it's full
	if len(bm.proposals) >= bm.batchSize {
		if bm.timer != nil {
			bm.timer.Stop()
		}
		go bm.processBatch()
	}
}

// processBatch processes the current batch
func (bm *BatchManager) processBatch() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Reset timer state
	bm.timerRunning = false

	// Skip if no proposals
	if len(bm.proposals) == 0 {
		return
	}

	// Create batch
	batch := &ProposalBatch{
		ID:        fmt.Sprintf("batch-%d", time.Now().UnixNano()),
		Proposals: bm.proposals,
		ShardID:   bm.shardID,
		NodeID:    bm.nodeID,
		Timestamp: time.Now(),
	}

	// Reset proposals
	bm.proposals = make([]*Proposal, 0, bm.batchSize)

	// Send batch
	bm.batchCh <- batch
}

// GetBatchChannel returns the batch channel
func (bm *BatchManager) GetBatchChannel() <-chan *ProposalBatch {
	return bm.batchCh
}
