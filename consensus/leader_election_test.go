package consensus

import (
	"crypto/sha256"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestLeaderElectionVRFRanking(t *testing.T) {
	node1 := NodeID("1")
	node2 := NodeID("2")
	node3 := NodeID("3")
	node4 := NodeID("4")

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	config := &Config{
		NodeID:           node1,
		ShardID:          ShardID(1),
		Role:             RoleShardFollower,
		PrimaryLeader:    node1,
		MajorityRequired: 3,
		ChannelID:        "test-channel",
		Logger:           logger,
		Network:          &mockNetwork{},
	}

	le := NewLeaderElection(config, 2) // 2 Shards
	le.activeNodes = []NodeID{node1, node2, node3, node4}

	// Start election
	le.StartElection()

	if le.GetState() != StateElecting {
		t.Fatalf("expected state StateElecting, got %v", le.GetState())
	}

	electionID := le.electionID
	term := le.term

	// Generate known VRF outputs for ranking:
	// Let node 3 have the highest VRF score (0xFF...), node 2 second highest (0xAA...), node 4 third (0x55...), node 1 lowest (0x11...)
	vrf3 := sha256.Sum256([]byte("highest-score-node-3"))
	vrf2 := sha256.Sum256([]byte("second-highest-node-2"))
	vrf4 := sha256.Sum256([]byte("third-highest-node-4"))

	// Receive Acks from nodes 2, 3, 4
	le.HandleMessage(node3, Message{
		Type: MsgElectionAck,
		From: node3,
		Payload: &ElectionAckMessage{
			ElectionID:   electionID,
			NodeID:       node3,
			Term:         term,
			VRFOutput:    vrf3[:],
			Acknowledged: true,
			Timestamp:    time.Now(),
		},
	})

	le.HandleMessage(node2, Message{
		Type: MsgElectionAck,
		From: node2,
		Payload: &ElectionAckMessage{
			ElectionID:   electionID,
			NodeID:       node2,
			Term:         term,
			VRFOutput:    vrf2[:],
			Acknowledged: true,
			Timestamp:    time.Now(),
		},
	})

	le.HandleMessage(node4, Message{
		Type: MsgElectionAck,
		From: node4,
		Payload: &ElectionAckMessage{
			ElectionID:   electionID,
			NodeID:       node4,
			Term:         term,
			VRFOutput:    vrf4[:],
			Acknowledged: true,
			Timestamp:    time.Now(),
		},
	})

	// Election should be completed
	if le.GetState() != StateElected {
		t.Fatalf("expected state StateElected, got %v", le.GetState())
	}

	primary := le.GetPrimaryLeader()
	if primary == "" {
		t.Fatalf("expected non-empty primary leader")
	}

	shardLeaders := le.GetShardLeaders()
	if len(shardLeaders) == 0 {
		t.Fatalf("expected shard leaders to be assigned")
	}

	// Primary should not be a shard leader
	for sid, leader := range shardLeaders {
		if leader == primary {
			t.Fatalf("primary leader %s is also assigned as leader for shard %d", primary, sid)
		}
	}
}

func TestLeaderElectionStaleTermRejection(t *testing.T) {
	node1 := NodeID("1")
	node2 := NodeID("2")

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	config := &Config{
		NodeID:    node1,
		ChannelID: "test-channel",
		Logger:    logger,
		Network:   &mockNetwork{},
	}

	le := NewLeaderElection(config, 1)
	le.term = 10
	le.electionID = "current-election-id"

	// Stale election message (term 5 < 10)
	staleMsg := Message{
		Type: MsgLeaderElection,
		From: node2,
		Payload: &LeaderElectionMessage{
			ElectionID:  "stale-election",
			CandidateID: node2,
			Term:        5,
			ActiveNodes: []NodeID{node1, node2},
		},
	}

	if err := le.HandleMessage(node2, staleMsg); err != nil {
		t.Fatalf("HandleMessage returned error on stale message: %v", err)
	}

	if le.term != 10 {
		t.Fatalf("expected term to remain 10, got %d", le.term)
	}
}
