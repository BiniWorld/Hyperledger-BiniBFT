package fabric

import (
	"binibft-poc/consensus"
	"bytes"
	"encoding/asn1"
	"log/slog"
	"os"
	"testing"
)

type mockNetwork struct{}

func (m *mockNetwork) Send(nodeID consensus.NodeID, message consensus.Message) error        { return nil }
func (m *mockNetwork) Broadcast(nodeIDs []consensus.NodeID, message consensus.Message) error { return nil }
func (m *mockNetwork) RegisterHandler(handler consensus.MessageHandler)                      {}
func (m *mockNetwork) SendTransaction(targetID consensus.NodeID, request []byte) error       { return nil }

func TestConsenterHandleChain(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	opts := ConsenterOptions{
		NodeID:        consensus.NodeID("1"),
		ShardID:       consensus.ShardID(1),
		Role:          consensus.RolePrimaryLeader,
		PrimaryLeader: consensus.NodeID("1"),
		ShardLeaders:  map[consensus.ShardID]consensus.NodeID{consensus.ShardID(1): consensus.NodeID("1")},
		ShardNodes:    map[consensus.ShardID][]consensus.NodeID{consensus.ShardID(1): {consensus.NodeID("1")}},
		Logger:        logger,
		Network:       &mockNetwork{},
	}

	consenter := NewConsenter(opts)
	if consenter == nil {
		t.Fatalf("NewConsenter returned nil")
	}

	support := NewMockConsenterSupport("test-channel")
	chain, err := consenter.HandleChain(support, nil)
	if err != nil {
		t.Fatalf("HandleChain failed: %v", err)
	}
	if chain == nil {
		t.Fatalf("HandleChain returned nil chain")
	}

	// Test WaitReady and Start
	if err := chain.WaitReady(); err != nil {
		t.Fatalf("WaitReady returned error: %v", err)
	}

	chain.Start()

	// Test Order with nil envelope
	if err := chain.Order(nil, 0); err == nil {
		t.Fatalf("expected error when ordering nil envelope")
	}

	// Test Order with valid envelope
	env := &Envelope{
		Payload:   []byte("sample-fabric-payload"),
		Signature: []byte("sample-signature"),
	}
	if err := chain.Order(env, 0); err != nil {
		t.Fatalf("Order failed for valid envelope: %v", err)
	}

	// Test Configure
	configEnv := &Envelope{
		Payload:   []byte("sample-config-payload"),
		Signature: []byte("sample-config-signature"),
	}
	if err := chain.Configure(configEnv, 1); err != nil {
		t.Fatalf("Configure failed for valid config envelope: %v", err)
	}

	// Test Halt
	chain.Halt()

	// Order after halt should fail
	if err := chain.Order(env, 0); err == nil {
		t.Fatalf("expected error when ordering on halted chain")
	}
}

func TestChainDeliver(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	support := NewMockConsenterSupport("test-channel")

	chain := NewBiniBFTChain(
		support,
		nil,
		consensus.NodeID("1"),
		consensus.ShardID(1),
		consensus.RolePrimaryLeader,
		logger,
	)

	// Construct BiniBFT proposal
	type BlockDataPayload struct {
		Transactions [][]byte
	}
	tx1 := []byte("tx-payload-1")
	tx2 := []byte("tx-payload-2")
	rawPayload, _ := asn1.Marshal(BlockDataPayload{Transactions: [][]byte{tx1, tx2}})

	proposal := consensus.Proposal{
		Header:   []byte("header-1"),
		Payload:  rawPayload,
		Metadata: []byte("metadata-qc-proof"),
	}

	// Deliver proposal to Fabric chain
	if err := chain.Deliver(proposal); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	// Verify block was committed to Fabric support
	if support.Height() != 1 {
		t.Fatalf("expected Fabric support height 1, got %d", support.Height())
	}

	block := support.Block(0)
	if block == nil {
		t.Fatalf("expected block 0 to exist")
	}
	if block.Header.Number != 0 {
		t.Fatalf("expected block number 0, got %d", block.Header.Number)
	}
	if len(block.Data.Data) != 2 {
		t.Fatalf("expected 2 transactions in block, got %d", len(block.Data.Data))
	}
	if !bytes.Equal(block.Data.Data[0], tx1) || !bytes.Equal(block.Data.Data[1], tx2) {
		t.Fatalf("transaction data mismatch in Fabric block")
	}
	if len(block.Metadata.Metadata) < 2 || !bytes.Equal(block.Metadata.Metadata[0], []byte("metadata-qc-proof")) {
		t.Fatalf("metadata QC proof mismatch in Fabric block")
	}
}
