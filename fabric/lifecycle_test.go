package fabric

import (
	"binibft-poc/consensus"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

// Helper to create a signed Fabric test envelope
func createTestEnvelope(t *testing.T, payload []byte, privKey *ecdsa.PrivateKey) *Envelope {
	t.Helper()
	h := sha256.Sum256(payload)
	sig, err := ecdsa.SignASN1(rand.Reader, privKey, h[:])
	if err != nil {
		t.Fatalf("failed to sign envelope: %v", err)
	}
	return &Envelope{
		Payload:   payload,
		Signature: sig,
	}
}

// Helper to create a fully initialized test chain backed by BiniBFT
func createTestChain(support ConsenterSupport, logger consensus.Logger) (Chain, error) {
	opts := ConsenterOptions{
		NodeID:        consensus.NodeID("orderer1"),
		ShardID:       consensus.ShardID(1),
		Role:          consensus.RolePrimaryLeader,
		PrimaryLeader: consensus.NodeID("orderer1"),
		ShardLeaders:  map[consensus.ShardID]consensus.NodeID{consensus.ShardID(1): consensus.NodeID("orderer1")},
		ShardNodes:    map[consensus.ShardID][]consensus.NodeID{consensus.ShardID(1): {consensus.NodeID("orderer1")}},
		Logger:        logger,
		Network:       &mockNetwork{},
	}
	consenter := NewConsenter(opts)
	return consenter.HandleChain(support, nil)
}

// TestChannelLifecycleGenesisToBlockStream tests full block stream creation and previous hash continuity
func TestChannelLifecycleGenesisToBlockStream(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	support := NewMockConsenterSupport("channel-production")

	// 1. Write Genesis Block (Block #0)
	genesisDataHash := sha256.Sum256([]byte("genesis-config-payload"))
	genesisBlock := &Block{
		Header: &BlockHeader{
			Number:       0,
			PreviousHash: nil,
			DataHash:     genesisDataHash[:],
		},
		Data: &BlockData{
			Data: [][]byte{[]byte("genesis-config-payload")},
		},
		Metadata: &BlockMetadata{
			Metadata: [][]byte{[]byte("genesis-metadata"), []byte("genesis-digest")},
		},
	}
	support.WriteConfigBlock(genesisBlock, []byte("genesis-metadata"))

	if support.Height() != 1 {
		t.Fatalf("expected support height 1 after genesis block, got %d", support.Height())
	}

	// 2. Initialize BiniBFT Chain via Consenter
	chain, err := createTestChain(support, logger)
	if err != nil {
		t.Fatalf("createTestChain failed: %v", err)
	}
	chain.Start()
	defer chain.Halt()

	// 3. Concurrently Order 30 Transactions from 3 Clients
	numClients := 3
	txsPerClient := 10
	var wg sync.WaitGroup
	errCh := make(chan error, numClients*txsPerClient)

	for c := 0; c < numClients; c++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			for i := 0; i < txsPerClient; i++ {
				payload := []byte(fmt.Sprintf("client-%d-tx-%d-timestamp-%d", clientID, i, time.Now().UnixNano()))
				env := createTestEnvelope(t, payload, privKey)
				if err := chain.Order(env, 0); err != nil {
					errCh <- err
				}
			}
		}(c)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent Order returned error: %v", err)
	}

	// 4. Simulate Consensus Engine Deliveries for 5 Blocks
	type BlockDataPayload struct {
		Transactions [][]byte
	}

	biniChain := chain.(*BiniBFTChain)
	for blockNum := uint64(1); blockNum <= 5; blockNum++ {
		tx1 := []byte(fmt.Sprintf("block-%d-tx-1", blockNum))
		tx2 := []byte(fmt.Sprintf("block-%d-tx-2", blockNum))
		rawPayload, _ := asn1.Marshal(BlockDataPayload{Transactions: [][]byte{tx1, tx2}})

		proposal := consensus.Proposal{
			Header:   []byte(fmt.Sprintf("proposal-header-%d", blockNum)),
			Payload:  rawPayload,
			Metadata: []byte(fmt.Sprintf("commit-qc-proof-%d", blockNum)),
		}

		if err := biniChain.Deliver(proposal); err != nil {
			t.Fatalf("Deliver failed for block %d: %v", blockNum, err)
		}
	}

	// 5. Validate Complete Chain Integrity (#0 to #5)
	if support.Height() != 6 {
		t.Fatalf("expected total 6 blocks, got %d", support.Height())
	}

	for num := uint64(1); num < support.Height(); num++ {
		prevBlock := support.Block(num - 1)
		currBlock := support.Block(num)

		if prevBlock == nil || currBlock == nil {
			t.Fatalf("missing block at height %d or %d", num-1, num)
		}

		// Verify Block Number sequence
		if currBlock.Header.Number != num {
			t.Fatalf("expected block number %d, got %d", num, currBlock.Header.Number)
		}

		// Verify PreviousHash chaining
		expectedPrevHash := prevBlock.ComputeHash()
		if !bytes.Equal(currBlock.Header.PreviousHash, expectedPrevHash) {
			t.Fatalf("previous hash mismatch at block %d:\nexpected: %x\ngot:      %x", num, expectedPrevHash, currBlock.Header.PreviousHash)
		}

		// Verify DataHash integrity
		dHash := sha256.New()
		for _, tx := range currBlock.Data.Data {
			dHash.Write(tx)
		}
		expectedDataHash := dHash.Sum(nil)
		if !bytes.Equal(currBlock.Header.DataHash, expectedDataHash) {
			t.Fatalf("data hash mismatch at block %d", num)
		}
	}
}

// TestChannelDynamicReconfiguration tests channel configuration updates (Config Blocks)
func TestChannelDynamicReconfiguration(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	support := NewMockConsenterSupport("channel-reconfig")

	chain, err := createTestChain(support, logger)
	if err != nil {
		t.Fatalf("createTestChain failed: %v", err)
	}
	chain.Start()
	defer chain.Halt()

	// 1. Submit standard transaction
	txEnv := createTestEnvelope(t, []byte("standard-user-tx"), privKey)
	if err := chain.Order(txEnv, 0); err != nil {
		t.Fatalf("Order failed: %v", err)
	}

	// 2. Submit Config transaction (e.g. shard rebalancing, adding an orderer node)
	configPayload := []byte("channel-update-config-envelope:add-shard-3")
	configEnv := createTestEnvelope(t, configPayload, privKey)
	if err := chain.Configure(configEnv, 1); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	biniChain := chain.(*BiniBFTChain)

	// Deliver normal block
	normalProposal := consensus.Proposal{
		Header:   []byte("normal-header"),
		Payload:  txEnv.Payload,
		Metadata: []byte("commit-qc-normal"),
	}
	if err := biniChain.Deliver(normalProposal); err != nil {
		t.Fatalf("Deliver normal proposal failed: %v", err)
	}

	// Deliver config block
	configProposal := consensus.Proposal{
		Header:   []byte("config-header"),
		Payload:  configEnv.Payload,
		Metadata: []byte("commit-qc-config"),
	}
	if err := biniChain.Deliver(configProposal); err != nil {
		t.Fatalf("Deliver config proposal failed: %v", err)
	}

	if support.Height() != 2 {
		t.Fatalf("expected 2 blocks after normal and config delivery, got %d", support.Height())
	}

	block1 := support.Block(1)
	if !bytes.Equal(block1.Metadata.Metadata[0], []byte("commit-qc-config")) {
		t.Fatalf("expected config commit QC in block metadata")
	}
}

// TestMultiChannelIsolation verifies that separate Fabric channels maintain isolated consensus state
func TestMultiChannelIsolation(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	supportAlpha := NewMockConsenterSupport("channel-alpha")
	supportBeta := NewMockConsenterSupport("channel-beta")

	chainAlpha, err := createTestChain(supportAlpha, logger)
	if err != nil {
		t.Fatalf("createTestChain alpha failed: %v", err)
	}
	chainBeta, err := createTestChain(supportBeta, logger)
	if err != nil {
		t.Fatalf("createTestChain beta failed: %v", err)
	}

	chainAlpha.Start()
	chainBeta.Start()
	defer chainAlpha.Halt()
	defer chainBeta.Halt()

	// Order on Channel Alpha
	envAlpha := createTestEnvelope(t, []byte("alpha-payload"), privKey)
	if err := chainAlpha.Order(envAlpha, 0); err != nil {
		t.Fatalf("Alpha Order failed: %v", err)
	}

	// Order on Channel Beta
	envBeta := createTestEnvelope(t, []byte("beta-payload"), privKey)
	if err := chainBeta.Order(envBeta, 0); err != nil {
		t.Fatalf("Beta Order failed: %v", err)
	}

	biniAlpha := chainAlpha.(*BiniBFTChain)
	biniBeta := chainBeta.(*BiniBFTChain)

	// Deliver 3 blocks to Alpha
	for i := 0; i < 3; i++ {
		p := consensus.Proposal{Header: []byte("alpha-hdr"), Payload: []byte(fmt.Sprintf("alpha-%d", i)), Metadata: []byte("qc-alpha")}
		biniAlpha.Deliver(p)
	}

	// Deliver 1 block to Beta
	pBeta := consensus.Proposal{Header: []byte("beta-hdr"), Payload: []byte("beta-0"), Metadata: []byte("qc-beta")}
	biniBeta.Deliver(pBeta)

	// Verify Isolation
	if supportAlpha.Height() != 3 {
		t.Fatalf("expected Channel Alpha height 3, got %d", supportAlpha.Height())
	}
	if supportBeta.Height() != 1 {
		t.Fatalf("expected Channel Beta height 1, got %d", supportBeta.Height())
	}
	if supportAlpha.ChannelID() != "channel-alpha" || supportBeta.ChannelID() != "channel-beta" {
		t.Fatalf("channel ID mixup detected")
	}
}

// TestChainHaltRejection verifies immediate and safe error handling upon chain halt
func TestChainHaltRejection(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	support := NewMockConsenterSupport("channel-halt")

	chain, err := createTestChain(support, logger)
	if err != nil {
		t.Fatalf("createTestChain failed: %v", err)
	}
	chain.Start()

	env := createTestEnvelope(t, []byte("test-payload"), privKey)
	if err := chain.Order(env, 0); err != nil {
		t.Fatalf("expected order to succeed before halt: %v", err)
	}

	// Halt chain
	chain.Halt()

	// All subsequent operations must be rejected
	if err := chain.Order(env, 0); err == nil {
		t.Fatalf("expected Order to fail on halted chain")
	}
	if err := chain.Configure(env, 1); err == nil {
		t.Fatalf("expected Configure to fail on halted chain")
	}
	if err := chain.WaitReady(); err == nil {
		t.Fatalf("expected WaitReady to fail on halted chain")
	}
}
