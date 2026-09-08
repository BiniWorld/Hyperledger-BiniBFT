package fabric

import (
	"binibft-poc/consensus"
	"log/slog"
	"os"
	"testing"
	"time"
)

func validTestConfig() *BiniBFTConfig {
	return &BiniBFTConfig{
		NumShards:     2,
		BatchTimeout:  1 * time.Second,
		BatchSize:     BatchSizeConfig{MaxMessageCount: 100, AbsoluteMaxBytes: 10 * 1024 * 1024, PreferredMaxBytes: 2 * 1024 * 1024},
		PrimaryLeader: "orderer1",
		Consenters: []ConsenterNodeConfig{
			{ID: "orderer1", Host: "127.0.0.1", Port: 7050, MSPID: "OrdererMSP"},
			{ID: "orderer2", Host: "127.0.0.1", Port: 7051, MSPID: "OrdererMSP"},
			{ID: "orderer3", Host: "127.0.0.1", Port: 7052, MSPID: "OrdererMSP"},
			{ID: "orderer4", Host: "127.0.0.1", Port: 7053, MSPID: "OrdererMSP"},
			{ID: "orderer5", Host: "127.0.0.1", Port: 7054, MSPID: "OrdererMSP"},
		},
		Shards: []ShardConfig{
			{ShardID: 1, Leader: "orderer2", Followers: []string{"orderer3"}},
			{ShardID: 2, Leader: "orderer4", Followers: []string{"orderer5"}},
		},
	}
}

func TestBiniBFTConfigValidation(t *testing.T) {
	// 1. Valid config
	cfg := validTestConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config to pass validation: %v", err)
	}

	// 2. NumShards < 1
	invalidCfg1 := validTestConfig()
	invalidCfg1.NumShards = 0
	if err := invalidCfg1.Validate(); err == nil {
		t.Fatalf("expected error for NumShards = 0")
	}

	// 3. Empty consenters
	invalidCfg2 := validTestConfig()
	invalidCfg2.Consenters = nil
	if err := invalidCfg2.Validate(); err == nil {
		t.Fatalf("expected error for empty consenters")
	}

	// 4. Duplicate consenter ID
	invalidCfg3 := validTestConfig()
	invalidCfg3.Consenters = append(invalidCfg3.Consenters, ConsenterNodeConfig{ID: "orderer1"})
	if err := invalidCfg3.Validate(); err == nil {
		t.Fatalf("expected error for duplicate consenter ID")
	}

	// 5. Unknown primary leader
	invalidCfg4 := validTestConfig()
	invalidCfg4.PrimaryLeader = "non-existent-orderer"
	if err := invalidCfg4.Validate(); err == nil {
		t.Fatalf("expected error for unknown primary leader")
	}

	// 6. Shard count mismatch
	invalidCfg5 := validTestConfig()
	invalidCfg5.Shards = invalidCfg5.Shards[:1]
	if err := invalidCfg5.Validate(); err == nil {
		t.Fatalf("expected error for shard count mismatch")
	}

	// 7. Node assigned to multiple shards
	invalidCfg6 := validTestConfig()
	invalidCfg6.Shards[1].Followers = []string{"orderer3"} // orderer3 is already in Shard 1
	if err := invalidCfg6.Validate(); err == nil {
		t.Fatalf("expected error for node assigned to multiple shards")
	}
}

func TestBiniBFTConfigMarshalUnmarshal(t *testing.T) {
	cfg := validTestConfig()

	raw, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalConfig failed: %v", err)
	}
	if len(raw) == 0 {
		t.Fatalf("expected non-empty marshaled config")
	}

	unmarshaled, err := UnmarshalConfig(raw)
	if err != nil {
		t.Fatalf("UnmarshalConfig failed: %v", err)
	}

	if unmarshaled.NumShards != cfg.NumShards {
		t.Fatalf("expected NumShards %d, got %d", cfg.NumShards, unmarshaled.NumShards)
	}
	if unmarshaled.PrimaryLeader != cfg.PrimaryLeader {
		t.Fatalf("expected PrimaryLeader %s, got %s", cfg.PrimaryLeader, unmarshaled.PrimaryLeader)
	}
	if len(unmarshaled.Consenters) != len(cfg.Consenters) {
		t.Fatalf("consenter count mismatch")
	}
}

func TestHandleChainWithDynamicMetadata(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := validTestConfig()
	rawMetadata, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalConfig failed: %v", err)
	}

	support := NewMockConsenterSupport("channel-dynamic-metadata")

	opts := ConsenterOptions{
		NodeID:  consensus.NodeID("orderer1"),
		ShardID: consensus.ShardID(1),
		Role:    consensus.RolePrimaryLeader,
		Logger:  logger,
		Network: &mockNetwork{},
	}
	consenter := NewConsenter(opts)

	metadata := &Metadata{
		Value: rawMetadata,
	}

	chain, err := consenter.HandleChain(support, metadata)
	if err != nil {
		t.Fatalf("HandleChain with dynamic metadata failed: %v", err)
	}
	if chain == nil {
		t.Fatalf("expected non-nil chain")
	}

	// Verify chain can order and deliver with dynamic metadata
	chain.Start()
	defer chain.Halt()

	env := &Envelope{
		Payload:   []byte("test-payload-with-metadata"),
		Signature: []byte("sig"),
	}
	if err := chain.Order(env, 0); err != nil {
		t.Fatalf("Order failed on dynamically configured chain: %v", err)
	}
}
