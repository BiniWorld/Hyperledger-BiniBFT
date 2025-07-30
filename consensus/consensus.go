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

	// Request pool for binibft processing
	requestPool RequestPoolInterface

	// Proposal tracking
	proposals      map[string]*Proposal
	votes          map[string]map[NodeID]*Vote
	shardDecisions map[string]map[ShardID]*ShardDecisionMessage

	// binibft message tracking
	prePrepMessages map[string]map[NodeID]*PrePrepMessage
	prepareMessages map[string]map[NodeID]*PreparePhaseMessage
	commitMessages  map[string]map[NodeID]*CommitRequestMessage

	// Batch processing
	batchManager *BatchManager
	batches      map[string]*ProposalBatch
	batchVotes   map[string]map[NodeID]*Vote

	// Channels for internal communication
	proposalCh      chan *Proposal
	voteCh          chan *Vote
	decisionCh      chan *Decision
	batchCh         chan *ProposalBatch
	requestCh       chan *Request
	prePrepCh       chan *PrePrepMessage
	preparePhaseCh  chan *PreparePhaseMessage
	commitReqCh     chan *CommitRequestMessage
	crossShardReqCh chan *CrossShardRequestMessage
	shardAckCh      chan *ShardAckMessage
	stopCh          chan struct{}

	// Cross-shard coordination tracking
	crossShardRequests map[string]*CrossShardRequestMessage
	shardAcks          map[string]map[NodeID]*ShardAckMessage

	// Finalization tracking
	finalizedRequests map[string]bool

	// Performance metrics
	metrics *ConsensusMetrics
}

// NewConsensus creates a new consensus instance
func NewConsensus(config *Config) *Consensus {
	hc := &Consensus{
		config:             config,
		requestPool:        NewRequestPool(),
		proposals:          make(map[string]*Proposal),
		votes:              make(map[string]map[NodeID]*Vote),
		shardDecisions:     make(map[string]map[ShardID]*ShardDecisionMessage),
		prePrepMessages:    make(map[string]map[NodeID]*PrePrepMessage),
		prepareMessages:    make(map[string]map[NodeID]*PreparePhaseMessage),
		commitMessages:     make(map[string]map[NodeID]*CommitRequestMessage),
		crossShardRequests: make(map[string]*CrossShardRequestMessage),
		shardAcks:          make(map[string]map[NodeID]*ShardAckMessage),
		finalizedRequests:  make(map[string]bool),
		batches:            make(map[string]*ProposalBatch),
		batchVotes:         make(map[string]map[NodeID]*Vote),
		proposalCh:         make(chan *Proposal, 100),
		voteCh:             make(chan *Vote, 100),
		decisionCh:         make(chan *Decision, 100),
		batchCh:            make(chan *ProposalBatch, 10),
		requestCh:          make(chan *Request, 100),
		prePrepCh:          make(chan *PrePrepMessage, 100),
		preparePhaseCh:     make(chan *PreparePhaseMessage, 100),
		commitReqCh:        make(chan *CommitRequestMessage, 100),
		crossShardReqCh:    make(chan *CrossShardRequestMessage, 100),
		shardAckCh:         make(chan *ShardAckMessage, 100),
		stopCh:             make(chan struct{}),
		metrics:            NewConsensusMetrics(),
	}

	// Initialize batch manager if this node can propose
	if config.Role == RolePrimaryLeader || config.Role == RoleShardLeader {
		hc.batchManager = NewBatchManager(
			config.BatchSize,
			config.MaxBatchDelay,
			config.NodeID,
			config.ShardID,
		)
	}

	return hc
}

// Start begins the consensus protocol
func (hc *Consensus) Start(ctx context.Context) error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if hc.isActive {
		return fmt.Errorf("consensus already active")
	}

	hc.isActive = true
	hc.config.Network.RegisterHandler(hc)

	// Start main consensus loop
	go hc.consensusLoop(ctx)

	hc.config.Logger.Info("consensus started",
		"nodeID", hc.config.NodeID,
		"role", hc.config.Role,
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

	hc.config.Logger.Info("consensus stopped")
	return nil
}

// SubmitRequest submits a new client request for binibft consensus
func (hc *Consensus) SubmitRequest(request *Request) error {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	if !hc.isActive {
		return fmt.Errorf("consensus not active")
	}

	request.Timestamp = time.Now()
	request.Phase = PhasePrePrep

	select {
	case hc.requestCh <- request:
		return nil
	default:
		return fmt.Errorf("request channel full")
	}
}

// Propose submits a new proposal for consensus
func (hc *Consensus) Propose(proposal *Proposal) error {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	if !hc.isActive {
		return fmt.Errorf("consensus not active")
	}

	// Only shard leaders and primary leader can propose
	if hc.config.Role == RoleShardFollower {
		return fmt.Errorf("shard followers cannot propose")
	}

	proposal.Proposer = hc.config.NodeID
	proposal.Timestamp = time.Now()

	select {
	case hc.proposalCh <- proposal:
		return nil
	default:
		return fmt.Errorf("proposal channel full")
	}
}

// GetStatus returns the current consensus status
func (hc *Consensus) GetStatus() Status {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	return Status{
		Role:     hc.config.Role,
		ShardID:  hc.config.ShardID,
		IsActive: hc.isActive,
	}
}

// consensusLoop is the main consensus processing loop
func (hc *Consensus) consensusLoop(ctx context.Context) {
	ticker := time.NewTicker(hc.config.Timeout)
	defer ticker.Stop()

	// Connect to batch manager if available
	var batchCh <-chan *ProposalBatch
	if hc.batchManager != nil {
		batchCh = hc.batchManager.GetBatchChannel()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-hc.stopCh:
			return
		case request := <-hc.requestCh:
			hc.config.Logger.Info("Processing request from requestCh", "requestID", request.ID, "nodeID", hc.config.NodeID)
			hc.handleRequest(request)

		case prePrepMsg := <-hc.prePrepCh:
			hc.handlePrePrep(prePrepMsg)

		case prepareMsg := <-hc.preparePhaseCh:
			hc.handlePreparePhase(prepareMsg)

		case commitMsg := <-hc.commitReqCh:
			hc.handleCommitRequest(commitMsg)

		case crossShardMsg := <-hc.crossShardReqCh:
			hc.handleCrossShardRequest(crossShardMsg)

		case shardAck := <-hc.shardAckCh:
			hc.handleShardAck(shardAck)

		case proposal := <-hc.proposalCh:
			hc.metrics.RecordProposalReceived()

			// If we're a proposer and have a batch manager, add to batch
			if hc.batchManager != nil && (hc.config.Role == RolePrimaryLeader || hc.config.Role == RoleShardLeader) {
				hc.batchManager.AddProposal(proposal)
			} else {
				// Store proposal for potential future use
				hc.proposals[proposal.ID] = proposal
				hc.votes[proposal.ID] = make(map[NodeID]*Vote)
			}

		case batch := <-batchCh:
			// Store batch for potential future use
			hc.batches[batch.ID] = batch
			hc.batchVotes[batch.ID] = make(map[NodeID]*Vote)

		case vote := <-hc.voteCh:
			hc.metrics.RecordVoteReceived()
			hc.handleVote(vote)

		case decision := <-hc.decisionCh:
			hc.metrics.RecordDecisionReached()
			hc.handleDecision(decision)

		case <-ticker.C:
			hc.handleTimeout()
		}
	}
}

// handleVote processes incoming votes
func (hc *Consensus) handleVote(vote *Vote) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.config.Logger.Debug("Handling vote", "proposalID", vote.ProposalID, "from", vote.NodeID)

	// Check if this is a batch vote or individual proposal vote
	if _, exists := hc.batches[vote.ProposalID]; exists {
		// This is a batch vote
		if _, exists := hc.batchVotes[vote.ProposalID]; !exists {
			hc.batchVotes[vote.ProposalID] = make(map[NodeID]*Vote)
		}
		hc.batchVotes[vote.ProposalID][vote.NodeID] = vote

		// Check if we have majority for the batch
		if hc.config.Role == RoleShardLeader {
			hc.checkBatchShardMajority(vote.ProposalID)
		}
	} else {
		// This is an individual proposal vote
		if _, exists := hc.votes[vote.ProposalID]; !exists {
			hc.votes[vote.ProposalID] = make(map[NodeID]*Vote)
		}
		hc.votes[vote.ProposalID][vote.NodeID] = vote

		// Check if we have majority in shard
		if hc.config.Role == RoleShardLeader {
			hc.checkShardMajority(vote.ProposalID)
		}
	}
}

// checkBatchShardMajority checks if shard has reached majority consensus on a batch
func (hc *Consensus) checkBatchShardMajority(batchID string) {
	votes := hc.batchVotes[batchID]
	shardNodes := hc.config.ShardNodes[hc.config.ShardID]

	approveCount := 0
	for _, vote := range votes {
		if vote.Approve {
			approveCount++
		}
	}

	// Calculate required majority based on threshold
	requiredVotes := int(float64(len(shardNodes)) * hc.config.ShardMajorityThreshold)
	if requiredVotes < 1 {
		requiredVotes = 1 // At least one vote required
	}

	if approveCount >= requiredVotes {
		// Shard has reached majority for batch - send decision to primary leader
		batch := hc.batches[batchID]

		shardDecision := &ShardDecisionMessage{
			ShardID:  hc.config.ShardID,
			Decision: true,
			Votes:    make([]Vote, 0, len(votes)),
			BatchID:  batchID,
		}

		for _, vote := range votes {
			shardDecision.Votes = append(shardDecision.Votes, *vote)
		}

		msg := Message{
			Type:      MsgShardDecision,
			From:      hc.config.NodeID,
			To:        hc.config.PrimaryLeader,
			ShardID:   hc.config.ShardID,
			Timestamp: time.Now(),
			Payload:   shardDecision,
		}

		hc.config.Network.Send(hc.config.PrimaryLeader, msg)
		hc.config.Logger.Info("Shard majority reached for batch",
			"batchID", batchID,
			"shardID", hc.config.ShardID,
			"proposals", len(batch.Proposals))

		// Record metrics
		hc.metrics.RecordShardConsensusTime(hc.config.ShardID, time.Since(batch.Timestamp))
	}
}

// checkShardMajority checks if shard has reached majority consensus
func (hc *Consensus) checkShardMajority(proposalID string) {
	votes := hc.votes[proposalID]
	shardNodes := hc.config.ShardNodes[hc.config.ShardID]

	approveCount := 0
	for _, vote := range votes {
		if vote.Approve {
			approveCount++
		}
	}

	// Calculate required majority based on threshold
	requiredVotes := int(float64(len(shardNodes)) * hc.config.ShardMajorityThreshold)
	if requiredVotes < 1 {
		requiredVotes = 1 // At least one vote required
	}

	if approveCount >= requiredVotes {
		// Shard has reached majority - send decision to primary leader
		shardDecision := &ShardDecisionMessage{
			ShardID:  hc.config.ShardID,
			Decision: true,
			Votes:    make([]Vote, 0, len(votes)),
		}

		for _, vote := range votes {
			shardDecision.Votes = append(shardDecision.Votes, *vote)
		}

		msg := Message{
			Type:      MsgShardDecision,
			From:      hc.config.NodeID,
			To:        hc.config.PrimaryLeader,
			ShardID:   hc.config.ShardID,
			Timestamp: time.Now(),
			Payload:   shardDecision,
		}

		hc.config.Network.Send(hc.config.PrimaryLeader, msg)
		hc.config.Logger.Info("Shard majority reached", "proposalID", proposalID, "shardID", hc.config.ShardID)

		// Record metrics
		if proposal, exists := hc.proposals[proposalID]; exists {
			hc.metrics.RecordShardConsensusTime(hc.config.ShardID, time.Since(proposal.Timestamp))
		}
	}
}

// handleDecision processes final consensus decisions
func (hc *Consensus) handleDecision(decision *Decision) {
	hc.config.Logger.Info("Final decision reached", "proposalID", decision.ProposalID, "committed", decision.Committed)

	// Record metrics
	if proposal, exists := hc.proposals[decision.ProposalID]; exists {
		hc.metrics.RecordCommitLatency(time.Since(proposal.Timestamp))
	}

	// Application-specific decision handling would go here
}

// handleTimeout handles consensus timeouts
func (hc *Consensus) handleTimeout() {
	// Implement timeout logic for proposals
	hc.config.Logger.Debug("Consensus timeout check")
}

// HandleMessage implements MessageHandler interface
func (hc *Consensus) HandleMessage(from NodeID, message Message) error {
	switch message.Type {
	case MsgRequest:
		var requestMsg RequestMessage
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &requestMsg); err == nil {
				hc.config.Logger.Info("Received MsgRequest, forwarding to requestCh", "requestID", requestMsg.Request.ID, "from", from)
				select {
				case hc.requestCh <- requestMsg.Request:
					hc.config.Logger.Info("Successfully sent request to requestCh", "requestID", requestMsg.Request.ID)
				default:
					hc.config.Logger.Error("Request channel is full, dropping request", "requestID", requestMsg.Request.ID)
				}
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
				hc.config.Logger.Debug("Received pre-prep message", "requestID", prePrepMsg.RequestID, "from", message.From)
				hc.prePrepCh <- &prePrepMsg
			} else {
				hc.config.Logger.Error("Failed to unmarshal pre-prep payload", "error", err)
			}
		}
	case MsgPreparePhase:
		var prepareMsg PreparePhaseMessage
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &prepareMsg); err == nil {
				hc.config.Logger.Debug("Received prepare phase message", "requestID", prepareMsg.RequestID, "from", message.From)
				hc.preparePhaseCh <- &prepareMsg
			} else {
				hc.config.Logger.Error("Failed to unmarshal prepare phase payload", "error", err)
			}
		}
	case MsgCommitRequest:
		var commitMsg CommitRequestMessage
		if payloadBytes, err := json.Marshal(message.Payload); err == nil {
			if err := json.Unmarshal(payloadBytes, &commitMsg); err == nil {
				hc.config.Logger.Debug("Received commit request message", "requestID", commitMsg.RequestID, "from", message.From)
				hc.commitReqCh <- &commitMsg
			} else {
				hc.config.Logger.Error("Failed to unmarshal commit request payload", "error", err)
			}
		}
	case MsgProposal:
		if payload, ok := message.Payload.(*ProposalMessage); ok {
			hc.proposalCh <- payload.Proposal
		}
	case MsgBatchProposal:
		if payload, ok := message.Payload.(*BatchProposalMessage); ok {
			hc.batchCh <- payload.Batch
		}
	case MsgVote:
		if payload, ok := message.Payload.(*VoteMessage); ok {
			hc.voteCh <- payload.Vote
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
				hc.crossShardReqCh <- &crossShardMsg
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
				hc.config.Logger.Debug("Received shard ack message", "requestID", shardAckMsg.RequestID, "from", message.From)
				hc.shardAckCh <- &shardAckMsg
			} else {
				hc.config.Logger.Error("Failed to unmarshal shard ack payload", "error", err)
			}
		}
	}
	return nil
}

// storeProposalAsBlock stores a committed proposal as a block in storage
func (hc *Consensus) storeProposalAsBlock(proposal *Proposal, batchID string) {
	// Get the latest block to determine the height and previous hash
	var height uint64 = 1
	var prevHash string

	latestBlock, err := hc.config.Storage.GetLatestBlock()
	if err == nil && latestBlock != nil {
		height = latestBlock.Height + 1
		prevHash = latestBlock.Hash
	}

	// Extract transactions from proposal data
	var transactions []Transaction
	if err := json.Unmarshal(proposal.Data, &transactions); err != nil {
		// If the data is not a transaction array, create a single transaction
		tx := Transaction{
			ID:        fmt.Sprintf("tx-%s", proposal.ID),
			Timestamp: proposal.Timestamp,
			Data:      proposal.Data,
		}
		transactions = []Transaction{tx}
	}

	// Create a new block
	block := &Block{
		ID:           proposal.ID,
		PreviousHash: prevHash,
		Height:       height,
		Timestamp:    time.Now(),
		Transactions: transactions,
		ProposalID:   proposal.ID,
		Proposer:     proposal.Proposer,
	}

	// Calculate block hash (simplified for example)
	blockData, _ := json.Marshal(block)
	block.Hash = fmt.Sprintf("%x", sha256.Sum256(blockData))

	// Store the block
	if err := hc.config.Storage.StoreBlock(block); err != nil {
		hc.config.Logger.Error("Failed to store block", "error", err, "blockID", block.ID)
	} else {
		hc.config.Logger.Info("Block stored successfully",
			"blockID", block.ID,
			"height", block.Height,
			"transactions", len(block.Transactions),
			"batchID", batchID)
	}
}

// handleRequest processes incoming client requests
func (hc *Consensus) handleRequest(request *Request) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.config.Logger.Debug("Handling request", "requestID", request.ID, "phase", request.Phase)

	// Add request to pool
	hc.requestPool.AddRequest(request)

	// Record metrics for request received
	hc.metrics.RecordProposalReceived()

	// NEW ROUTING LOGIC: All requests must go to primary leader first
	switch hc.config.Role {
	case RoleShardFollower:
		// Followers always forward to primary leader first
		hc.forwardToPrimaryLeader(request)
	case RoleShardLeader:
		// Shard leaders always forward to primary leader first (even for their own shard)
		hc.forwardToPrimaryLeader(request)
	case RolePrimaryLeader:
		// Primary leader starts binibft consensus directly with shard leaders
		hc.config.Logger.Info("Primary leader received request, starting binibft consensus", "requestID", request.ID, "clientID", request.ClientID)
		hc.startConsensusAtShardLeaders(request)
	}
}

// forwardToPrimaryLeader forwards request to primary leader
func (hc *Consensus) forwardToPrimaryLeader(request *Request) {
	msg := Message{
		Type:      MsgRequest,
		From:      hc.config.NodeID,
		To:        hc.config.PrimaryLeader,
		ShardID:   request.ShardID,
		Timestamp: time.Now(),
		Payload:   &RequestMessage{Request: request},
	}

	hc.config.Network.Send(hc.config.PrimaryLeader, msg)
	hc.config.Logger.Debug("Forwarded request to primary leader", "requestID", request.ID)
}

// handlePrePrep processes pre-preparation messages
func (hc *Consensus) handlePrePrep(prePrepMsg *PrePrepMessage) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	// Check if request is already finalized
	if hc.finalizedRequests[prePrepMsg.RequestID] {
		hc.config.Logger.Debug("Request already finalized, skipping PrePrep phase handling", "requestID", prePrepMsg.RequestID)
		return
	}

	hc.config.Logger.Info("Handling pre-prep", "requestID", prePrepMsg.RequestID, "from", prePrepMsg.NodeID, "role", hc.config.Role.String())

	// Create and add request to pool if it doesn't exist
	if _, exists := hc.requestPool.GetRequest(prePrepMsg.RequestID); !exists {
		// Create a new request based on the pre-prep message
		request := &Request{
			ID:        prePrepMsg.RequestID,
			Data:      prePrepMsg.Digest, // Use digest as data
			ClientID:  "client",          // Default client ID
			Timestamp: time.Now(),
			Phase:     PhasePrePrep,
			ShardID:   prePrepMsg.ShardID,
		}

		// Add to request pool
		hc.requestPool.AddRequest(request)
		hc.config.Logger.Info("Added request to pool from pre-prep", "requestID", prePrepMsg.RequestID)
	}

	// Store pre-prep message
	if _, exists := hc.prePrepMessages[prePrepMsg.RequestID]; !exists {
		hc.prePrepMessages[prePrepMsg.RequestID] = make(map[NodeID]*PrePrepMessage)
	}
	hc.prePrepMessages[prePrepMsg.RequestID][prePrepMsg.NodeID] = prePrepMsg

	switch hc.config.Role {
	case RoleShardLeader:
		// Shard leaders forward Pre-Prep to their followers and wait for ACKs
		hc.forwardPrePrepToFollowers(prePrepMsg)
		// Also check if we have majority (including self) and send ACK to primary
		hc.checkFollowerMajority(prePrepMsg.RequestID)
	case RoleShardFollower:
		// Followers send ACK back to their shard leader
		hc.sendAckToShardLeader(prePrepMsg)
	}
}

// sendPrepareAckToPrimary sends prepare acknowledgment from shard leader to primary
func (hc *Consensus) sendPrepareAckToPrimary(requestID string) {
	ack := &ShardAckMessage{
		RequestID:    requestID,
		ShardID:      hc.config.ShardID,
		NodeID:       hc.config.NodeID,
		Acknowledged: true,
		Phase:        "prepare",
		Timestamp:    time.Now(),
	}

	msg := Message{
		Type:      MsgShardAck,
		From:      hc.config.NodeID,
		To:        hc.config.PrimaryLeader,
		ShardID:   hc.config.ShardID,
		Timestamp: time.Now(),
		Payload:   ack,
	}

	hc.config.Network.Send(hc.config.PrimaryLeader, msg)
	hc.config.Logger.Info("Sent prepare ACK to primary leader", "requestID", requestID)
}

// sendPrepareAckToShardLeader sends prepare ACK from follower to shard leader
func (hc *Consensus) sendPrepareAckToShardLeader(requestID string) {
	shardLeader := hc.config.ShardLeaders[hc.config.ShardID]

	ack := &ShardAckMessage{
		RequestID:    requestID,
		ShardID:      hc.config.ShardID,
		NodeID:       hc.config.NodeID,
		Acknowledged: true,
		Phase:        "prepare",
		Timestamp:    time.Now(),
	}

	msg := Message{
		Type:      MsgShardAck,
		From:      hc.config.NodeID,
		To:        shardLeader,
		ShardID:   hc.config.ShardID,
		Timestamp: time.Now(),
		Payload:   ack,
	}

	hc.config.Network.Send(shardLeader, msg)
	hc.config.Logger.Info("Sent prepare ACK to shard leader",
		"requestID", requestID,
		"shardLeader", shardLeader,
		"fromFollower", hc.config.NodeID,
		"phase", "prepare")
}

// checkFollowerPrepareMajority checks if majority of followers have sent prepare ACKs
func (hc *Consensus) checkFollowerPrepareMajority(requestID string) {
	shardNodes := hc.config.ShardNodes[hc.config.ShardID]
	requiredCount := int(float64(len(shardNodes)) * hc.config.ShardMajorityThreshold)
	if requiredCount < 1 {
		requiredCount = 1
	}

	// Count prepare ACKs from followers in this shard (including self)
	ackCount := 1 // Count self as ACK
	if acks, exists := hc.shardAcks[requestID]; exists {
		for _, ack := range acks {
			// Check if this node belongs to our shard, has acknowledged, and is prepare phase
			if ack.ShardID == hc.config.ShardID && ack.Acknowledged && ack.Phase == "prepare" {
				ackCount++
				hc.config.Logger.Info("Found prepare ACK from shard member",
					"requestID", requestID,
					"nodeID", ack.NodeID,
					"shardID", ack.ShardID,
					"phase", ack.Phase)
			}
		}
	}

	hc.config.Logger.Info("Checking follower prepare majority",
		"requestID", requestID,
		"ackCount", ackCount,
		"required", requiredCount,
		"shardNodes", len(shardNodes),
		"myShardID", hc.config.ShardID)

	if ackCount >= requiredCount {
		// Check if we already sent prepare ACK to primary for this request
		prepareAckKey := string(hc.config.NodeID) + "-prepare"
		alreadySentPrepareAck := false
		if acks, exists := hc.shardAcks[requestID]; exists {
			if _, exists := acks[NodeID(prepareAckKey)]; exists {
				alreadySentPrepareAck = true
			}
		}

		if !alreadySentPrepareAck {
			// Majority reached - send prepare ACK to primary leader
			hc.config.Logger.Info("Follower prepare majority reached - sending prepare ACK to primary",
				"requestID", requestID,
				"ackCount", ackCount,
				"required", requiredCount)
			hc.sendPrepareAckToPrimary(requestID)
		} else {
			hc.config.Logger.Debug("Already sent prepare ACK to primary, skipping duplicate",
				"requestID", requestID,
				"ackCount", ackCount)
		}
	}
}

// checkPrimaryPrepareQuorum checks if majority of shard leaders have prepared
func (hc *Consensus) checkPrimaryPrepareQuorum(requestID string) {
	// Count prepare ACKs from shard leaders
	prepareAckCount := 0
	if acks, exists := hc.shardAcks[requestID]; exists {
		for _, ack := range acks {
			if ack.Phase == "prepare" && ack.Acknowledged {
				prepareAckCount++
			}
		}
	}

	totalShardLeaders := len(hc.config.ShardLeaders)
	requiredCount := int(float64(totalShardLeaders) * hc.config.CrossShardThreshold)
	if requiredCount < 1 {
		requiredCount = 1
	}

	hc.config.Logger.Info("Checking primary prepare quorum",
		"requestID", requestID,
		"prepareAckCount", prepareAckCount,
		"requiredCount", requiredCount)

	if prepareAckCount >= requiredCount {
		hc.config.Logger.Info("Primary prepare quorum reached", "requestID", requestID)
		hc.startCommitPhaseWithShardLeaders(requestID)
	}
}

// sendCommitAckToPrimary sends commit acknowledgment from shard leader to primary
func (hc *Consensus) sendCommitAckToPrimary(requestID string) {
	ack := &ShardAckMessage{
		RequestID:    requestID,
		ShardID:      hc.config.ShardID,
		NodeID:       hc.config.NodeID,
		Acknowledged: true,
		Phase:        "commit",
		Timestamp:    time.Now(),
	}

	msg := Message{
		Type:      MsgShardAck,
		From:      hc.config.NodeID,
		To:        hc.config.PrimaryLeader,
		ShardID:   hc.config.ShardID,
		Timestamp: time.Now(),
		Payload:   ack,
	}

	hc.config.Network.Send(hc.config.PrimaryLeader, msg)
	hc.config.Logger.Info("Sent commit ACK to primary leader", "requestID", requestID)
}

// sendCommitAckToShardLeader sends commit ACK from follower to shard leader
func (hc *Consensus) sendCommitAckToShardLeader(requestID string) {
	shardLeader := hc.config.ShardLeaders[hc.config.ShardID]

	ack := &ShardAckMessage{
		RequestID:    requestID,
		ShardID:      hc.config.ShardID,
		NodeID:       hc.config.NodeID,
		Acknowledged: true,
		Phase:        "commit",
		Timestamp:    time.Now(),
	}

	msg := Message{
		Type:      MsgShardAck,
		From:      hc.config.NodeID,
		To:        shardLeader,
		ShardID:   hc.config.ShardID,
		Timestamp: time.Now(),
		Payload:   ack,
	}

	hc.config.Network.Send(shardLeader, msg)
	hc.config.Logger.Info("Sent commit ACK to shard leader",
		"requestID", requestID,
		"shardLeader", shardLeader,
		"fromFollower", hc.config.NodeID,
		"phase", "commit")
}

// checkPrimaryCommitQuorum checks if majority of shard leaders have committed
func (hc *Consensus) checkPrimaryCommitQuorum(requestID string) {
	// Count commit ACKs from shard leaders
	commitAckCount := 0
	if acks, exists := hc.shardAcks[requestID]; exists {
		for _, ack := range acks {
			if ack.Phase == "commit" && ack.Acknowledged {
				commitAckCount++
			}
		}
	}

	totalShardLeaders := len(hc.config.ShardLeaders)
	requiredCount := int(float64(totalShardLeaders) * hc.config.CrossShardThreshold)
	if requiredCount < 1 {
		requiredCount = 1
	}

	hc.config.Logger.Info("Checking primary commit quorum",
		"requestID", requestID,
		"commitAckCount", commitAckCount,
		"requiredCount", requiredCount)

	if commitAckCount >= requiredCount {
		hc.config.Logger.Info("Primary commit quorum reached - consensus complete", "requestID", requestID)

		// Consensus is complete - all nodes have decided and created blocks
		// Primary can now determine the next sequence number for future requests
		hc.config.Logger.Info("Consensus completed successfully",
			"requestID", requestID,
			"commitAckCount", commitAckCount,
			"requiredCount", requiredCount)

		// Mark as finalized to prevent further processing
		hc.finalizedRequests[requestID] = true

		// Clean up tracking data
		hc.cleanupRequestTracking(requestID)
	}
}

// cleanupRequestTracking cleans up tracking data for a completed request
func (hc *Consensus) cleanupRequestTracking(requestID string) {
	// Clean up request from pool
	hc.requestPool.RemoveRequest(requestID)

	// Clean up message tracking
	delete(hc.prePrepMessages, requestID)
	delete(hc.prepareMessages, requestID)
	delete(hc.commitMessages, requestID)
	delete(hc.shardAcks, requestID)

	hc.config.Logger.Info("Cleaned up tracking data for completed request", "requestID", requestID)
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

	// Store the cross-shard request
	hc.crossShardRequests[request.ID] = crossShardMsg

	// Send acknowledgment back to primary leader
	ack := &ShardAckMessage{
		RequestID:    request.ID,
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

	// Start binibft consensus within this shard
	hc.config.Logger.Info("Starting binibft consensus in shard",
		"requestID", request.ID,
		"shardID", hc.config.ShardID,
		"nodeRole", hc.config.Role.String())

	// Start binibft consensus for this request
	hc.startConsensusAtShardLeaders(request)
}

// startConsensusAtShardLeaders starts binibft consensus directly with shard leaders
func (hc *Consensus) startConsensusAtShardLeaders(request *Request) {
	hc.config.Logger.Info("Starting binibft consensus with shard leaders",
		"requestID", request.ID,
		"primaryLeader", hc.config.NodeID)

	// Update request phase
	hc.requestPool.UpdateRequestPhase(request.ID, PhasePrePrep)

	// Create pre-prep message for shard leaders
	prePrepMsg := &PrePrepMessage{
		RequestID: request.ID,
		View:      hc.currentView,
		Sequence:  uint64(time.Now().UnixNano()),
		Digest:    hc.computeDigest(request.Data),
		NodeID:    hc.config.NodeID,
		ShardID:   request.ShardID,
	}

	// Initialize tracking
	hc.prePrepMessages[request.ID] = make(map[NodeID]*PrePrepMessage)
	hc.prePrepMessages[request.ID][hc.config.NodeID] = prePrepMsg

	// Send Pre-Prep to shard leaders (Nodes 2 & 4)
	shardLeaders := make([]NodeID, 0, len(hc.config.ShardLeaders))
	for _, leaderID := range hc.config.ShardLeaders {
		if leaderID != hc.config.NodeID { // Don't send to self
			shardLeaders = append(shardLeaders, leaderID)
		}
	}

	msg := Message{
		Type:      MsgPrePrep,
		From:      hc.config.NodeID,
		Timestamp: time.Now(),
		Payload:   prePrepMsg,
	}

	hc.config.Network.Broadcast(shardLeaders, msg)
	hc.config.Logger.Info("Sent Pre-Prep to shard leaders",
		"requestID", request.ID,
		"shardLeaders", len(shardLeaders))
}

// forwardPrePrepToFollowers forwards Pre-Prep to shard followers
func (hc *Consensus) forwardPrePrepToFollowers(prePrepMsg *PrePrepMessage) {
	// Get followers for this shard
	shardNodes := hc.config.ShardNodes[hc.config.ShardID]
	followers := make([]NodeID, 0)

	for _, nodeID := range shardNodes {
		if nodeID != hc.config.NodeID { // Don't send to self
			followers = append(followers, nodeID)
		}
	}

	if len(followers) > 0 {
		msg := Message{
			Type:      MsgPrePrep,
			From:      hc.config.NodeID,
			ShardID:   hc.config.ShardID,
			Timestamp: time.Now(),
			Payload:   prePrepMsg,
		}

		hc.config.Network.Broadcast(followers, msg)
		hc.config.Logger.Info("Forwarded Pre-Prep to followers",
			"requestID", prePrepMsg.RequestID,
			"followers", len(followers))
	}
}

// sendAckToShardLeader sends ACK from follower to shard leader
func (hc *Consensus) sendAckToShardLeader(prePrepMsg *PrePrepMessage) {
	shardLeader := hc.config.ShardLeaders[hc.config.ShardID]

	// Create ACK message (using ShardAckMessage)
	ack := &ShardAckMessage{
		RequestID:    prePrepMsg.RequestID,
		ShardID:      hc.config.ShardID,
		NodeID:       hc.config.NodeID,
		Acknowledged: true,
		Phase:        "preprep",
		Timestamp:    time.Now(),
	}

	msg := Message{
		Type:      MsgShardAck,
		From:      hc.config.NodeID,
		To:        shardLeader,
		ShardID:   hc.config.ShardID,
		Timestamp: time.Now(),
		Payload:   ack,
	}

	hc.config.Network.Send(shardLeader, msg)
	hc.config.Logger.Info("Sent preprep ACK to shard leader",
		"requestID", prePrepMsg.RequestID,
		"shardLeader", shardLeader,
		"fromFollower", hc.config.NodeID,
		"phase", "preprep")
}

// handleShardAck processes shard acknowledgment messages (updated for new flow)
func (hc *Consensus) handleShardAck(ack *ShardAckMessage) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.config.Logger.Info("Handling shard ack",
		"requestID", ack.RequestID,
		"fromNode", ack.NodeID,
		"shardID", ack.ShardID,
		"phase", ack.Phase,
		"acknowledged", ack.Acknowledged,
		"myRole", hc.config.Role.String(),
		"myShardID", hc.config.ShardID)

	// Store the acknowledgment
	if _, exists := hc.shardAcks[ack.RequestID]; !exists {
		hc.shardAcks[ack.RequestID] = make(map[NodeID]*ShardAckMessage)
	}

	// Check if we already have an ACK from this node for this request and phase
	ackKey := string(ack.NodeID) + "-" + ack.Phase
	if existingAck, exists := hc.shardAcks[ack.RequestID][NodeID(ackKey)]; exists {
		hc.config.Logger.Debug("Duplicate ACK received, ignoring",
			"requestID", ack.RequestID,
			"fromNode", ack.NodeID,
			"phase", ack.Phase,
			"existingTimestamp", existingAck.Timestamp,
			"newTimestamp", ack.Timestamp)
		return
	}

	hc.shardAcks[ack.RequestID][NodeID(ackKey)] = ack // Store by NodeID-Phase

	switch hc.config.Role {
	case RoleShardLeader:
		hc.config.Logger.Info("Shard leader processing ACK",
			"requestID", ack.RequestID,
			"fromNode", ack.NodeID,
			"phase", ack.Phase,
			"role", hc.config.Role.String())
		// Shard leader checks if majority of followers have ACKed
		switch ack.Phase {
		case "preprep":
			hc.config.Logger.Info("Processing preprep ACK", "requestID", ack.RequestID)
			hc.checkFollowerMajority(ack.RequestID)
		case "prepare":
			hc.config.Logger.Info("Processing prepare ACK", "requestID", ack.RequestID)
			hc.checkFollowerPrepareMajority(ack.RequestID)
		case "commit":
			hc.config.Logger.Info("Processing commit ACK", "requestID", ack.RequestID)
			// Commit ACKs are just for tracking - followers have already decided
		default:
			hc.config.Logger.Info("Unknown ACK phase", "requestID", ack.RequestID, "phase", ack.Phase)
		}
	case RolePrimaryLeader:
		hc.config.Logger.Info("Primary leader processing ACK",
			"requestID", ack.RequestID,
			"fromNode", ack.NodeID,
			"phase", ack.Phase,
			"role", hc.config.Role.String())
		// Primary leader handles different phases of ACKs
		switch ack.Phase {
		case "preprep":
			hc.config.Logger.Info("Primary processing preprep ACK", "requestID", ack.RequestID)
			hc.checkShardLeaderAcks(ack.RequestID)
		case "prepare":
			hc.config.Logger.Info("Primary processing prepare ACK", "requestID", ack.RequestID)
			hc.checkPrimaryPrepareQuorum(ack.RequestID)
		case "commit":
			hc.config.Logger.Info("Primary processing commit ACK", "requestID", ack.RequestID)
			hc.checkPrimaryCommitQuorum(ack.RequestID)
		default:
			hc.config.Logger.Info("Primary received unknown ACK phase", "requestID", ack.RequestID, "phase", ack.Phase)
		}
	}
}

// checkFollowerMajority checks if majority of followers in shard have ACKed
func (hc *Consensus) checkFollowerMajority(requestID string) {
	shardNodes := hc.config.ShardNodes[hc.config.ShardID]
	requiredCount := int(float64(len(shardNodes)) * hc.config.ShardMajorityThreshold)
	if requiredCount < 1 {
		requiredCount = 1
	}

	// Count ACKs from followers in this shard (including self)
	ackCount := 1 // Count self as ACK
	if acks, exists := hc.shardAcks[requestID]; exists {
		for _, ack := range acks {
			// Check if this node belongs to our shard, has acknowledged, and is preprep phase
			if ack.ShardID == hc.config.ShardID && ack.Acknowledged && ack.Phase == "preprep" {
				ackCount++
				hc.config.Logger.Debug("Found preprep ACK from shard member",
					"requestID", requestID,
					"nodeID", ack.NodeID,
					"shardID", ack.ShardID,
					"phase", ack.Phase)
			}
		}
	}

	hc.config.Logger.Info("Checking follower majority",
		"requestID", requestID,
		"ackCount", ackCount,
		"required", requiredCount,
		"shardNodes", len(shardNodes),
		"myShardID", hc.config.ShardID)

	if ackCount >= requiredCount {
		// Check if we already sent preprep ACK to primary for this request
		preprepAckKey := string(hc.config.NodeID) + "-preprep"
		alreadySentAck := false
		if acks, exists := hc.shardAcks[requestID]; exists {
			if _, exists := acks[NodeID(preprepAckKey)]; exists {
				alreadySentAck = true
			}
		}

		if !alreadySentAck {
			// Majority reached - send ACK to primary leader
			hc.config.Logger.Info("Follower majority reached - sending ACK to primary",
				"requestID", requestID,
				"ackCount", ackCount,
				"required", requiredCount)
			hc.sendAckToPrimaryLeader(requestID)
		} else {
			hc.config.Logger.Debug("Already sent ACK to primary, skipping duplicate",
				"requestID", requestID,
				"ackCount", ackCount)
		}
	}
}

// sendAckToPrimaryLeader sends ACK from shard leader to primary leader
func (hc *Consensus) sendAckToPrimaryLeader(requestID string) {
	ack := &ShardAckMessage{
		RequestID:    requestID,
		ShardID:      hc.config.ShardID,
		NodeID:       hc.config.NodeID,
		Acknowledged: true,
		Phase:        "preprep", // This is for preprep phase ACKs
		Timestamp:    time.Now(),
	}

	msg := Message{
		Type:      MsgShardAck,
		From:      hc.config.NodeID,
		To:        hc.config.PrimaryLeader,
		ShardID:   hc.config.ShardID,
		Timestamp: time.Now(),
		Payload:   ack,
	}

	hc.config.Network.Send(hc.config.PrimaryLeader, msg)
	hc.config.Logger.Info("Sent ACK to primary leader",
		"requestID", requestID,
		"primaryLeader", hc.config.PrimaryLeader)
}

// checkShardLeaderAcks checks if all shard leaders have ACKed
func (hc *Consensus) checkShardLeaderAcks(requestID string) {
	// Primary leader waits for ACKs from all shard leaders (not including itself)
	totalShardLeaders := len(hc.config.ShardLeaders)

	// Count preprep ACKs from shard leaders
	ackCount := 0
	if acks, exists := hc.shardAcks[requestID]; exists {
		for _, ack := range acks {
			if ack.Acknowledged && ack.Phase == "preprep" {
				ackCount++
			}
		}
	}

	hc.config.Logger.Info("Checking shard leader ACKs",
		"requestID", requestID,
		"ackCount", ackCount,
		"totalShardLeaders", totalShardLeaders)

	if ackCount >= totalShardLeaders {
		// All shard leaders have ACKed - continue with Prepare phase
		hc.config.Logger.Info("All shard leaders ACKed - starting Prepare phase", "requestID", requestID)
		hc.startPreparePhaseWithShardLeaders(requestID)
	}
}

// startPreparePhaseWithShardLeaders starts Prepare phase with shard leaders
func (hc *Consensus) startPreparePhaseWithShardLeaders(requestID string) {
	// Check if request is already finalized
	if hc.finalizedRequests[requestID] {
		hc.config.Logger.Debug("Request already finalized, skipping Prepare phase", "requestID", requestID)
		return
	}

	// Check if prepare phase already started for this request
	if _, exists := hc.prepareMessages[requestID]; exists {
		hc.config.Logger.Debug("Prepare phase already started, skipping duplicate", "requestID", requestID)
		return
	}

	hc.requestPool.UpdateRequestPhase(requestID, PhasePrepare)

	// Create prepare message
	prepareMsg := &PreparePhaseMessage{
		RequestID: requestID,
		View:      hc.currentView,
		Sequence:  uint64(time.Now().UnixNano()),
		NodeID:    hc.config.NodeID,
		ShardID:   hc.config.ShardID,
	}

	if hc.config.Role == RolePrimaryLeader {
		// Primary leader sends to shard leaders
		shardLeaders := make([]NodeID, 0, len(hc.config.ShardLeaders))
		for _, leaderID := range hc.config.ShardLeaders {
			if leaderID != hc.config.NodeID {
				shardLeaders = append(shardLeaders, leaderID)
			}
		}

		msg := Message{
			Type:      MsgPreparePhase,
			From:      hc.config.NodeID,
			Timestamp: time.Now(),
			Payload:   prepareMsg,
		}

		hc.config.Network.Broadcast(shardLeaders, msg)
		hc.config.Logger.Info("Started Prepare phase with shard leaders",
			"requestID", requestID,
			"shardLeaders", len(shardLeaders))

		// Wait for prepare phase responses before starting commit
		// Commit phase will be started when prepare quorum is reached
	}
}

// handlePreparePhase processes prepare phase messages (updated for new flow)
func (hc *Consensus) handlePreparePhase(prepareMsg *PreparePhaseMessage) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	// Check if request is already finalized
	if hc.finalizedRequests[prepareMsg.RequestID] {
		hc.config.Logger.Debug("Request already finalized, skipping Prepare phase handling", "requestID", prepareMsg.RequestID)
		return
	}

	hc.config.Logger.Info("Handling prepare phase", "requestID", prepareMsg.RequestID, "from", prepareMsg.NodeID, "role", hc.config.Role.String())

	// Store prepare message
	if _, exists := hc.prepareMessages[prepareMsg.RequestID]; !exists {
		hc.prepareMessages[prepareMsg.RequestID] = make(map[NodeID]*PreparePhaseMessage)
	}
	hc.prepareMessages[prepareMsg.RequestID][prepareMsg.NodeID] = prepareMsg

	switch hc.config.Role {
	case RoleShardLeader:
		// Shard leaders forward Prepare to followers
		hc.config.Logger.Info("Shard leader forwarding prepare to followers",
			"requestID", prepareMsg.RequestID,
			"leaderID", hc.config.NodeID,
			"shardID", hc.config.ShardID)
		hc.forwardPrepareToFollowers(prepareMsg)
		// Wait for prepare ACKs from followers - checkFollowerPrepareMajority will be called in handleShardAck
	case RoleShardFollower:
		// Followers send prepare ACK back to their shard leader
		hc.config.Logger.Info("Follower sending prepare ACK to shard leader",
			"requestID", prepareMsg.RequestID,
			"followerID", hc.config.NodeID,
			"shardID", hc.config.ShardID)
		hc.sendPrepareAckToShardLeader(prepareMsg.RequestID)
	case RolePrimaryLeader:
		// Primary leader should not process its own prepare messages
		// It waits for prepare ACKs from shard leaders instead
		hc.config.Logger.Debug("Primary leader ignoring own prepare message", "requestID", prepareMsg.RequestID)
	}
}

// forwardPrepareToFollowers forwards Prepare to shard followers
func (hc *Consensus) forwardPrepareToFollowers(prepareMsg *PreparePhaseMessage) {
	shardNodes := hc.config.ShardNodes[hc.config.ShardID]
	followers := make([]NodeID, 0)

	for _, nodeID := range shardNodes {
		if nodeID != hc.config.NodeID {
			followers = append(followers, nodeID)
		}
	}

	if len(followers) > 0 {
		msg := Message{
			Type:      MsgPreparePhase,
			From:      hc.config.NodeID,
			ShardID:   hc.config.ShardID,
			Timestamp: time.Now(),
			Payload:   prepareMsg,
		}

		hc.config.Network.Broadcast(followers, msg)
		hc.config.Logger.Info("Forwarded Prepare to followers",
			"requestID", prepareMsg.RequestID,
			"followers", len(followers))
	}
}

// startCommitPhaseWithShardLeaders starts Commit phase with shard leaders
func (hc *Consensus) startCommitPhaseWithShardLeaders(requestID string) {
	// Check if request is already finalized
	if hc.finalizedRequests[requestID] {
		hc.config.Logger.Debug("Request already finalized, skipping Commit phase", "requestID", requestID)
		return
	}

	// Check if commit phase already started for this request
	if _, exists := hc.commitMessages[requestID]; exists {
		hc.config.Logger.Debug("Commit phase already started, skipping duplicate", "requestID", requestID)
		return
	}

	hc.requestPool.UpdateRequestPhase(requestID, PhaseCommit)

	// Create commit message
	commitMsg := &CommitRequestMessage{
		RequestID: requestID,
		View:      hc.currentView,
		Sequence:  uint64(time.Now().UnixNano()),
		NodeID:    hc.config.NodeID,
		ShardID:   hc.config.ShardID,
	}

	if hc.config.Role == RolePrimaryLeader {
		// Primary leader sends to shard leaders
		shardLeaders := make([]NodeID, 0, len(hc.config.ShardLeaders))
		for _, leaderID := range hc.config.ShardLeaders {
			if leaderID != hc.config.NodeID {
				shardLeaders = append(shardLeaders, leaderID)
			}
		}

		msg := Message{
			Type:      MsgCommitRequest,
			From:      hc.config.NodeID,
			Timestamp: time.Now(),
			Payload:   commitMsg,
		}

		hc.config.Network.Broadcast(shardLeaders, msg)
		hc.config.Logger.Info("Started Commit phase with shard leaders",
			"requestID", requestID,
			"shardLeaders", len(shardLeaders))

		// Primary Leader also creates block when starting commit phase
		hc.config.Logger.Info("Primary Leader about to finalize request", "requestID", requestID)
		hc.finalizeRequestAndCreateBlock(requestID)
	}
}

// handleCommitRequest processes commit request messages (updated for new flow)
func (hc *Consensus) handleCommitRequest(commitMsg *CommitRequestMessage) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.config.Logger.Info("Handling commit request", "requestID", commitMsg.RequestID, "from", commitMsg.NodeID, "role", hc.config.Role.String())

	// Check if request has already been finalized
	if hc.finalizedRequests[commitMsg.RequestID] {
		hc.config.Logger.Debug("Request already finalized, skipping", "requestID", commitMsg.RequestID)
		return
	}

	// Store commit message
	if _, exists := hc.commitMessages[commitMsg.RequestID]; !exists {
		hc.commitMessages[commitMsg.RequestID] = make(map[NodeID]*CommitRequestMessage)
	}
	hc.commitMessages[commitMsg.RequestID][commitMsg.NodeID] = commitMsg

	// All nodes decide and create block immediately upon receiving commit
	hc.config.Logger.Info("Node received commit - deciding and creating block",
		"requestID", commitMsg.RequestID,
		"nodeID", hc.config.NodeID,
		"role", hc.config.Role.String())

	hc.finalizeRequestAndCreateBlock(commitMsg.RequestID)

	switch hc.config.Role {
	case RoleShardLeader:
		// Shard leaders forward Commit to followers
		hc.forwardCommitToFollowers(commitMsg)
		// Send commit ACK back to primary after deciding
		hc.sendCommitAckToPrimary(commitMsg.RequestID)
	case RoleShardFollower:
		// Followers send commit ACK back to their shard leader after deciding
		hc.sendCommitAckToShardLeader(commitMsg.RequestID)
	case RolePrimaryLeader:
		// Primary leader has already decided, now wait for ACKs to determine next sequence
		// This will be used for sequence number management
		hc.config.Logger.Info("Primary leader decided, waiting for commit ACKs for sequencing",
			"requestID", commitMsg.RequestID)
	}
}

// forwardCommitToFollowers forwards Commit to shard followers
func (hc *Consensus) forwardCommitToFollowers(commitMsg *CommitRequestMessage) {
	shardNodes := hc.config.ShardNodes[hc.config.ShardID]
	followers := make([]NodeID, 0)

	for _, nodeID := range shardNodes {
		if nodeID != hc.config.NodeID {
			followers = append(followers, nodeID)
		}
	}

	if len(followers) > 0 {
		msg := Message{
			Type:      MsgCommitRequest,
			From:      hc.config.NodeID,
			ShardID:   hc.config.ShardID,
			Timestamp: time.Now(),
			Payload:   commitMsg,
		}

		hc.config.Network.Broadcast(followers, msg)
		hc.config.Logger.Info("Forwarded Commit to followers",
			"requestID", commitMsg.RequestID,
			"followers", len(followers))
	}
}

// finalizeRequestAndCreateBlock finalizes request and creates block in ALL nodes
func (hc *Consensus) finalizeRequestAndCreateBlock(requestID string) {
	hc.config.Logger.Info("Starting finalization process", "requestID", requestID, "nodeID", hc.config.NodeID, "role", hc.config.Role.String())

	// Check if already finalized
	if hc.finalizedRequests[requestID] {
		hc.config.Logger.Debug("Request already finalized, skipping duplicate", "requestID", requestID)
		return
	}

	request, exists := hc.requestPool.GetRequest(requestID)
	if !exists {
		hc.config.Logger.Error("Request not found for finalization", "requestID", requestID, "poolSize", hc.requestPool.Size())
		return
	}

	hc.config.Logger.Info("Found request for finalization", "requestID", requestID, "phase", request.Phase.String())

	// Mark as finalized to prevent duplicates
	hc.finalizedRequests[requestID] = true

	// Record metrics for decision reached and commit latency
	hc.metrics.RecordDecisionReached()
	if !request.Timestamp.IsZero() {
		hc.metrics.RecordCommitLatency(time.Since(request.Timestamp))
		hc.metrics.RecordShardConsensusTime(request.ShardID, time.Since(request.Timestamp))
	}

	// Convert request to proposal and directly create block
	proposal := &Proposal{
		ID:        requestID,
		Data:      request.Data,
		Timestamp: time.Now(),
		ShardID:   request.ShardID,
		Proposer:  hc.config.NodeID,
	}

	// Directly create and store block in ALL nodes
	if hc.config.Storage != nil {
		hc.storeProposalAsBlock(proposal, "")
		hc.config.Logger.Info("Block created and stored for finalized request",
			"requestID", requestID,
			"nodeID", hc.config.NodeID,
			"role", hc.config.Role.String())
	} else {
		hc.config.Logger.Error("No storage configured, block not stored", "requestID", requestID)
	}

	// Clean up request from pool
	hc.requestPool.RemoveRequest(requestID)

	// Clean up message tracking
	delete(hc.prePrepMessages, requestID)
	// delete(hc.prepMessages, requestID)
	delete(hc.prepareMessages, requestID)
	delete(hc.commitMessages, requestID)

	hc.config.Logger.Info("Request finalized and block created", "requestID", requestID, "nodeID", hc.config.NodeID)
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
	baseMetrics["finalized_requests"] = len(hc.finalizedRequests)

	return baseMetrics
}
