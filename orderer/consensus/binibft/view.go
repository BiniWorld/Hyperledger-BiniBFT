/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// View represents a consensus view with its state and message tracking
type View struct {
	Number          uint64
	Primary         NodeID
	ShardID         ShardID
	Sequence        uint64
	DecisionsInView uint64

	// Phase tracking
	mu    sync.RWMutex
	phase ViewPhase

	// Message storage for current view - sequence-based
	prePrepMessages map[uint64]map[NodeID]*PrePrepMessage
	prepareMessages map[uint64]map[NodeID]*PreparePhaseMessage
	commitMessages  map[uint64]map[NodeID]*CommitRequestMessage

	// Shard acknowledgments for cross-shard coordination
	shardAcks map[uint64]map[NodeID]*ShardAckMessage

	// In-flight requests tracking
	inFlightRequests map[uint64]*RequestInfo

	// Finalization tracking
	finalizedSequences map[uint64]bool

	// Intra-shard vote tracking
	intraShardVotes map[uint64]map[string]map[NodeID]bool

	// Configuration
	config *ViewConfig
}

// ViewPhase represents the current phase of consensus in a view
type ViewPhase int

const (
	ViewPhaseIdle ViewPhase = iota
	ViewPhasePrePrepare
	ViewPhasePrepare
	ViewPhaseCommit
	ViewPhaseDecided
)

func (p ViewPhase) String() string {
	switch p {
	case ViewPhaseIdle:
		return "Idle"
	case ViewPhasePrePrepare:
		return "PrePrepare"
	case ViewPhasePrepare:
		return "Prepare"
	case ViewPhaseCommit:
		return "Commit"
	case ViewPhaseDecided:
		return "Decided"
	default:
		return "Unknown"
	}
}

// ViewConfig holds configuration for a view
type ViewConfig struct {
	NodeID                 NodeID
	Role                   NodeRole
	ShardID                ShardID
	PrimaryLeader          NodeID
	ShardLeaders           map[ShardID]NodeID
	ShardNodes             map[ShardID][]NodeID
	ShardMajorityThreshold float64
	Network                NetworkInterface
	Logger                 Logger
	Application            ApplicationDelivery
	Signer                 BiniBFTSigner
}

// NewView creates a new consensus view
func NewView(primary NodeID, shardID ShardID, config *ViewConfig) *View {
	return &View{
		Number:             0,
		Primary:            primary,
		ShardID:            shardID,
		Sequence:           1,
		phase:              ViewPhaseIdle,
		prePrepMessages:    make(map[uint64]map[NodeID]*PrePrepMessage),
		prepareMessages:    make(map[uint64]map[NodeID]*PreparePhaseMessage),
		commitMessages:     make(map[uint64]map[NodeID]*CommitRequestMessage),
		shardAcks:          make(map[uint64]map[NodeID]*ShardAckMessage),
		inFlightRequests:   make(map[uint64]*RequestInfo),
		finalizedSequences: make(map[uint64]bool),
		intraShardVotes:    make(map[uint64]map[string]map[NodeID]bool),
		config:             config,
	}
}

// Propose initiates consensus for a proposal (Primary Leader only)
func (v *View) Propose(proposal BiniBFTProposal) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Check if sequence is already finalized
	if v.finalizedSequences[v.Sequence] {
		v.config.Logger.Debug("Sequence already finalized, skipping", "sequence", v.Sequence)
		return nil
	}

	// Check if there's already a proposal in progress for this sequence
	if _, exists := v.prePrepMessages[v.Sequence]; exists {
		v.config.Logger.Debug("Proposal already in progress for sequence, skipping", "sequence", v.Sequence)
		return nil
	}

	// Only primary leader can start pre-prepare
	if v.config.Role != RolePrimaryLeader {
		return fmt.Errorf("only primary leader can start pre-prepare")
	}

	v.config.Logger.Info("Primary leader starting consensus for proposal",
		"view", v.Number,
		"sequence", v.Sequence,
		"nodeID", v.config.NodeID)

	// Store in-flight request info
	v.inFlightRequests[v.Sequence] = &RequestInfo{
		ClientID: "batch",
		ID:       fmt.Sprintf("seq-%d", v.Sequence),
	}

	prePrepMsg := &PrePrepMessage{
		Proposal:  proposal,
		View:      v.Number,
		Sequence:  v.Sequence,
		Digest:    proposal.Digest(),
		NodeID:    v.config.NodeID,
		ShardID:   v.ShardID,
		Signature: v.signMessage(proposal.Payload),
	}

	// Store pre-prepare
	if _, exists := v.prePrepMessages[v.Sequence]; !exists {
		v.prePrepMessages[v.Sequence] = make(map[NodeID]*PrePrepMessage)
	}
	v.prePrepMessages[v.Sequence][v.config.NodeID] = prePrepMsg
	v.phase = ViewPhasePrePrepare

	// Send pre-prepare to all shard leaders
	otherShardLeaderCount := v.sendPrePrepToShardLeaders(prePrepMsg)

	// Only proceed immediately if there are no other shard leaders
	if otherShardLeaderCount == 0 {
		v.config.Logger.Info("No other shard leaders - proceeding to prepare phase", "sequence", v.Sequence)
		v.startPreparePhaseWithShardLeaders(v.Sequence)
	} else {
		v.config.Logger.Info("Waiting for shard leader responses", "sequence", v.Sequence, "otherShardLeaders", otherShardLeaderCount)
	}

	return nil
}

// HandlePrePrepare processes a pre-prepare message
func (v *View) HandlePrePrepare(msg *PrePrepMessage) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Check if sequence is already finalized
	if v.finalizedSequences[msg.Sequence] {
		v.config.Logger.Debug("Sequence already finalized, skipping PrePrep phase handling", "sequence", msg.Sequence)
		return nil
	}

	v.config.Logger.Info("Handling pre-prep message",
		"sequence", msg.Sequence,
		"view", msg.View,
		"from", msg.NodeID,
		"role", v.config.Role.String())

	// Store pre-prep message
	if _, exists := v.prePrepMessages[msg.Sequence]; !exists {
		v.prePrepMessages[msg.Sequence] = make(map[NodeID]*PrePrepMessage)
	}
	v.prePrepMessages[msg.Sequence][msg.NodeID] = msg

	// Store in-flight request info
	v.inFlightRequests[msg.Sequence] = &RequestInfo{
		ClientID: "batch",
		ID:       fmt.Sprintf("seq-%d", msg.Sequence),
	}

	switch v.config.Role {
	case RoleShardLeader:
		// Shard leaders forward Pre-Prep to their followers and wait for ACKs
		v.forwardPrePrepToFollowers(msg)
		v.checkFollowerMajority(msg.Sequence)
	case RoleShardFollower:
		// Followers send ACK back to their shard leader
		v.sendAckToShardLeader(msg)
	}

	return nil
}

// HandlePreparePhase processes prepare phase messages
func (v *View) HandlePreparePhase(msg *PreparePhaseMessage) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Check if sequence is already finalized
	if v.finalizedSequences[msg.Sequence] {
		v.config.Logger.Debug("Sequence already finalized, skipping Prepare phase handling", "sequence", msg.Sequence)
		return nil
	}

	v.config.Logger.Info("Handling prepare phase message",
		"sequence", msg.Sequence,
		"from", msg.NodeID,
		"role", v.config.Role.String())

	// Store prepare message
	if _, exists := v.prepareMessages[msg.Sequence]; !exists {
		v.prepareMessages[msg.Sequence] = make(map[NodeID]*PreparePhaseMessage)
	}
	v.prepareMessages[msg.Sequence][msg.NodeID] = msg

	// Check if we have enough prepare messages to proceed to commit
	if v.checkPrepareQuorum(msg.Sequence) {
		v.config.Logger.Info("Prepare phase quorum reached", "sequence", msg.Sequence)
		v.startCommitPhaseWithShardLeaders(msg.Sequence)
	}

	return nil
}

// HandleCommitRequest processes commit request messages
func (v *View) HandleCommitRequest(msg *CommitRequestMessage) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Check if sequence is already finalized
	if v.finalizedSequences[msg.Sequence] {
		v.config.Logger.Debug("Sequence already finalized, skipping Commit phase handling", "sequence", msg.Sequence)
		return nil
	}

	v.config.Logger.Info("Handling commit request message",
		"sequence", msg.Sequence,
		"from", msg.NodeID,
		"role", v.config.Role.String())

	// Store commit message
	if _, exists := v.commitMessages[msg.Sequence]; !exists {
		v.commitMessages[msg.Sequence] = make(map[NodeID]*CommitRequestMessage)
	}
	v.commitMessages[msg.Sequence][msg.NodeID] = msg

	// Check if we have enough commit messages to finalize
	if v.checkCommitQuorum(msg.Sequence) {
		v.config.Logger.Info("Commit phase quorum reached", "sequence", msg.Sequence)
		v.finalizeProposalAndCreateBlock(msg.Sequence, msg.Proposal)
	}

	return nil
}

// HandleShardAck processes shard acknowledgment messages
func (v *View) HandleShardAck(ack *ShardAckMessage) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.config.Logger.Info("Handling shard ACK",
		"sequence", ack.Sequence,
		"from", ack.NodeID,
		"phase", ack.Phase,
		"shardID", ack.ShardID)

	// Store shard ACK
	if _, exists := v.shardAcks[ack.Sequence]; !exists {
		v.shardAcks[ack.Sequence] = make(map[NodeID]*ShardAckMessage)
	}
	v.shardAcks[ack.Sequence][ack.NodeID] = ack

	// Handle different phases
	switch ack.Phase {
	case "preprep":
		if v.checkPrePrepAckQuorum(ack.Sequence) {
			v.config.Logger.Info("Pre-prep ACK quorum reached", "sequence", ack.Sequence)
			v.startPreparePhaseWithShardLeaders(ack.Sequence)
		}
	case "prepare":
		if v.checkPrepareAckQuorum(ack.Sequence) {
			v.config.Logger.Info("Prepare ACK quorum reached", "sequence", ack.Sequence)
			v.startCommitPhaseWithShardLeaders(ack.Sequence)
		}
	case "commit":
		if v.checkCommitAckQuorum(ack.Sequence) {
			v.config.Logger.Info("Commit ACK quorum reached", "sequence", ack.Sequence)
			// Finalization is handled in commit phase
		}
	}

	return nil
}

// HandleIntraShardVote processes intra-shard vote requests
func (v *View) HandleIntraShardVote(voteMsg *IntraShardVoteMessage) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.config.Logger.Info("Handling intra-shard vote request",
		"sequence", voteMsg.Sequence,
		"phase", voteMsg.Phase,
		"from", voteMsg.NodeID)

	// For now, always vote yes
	vote := true

	// Send vote response back
	response := &IntraShardVoteResponse{
		Sequence:  voteMsg.Sequence,
		Phase:     voteMsg.Phase,
		ShardID:   v.config.ShardID,
		NodeID:    v.config.NodeID,
		Vote:      vote,
		Signature: v.signMessage([]byte(fmt.Sprintf("%d-%s-%t", voteMsg.Sequence, voteMsg.Phase, vote))),
		Timestamp: time.Now(),
	}

	msg := BiniBFTMessage{
		Type:      MsgIntraShardVoteResponse,
		From:      v.config.NodeID,
		To:        voteMsg.NodeID,
		ShardID:   v.config.ShardID,
		Timestamp: time.Now(),
	}

	payload, _ := json.Marshal(response)
	msg.Payload = payload

	v.config.Network.Send(voteMsg.NodeID, msg)

	return nil
}

// HandleIntraShardVoteResponse processes intra-shard vote responses
func (v *View) HandleIntraShardVoteResponse(response *IntraShardVoteResponse) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.config.Logger.Info("Handling intra-shard vote response",
		"sequence", response.Sequence,
		"phase", response.Phase,
		"from", response.NodeID,
		"vote", response.Vote)

	// Record the vote
	v.recordIntraShardVote(response.Sequence, response.Phase, response.NodeID, response.Vote)

	// Check if we have majority
	v.checkIntraShardMajority(response.Sequence, response.Phase)

	return nil
}

// Helper methods

func (v *View) sendPrePrepToShardLeaders(prePrepMsg *PrePrepMessage) int {
	shardLeaders := make([]NodeID, 0, len(v.config.ShardLeaders))
	for _, leaderID := range v.config.ShardLeaders {
		if leaderID != v.config.NodeID {
			shardLeaders = append(shardLeaders, leaderID)
		}
	}

	msg := BiniBFTMessage{
		Type:      MsgPrePrep,
		From:      v.config.NodeID,
		ShardID:   v.ShardID,
		Timestamp: time.Now(),
	}

	payload, _ := json.Marshal(prePrepMsg)
	msg.Payload = payload

	v.config.Network.Broadcast(shardLeaders, msg)

	return len(shardLeaders)
}

func (v *View) forwardPrePrepToFollowers(prePrepMsg *PrePrepMessage) {
	shardNodes := v.config.ShardNodes[v.config.ShardID]
	for _, nodeID := range shardNodes {
		if nodeID != v.config.NodeID {
			msg := BiniBFTMessage{
				Type:      MsgPrePrep,
				From:      v.config.NodeID,
				To:        nodeID,
				ShardID:   v.config.ShardID,
				Timestamp: time.Now(),
			}
			payload, _ := json.Marshal(prePrepMsg)
			msg.Payload = payload
			v.config.Network.Send(nodeID, msg)
		}
	}
}

func (v *View) checkFollowerMajority(sequence uint64) {
	shardNodes := v.config.ShardNodes[v.config.ShardID]
	requiredCount := int(float64(len(shardNodes)) * v.config.ShardMajorityThreshold)
	if requiredCount < 1 {
		requiredCount = 1
	}

	ackCount := 1 // Count self as ACK
	if acks, exists := v.shardAcks[sequence]; exists {
		for _, ack := range acks {
			if ack.ShardID == v.config.ShardID && ack.Acknowledged && ack.Phase == "preprep" {
				ackCount++
			}
		}
	}

	if ackCount >= requiredCount {
		v.config.Logger.Info("Follower majority reached - sending pre-prep ACK to primary", "sequence", sequence)
		v.sendPrePrepAckToPrimary(sequence)
	}
}

func (v *View) sendPrePrepAckToPrimary(sequence uint64) {
	ack := &ShardAckMessage{
		Sequence:     sequence,
		ShardID:      v.config.ShardID,
		NodeID:       v.config.NodeID,
		Acknowledged: true,
		Phase:        "preprep",
		Timestamp:    time.Now(),
	}

	msg := BiniBFTMessage{
		Type:      MsgShardAck,
		From:      v.config.NodeID,
		To:        v.config.PrimaryLeader,
		ShardID:   v.config.ShardID,
		Timestamp: time.Now(),
	}

	payload, _ := json.Marshal(ack)
	msg.Payload = payload

	v.config.Network.Send(v.config.PrimaryLeader, msg)
}

func (v *View) sendAckToShardLeader(prePrepMsg *PrePrepMessage) {
	v.broadcastToShardNodes(prePrepMsg, "preprep")
}

func (v *View) broadcastToShardNodes(payload interface{}, phase string) {
	shardNodes := v.config.ShardNodes[v.config.ShardID]

	var sequence uint64
	switch msg := payload.(type) {
	case *PrePrepMessage:
		sequence = msg.Sequence
	case *PreparePhaseMessage:
		sequence = msg.Sequence
	case *CommitRequestMessage:
		sequence = msg.Sequence
	}

	voteMsg := &IntraShardVoteMessage{
		Sequence:  sequence,
		Phase:     phase,
		ShardID:   v.config.ShardID,
		NodeID:    v.config.NodeID,
		Timestamp: time.Now(),
	}

	for _, nodeID := range shardNodes {
		if nodeID != v.config.NodeID {
			msg := BiniBFTMessage{
				Type:      MsgIntraShardVote,
				From:      v.config.NodeID,
				To:        nodeID,
				ShardID:   v.config.ShardID,
				Timestamp: time.Now(),
			}
			msgPayload, _ := json.Marshal(voteMsg)
			msg.Payload = msgPayload
			v.config.Network.Send(nodeID, msg)
		}
	}

	v.recordIntraShardVote(sequence, phase, v.config.NodeID, true)
	v.checkIntraShardMajority(sequence, phase)
}

func (v *View) recordIntraShardVote(sequence uint64, phase string, nodeID NodeID, vote bool) {
	if _, exists := v.intraShardVotes[sequence]; !exists {
		v.intraShardVotes[sequence] = make(map[string]map[NodeID]bool)
	}
	if _, exists := v.intraShardVotes[sequence][phase]; !exists {
		v.intraShardVotes[sequence][phase] = make(map[NodeID]bool)
	}
	v.intraShardVotes[sequence][phase][nodeID] = vote
}

func (v *View) checkIntraShardMajority(sequence uint64, phase string) {
	shardNodes := v.config.ShardNodes[v.config.ShardID]
	requiredCount := (len(shardNodes) / 2) + 1

	votes, exists := v.intraShardVotes[sequence][phase]
	if !exists {
		return
	}

	approveCount := 0
	for _, vote := range votes {
		if vote {
			approveCount++
		}
	}

	if approveCount >= requiredCount {
		v.config.Logger.Info("Intra-shard majority reached", "sequence", sequence, "phase", phase)
		v.sendAckToShardLeaderAfterConsensus(sequence, phase)
	}
}

func (v *View) sendAckToShardLeaderAfterConsensus(sequence uint64, phase string) {
	shardLeader := v.config.ShardLeaders[v.config.ShardID]

	ack := &ShardAckMessage{
		Sequence:     sequence,
		ShardID:      v.config.ShardID,
		NodeID:       v.config.NodeID,
		Acknowledged: true,
		Phase:        phase,
		Timestamp:    time.Now(),
	}

	msg := BiniBFTMessage{
		Type:      MsgShardAck,
		From:      v.config.NodeID,
		To:        shardLeader,
		ShardID:   v.config.ShardID,
		Timestamp: time.Now(),
	}

	payload, _ := json.Marshal(ack)
	msg.Payload = payload

	v.config.Network.Send(shardLeader, msg)
}

func (v *View) startPreparePhaseWithShardLeaders(sequence uint64) {
	var proposal BiniBFTProposal
	if prePrepMsgs, exists := v.prePrepMessages[sequence]; exists {
		for _, msg := range prePrepMsgs {
			proposal = msg.Proposal
			break
		}
	}

	prepareMsg := &PreparePhaseMessage{
		Proposal:  proposal,
		View:      v.Number,
		Sequence:  sequence,
		Digest:    proposal.Digest(),
		NodeID:    v.config.NodeID,
		ShardID:   v.config.ShardID,
		Signature: v.signMessage(proposal.Payload),
	}

	for shardID, shardLeader := range v.config.ShardLeaders {
		if shardLeader != v.config.NodeID {
			msg := BiniBFTMessage{
				Type:      MsgPreparePhase,
				From:      v.config.NodeID,
				To:        shardLeader,
				ShardID:   shardID,
				Timestamp: time.Now(),
			}
			payload, _ := json.Marshal(prepareMsg)
			msg.Payload = payload
			v.config.Network.Send(shardLeader, msg)
		}
	}
}

func (v *View) startCommitPhaseWithShardLeaders(sequence uint64) {
	var proposal BiniBFTProposal
	if prePrepMsgs, exists := v.prePrepMessages[sequence]; exists {
		for _, msg := range prePrepMsgs {
			proposal = msg.Proposal
			break
		}
	}

	commitMsg := &CommitRequestMessage{
		Proposal:  proposal,
		View:      v.Number,
		Sequence:  sequence,
		Digest:    proposal.Digest(),
		NodeID:    v.config.NodeID,
		ShardID:   v.config.ShardID,
		Signature: v.signMessage(proposal.Payload),
	}

	for shardID, shardLeader := range v.config.ShardLeaders {
		if shardLeader != v.config.NodeID {
			msg := BiniBFTMessage{
				Type:      MsgCommitRequest,
				From:      v.config.NodeID,
				To:        shardLeader,
				ShardID:   shardID,
				Timestamp: time.Now(),
			}
			payload, _ := json.Marshal(commitMsg)
			msg.Payload = payload
			v.config.Network.Send(shardLeader, msg)
		}
	}
}

func (v *View) checkPrepareQuorum(sequence uint64) bool {
	requiredCount := (len(v.config.ShardLeaders) / 2) + 1
	if prepMsgs, exists := v.prepareMessages[sequence]; exists {
		return len(prepMsgs) >= requiredCount
	}
	return false
}

func (v *View) checkCommitQuorum(sequence uint64) bool {
	requiredCount := (len(v.config.ShardLeaders) / 2) + 1
	if commitMsgs, exists := v.commitMessages[sequence]; exists {
		return len(commitMsgs) >= requiredCount
	}
	return false
}

func (v *View) checkPrePrepAckQuorum(sequence uint64) bool {
	requiredCount := (len(v.config.ShardLeaders) / 2) + 1
	ackCount := 0
	if acks, exists := v.shardAcks[sequence]; exists {
		for _, ack := range acks {
			if ack.Phase == "preprep" && ack.Acknowledged {
				ackCount++
			}
		}
	}
	return ackCount >= requiredCount
}

func (v *View) checkPrepareAckQuorum(sequence uint64) bool {
	requiredCount := (len(v.config.ShardLeaders) / 2) + 1
	ackCount := 0
	if acks, exists := v.shardAcks[sequence]; exists {
		for _, ack := range acks {
			if ack.Phase == "prepare" && ack.Acknowledged {
				ackCount++
			}
		}
	}
	return ackCount >= requiredCount
}

func (v *View) checkCommitAckQuorum(sequence uint64) bool {
	requiredCount := (len(v.config.ShardLeaders) / 2) + 1
	ackCount := 0
	if acks, exists := v.shardAcks[sequence]; exists {
		for _, ack := range acks {
			if ack.Phase == "commit" && ack.Acknowledged {
				ackCount++
			}
		}
	}
	return ackCount >= requiredCount
}

func (v *View) finalizeProposalAndCreateBlock(sequence uint64, proposal BiniBFTProposal) {
	v.config.Logger.Info("Finalizing proposal and creating block", "sequence", sequence)

	// Mark sequence as finalized
	v.finalizedSequences[sequence] = true

	// Create signatures (simplified)
	signatures := []BiniBFTSignature{
		{
			ID:    uint64(1), // Simplified
			Value: v.signMessage(proposal.Payload),
			Msg:   proposal.Payload,
		},
	}

	// Deliver to application
	if v.config.Application != nil {
		v.config.Application.Deliver(proposal, signatures)
	}

	// Advance sequence
	v.Sequence++
}

func (v *View) signMessage(data []byte) []byte {
	if v.config.Signer != nil {
		return v.config.Signer.Sign(data)
	}
	return []byte(fmt.Sprintf("sig-%s", string(data)))
}

// GetViewNumber returns the view number
func (v *View) GetViewNumber() uint64 {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.Number
}

// IsProposalInProgress checks if there's already a proposal in progress
func (v *View) IsProposalInProgress() bool {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.finalizedSequences[v.Sequence] {
		return false
	}

	if prePrepMsgs, exists := v.prePrepMessages[v.Sequence]; exists && len(prePrepMsgs) > 0 {
		return true
	}

	return false
}

// Reset resets the view state
func (v *View) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.phase = ViewPhaseIdle
	v.prePrepMessages = make(map[uint64]map[NodeID]*PrePrepMessage)
	v.prepareMessages = make(map[uint64]map[NodeID]*PreparePhaseMessage)
	v.commitMessages = make(map[uint64]map[NodeID]*CommitRequestMessage)
	v.shardAcks = make(map[uint64]map[NodeID]*ShardAckMessage)
	v.inFlightRequests = make(map[uint64]*RequestInfo)
	v.finalizedSequences = make(map[uint64]bool)
	v.intraShardVotes = make(map[uint64]map[string]map[NodeID]bool)
}
