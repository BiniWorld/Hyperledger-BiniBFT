package fabric

import (
	"binibft-poc/consensus"
	"context"
	"crypto/sha256"
	"encoding/asn1"
	"fmt"
	"sync"
	"time"
)

// BiniBFTChain implements the Fabric consensus.Chain interface backed by BiniBFT
type BiniBFTChain struct {
	support         ConsenterSupport
	consensus       *consensus.Consensus
	nodeID          consensus.NodeID
	shardID         consensus.ShardID
	role            consensus.NodeRole
	channelID       string
	errorChan       chan struct{}
	haltChan        chan struct{}
	logger          consensus.Logger
	mu              sync.RWMutex
	started         bool
	lastBlockNumber uint64
	lastBlockHash   []byte
}

// NewBiniBFTChain creates a new BiniBFTChain instance
func NewBiniBFTChain(
	support ConsenterSupport,
	biniConsensus *consensus.Consensus,
	nodeID consensus.NodeID,
	shardID consensus.ShardID,
	role consensus.NodeRole,
	logger consensus.Logger,
) *BiniBFTChain {
	lastHeight := support.Height()
	var lastHash []byte
	var lastNum uint64 = 0

	if lastHeight > 0 {
		lastNum = lastHeight - 1
		if latestBlock := support.Block(lastNum); latestBlock != nil {
			lastHash = latestBlock.ComputeHash()
		}
	}

	return &BiniBFTChain{
		support:         support,
		consensus:       biniConsensus,
		nodeID:          nodeID,
		shardID:         shardID,
		role:            role,
		channelID:       support.ChannelID(),
		errorChan:       make(chan struct{}),
		haltChan:        make(chan struct{}),
		logger:          logger,
		lastBlockNumber: lastNum,
		lastBlockHash:   lastHash,
	}
}

// Order submits a normal transaction envelope for consensus ordering
func (c *BiniBFTChain) Order(env *Envelope, configSeq uint64) error {
	if env == nil {
		return fmt.Errorf("cannot order nil envelope")
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	select {
	case <-c.haltChan:
		return fmt.Errorf("chain is halted")
	default:
	}

	rawEnv, err := env.ToBytes()
	if err != nil {
		return fmt.Errorf("failed to encode transaction envelope: %w", err)
	}

	if c.consensus == nil {
		return fmt.Errorf("consensus engine not initialized")
	}

	return c.consensus.SubmitRequest(rawEnv)
}

// Configure submits a configuration transaction envelope for consensus ordering
func (c *BiniBFTChain) Configure(config *Envelope, configSeq uint64) error {
	if config == nil {
		return fmt.Errorf("cannot configure nil config envelope")
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	select {
	case <-c.haltChan:
		return fmt.Errorf("chain is halted")
	default:
	}

	rawConfig, err := config.ToBytes()
	if err != nil {
		return fmt.Errorf("failed to encode config envelope: %w", err)
	}

	if c.consensus == nil {
		return fmt.Errorf("consensus engine not initialized")
	}

	return c.consensus.SubmitRequest(rawConfig)
}

// Deliver receives finalized proposals from BiniBFT and writes them as Fabric blocks
func (c *BiniBFTChain) Deliver(proposal consensus.Proposal) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Extract transaction raw bytes from proposal payload
	var rawTxns [][]byte
	var blockData struct {
		Transactions [][]byte
	}
	if _, err := asn1.Unmarshal(proposal.Payload, &blockData); err == nil && len(blockData.Transactions) > 0 {
		rawTxns = blockData.Transactions
	} else {
		rawTxns = [][]byte{proposal.Payload}
	}

	// Compute DataHash over all transactions
	dHash := sha256.New()
	for _, tx := range rawTxns {
		dHash.Write(tx)
	}
	dataHash := dHash.Sum(nil)

	nextNumber := c.lastBlockNumber + 1
	if c.support.Height() == 0 {
		nextNumber = 0
	}

	fabricBlock := &Block{
		Header: &BlockHeader{
			Number:       nextNumber,
			PreviousHash: c.lastBlockHash,
			DataHash:     dataHash,
		},
		Data: &BlockData{
			Data: rawTxns,
		},
		Metadata: &BlockMetadata{
			Metadata: [][]byte{
				proposal.Metadata,
				[]byte(proposal.Digest()),
			},
		},
	}

	c.support.WriteBlock(fabricBlock, proposal.Metadata)
	c.lastBlockNumber = nextNumber
	c.lastBlockHash = fabricBlock.ComputeHash()

	if c.logger != nil {
		c.logger.Info("Delivered Fabric block from BiniBFT consensus",
			"channel", c.channelID,
			"blockNumber", nextNumber,
			"txCount", len(rawTxns))
	}

	return nil
}

// ReceiveHeartbeat satisfies consensus.ApplicationDelivery interface
func (c *BiniBFTChain) ReceiveHeartbeat(from consensus.NodeID, timestamp time.Time) {}

// Start starts the consensus chain
func (c *BiniBFTChain) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return
	}
	c.started = true
	if c.consensus != nil {
		_ = c.consensus.Start(context.Background())
	}
}

// Halt stops the consensus chain
func (c *BiniBFTChain) Halt() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return
	}
	c.started = false
	close(c.haltChan)

	if c.consensus != nil {
		_ = c.consensus.Stop()
	}
}

// WaitReady blocks until the consensus engine is ready to accept transactions
func (c *BiniBFTChain) WaitReady() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	select {
	case <-c.haltChan:
		return fmt.Errorf("chain is halted")
	default:
		return nil
	}
}

// Errored returns a channel that is closed if the chain encounters an unrecoverable error
func (c *BiniBFTChain) Errored() <-chan struct{} {
	return c.errorChan
}
