package consensus

import (
	"fmt"
	"log"
	"strconv"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// BlockStorage provides storage for consensus blocks
type BlockStorage interface {
	Put(key []byte, value []byte) error
	Get(key []byte) ([]byte, error)
	Close() error
	GetLatestBlock() (*Block, error)
	GetBlockByHeight(height uint64) (*Block, error)
	StoreBlock(block *Block) error
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

	// Try to load the latest block from storage
	if latestBlockBytes, err := db.Get([]byte("latest_block"), nil); err == nil {
		latestBlock := BlockFromBytes(latestBlockBytes)
		storage.latestBlock = latestBlock
	}

	return storage, nil
}

// Put implements BlockStorage interface
func (s *LevelDBStorage) Put(key []byte, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Put(key, value, nil)
}

// Get implements BlockStorage interface
func (s *LevelDBStorage) Get(key []byte) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.db.Get(key, nil)
}

// StoreBlock stores a block in LevelDB
func (s *LevelDBStorage) StoreBlock(block *Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.db.Put([]byte(strconv.FormatUint(uint64(block.Sequence), 10)), block.ToBytes(), &opt.WriteOptions{})
	if err != nil {
		log.Panicf("Error storing block: %v", err)
	}
	// set index for latest block
	err = s.db.Put([]byte("latest_block"), block.ToBytes(), &opt.WriteOptions{})
	if err != nil {
		log.Panicf("Error storing latest block: %v", err)
	}

	// Update the in-memory cache
	s.latestBlock = block

	return nil
}

// GetBlock retrieves a block by sequence number
func (s *LevelDBStorage) GetBlock(sequenceKey string) (*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	blockBytes, err := s.db.Get([]byte(sequenceKey), nil)
	if err != nil {
		return nil, fmt.Errorf("block not found: %w", err)
	}

	block := BlockFromBytes(blockBytes)
	return block, nil
}

// GetBlockByHeight retrieves a block by height
func (s *LevelDBStorage) GetBlockByHeight(height uint64) (*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.GetBlock(strconv.FormatUint(height, 10))
}

// GetLatestBlock returns the latest block
func (s *LevelDBStorage) GetLatestBlock() (*Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.latestBlock == nil {
		latestBlockBytes, err := s.db.Get([]byte("latest_block"), nil)
		if err != nil {
			return nil, fmt.Errorf("no latest block found: %w", err)
		}
		s.latestBlock = BlockFromBytes(latestBlockBytes)
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

	sequenceKey, err := s.db.Get([]byte("tx:"+txID), nil)
	if err != nil {
		return nil, fmt.Errorf("transaction not found: %w", err)
	}

	return s.GetBlock(string(sequenceKey))
}
