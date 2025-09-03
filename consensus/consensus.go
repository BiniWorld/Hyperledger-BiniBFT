package consensus

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Consensus implements the consensus protocol
type Consensus struct {
	config *Config

	// State management
	mu          sync.RWMutex
	isActive    bool
	currentView uint64

	// View-based consensus management
	currentViewObj *View

	// Request pool for binibft processing
	requestPool RequestPoolInterface

	// Batch processing
	// batchManager *BatchManager

	// Channels for internal communication
	stopCh  chan struct{}
	Batcher Batcher

	assembler Assembler

	requestInspector RequestInspector
	// Performance metrics
	metrics *ConsensusMetrics
}

// NewConsensus creates a new consensus instance
func NewConsensus(config *Config) *Consensus {
	submittedChan := make(chan struct{}, 1)
	hc := &Consensus{
		config: config,
		requestPool: NewRequestPoolWithOptions(RequestPoolOptions{
			Logger:        config.Logger,
			insepctor:     config.RequestInspector,
			submittedChan: submittedChan,
			Network:       config.Network,
			NodeID:        config.NodeID,
			Role:          config.Role,
			PrimaryLeader: config.PrimaryLeader,
		}),
		stopCh:  make(chan struct{}),
		metrics: NewConsensusMetrics(),
	}

	hc.currentViewObj = NewView(config.NodeID, config.ShardID, config)
	hc.Batcher = NewBatchBuilder(hc.requestPool, submittedChan, config.RequestBatchMaxCount, config.RequestBatchMaxBytes, config.RequestBatchMaxInterval)

	return hc
}

// Start begins the consensus protocol
func (hc *Consensus) Start(ctx context.Context) error {
	if hc.isActive {
		return fmt.Errorf("consensus already active")
	}

	hc.isActive = true
	hc.config.Network.RegisterHandler(hc)

	// Start batch processing only on primary leader
	if hc.config.Role == RolePrimaryLeader {
		hc.config.Logger.Info("Starting batch processing - Primary Leader", "nodeID", hc.config.NodeID)
		hc.processBatch()
	} else {
		hc.config.Logger.Info("Skipping batch processing - Not Primary Leader",
			"nodeID", hc.config.NodeID,
			"role", hc.config.Role.String())
	}

	hc.config.Logger.Info("consensus started",
		"nodeID", hc.config.NodeID,
		"role", hc.config.Role.String(),
		"shardID", hc.config.ShardID)

	return nil
}

// Stop stops the consensus protocol
func (hc *Consensus) Stop() error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if !hc.isActive {
		return nil
	}

	hc.isActive = false
	close(hc.stopCh)

	// Reset view
	if hc.currentViewObj != nil {
		hc.currentViewObj.Reset()
	}

	hc.config.Logger.Info("consensus stopped")
	return nil
}

// SubmitRequest submits a new client request for view-based consensus
func (hc *Consensus) SubmitRequest(request []byte) error {
	if !hc.isActive {
		return fmt.Errorf("consensus not active")
	}

	hc.config.Logger.Info("Submitting request",
		"role", hc.config.Role.String(),
		"nodeID", hc.config.NodeID)

	return hc.requestPool.Submit(request)
}

// GetStatus returns the current consensus status
func (hc *Consensus) GetStatus() Status {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	var currentView uint64
	var isPrimary bool

	if hc.currentViewObj != nil {
		currentView = hc.currentViewObj.Number
		isPrimary = hc.currentViewObj.Primary == hc.config.NodeID
	}

	return Status{
		Role:        hc.config.Role,
		ShardID:     hc.config.ShardID,
		IsActive:    hc.isActive,
		CurrentView: currentView,
		IsPrimary:   isPrimary,
	}
}

// GetCurrentView returns the current view information
func (hc *Consensus) GetCurrentView() *View {
	return hc.currentViewObj
}

// processBatchFromManager processes batches from the batch manager
func (hc *Consensus) processBatch() {
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond) // Add a reasonable delay between batch checks
		defer ticker.Stop()

		for {
			select {
			case <-hc.stopCh:
				return
			case <-ticker.C:
				hc.propose()
			}
		}
	}()
}

func (c *Consensus) propose() {
	if c.Batcher.Closed() {
		return
	}

	// Check if there's already a proposal in progress for the current sequence
	if c.currentViewObj.IsProposalInProgress() {
		c.config.Logger.Debug("Proposal already in progress, will retry later",
			"sequence", c.currentViewObj.Sequence)
		return
	}

	nextBatch := c.Batcher.NextBatch()
	if len(nextBatch) == 0 {
		return
	}

	// Get fresh metadata to ensure we have the latest sequence
	metadata := c.currentViewObj.GetMetadata()
	proposal := c.config.Assembler.AssembleProposal(metadata, nextBatch)

	// Try to propose - the view will handle sequence validation
	if err := c.currentViewObj.Propose(proposal); err != nil {
		c.config.Logger.Debug("Failed to propose", "error", err)
	}
}

// HandleMessage implements MessageHandler interface
func (hc *Consensus) HandleMessage(from NodeID, message Message) error {
	switch message.Type {
	case MsgRequest:
		var requestMsg RequestMessage
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &requestMsg); err == nil {
				hc.config.Logger.Info("Received MsgRequest, processing directly", "from", from)
				hc.handleRequest(requestMsg.Request.Data)
			} else {
				hc.config.Logger.Error("Failed to unmarshal request payload", "error", err, "from", from)
			}
		} else {
			hc.config.Logger.Error("Failed to marshal request payload", "error", err, "from", from)
		}
	case MsgPrePrep:
		var prePrepMsg PrePrepMessage
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &prePrepMsg); err == nil {
				hc.config.Logger.Debug("Received pre-prep message", "sequence", prePrepMsg.Sequence, "from", message.From)
				// Use view for view-based consensus only
				if hc.currentViewObj != nil {
					if err := hc.currentViewObj.HandlePrePrepare(&prePrepMsg); err != nil {
						hc.config.Logger.Error("Failed to handle pre-prepare in view", "error", err)
					}
				}
			} else {
				hc.config.Logger.Error("Failed to unmarshal pre-prep payload", "error", err)
			}
		}
	case MsgPreparePhase:
		var prepareMsg PreparePhaseMessage
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &prepareMsg); err == nil {
				hc.config.Logger.Debug("Received prepare phase message", "sequence", prepareMsg.Sequence, "from", message.From)
				// Use view for view-based consensus only
				if hc.currentViewObj != nil {
					if err := hc.currentViewObj.HandlePreparePhase(&prepareMsg); err != nil {
						hc.config.Logger.Error("Failed to handle prepare phase in view", "error", err)
					}
				}
			} else {
				hc.config.Logger.Error("Failed to unmarshal prepare phase payload", "error", err)
			}
		}
	case MsgPrepare:
		var prepareMsg PrepareMessage
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &prepareMsg); err == nil {
				hc.config.Logger.Debug("Received prepare message", "sequence", prepareMsg.Sequence, "from", message.From)
				// Use view for view-based consensus
				if hc.currentViewObj != nil {
					if err := hc.currentViewObj.HandlePreparePhase(&PreparePhaseMessage{
						Proposal: prepareMsg.Proposal,
						View:     prepareMsg.View,
						Sequence: prepareMsg.Sequence,
						// Digest:    prepareMsg.Digest,
						NodeID:    prepareMsg.NodeID,
						ShardID:   hc.config.ShardID,
						Signature: prepareMsg.Signature,
					}); err != nil {
						hc.config.Logger.Error("Failed to handle prepare in view", "error", err)
					}
				}
			} else {
				hc.config.Logger.Error("Failed to unmarshal prepare payload", "error", err)
			}
		}
	case MsgCommitRequest:
		var commitMsg CommitRequestMessage
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &commitMsg); err == nil {
				hc.config.Logger.Debug("Received commit request message", "sequence", commitMsg.Sequence, "from", message.From)
				// Use view for view-based consensus only
				if hc.currentViewObj != nil {
					if err := hc.currentViewObj.HandleCommitRequest(&commitMsg); err != nil {
						hc.config.Logger.Error("Failed to handle commit request in view", "error", err)
					}
				}
			} else {
				hc.config.Logger.Error("Failed to unmarshal commit request payload", "error", err)
			}
		}
	case MsgCommitPhase:
		var commitMsg CommitMessage
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &commitMsg); err == nil {
				hc.config.Logger.Debug("Received commit message", "proposalID", commitMsg.ProposalID, "from", message.From)
				// Use view for view-based consensus
				if hc.currentViewObj != nil {
					// Convert CommitMessage to CommitRequestMessage for view handling
					// Create empty proposal for now - in real implementation this should come from the message
					emptyProposal := Proposal{
						Payload: []byte(commitMsg.ProposalID),
					}
					commitReqMsg := &CommitRequestMessage{
						Proposal: emptyProposal,
						View:     0, // Will be set by view
						Sequence: 0, // Will be set by view
						// Digest:    []byte(commitMsg.ProposalID), // Simple digest
						NodeID:    message.From,
						ShardID:   hc.config.ShardID,
						Signature: []byte("commit-sig"), // Simple signature
					}
					if err := hc.currentViewObj.HandleCommitRequest(commitReqMsg); err != nil {
						hc.config.Logger.Error("Failed to handle commit in view", "error", err)
					}
				}
			} else {
				hc.config.Logger.Error("Failed to unmarshal commit payload", "error", err)
			}
		}
	case MsgViewChange:
		var vcReq ViewChangeRequest
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &vcReq); err == nil {
				hc.config.Logger.Debug("Received view change request", "newView", vcReq.NewView, "from", message.From)
				// View change handling would go here
				hc.config.Logger.Info("View change request received but not implemented", "newView", vcReq.NewView)
			} else {
				hc.config.Logger.Error("Failed to unmarshal view change payload", "error", err)
			}
		}

	case MsgCrossShardRequest:
		// Handle JSON payload deserialization for cross-shard requests
		var crossShardMsg CrossShardRequestMessage
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &crossShardMsg); err == nil {
				hc.config.Logger.Info("Received cross-shard request message",
					"requestID", crossShardMsg.Request.ID,
					"from", message.From,
					"nodeID", hc.config.NodeID)
				hc.handleCrossShardRequest(&crossShardMsg)
			} else {
				hc.config.Logger.Error("Failed to unmarshal cross-shard request payload",
					"from", message.From,
					"nodeID", hc.config.NodeID,
					"error", err)
			}
		} else {
			hc.config.Logger.Error("Failed to marshal cross-shard request payload",
				"from", message.From,
				"nodeID", hc.config.NodeID,
				"error", err)
		}
	case MsgShardAck:
		var shardAckMsg ShardAckMessage
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &shardAckMsg); err == nil {
				hc.config.Logger.Debug("Received shard ack message", "sequence", shardAckMsg.Sequence, "from", message.From)
				// Use view for view-based consensus only
				if hc.currentViewObj != nil {
					if err := hc.currentViewObj.HandleShardAck(&shardAckMsg); err != nil {
						hc.config.Logger.Error("Failed to handle shard ack in view", "error", err)
					}
				}
			} else {
				hc.config.Logger.Error("Failed to unmarshal shard ack payload", "error", err)
			}
		}
	case MsgFinalizedBlock:
		var finalizedBlockMsg FinalizedBlockMessage
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &finalizedBlockMsg); err == nil {
				hc.config.Logger.Debug("Received finalized block message", "sequence", finalizedBlockMsg.Sequence, "from", message.From)
				hc.handleFinalizedBlock(&finalizedBlockMsg)
			} else {
				hc.config.Logger.Error("Failed to unmarshal finalized block payload", "error", err)
			}
		}
	case MsgIntraShardVote:
		var intraShardVoteMsg IntraShardVoteMessage
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &intraShardVoteMsg); err == nil {
				hc.config.Logger.Debug("Received intra-shard vote message", "sequence", intraShardVoteMsg.Sequence, "phase", intraShardVoteMsg.Phase, "from", message.From)
				if hc.currentViewObj != nil {
					if err := hc.currentViewObj.HandleIntraShardVote(&intraShardVoteMsg); err != nil {
						hc.config.Logger.Error("Failed to handle intra-shard vote in view", "error", err)
					}
				}
			} else {
				hc.config.Logger.Error("Failed to unmarshal intra-shard vote payload", "error", err)
			}
		}
	case MsgIntraShardVoteResponse:
		var intraShardVoteResponse IntraShardVoteResponse
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &intraShardVoteResponse); err == nil {
				hc.config.Logger.Debug("Received intra-shard vote response", "sequence", intraShardVoteResponse.Sequence, "phase", intraShardVoteResponse.Phase, "from", message.From)
				if hc.currentViewObj != nil {
					if err := hc.currentViewObj.HandleIntraShardVoteResponse(&intraShardVoteResponse); err != nil {
						hc.config.Logger.Error("Failed to handle intra-shard vote response in view", "error", err)
					}
				}
			} else {
				hc.config.Logger.Error("Failed to unmarshal intra-shard vote response payload", "error", err)
			}
		}
	}
	return nil
}

// handleRequest processes incoming client requests
func (hc *Consensus) handleRequest(request []byte) {
	hc.config.Logger.Debug("Handling request")

	// Add request to pool (this has its own mutex)
	hc.requestPool.Submit(request)

	// Record metrics for request received
	hc.metrics.RecordProposalReceived()

	// Get role reference while holding lock briefly
	hc.mu.Lock()
	role := hc.config.Role
	hc.mu.Unlock()

	// All requests go to primary leader first, then processed via batch system
	switch role {
	case RoleShardFollower, RoleShardLeader:
		// Forward to primary leader - but we need to create a proper request structure
		// For now, just add to pool and let batch system handle it
		hc.config.Logger.Info("Non-primary node received request - added to batch processing",
			"role", role.String(),
			"nodeID", hc.config.NodeID)
	case RolePrimaryLeader:
		// Primary leader processes through batch system
		hc.config.Logger.Info("Primary leader received request - will be processed via batch system",
			"nodeID", hc.config.NodeID)
	}
}

// computeDigest computes a digest for the given data
func (hc *Consensus) computeDigest(data []byte) []byte {
	hash := sha256.Sum256(data)
	return hash[:]
}

// handleCrossShardRequest processes cross-shard request messages
func (hc *Consensus) handleCrossShardRequest(crossShardMsg *CrossShardRequestMessage) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	request := crossShardMsg.Request
	hc.config.Logger.Info("Handling cross-shard request",
		"requestID", request.ID,
		"fromShard", crossShardMsg.OriginShard,
		"myShard", hc.config.ShardID,
		"nodeID", hc.config.NodeID,
		"role", hc.config.Role.String())

	// Cross-shard requests are now processed through the batch system
	// Add to request pool and let the batch manager handle it
	hc.requestPool.Submit(request.Data)
	hc.config.Logger.Info("Cross-shard request added to batch processing", "requestID", request.ID)

	// Send acknowledgment back to primary leader
	ack := &ShardAckMessage{
		Sequence:     0, // Will be set by the receiving view
		ShardID:      hc.config.ShardID,
		NodeID:       hc.config.NodeID,
		Acknowledged: true,
		Timestamp:    time.Now(),
	}

	ackMsg := Message{
		Type:      MsgShardAck,
		From:      hc.config.NodeID,
		To:        hc.config.PrimaryLeader,
		ShardID:   hc.config.ShardID,
		Timestamp: time.Now(),
		Payload:   ack,
	}

	hc.config.Logger.Info("Sending shard acknowledgment",
		"requestID", request.ID,
		"fromNode", hc.config.NodeID,
		"toPrimary", hc.config.PrimaryLeader)

	hc.config.Network.Send(hc.config.PrimaryLeader, ackMsg)

	hc.config.Logger.Info("Cross-shard request processed through view system",
		"requestID", request.ID,
		"shardID", hc.config.ShardID,
		"nodeRole", hc.config.Role.String())
}

// handleFinalizedBlock processes finalized block messages from shard leaders
func (hc *Consensus) handleFinalizedBlock(finalizedMsg *FinalizedBlockMessage) {
	hc.config.Logger.Info("Handling finalized block from shard leader",
		"sequence", finalizedMsg.Sequence,
		"leaderID", finalizedMsg.LeaderID,
		"shardID", finalizedMsg.ShardID,
		"nodeID", hc.config.NodeID)

	// Deliver the finalized proposal to the application
	if hc.config.Application != nil {
		if err := hc.config.Application.Deliver(finalizedMsg.Proposal); err != nil {
			hc.config.Logger.Error("Failed to deliver finalized block to application",
				"error", err,
				"sequence", finalizedMsg.Sequence)
		}
	} else if hc.config.Storage != nil {
		// Fallback to direct storage if no application delivery is configured
		if hc.currentViewObj != nil {
			hc.currentViewObj.storeProposalAsBlock(finalizedMsg.Proposal, finalizedMsg.Sequence)
		}
	}

	hc.config.Logger.Info("Finalized block processed successfully",
		"sequence", finalizedMsg.Sequence,
		"nodeID", hc.config.NodeID)
}

// GetMetrics returns the current consensus metrics
func (hc *Consensus) GetMetrics() map[string]interface{} {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	baseMetrics := hc.metrics.GetMetrics()

	// Add additional consensus-specific metrics
	baseMetrics["node_id"] = string(hc.config.NodeID)
	baseMetrics["shard_id"] = hc.config.ShardID
	baseMetrics["role"] = hc.config.Role.String()
	baseMetrics["is_active"] = hc.isActive
	baseMetrics["current_view"] = hc.currentView
	baseMetrics["request_pool_size"] = hc.requestPool.Size()
	// Finalized requests tracking is now handled by the view system

	return baseMetrics
}
