package consensus

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// BlockStorage provides storage for consensus blocks
type BlockStorage interface {
	StoreBlock(block *Block) error
	GetBlock(blockID string) (*Block, error)
	GetBlockByHeight(height uint64) (*Block, error)
	GetLatestBlock() (*Block, error)
	Close() error
}

// LevelDBStorage implements BlockStorage using LevelDB
type LevelDBStorage struct {
	db          *leveldb.DB
	mu          sync.RWMutex
	latestBlock *Block
}

// NewLevelDBStorage creates a new LevelDB storage instance
func NewLevelDBStorage(dbPath string) (*LevelDBStorage, error) {
	opts := &opt.Options{
		CompactionTableSize: 2 * opt.MiB,
	}

	db, err := leveldb.OpenFile(dbPath, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open leveldb: %w", err)
	}

	storage := &LevelDBStorage{
		db: db,
	}

	// Try to load the latest block
	latestBlockBytes, err := db.Get([]byte("latest_block"), nil)
	if err == nil {
		var latestBlock Block
		if err := json.Unmarshal(latestBlockBytes, &latestBlock); err == nil {
			storage.latestBlock = &latestBlock
		}
	}

	return storage, nil
}

// StoreBlock stores a block in LevelDB
func (s *LevelDBStorage) StoreBlock(block *Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Serialize block
	blockBytes, err := json.Marshal(block)
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}

	// Store by ID
	if err := s.db.Put([]byte("block:"+block.ID), blockBytes, nil); err != nil {
		return fmt.Errorf("failed to store block by ID: %w", err)
	}

	// Store by height
	heightKey := fmt.Sprintf("height:%020d", block.Height)
	if err := s.db.Put([]byte(heightKey), []byte(block.ID), nil); err != nil {
		return fmt.Errorf("failed to store block by height: %w", err)
	}

	// Update latest block
	if s.latestBlock == nil || block.Height > s.latestBlock.Height {
		s.latestBlock = block
		if err := s.db.Put([]byte("latest_block"), blockBytes, nil); err != nil {
			return fmt.Errorf("failed to update latest block: %w", err)
		}
	}

	// Store transactions with references to the block
	for _, tx := range block.Transactions {
		txKey := []byte("tx:" + tx.ID)
		if err := s.db.Put(txKey, []byte(block.ID), nil); err != nil {
			return fmt.Errorf("failed to store transaction reference: %w", err)
		}
	}

	return nil
}

// GetBlock retrieves a block by ID
func (s *LevelDBStorage) GetBlock(blockID string) (*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	blockBytes, err := s.db.Get([]byte("block:"+blockID), nil)
	if err != nil {
		return nil, fmt.Errorf("block not found: %w", err)
	}

	var block Block
	if err := json.Unmarshal(blockBytes, &block); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}

	return &block, nil
}

// GetBlockByHeight retrieves a block by height
func (s *LevelDBStorage) GetBlockByHeight(height uint64) (*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	heightKey := fmt.Sprintf("height:%020d", height)
	blockID, err := s.db.Get([]byte(heightKey), nil)
	if err != nil {
		return nil, fmt.Errorf("block height not found: %w", err)
	}

	return s.GetBlock(string(blockID))
}

// GetLatestBlock returns the latest block
func (s *LevelDBStorage) GetLatestBlock() (*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.latestBlock == nil {
		return nil, fmt.Errorf("no blocks stored yet")
	}

	return s.latestBlock, nil
}

// Close closes the database
func (s *LevelDBStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.Close()
}

// GetTransactionBlock returns the block containing a transaction
func (s *LevelDBStorage) GetTransactionBlock(txID string) (*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	blockID, err := s.db.Get([]byte("tx:"+txID), nil)
	if err != nil {
		return nil, fmt.Errorf("transaction not found: %w", err)
	}

	return s.GetBlock(string(blockID))
}
