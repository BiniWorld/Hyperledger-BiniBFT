package consensus

import (
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

// trackingApplication records delivered proposals so tests can assert finalization occurred.
type trackingApplication struct {
	mu        sync.Mutex
	delivered []Proposal
}

func (a *trackingApplication) Deliver(proposal Proposal) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.delivered = append(a.delivered, proposal)
	return nil
}

func (a *trackingApplication) ReceiveHeartbeat(_ NodeID, _ time.Time) {}

func (a *trackingApplication) DeliveredCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.delivered)
}

// TestSingleShardLeaderCompletesRound verifies that a single-shard-leader
// topology (Primary Leader is the only shard leader) can complete a full
// consensus round without stalling.
//
// Before the fix, startPreparePhaseWithShardLeaders had no fallback for
// sentCount == 0, causing the protocol to hang indefinitely.
func TestSingleShardLeaderCompletesRound(t *testing.T) {
	app := &trackingApplication{}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	primaryID := NodeID("primary-1")

	config := &Config{
		NodeID:        primaryID,
		ShardID:       ShardID(1),
		Role:          RolePrimaryLeader,
		PrimaryLeader: primaryID,
		// The primary leader is the only shard leader
		ShardLeaders:     map[ShardID]NodeID{ShardID(1): primaryID},
		ShardNodes:       map[ShardID][]NodeID{ShardID(1): {primaryID}},
		MajorityRequired: 1,
		Timeout:          5 * time.Second,
		ChannelID:        "test-channel",
		Logger:           logger,
		Network:          &mockNetwork{},
		Application:      app,
	}

	view := NewView(primaryID, ShardID(1), config)

	proposal := Proposal{
		Header:  []byte("block-header-1"),
		Payload: []byte("transaction-data-1"),
	}

	// This call should complete the full cycle:
	//   Propose → startPreparePhaseWithShardLeaders (sentCount=0)
	//           → startCommitPhaseWithShardLeaders  (sentCount=0)
	//           → finalizeProposalAndCreateBlock
	err := view.Propose(proposal)
	if err != nil {
		t.Fatalf("Propose failed: %v", err)
	}

	// Verify finalization actually happened
	if app.DeliveredCount() != 1 {
		t.Fatalf("expected 1 delivered proposal, got %d", app.DeliveredCount())
	}

	// Verify the sequence advanced (from 1 to 2)
	if view.Sequence != 2 {
		t.Fatalf("expected sequence to advance to 2, got %d", view.Sequence)
	}

	// Verify the sequence was marked as finalized
	if !view.finalizedSequences[1] {
		t.Fatal("expected sequence 1 to be marked as finalized")
	}
}

// TestSingleShardLeaderMultipleRounds verifies that multiple consecutive
// proposals can be finalized in a single-shard-leader topology without
// the view getting stuck.
func TestSingleShardLeaderMultipleRounds(t *testing.T) {
	app := &trackingApplication{}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	primaryID := NodeID("primary-1")

	config := &Config{
		NodeID:           primaryID,
		ShardID:          ShardID(1),
		Role:             RolePrimaryLeader,
		PrimaryLeader:    primaryID,
		ShardLeaders:     map[ShardID]NodeID{ShardID(1): primaryID},
		ShardNodes:       map[ShardID][]NodeID{ShardID(1): {primaryID}},
		MajorityRequired: 1,
		Timeout:          5 * time.Second,
		ChannelID:        "test-channel",
		Logger:           logger,
		Network:          &mockNetwork{},
		Application:      app,
	}

	view := NewView(primaryID, ShardID(1), config)

	for i := 1; i <= 5; i++ {
		proposal := Proposal{
			Header:  []byte("block-header"),
			Payload: []byte("transaction-data"),
		}

		if err := view.Propose(proposal); err != nil {
			t.Fatalf("Propose #%d failed: %v", i, err)
		}
	}

	if app.DeliveredCount() != 5 {
		t.Fatalf("expected 5 delivered proposals, got %d", app.DeliveredCount())
	}

	// Sequence should have advanced from 1 to 6
	if view.Sequence != 6 {
		t.Fatalf("expected sequence 6 after 5 rounds, got %d", view.Sequence)
	}
}

// TestSingleShardLeaderNoFollowers verifies the edge case where the primary
// leader is the only node in the entire network (1 shard, 1 node).
func TestSingleShardLeaderNoFollowers(t *testing.T) {
	app := &trackingApplication{}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	primaryID := NodeID("solo-node")

	config := &Config{
		NodeID:           primaryID,
		ShardID:          ShardID(0),
		Role:             RolePrimaryLeader,
		PrimaryLeader:    primaryID,
		ShardLeaders:     map[ShardID]NodeID{ShardID(0): primaryID},
		ShardNodes:       map[ShardID][]NodeID{ShardID(0): {primaryID}},
		MajorityRequired: 1,
		Timeout:          5 * time.Second,
		ChannelID:        "solo-channel",
		Logger:           logger,
		Network:          &mockNetwork{},
		Application:      app,
	}

	view := NewView(primaryID, ShardID(0), config)

	proposal := Proposal{
		Header:  []byte("genesis"),
		Payload: []byte("genesis-tx"),
	}

	if err := view.Propose(proposal); err != nil {
		t.Fatalf("Propose failed for solo node: %v", err)
	}

	if app.DeliveredCount() != 1 {
		t.Fatalf("expected 1 delivered proposal for solo node, got %d", app.DeliveredCount())
	}

	if view.Sequence != 2 {
		t.Fatalf("expected sequence 2 after solo finalization, got %d", view.Sequence)
	}
}
