package main

import (
	"binibft-poc/consensus"
	"time"
)

type Chain struct {
	deliverChan <-chan *Block
	node        *Node
}

func NewChain(
	id string,
	address string,
	opsAddress string,
	mapNodes map[string]*NodeInfo,
	logger consensus.Logger,
	opts NetworkOptions,
	walDir string,
	blocksDir string,
	shardId consensus.ShardID,
	shardLeaderId consensus.NodeID,
	followers []consensus.NodeID,
	role consensus.NodeRole,
	primaryId consensus.NodeID,
	clusterConfig clusterConfig, // Add cluster config parameter
) *Chain {
	deliverChan := make(chan *Block)
	node := NewNode(
		consensus.NodeID(id),
		address,
		opsAddress,
		mapNodes,
		deliverChan,
		logger,
		opts,
		walDir,
		blocksDir,
		shardId,
		shardLeaderId,
		followers,
		role,
		primaryId,
		clusterConfig, // Pass cluster config to NewNode
	)
	return &Chain{
		node:        node,
		deliverChan: deliverChan,
	}
}

func (chain *Chain) Listen() Block {
	block := <-chain.deliverChan
	return *block
}

func (chain *Chain) Order(txn Transaction) error {
	// Create a consensus request from the transaction
	request := &consensus.Request{
		ID:        txn.ID,
		Data:      txn.ToBytes(),
		ClientID:  txn.ClientID,
		Timestamp: time.Now(),
		Phase:     consensus.PhasePrePrep,
		ShardID:   chain.node.shardId,
	}

	// Submit the request to the consensus system
	return chain.node.consensus.SubmitRequest(request)
}
