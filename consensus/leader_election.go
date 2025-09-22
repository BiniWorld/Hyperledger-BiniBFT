package consensus

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"
)

// ElectionState represents the current state of leader election
type ElectionState int

const (
	StateIdle ElectionState = iota
	StateElecting
	StateElected
	StateReElecting
)

// LeaderElection manages the leader election process
type LeaderElection struct {
	config *Config

	// Election state
	mu                sync.RWMutex
	state             ElectionState
	electionID        string
	activeNodes       []NodeID
	primaryLeader     NodeID
	shardLeaders      map[ShardID]NodeID
	shardAssignments  map[ShardID][]NodeID
	numShards         int
	electionTimeout   time.Duration
	heartbeatTimeout  time.Duration

	// Tracking acknowledgments and responses
	acksReceived      map[NodeID]bool
	electionStartTime time.Time
	lastHeartbeat     map[NodeID]time.Time

	// Callbacks
	onLeaderElected   func(primary NodeID, shards map[ShardID]NodeID)
	onShardAssigned   func(shardID ShardID, leader NodeID, nodes []NodeID)

	// Channels for internal communication
	stopCh            chan struct{}
	electionTimer     *time.Timer
}

// NewLeaderElection creates a new leader election instance
func NewLeaderElection(config *Config, numShards int) *LeaderElection {
	le := &LeaderElection{
		config:            config,
		state:             StateIdle,
		activeNodes:       make([]NodeID, 0),
		shardLeaders:      make(map[ShardID]NodeID),
		shardAssignments:  make(map[ShardID][]NodeID),
		numShards:         numShards,
		electionTimeout:   30 * time.Second,
		heartbeatTimeout:  10 * time.Second,
		acksReceived:      make(map[NodeID]bool),
		lastHeartbeat:     make(map[NodeID]time.Time),
		stopCh:            make(chan struct{}),
	}

	// Initialize with current node as active
	le.activeNodes = append(le.activeNodes, config.NodeID)
	le.lastHeartbeat[config.NodeID] = time.Now()

	return le
}

// Start begins the leader election process
func (le *LeaderElection) Start() error {
	le.mu.Lock()
	defer le.mu.Unlock()

	if le.state != StateIdle {
		return fmt.Errorf("leader election already started")
	}

	le.config.Logger.Info("Starting leader election",
		"nodeID", le.config.NodeID,
		"numShards", le.numShards)

	// Start heartbeat monitoring
	go le.monitorHeartbeats()

	// Start initial election
	le.startElection()

	return nil
}

// Stop stops the leader election process
func (le *LeaderElection) Stop() {
	le.mu.Lock()
	defer le.mu.Unlock()

	le.state = StateIdle
	close(le.stopCh)

	if le.electionTimer != nil {
		le.electionTimer.Stop()
	}

	le.config.Logger.Info("Leader election stopped", "nodeID", le.config.NodeID)
}

// StartElection initiates a new leader election
func (le *LeaderElection) StartElection() {
	le.mu.Lock()
	defer le.mu.Unlock()

	le.startElection()
}

func (le *LeaderElection) startElection() {
	le.state = StateElecting
	le.electionID = le.generateElectionID()
	le.electionStartTime = time.Now()
	le.acksReceived = make(map[NodeID]bool)

	le.config.Logger.Info("Starting new election",
		"electionID", le.electionID,
		"activeNodes", len(le.activeNodes))

	// Send election message to all known nodes
	lelectionMsg := &LeaderElectionMessage{
		ElectionID:  le.electionID,
		CandidateID: le.config.NodeID,
		ActiveNodes: le.activeNodes,
		Timestamp:   time.Now(),
	}

	if le.config.Signer != nil {
		lelectionMsg.Signature = le.config.Signer.Sign([]byte(le.electionID))
	}

	msg := Message{
		Type:      MsgLeaderElection,
		From:      le.config.NodeID,
		Timestamp: time.Now(),
		Payload:   lelectionMsg,
	}

	// Broadcast to all nodes (assuming network has all nodes)
	for _, nodeID := range le.activeNodes {
		if nodeID != le.config.NodeID {
			msg.To = nodeID
			le.config.Network.Send(nodeID, msg)
		}
	}

	// Set election timeout
	le.electionTimer = time.AfterFunc(le.electionTimeout, le.handleElectionTimeout)
}

// HandleMessage processes incoming leader election messages
func (le *LeaderElection) HandleMessage(from NodeID, message Message) error {
	switch message.Type {
	case MsgLeaderElection:
		return le.handleLeaderElection(from, message.Payload.(*LeaderElectionMessage))
	case MsgElectionAck:
		return le.handleElectionAck(from, message.Payload.(*ElectionAckMessage))
	case MsgShardAssignment:
		return le.handleShardAssignment(from, message.Payload.(*ShardAssignmentMessage))
	case MsgLeaderAnnouncement:
		return le.handleLeaderAnnouncement(from, message.Payload.(*LeaderAnnouncementMessage))
	default:
		return fmt.Errorf("unknown message type for leader election: %v", message.Type)
	}
}

func (le *LeaderElection) handleLeaderElection(from NodeID, msg *LeaderElectionMessage) error {
	le.mu.Lock()
	defer le.mu.Unlock()

	le.config.Logger.Info("Received leader election message",
		"from", from,
		"electionID", msg.ElectionID,
		"candidate", msg.CandidateID)

	// Update active nodes list
	le.updateActiveNodes(msg.ActiveNodes)

	// Send acknowledgment
	ackMsg := &ElectionAckMessage{
		ElectionID:   msg.ElectionID,
		NodeID:       le.config.NodeID,
		Acknowledged: true,
		Timestamp:    time.Now(),
	}

	if le.config.Signer != nil {
		ackMsg.Signature = le.config.Signer.Sign([]byte(msg.ElectionID + string(le.config.NodeID)))
	}

	response := Message{
		Type:      MsgElectionAck,
		From:      le.config.NodeID,
		To:        from,
		Timestamp: time.Now(),
		Payload:   ackMsg,
	}

	le.config.Network.Send(from, response)

	// If this node has higher priority, start its own election
	if le.shouldStartElection(msg.CandidateID) {
		le.startElection()
	}

	return nil
}

func (le *LeaderElection) handleElectionAck(from NodeID, msg *ElectionAckMessage) error {
	le.mu.Lock()
	defer le.mu.Unlock()

	if msg.ElectionID != le.electionID {
		le.config.Logger.Debug("Ignoring stale election ack",
			"expected", le.electionID,
			"received", msg.ElectionID)
		return nil
	}

	le.acksReceived[from] = msg.Acknowledged
	le.config.Logger.Info("Received election ack",
		"from", from,
		"electionID", msg.ElectionID,
		"acksReceived", len(le.acksReceived))

	// Check if we have majority acknowledgment
	if le.hasMajorityAcks() && le.state == StateElecting {
		le.selectLeaders()
	}

	return nil
}

func (le *LeaderElection) handleShardAssignment(from NodeID, msg *ShardAssignmentMessage) error {
	le.mu.Lock()
	defer le.mu.Unlock()

	le.config.Logger.Info("Received shard assignment",
		"from", from,
		"electionID", msg.ElectionID,
		"primaryLeader", msg.PrimaryLeader)

	// Update local state
	le.primaryLeader = msg.PrimaryLeader
	le.shardLeaders = msg.ShardLeaders
	le.shardAssignments = msg.ShardNodes
	le.state = StateElected

	// Update config
	le.config.PrimaryLeader = msg.PrimaryLeader
	le.config.ShardLeaders = msg.ShardLeaders
	le.config.ShardNodes = msg.ShardNodes

	// Update nodes with new configuration
	if le.config.Node != nil {
		le.config.Node.UpdateNodeConfig(msg.PrimaryLeader, msg.ShardLeaders, msg.ShardNodes)
	}

	if le.onShardAssigned != nil {
		for shardID, nodes := range msg.ShardNodes {
			leader := msg.ShardLeaders[shardID]
			le.onShardAssigned(shardID, leader, nodes)
		}
	}

	// Restart heartbeat monitor with new configuration
	le.restartHeartbeatMonitor()

	return nil
}

func (le *LeaderElection) handleLeaderAnnouncement(from NodeID, msg *LeaderAnnouncementMessage) error {
	le.mu.Lock()
	defer le.mu.Unlock()

	le.config.Logger.Info("Received leader announcement",
		"from", from,
		"electionID", msg.ElectionID,
		"primaryLeader", msg.PrimaryLeader)

	// Update local state
	le.primaryLeader = msg.PrimaryLeader
	le.shardLeaders = msg.ShardLeaders
	le.state = StateElected

	// Update config
	le.config.PrimaryLeader = msg.PrimaryLeader
	le.config.ShardLeaders = msg.ShardLeaders

	// Update shard assignments if available
	if msg.ShardNodes != nil {
		le.shardAssignments = msg.ShardNodes
		le.config.ShardNodes = msg.ShardNodes
	}

	// Update nodes with new configuration
	if le.config.Node != nil {
		le.config.Node.UpdateNodeConfig(msg.PrimaryLeader, msg.ShardLeaders, msg.ShardNodes)
	}

	if le.onLeaderElected != nil {
		le.onLeaderElected(msg.PrimaryLeader, msg.ShardLeaders)
	}

	// Restart heartbeat monitor with new configuration
	le.restartHeartbeatMonitor()

	return nil
}

// selectLeaders performs leader selection using random number generation
func (le *LeaderElection) selectLeaders() {
	le.config.Logger.Info("Selecting leaders using random number algorithm",
		"electionID", le.electionID,
		"activeNodes", len(le.activeNodes))

	// Generate random numbers for each active node
	randomNumbers := le.generateRandomNumbers()

	// Sort nodes based on their random numbers
	sortedNodes := le.sortNodesByRandomNumber(randomNumbers)

	// Calculate leader index by summing all random numbers and taking mod
	leaderIndex := le.calculateLeaderIndex(randomNumbers)

	if leaderIndex >= len(sortedNodes) {
		leaderIndex = 0 // Fallback to first node
	}

	le.primaryLeader = sortedNodes[leaderIndex]

	le.config.Logger.Info("Selected primary leader",
		"primaryLeader", le.primaryLeader,
		"randomNumbers", randomNumbers,
		"leaderIndex", leaderIndex)

	// Select shard leaders from remaining nodes
	remainingNodes := make([]NodeID, 0, len(sortedNodes)-1)
	for i, node := range sortedNodes {
		if i != leaderIndex {
			remainingNodes = append(remainingNodes, node)
		}
	}

	le.selectShardLeaders(remainingNodes)

	// Announce assignments
	le.announceAssignments()
}

func (le *LeaderElection) selectShardLeaders(nodes []NodeID) {
	le.shardLeaders = make(map[ShardID]NodeID)
	le.shardAssignments = make(map[ShardID][]NodeID)

	if len(nodes) == 0 {
		le.config.Logger.Info("No nodes available for shard assignment")
		return
	}

	nodesPerShard := len(nodes) / le.numShards
	extraNodes := len(nodes) % le.numShards

	start := 0
	for i := 0; i < le.numShards; i++ {
		shardID := ShardID(i)
		end := start + nodesPerShard
		if i < extraNodes {
			end++
		}

		if start >= len(nodes) {
			break
		}

		shardNodes := nodes[start:end]
		if len(shardNodes) > 0 {
			// First node in shard becomes leader
			le.shardLeaders[shardID] = shardNodes[0]
			le.shardAssignments[shardID] = shardNodes

			le.config.Logger.Info("Assigned shard",
				"shardID", shardID,
				"leader", shardNodes[0],
				"nodes", len(shardNodes))
		}

		start = end
	}
}

func (le *LeaderElection) announceAssignments() {
	assignmentMsg := &ShardAssignmentMessage{
		ElectionID:    le.electionID,
		PrimaryLeader: le.primaryLeader,
		ShardLeaders:  le.shardLeaders,
		ShardNodes:    le.shardAssignments,
		Timestamp:     time.Now(),
	}

	if le.config.Signer != nil {
		data := fmt.Sprintf("%s%s", le.electionID, le.primaryLeader)
		assignmentMsg.Signature = le.config.Signer.Sign([]byte(data))
	}

	msg := Message{
		Type:      MsgShardAssignment,
		From:      le.config.NodeID,
		Timestamp: time.Now(),
		Payload:   assignmentMsg,
	}

	// Broadcast to all nodes
	for _, nodeID := range le.activeNodes {
		if nodeID != le.config.NodeID {
			msg.To = nodeID
			le.config.Network.Send(nodeID, msg)
		}
	}

	// Also send announcement
	announcementMsg := &LeaderAnnouncementMessage{
		ElectionID:    le.electionID,
		PrimaryLeader: le.primaryLeader,
		ShardLeaders:  le.shardLeaders,
		ShardNodes:    le.shardAssignments,
		Timestamp:     time.Now(),
	}

	if le.config.Signer != nil {
		data := fmt.Sprintf("%s%s", le.electionID, le.primaryLeader)
		announcementMsg.Signature = le.config.Signer.Sign([]byte(data))
	}

	announceMsg := Message{
		Type:      MsgLeaderAnnouncement,
		From:      le.config.NodeID,
		Timestamp: time.Now(),
		Payload:   announcementMsg,
	}

	for _, nodeID := range le.activeNodes {
		if nodeID != le.config.NodeID {
			announceMsg.To = nodeID
			le.config.Network.Send(nodeID, announceMsg)
		}
	}

	le.state = StateElected

	le.config.Logger.Info("Announced leader assignments",
		"electionID", le.electionID,
		"primaryLeader", le.primaryLeader)

	// Update node configuration and role
	le.UpdateNodeConfig(le.primaryLeader, le.shardLeaders, le.shardAssignments)

	// Restart heartbeat monitor with new configuration
	le.restartHeartbeatMonitor()
}

func (le *LeaderElection) updateNodeRole() {
	oldRole := le.config.Role
	oldShardID := le.config.ShardID

	if le.config.NodeID == le.primaryLeader {
		le.config.Role = RolePrimaryLeader
		le.config.ShardID = 0 // Primary leader not in a specific shard
	} else {
		// Find which shard this node belongs to
		for shardID, nodes := range le.shardAssignments {
			for _, nodeID := range nodes {
				if nodeID == le.config.NodeID {
					le.config.ShardID = shardID
					if le.shardLeaders[shardID] == le.config.NodeID {
						le.config.Role = RoleShardLeader
					} else {
						le.config.Role = RoleShardFollower
					}
					break
				}
			}
		}
	}

	le.config.Logger.Info("Updated node role",
		"nodeID", le.config.NodeID,
		"role", le.config.Role.String(),
		"shardID", le.config.ShardID,
		"oldRole", oldRole.String(),
		"oldShardID", oldShardID)

	// Notify the node about the role change if it changed
	if oldRole != le.config.Role || oldShardID != le.config.ShardID {
		if le.config.Node != nil {
			le.config.Node.UpdateNodeRole(le.config.Role)
		}
	}
}

// UpdateNodeConfig updates both the local node configuration and notifies the node component
func (le *LeaderElection) UpdateNodeConfig(primaryLeader NodeID, shardLeaders map[ShardID]NodeID, shardAssignments map[ShardID][]NodeID) {
	// Update local state
	le.primaryLeader = primaryLeader
	le.shardLeaders = shardLeaders
	le.shardAssignments = shardAssignments

	// Update config
	le.config.PrimaryLeader = primaryLeader
	le.config.ShardLeaders = shardLeaders
	le.config.ShardNodes = shardAssignments

	// Update node role based on new configuration
	le.updateNodeRole()

	// Notify the node component about the new configuration
	if le.config.Node != nil {
		le.config.Node.UpdateNodeConfig(primaryLeader, shardLeaders, shardAssignments)
	}
}

// Utility methods

func (le *LeaderElection) sortNodes(nodes []NodeID) []NodeID {
	sorted := make([]NodeID, len(nodes))
	copy(sorted, nodes)
	sort.Slice(sorted, func(i, j int) bool {
		return string(sorted[i]) < string(sorted[j])
	})
	return sorted
}

func (le *LeaderElection) shouldStartElection(otherCandidate NodeID) bool {
	// Start election if this node has higher priority (lower ID)
	return string(le.config.NodeID) < string(otherCandidate)
}

func (le *LeaderElection) hasMajorityAcks() bool {
	acks := 0
	for _, acked := range le.acksReceived {
		if acked {
			acks++
		}
	}
	// Need majority of active nodes
	return acks >= (len(le.activeNodes)+1)/2
}

func (le *LeaderElection) updateActiveNodes(nodes []NodeID) {
	le.activeNodes = le.sortNodes(nodes)
	le.config.Logger.Info("Updated active nodes",
		"count", len(le.activeNodes),
		"nodes", le.activeNodes)
}

func (le *LeaderElection) generateElectionID() string {
	data := fmt.Sprintf("%s%d", le.config.NodeID, time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:8])
}

// generateRandomNumbers generates a random number for each active node in range [1, len(activeNodes)]
func (le *LeaderElection) generateRandomNumbers() map[NodeID]int {
	randomNumbers := make(map[NodeID]int)
	max := big.NewInt(int64(len(le.activeNodes)))

	for _, nodeID := range le.activeNodes {
		// Generate random number between 1 and len(activeNodes)
		randomNum, err := rand.Int(rand.Reader, max)
		if err != nil {
			le.config.Logger.Error("Failed to generate random number", "error", err)
			randomNum = big.NewInt(1) // Fallback
		}

		// Ensure it's at least 1
		num := int(randomNum.Int64()) + 1
		randomNumbers[nodeID] = num
	}

	le.config.Logger.Info("Generated random numbers for nodes", "randomNumbers", randomNumbers)
	return randomNumbers
}

// sortNodesByRandomNumber sorts nodes based on their random numbers (ascending)
func (le *LeaderElection) sortNodesByRandomNumber(randomNumbers map[NodeID]int) []NodeID {
	sorted := make([]NodeID, 0, len(le.activeNodes))
	for nodeID := range randomNumbers {
		sorted = append(sorted, nodeID)
	}

	sort.Slice(sorted, func(i, j int) bool {
		return randomNumbers[sorted[i]] < randomNumbers[sorted[j]]
	})

	le.config.Logger.Info("Sorted nodes by random numbers", "sortedNodes", sorted)
	return sorted
}

// calculateLeaderIndex calculates the leader index by summing all random numbers and taking mod with active node count
func (le *LeaderElection) calculateLeaderIndex(randomNumbers map[NodeID]int) int {
	sum := 0
	for _, num := range randomNumbers {
		sum += num
	}

	activeCount := len(le.activeNodes)
	if activeCount == 0 {
		return 0
	}

	leaderIndex := sum % activeCount
	le.config.Logger.Info("Calculated leader index",
		"sum", sum,
		"activeCount", activeCount,
		"leaderIndex", leaderIndex)

	return leaderIndex
}

func (le *LeaderElection) handleElectionTimeout() {
	le.mu.Lock()
	defer le.mu.Unlock()

	if le.state == StateElecting {
		le.config.Logger.Info("Election timeout, starting re-election",
			"electionID", le.electionID)
		le.state = StateReElecting
		le.startElection()
	}
}

func (le *LeaderElection) monitorHeartbeats() {
	ticker := time.NewTicker(le.heartbeatTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-le.stopCh:
			return
		case <-ticker.C:
			le.checkHeartbeats()
		}
	}
}

// restartHeartbeatMonitor restarts the heartbeat monitoring with updated configuration
func (le *LeaderElection) restartHeartbeatMonitor() {
	le.config.Logger.Info("Restarting heartbeat monitor after election",
		"nodeID", le.config.NodeID,
		"primaryLeader", le.primaryLeader,
		"activeNodes", len(le.activeNodes))

	le.mu.Lock()

	// Create a new heartbeat map to track current active nodes and leaders
	newHeartbeatMap := make(map[NodeID]time.Time)

	// Preserve existing heartbeat timestamps for active nodes if they're recent
	now := time.Now()
	for _, nodeID := range le.activeNodes {
		if existingTime, exists := le.lastHeartbeat[nodeID]; exists {
			// Keep existing timestamp if it's within the heartbeat timeout window
			if now.Sub(existingTime) < le.heartbeatTimeout {
				newHeartbeatMap[nodeID] = existingTime
			} else {
				// Set to current time if the existing timestamp is too old
				newHeartbeatMap[nodeID] = now
			}
		} else {
			// Set to current time for new active nodes
			newHeartbeatMap[nodeID] = now
		}
	}

	// Ensure primary leader is tracked
	if _, exists := newHeartbeatMap[le.primaryLeader]; !exists {
		newHeartbeatMap[le.primaryLeader] = now
	}

	// Ensure all shard leaders are tracked and update their heartbeat timestamps
	for shardID, shardLeader := range le.shardLeaders {
		if _, exists := newHeartbeatMap[shardLeader]; !exists {
			newHeartbeatMap[shardLeader] = now
		} else {
			// Update heartbeat timestamp for existing shard leader
			newHeartbeatMap[shardLeader] = now
		}
		le.config.Logger.Info("Updated heartbeat for shard leader",
			"shardID", shardID,
			"shardLeader", shardLeader)
	}

	// Update the heartbeat map
	le.lastHeartbeat = newHeartbeatMap

	le.mu.Unlock()

	// The existing monitorHeartbeats goroutine will continue running
	// No need to restart it as it uses channels and will pick up the new configuration
}

func (le *LeaderElection) checkHeartbeats() {
	le.mu.Lock()
	defer le.mu.Unlock()

	now := time.Now()
	newActiveNodes := make([]NodeID, 0)

	for _, nodeID := range le.activeNodes {
		if lastHB, exists := le.lastHeartbeat[nodeID]; exists {
			if now.Sub(lastHB) < le.heartbeatTimeout {
				newActiveNodes = append(newActiveNodes, nodeID)
			} else {
				le.config.Logger.Info("Node failed heartbeat check",
					"nodeID", nodeID,
					"lastHeartbeat", lastHB)
			}
		}
	}

	if len(newActiveNodes) != len(le.activeNodes) {
		le.activeNodes = newActiveNodes
		le.config.Logger.Info("Active nodes changed",
			"oldCount", len(le.activeNodes),
			"newCount", len(newActiveNodes),
			"currentState", le.state)

		// Only trigger re-election if leader is no longer active AND no election is in progress
		if !le.isNodeActive(le.primaryLeader) && le.state != StateElecting && le.state != StateReElecting {
			le.config.Logger.Info("Primary leader failed, triggering re-election",
				"primaryLeader", le.primaryLeader,
				"currentState", le.state)
			le.startElection()
		} else if !le.isNodeActive(le.primaryLeader) {
			le.config.Logger.Info("Primary leader failed but election already in progress",
				"primaryLeader", le.primaryLeader,
				"currentState", le.state)
		}
	}
}

func (le *LeaderElection) isNodeActive(nodeID NodeID) bool {
	for _, active := range le.activeNodes {
		if active == nodeID {
			return true
		}
	}
	return false
}

// ReceiveHeartbeat updates the last heartbeat time for a node
func (le *LeaderElection) ReceiveHeartbeat(from NodeID, timestamp time.Time) {
	le.mu.Lock()
	defer le.mu.Unlock()

	le.lastHeartbeat[from] = timestamp

	// Add to active nodes if not present
	if !le.isNodeActive(from) {
		le.activeNodes = append(le.activeNodes, from)
		le.activeNodes = le.sortNodes(le.activeNodes)
		le.config.Logger.Info("Added new active node",
			"nodeID", from,
			"totalActive", len(le.activeNodes))
	}
}

// Getters

func (le *LeaderElection) GetPrimaryLeader() NodeID {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.primaryLeader
}

func (le *LeaderElection) GetShardLeaders() map[ShardID]NodeID {
	le.mu.RLock()
	defer le.mu.RUnlock()
	shardLeaders := make(map[ShardID]NodeID)
	for k, v := range le.shardLeaders {
		shardLeaders[k] = v
	}
	return shardLeaders
}

func (le *LeaderElection) GetShardAssignments() map[ShardID][]NodeID {
	le.mu.RLock()
	defer le.mu.RUnlock()
	assignments := make(map[ShardID][]NodeID)
	for k, v := range le.shardAssignments {
		assignments[k] = make([]NodeID, len(v))
		copy(assignments[k], v)
	}
	return assignments
}

func (le *LeaderElection) GetState() ElectionState {
	le.mu.RLock()
	defer le.mu.RUnlock()
	return le.state
}

// SetCallbacks sets callback functions for election events
func (le *LeaderElection) SetCallbacks(onLeaderElected func(primary NodeID, shards map[ShardID]NodeID),
	onShardAssigned func(shardID ShardID, leader NodeID, nodes []NodeID)) {
	le.onLeaderElected = onLeaderElected
	le.onShardAssigned = onShardAssigned
}
