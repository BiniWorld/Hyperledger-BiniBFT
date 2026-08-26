package consensus

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"testing"
)

type memoryBlockStorage struct {
	blocks      map[uint64]*Block
	latestBlock *Block
}

func newMemoryBlockStorage() *memoryBlockStorage {
	return &memoryBlockStorage{
		blocks: make(map[uint64]*Block),
	}
}

func (m *memoryBlockStorage) Put(key []byte, value []byte) error { return nil }
func (m *memoryBlockStorage) Get(key []byte) ([]byte, error)     { return nil, nil }
func (m *memoryBlockStorage) Close() error                       { return nil }

func (m *memoryBlockStorage) GetLatestBlock() (*Block, error) {
	if m.latestBlock == nil {
		return nil, fmt.Errorf("no blocks")
	}
	return m.latestBlock, nil
}

func (m *memoryBlockStorage) GetBlockByHeight(height uint64) (*Block, error) {
	b, ok := m.blocks[height]
	if !ok {
		return nil, fmt.Errorf("block not found at height %d", height)
	}
	return b, nil
}

func (m *memoryBlockStorage) GetBlockRange(startSeq, endSeq uint64) ([]*Block, error) {
	var result []*Block
	for seq := startSeq; seq <= endSeq; seq++ {
		b, ok := m.blocks[seq]
		if !ok {
			break
		}
		result = append(result, b)
	}
	return result, nil
}

func (m *memoryBlockStorage) StoreBlock(block *Block) error {
	if block == nil {
		return fmt.Errorf("nil block")
	}
	m.blocks[uint64(block.Sequence)] = block
	m.latestBlock = block
	return nil
}

func createTestBlockChain(count int) []*Block {
	blocks := make([]*Block, count)
	var prevHash string
	for i := 1; i <= count; i++ {
		b := &Block{
			Sequence: int64(i),
			PrevHash: prevHash,
			Metadata: []byte(fmt.Sprintf("meta-%d", i)),
			Transactions: []Transaction{
				{ClientID: "client-1", TS: 1000, ID: fmt.Sprintf("tx-%d", i), Data: "payload"},
			},
		}
		prevHash = fmt.Sprintf("%x", sha256.Sum256(b.ToBytes()))
		blocks[i-1] = b
	}
	return blocks
}

func TestVerifyBlockChain(t *testing.T) {
	blocks := createTestBlockChain(5)

	// 1. Valid block chain
	if err := VerifyBlockChain(blocks, 1, ""); err != nil {
		t.Fatalf("expected valid block chain to verify: %v", err)
	}

	// 2. Sequence gap
	gapBlocks := []*Block{blocks[0], blocks[1], blocks[3], blocks[4]}
	if err := VerifyBlockChain(gapBlocks, 1, ""); err == nil {
		t.Fatalf("expected error for sequence gap")
	}

	// 3. PrevHash mismatch
	corruptedBlocks := createTestBlockChain(3)
	corruptedBlocks[1].PrevHash = "tampered-prev-hash"
	if err := VerifyBlockChain(corruptedBlocks, 1, ""); err == nil {
		t.Fatalf("expected error for prevHash mismatch")
	}
}

func TestSyncEngineCatchUp(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Serving node with 10 blocks
	servingStorage := newMemoryBlockStorage()
	allBlocks := createTestBlockChain(10)
	for _, b := range allBlocks {
		servingStorage.StoreBlock(b)
	}

	servingConfig := &Config{
		NodeID:    NodeID("serving-node"),
		ShardID:   ShardID(1),
		ChannelID: "test-channel",
		Logger:    logger,
	}
	servingEngine := NewSyncEngine(servingConfig, servingStorage, nil, logger)

	// Lagging node with 3 blocks
	laggingStorage := newMemoryBlockStorage()
	for _, b := range allBlocks[:3] {
		laggingStorage.StoreBlock(b)
	}

	laggingConfig := &Config{
		NodeID:    NodeID("lagging-node"),
		ShardID:   ShardID(1),
		ChannelID: "test-channel",
		Logger:    logger,
		Network:   &mockNetwork{},
	}
	laggingEngine := NewSyncEngine(laggingConfig, laggingStorage, nil, logger)

	if laggingEngine.GetLastSyncedSequence() != 3 {
		t.Fatalf("expected initial sequence 3, got %d", laggingEngine.GetLastSyncedSequence())
	}

	// Lagging node requests sync up to sequence 10
	if err := laggingEngine.InitiateSync(10, NodeID("serving-node")); err != nil {
		t.Fatalf("InitiateSync failed: %v", err)
	}

	if laggingEngine.GetState() != SyncStateSyncing {
		t.Fatalf("expected state SyncStateSyncing, got %v", laggingEngine.GetState())
	}

	// Serving node processes SyncRequest for sequences 4..10
	syncReq := &SyncRequestMessage{
		FromNodeID:    NodeID("lagging-node"),
		ShardID:       ShardID(1),
		StartSequence: 4,
		EndSequence:   10,
	}
	resp, err := servingEngine.HandleSyncRequest(syncReq)
	if err != nil {
		t.Fatalf("HandleSyncRequest failed: %v", err)
	}
	if len(resp.Blocks) != 7 {
		t.Fatalf("expected 7 blocks in sync response, got %d", len(resp.Blocks))
	}

	// Lagging node processes SyncResponse
	if err := laggingEngine.HandleSyncResponse(resp); err != nil {
		t.Fatalf("HandleSyncResponse failed: %v", err)
	}

	if laggingEngine.GetState() != SyncStateComplete {
		t.Fatalf("expected state SyncStateComplete, got %v", laggingEngine.GetState())
	}

	if laggingEngine.GetLastSyncedSequence() != 10 {
		t.Fatalf("expected synced sequence 10, got %d", laggingEngine.GetLastSyncedSequence())
	}

	// Verify all 10 blocks exist in lagging storage
	latest, err := laggingStorage.GetLatestBlock()
	if err != nil || latest == nil || latest.Sequence != 10 {
		t.Fatalf("lagging storage missing final block sequence 10: latest=%+v, err=%v", latest, err)
	}
}

func TestSyncRequestChunking(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	storage := newMemoryBlockStorage()
	blocks := createTestBlockChain(150)
	for _, b := range blocks {
		storage.StoreBlock(b)
	}

	config := &Config{
		NodeID:    NodeID("node-1"),
		ShardID:   ShardID(1),
		ChannelID: "test-channel",
		Logger:    logger,
	}
	engine := NewSyncEngine(config, storage, nil, logger)
	engine.maxChunkSize = 50 // Chunk limit 50

	req := &SyncRequestMessage{
		StartSequence: 1,
		EndSequence:   150,
	}
	resp, err := engine.HandleSyncRequest(req)
	if err != nil {
		t.Fatalf("HandleSyncRequest failed: %v", err)
	}

	if len(resp.Blocks) != 50 {
		t.Fatalf("expected chunked response with 50 blocks, got %d", len(resp.Blocks))
	}
	if resp.Blocks[0].Sequence != 1 || resp.Blocks[49].Sequence != 50 {
		t.Fatalf("unexpected chunk sequence range: first=%d, last=%d", resp.Blocks[0].Sequence, resp.Blocks[49].Sequence)
	}
}
