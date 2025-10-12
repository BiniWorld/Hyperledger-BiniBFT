package consensus

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
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
			MaxSize:       config.RequestBatchMaxBytes, // Use configured max bytes instead of default
			Logger:        config.Logger,
			Inspector:     config.RequestInspector,
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

	// Log batch configuration for debugging
	config.Logger.Info("Creating BatchBuilder with configuration",
		"RequestBatchMaxCount", config.RequestBatchMaxCount,
		"RequestBatchMaxBytes", config.RequestBatchMaxBytes,
		"RequestBatchMaxInterval", config.RequestBatchMaxInterval)

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

	hc.config.Logger.Info("Submitting request to consensus",
		"role", hc.config.Role.String(),
		"nodeID", hc.config.NodeID,
		"requestSize", len(request),
		"isActive", hc.isActive,
		"primaryLeader", hc.config.PrimaryLeader)

	err := hc.requestPool.Submit(request)
	if err != nil {
		hc.config.Logger.Error("Failed to submit request to pool", "error", err)
		return err
	}

	hc.config.Logger.Debug("Request successfully submitted to pool")
	return nil
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
		ticker := time.NewTicker(10 * time.Millisecond) // Very fast batch processing for immediate response
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

func (c *Consensus) propose(){
	if c.Batcher.Closed() {
		c.config.Logger.Debug("Batcher is closed, skipping propose")
		return
	}

	// Check if there's already a proposal in progress for the current sequence
	if c.currentViewObj.IsProposalInProgress() {
		c.config.Logger.Debug("Proposal already in progress, will retry later",
			"sequence", c.currentViewObj.Sequence,
			"poolSize", c.requestPool.Size(),
			"role", c.config.Role.String())
		return
	}

	nextBatch := c.Batcher.NextBatch()
	if len(nextBatch) == 0 {
		return
	}

	c.config.Logger.Info("Processing batch",
		"batchSize", len(nextBatch),
		"sequence", c.currentViewObj.Sequence)

	// Get fresh metadata to ensure we have the latest sequence
	metadata := c.currentViewObj.GetMetadata()
	proposal := c.config.Assembler.AssembleProposal(metadata, nextBatch)

	// Try to propose - the view will handle sequence validation
	if err := c.currentViewObj.Propose(proposal); err != nil {
		c.config.Logger.Error("Failed to propose batch",
			"error", err,
			"batchSize", len(nextBatch),
			"sequence", c.currentViewObj.Sequence)
	} else {
		c.config.Logger.Info("Successfully proposed batch",
			"batchSize", len(nextBatch),
			"sequence", c.currentViewObj.Sequence)
	}
}

// HandleMessage implements MessageHandler interface
func (hc *Consensus) HandleMessage(from NodeID, message Message) error {
	switch message.Type {
	case MsgRequest:
		if decoded, err := base64.StdEncoding.DecodeString(string(message.Payload.([]byte))); err == nil {
			hc.config.Logger.Info("Received MsgRequest, processing directly", "from", from)
			hc.handleRequest(decoded)
		}
	case MsgPrePrep:
		var prePrepMsg PrePrepMessage
		if err := json.Unmarshal(message.Payload.([]byte), &prePrepMsg); err == nil {
			hc.config.Logger.Debug("Received pre-prep message", "sequence", prePrepMsg.Sequence, "from", message.From)
			// Use view for view-based consensus only
			if hc.currentViewObj != nil {
				if err := hc.currentViewObj.HandlePrePrepare(&prePrepMsg); err != nil {
					hc.config.Logger.Error("Failed to handle pre-prepare in view", "error", err)
				}
			}
		}
	case MsgPreparePhase:
		var prepareMsg PreparePhaseMessage
		if err := json.Unmarshal(message.Payload.([]byte), &prepareMsg); err == nil {
			hc.config.Logger.Debug("Received prepare phase message", "sequence", prepareMsg.Sequence, "from", message.From)
			// Use view for view-based consensus only
			if hc.currentViewObj != nil {
				if err := hc.currentViewObj.HandlePreparePhase(&prepareMsg); err != nil {
					hc.config.Logger.Error("Failed to handle prepare phase in view", "error", err)
				}
			}
		}
	case MsgPrepare:
		var prepareMsg PrepareMessage
		if err := json.Unmarshal(message.Payload.([]byte), &prepareMsg); err == nil {
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
		}
	case MsgCommitRequest:
		var commitMsg CommitRequestMessage
		if err := json.Unmarshal(message.Payload.([]byte), &commitMsg); err == nil {
			hc.config.Logger.Debug("Received commit request message", "sequence", commitMsg.Sequence, "from", message.From)
			// Use view for view-based consensus only
			if hc.currentViewObj != nil {
				if err := hc.currentViewObj.HandleCommitRequest(&commitMsg); err != nil {
					hc.config.Logger.Error("Failed to handle commit request in view", "error", err)
				}
			}
		}
	case MsgCommitPhase:
		var commitMsg CommitMessage
		if err := json.Unmarshal(message.Payload.([]byte), &commitMsg); err == nil {
			hc.config.Logger.Debug("Received commit message", "proposalID", commitMsg.ProposalID, "from", message.From)
			// Use view for view-based consensus
			if hc.currentViewObj != nil {
				// Convert CommitMessage to CommitRequestMessage for view handling
				// Create empty proposal for now - in real implementation this should come from the message
				emptyProposal := Proposal{
					Payload: []byte(commitMsg.ProposalID),
				}
				// Create proper digest from proposal
				var digest []byte
				if len(commitMsg.ProposalID) > 0 {
					digest = []byte(commitMsg.ProposalID)
				} else if emptyProposal.Payload != nil {
					hash := sha256.Sum256(emptyProposal.Payload)
					digest = hash[:]
				}

				// Create proper signature
				var signature []byte
				if hc.config.Signer != nil {
					signature = hc.config.Signer.Sign(digest)
				}

				commitReqMsg := &CommitRequestMessage{
					Proposal:  emptyProposal,
					View:      0, // Will be set by view
					Sequence:  0, // Will be set by view
					Digest:    string(digest),
					NodeID:    message.From,
					ShardID:   hc.config.ShardID,
					Signature: signature,
				}
				if err := hc.currentViewObj.HandleCommitRequest(commitReqMsg); err != nil {
					hc.config.Logger.Error("Failed to handle commit in view", "error", err)
				}
			}
		}
	case MsgViewChange:
		var vcReq ViewChangeRequest
		if err := json.Unmarshal(message.Payload.([]byte), &vcReq); err == nil {
			hc.config.Logger.Debug("Received view change request", "newView", vcReq.NewView, "from", message.From)
			// View change handling would go here
			hc.config.Logger.Info("View change request received but not implemented", "newView", vcReq.NewView)
		}

	case MsgCrossShardRequest:
		// Handle JSON payload deserialization for cross-shard requests
		var crossShardMsg CrossShardRequestMessage
		if err := json.Unmarshal(message.Payload.([]byte), &crossShardMsg); err == nil {
			hc.config.Logger.Info("Received cross-shard request message",
				"requestID", crossShardMsg.Request.ID,
				"from", message.From,
				"nodeID", hc.config.NodeID)
			hc.handleCrossShardRequest(&crossShardMsg)
		}
	case MsgShardAck:
		var shardAckMsg ShardAckMessage
		if err := json.Unmarshal(message.Payload.([]byte), &shardAckMsg); err == nil {
			hc.config.Logger.Debug("Received shard ack message", "sequence", shardAckMsg.Sequence, "from", message.From)
			// Use view for view-based consensus only
			if hc.currentViewObj != nil {
				if err := hc.currentViewObj.HandleShardAck(&shardAckMsg); err != nil {
					hc.config.Logger.Error("Failed to handle shard ack in view", "error", err)
				}
			}
		}
	case MsgFinalizedBlock:
		var finalizedBlockMsg FinalizedBlockMessage
		if err := json.Unmarshal(message.Payload.([]byte), &finalizedBlockMsg); err == nil {
			hc.config.Logger.Debug("Received finalized block message", "sequence", finalizedBlockMsg.Sequence, "from", message.From)
			hc.handleFinalizedBlock(&finalizedBlockMsg)
		}
	case MsgIntraShardVote:
		var intraShardVoteMsg IntraShardVoteMessage
		if err := json.Unmarshal(message.Payload.([]byte), &intraShardVoteMsg); err == nil {
			hc.config.Logger.Debug("Received intra-shard vote message", "sequence", intraShardVoteMsg.Sequence, "phase", intraShardVoteMsg.Phase, "from", message.From)
			if hc.currentViewObj != nil {
				if err := hc.currentViewObj.HandleIntraShardVote(&intraShardVoteMsg); err != nil {
					hc.config.Logger.Error("Failed to handle intra-shard vote in view", "error", err)
				}
			}
		}
	case MsgIntraShardVoteResponse:
		var intraShardVoteResponse IntraShardVoteResponse
		if err := json.Unmarshal(message.Payload.([]byte), &intraShardVoteResponse); err == nil {
			hc.config.Logger.Debug("Received intra-shard vote response", "sequence", intraShardVoteResponse.Sequence, "phase", intraShardVoteResponse.Phase, "from", message.From)
			if hc.currentViewObj != nil {
				if err := hc.currentViewObj.HandleIntraShardVoteResponse(&intraShardVoteResponse); err != nil {
					hc.config.Logger.Error("Failed to handle intra-shard vote response in view", "error", err)
				}
			}
		}
	}
	return nil
}

// handleRequest processes incoming client requests
func (hc *Consensus) handleRequest(request []byte) {
	hc.config.Logger.Debug("Handling request", "requestSize", len(request))
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

// collectSignaturesFromFinalized collects signatures from a finalized block message
func (hc *Consensus) collectSignaturesFromFinalized(finalizedMsg *FinalizedBlockMessage) []Signature {
	var signatures []Signature

	// Collect signatures from all nodes that participated in consensus
	// This should include signatures from the finalized message's vote set
	if finalizedMsg.Signatures != nil {
		signatures = append(signatures, finalizedMsg.Signatures...)
	}

	// If no signatures in finalized message, collect from consensus participants
	if len(signatures) == 0 && hc.config.Signer != nil {
		// Create signature from current node as fallback
		digest := sha256.Sum256(finalizedMsg.Proposal.Payload)
		sig := hc.config.Signer.Sign(digest[:])

		nodeIDUint64 := hc.nodeIDToUint64(hc.config.NodeID)

		signatures = append(signatures, Signature{
			ID:    nodeIDUint64,
			Value: sig,
			Msg:   digest[:],
		})

		// TODO: Collect signatures from other consensus participants
		// This requires implementing proper BFT signature aggregation
		hc.config.Logger.Info("BiniBFT: Only single signature available - peer validation may fail")
	}

	hc.config.Logger.Info("Collected signatures for block delivery",
		"count", len(signatures),
		"signers", func() []uint64 {
			var signers []uint64
			for _, sig := range signatures {
				signers = append(signers, sig.ID)
			}
			return signers
		}())

	return signatures
}

// nodeIDToUint64 converts NodeID string to uint64 (same approach as SmartBFT)
func (hc *Consensus) nodeIDToUint64(nodeID NodeID) uint64 {
	// Try to parse as numeric first
	if id, err := strconv.ParseUint(string(nodeID), 10, 64); err == nil {
		return id
	}

	// Fallback to hash-based conversion
	hash := sha256.Sum256([]byte(nodeID))
	// Use first 8 bytes of hash as uint64
	return uint64(hash[0])<<56 | uint64(hash[1])<<48 | uint64(hash[2])<<40 | uint64(hash[3])<<32 |
		uint64(hash[4])<<24 | uint64(hash[5])<<16 | uint64(hash[6])<<8 | uint64(hash[7])
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
		// Collect signatures from the finalized message
		signatures := hc.collectSignaturesFromFinalized(finalizedMsg)
		if err := hc.config.Application.Deliver(finalizedMsg.Proposal, signatures); err != nil {
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
