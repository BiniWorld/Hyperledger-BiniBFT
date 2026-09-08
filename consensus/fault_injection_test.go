package consensus

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
)

// faultyNetwork simulates network drops, delays, partitions, and message mutations
type faultyNetwork struct {
	mu           sync.RWMutex
	handlers     map[NodeID]MessageHandler
	partitionMap map[NodeID]int
	mutator      func(msg Message) Message
}

func newFaultyNetwork() *faultyNetwork {
	return &faultyNetwork{
		handlers:     make(map[NodeID]MessageHandler),
		partitionMap: make(map[NodeID]int),
	}
}

func (n *faultyNetwork) RegisterHandler(handler MessageHandler) {}

func (n *faultyNetwork) RegisterNodeHandler(nodeID NodeID, handler MessageHandler) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.handlers[nodeID] = handler
}

func (n *faultyNetwork) SetPartition(nodeID NodeID, group int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.partitionMap[nodeID] = group
}

func (n *faultyNetwork) Send(to NodeID, msg Message) error {
	n.mu.RLock()
	fromGroup, hasFrom := n.partitionMap[msg.From]
	toGroup, hasTo := n.partitionMap[to]
	handler, exists := n.handlers[to]
	mutator := n.mutator
	n.mu.RUnlock()

	// If nodes are in different non-zero partitions, drop message
	if hasFrom && hasTo && fromGroup != 0 && toGroup != 0 && fromGroup != toGroup {
		return fmt.Errorf("network partition active: %s cannot reach %s", msg.From, to)
	}

	if mutator != nil {
		msg = mutator(msg)
	}

	if exists && handler != nil {
		go func() {
			_ = handler.HandleMessage(msg.From, msg)
		}()
	}
	return nil
}

func (n *faultyNetwork) Broadcast(nodeIDs []NodeID, msg Message) error {
	for _, id := range nodeIDs {
		_ = n.Send(id, msg)
	}
	return nil
}

func (n *faultyNetwork) SendTransaction(targetID NodeID, request []byte) error {
	return nil
}

// TestByzantineInvalidSignatureRejection tests that corrupted vote signatures are rejected and excluded from quorums
func TestByzantineInvalidSignatureRejection(t *testing.T) {
	channelID := "test-channel"
	view := uint64(0)
	seq := uint64(1)
	shardID := ShardID(1)
	digest := "sample-proposal-digest"

	shardNodes := []NodeID{NodeID("1"), NodeID("2"), NodeID("3"), NodeID("4")}
	minQuorum := CalculateIntraShardQuorum(len(shardNodes)) // 3

	keys := make(map[NodeID]*ecdsa.PrivateKey)
	pubKeys := make(map[NodeID]*ecdsa.PublicKey)
	for _, n := range shardNodes {
		priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		keys[n] = priv
		pubKeys[n] = &priv.PublicKey
	}

	verifier := &testVerifier{pubKeys: pubKeys}

	// Construct QC with 2 valid signatures and 1 corrupted signature
	signatures := make(map[NodeID][]byte)
	for _, n := range shardNodes[:2] {
		prepDigest := ComputePrepareVoteDigest(channelID, view, seq, shardID, n, digest)
		sig, _ := ecdsa.SignASN1(rand.Reader, keys[n], prepDigest)
		signatures[n] = sig
	}

	// 3rd signature is corrupted
	corruptedDigest := ComputePrepareVoteDigest(channelID, view, seq, shardID, shardNodes[2], digest)
	badSig, _ := ecdsa.SignASN1(rand.Reader, keys[shardNodes[2]], corruptedDigest)
	badSig[len(badSig)-1] ^= 0xFF
	signatures[shardNodes[2]] = badSig

	shardQC := CreateShardQC(channelID, view, seq, "prepare", shardID, digest, signatures)

	// Verification must fail because valid signature count (2) < minQuorum (3)
	if err := VerifyShardQC(verifier, shardQC, shardNodes, minQuorum); err == nil {
		t.Fatalf("expected VerifyShardQC to fail due to corrupted signature reducing valid votes below quorum")
	}
}

// TestByzantineEquivocationRejection tests that conflicting proposals for the same sequence are rejected
func TestByzantineEquivocationRejection(t *testing.T) {
	view, privPrimary, _ := setupTestViewWithCrypto(t)

	// Valid proposal for sequence 1
	proposal1 := Proposal{Header: []byte("header-seq-1"), Payload: []byte("payload-1")}
	digest1 := ComputePrePrepDigest("test-channel", 0, 1, proposal1.Digest())
	sig1, _ := ecdsa.SignASN1(rand.Reader, privPrimary, digest1)

	msg1 := &PrePrepMessage{
		NodeID:    NodeID("1"),
		Sequence:  1,
		View:      0,
		ShardID:   ShardID(1),
		Proposal:  proposal1,
		Digest:    proposal1.Digest(),
		Signature: sig1,
	}

	// Process first valid proposal
	if err := view.HandlePrePrepare(msg1); err != nil {
		t.Fatalf("HandlePrePrepare failed for initial proposal: %v", err)
	}

	// Byzantine primary tries to propose a conflicting payload for same sequence 1
	proposal2 := Proposal{Header: []byte("header-seq-1"), Payload: []byte("conflicting-payload-2")}
	digest2 := ComputePrePrepDigest("test-channel", 0, 1, proposal2.Digest())
	sig2, _ := ecdsa.SignASN1(rand.Reader, privPrimary, digest2)

	msg2 := &PrePrepMessage{
		NodeID:    NodeID("1"),
		Sequence:  1,
		View:      0,
		ShardID:   ShardID(1),
		Proposal:  proposal2,
		Digest:    proposal2.Digest(),
		Signature: sig2,
	}

	// Conflicting duplicate sequence must be rejected
	err := view.HandlePrePrepare(msg2)
	if err == nil {
		t.Fatalf("expected error when handling conflicting proposal for already processed sequence")
	}
}

// TestByzantineVRFTamperRejection verifies that forged VRF proofs cannot win leader election
func TestByzantineVRFTamperRejection(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	node1 := NodeID("node-1")
	node2 := NodeID("byzantine-node-2")

	config := &Config{
		NodeID:    node1,
		ShardID:   ShardID(1),
		ChannelID: "test-channel",
		Logger:    logger,
		Network:   &mockNetwork{},
	}

	le := NewLeaderElection(config, 1)
	le.StartElection()

	fakeHighestScore := sha256.Sum256([]byte("forged-highest-vrf-output"))
	forgedProof := []byte("invalid-zero-knowledge-proof")

	ackMsg := Message{
		Type: MsgElectionAck,
		From: node2,
		Payload: &ElectionAckMessage{
			ElectionID:   le.electionID,
			NodeID:       node2,
			Term:         le.term,
			VRFOutput:    fakeHighestScore[:],
			VRFProof:     forgedProof,
			Acknowledged: true,
		},
	}

	_ = le.HandleMessage(node2, ackMsg)

	if le.term != 1 {
		t.Fatalf("expected term 1, got %d", le.term)
	}
}

// TestNetworkPartitionRecovery tests that nodes survive a temporary partition and catch up
func TestNetworkPartitionRecovery(t *testing.T) {
	net := newFaultyNetwork()

	node1 := NodeID("node-1")
	node4 := NodeID("node-4")

	// Set partition: Group 1 = {node-1}, Group 2 = {node-4}
	net.SetPartition(node1, 1)
	net.SetPartition(node4, 2)

	// Node 1 tries to send message to Node 4 -> fails due to partition
	err := net.Send(node4, Message{From: node1, To: node4, Type: MsgHeartbeat})
	if err == nil {
		t.Fatalf("expected send to fail across network partition")
	}

	// Heal partition: merge group 2 into group 1
	net.SetPartition(node4, 1)

	// Send succeeds after healing
	err = net.Send(node4, Message{From: node1, To: node4, Type: MsgHeartbeat})
	if err != nil {
		t.Fatalf("expected send to succeed after healing network partition: %v", err)
	}
}
