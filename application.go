package main

import (
	"binibft-poc/consensus"
	"binibft-poc/consensus/protos"
	"crypto/sha256"
	"fmt"

	"google.golang.org/protobuf/proto"
)

// Deliver processes finalized proposals and creates blocks for the application
func (n *Node) Deliver(proposal consensus.Proposal) error {
	blockData := BlockDataFromBytes(proposal.Payload)
	metadata := &protos.ViewMetadata{}
	if err := proto.Unmarshal(proposal.Metadata, metadata); err != nil {
		return fmt.Errorf("unable to unmarshal metadata: %v", err)
	}

	n.logger.Info("Node received proposal for delivery",
		"nodeID", n.id,
		"sequence", metadata.LatestSequence,
		"transactionCount", len(blockData.Transactions))

	// Convert raw transaction bytes to Transaction structs
	txns := make([]consensus.Transaction, 0, len(blockData.Transactions))
	for _, rawTxn := range blockData.Transactions {
		txn := TransactionFromBytes(rawTxn)
		txns = append(txns, consensus.Transaction{
			ClientID: txn.ClientID,
			TS:       txn.TS,
			ID:       txn.ID,
			Data:     txn.Data,
		})
	}

	// Extract block header information
	header := BlockHeaderFromBytes(proposal.Header)

	// Store the block using the consensus storage system
	block := &consensus.Block{
		Sequence:     header.Sequence,
		PrevHash:     header.PrevHash,
		Metadata:     proposal.Metadata,
		Transactions: txns,
	}

	// Store block using the storage interface
	if err := n.storage.StoreBlock(block); err != nil {
		n.logger.Error("Failed to store block in application delivery",
			"error", err,
			"sequence", block.Sequence)
		return fmt.Errorf("failed to store block: %v", err)
	}

	// Update prevHash for next block
	n.prevHash = fmt.Sprintf("%x", sha256.Sum256(block.ToBytes()))

	// Deliver block to the chain listener
	select {
	case n.deliverChan <- block:
		n.logger.Info("Block delivered to application chain",
			"sequence", block.Sequence,
			"transactionCount", len(block.Transactions))
	default:
		n.logger.Info("Block delivery channel full, dropping block",
			"sequence", block.Sequence)
	}

	n.logger.Info("Application delivery completed successfully",
		"nodeID", n.id,
		"sequence", block.Sequence)

	return nil
}
