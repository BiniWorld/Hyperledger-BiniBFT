package main

import (
	"binibft-poc/consensus"
)

type Chain struct {
	deliverChan <-chan *consensus.Block
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
	deliverChan := make(chan *consensus.Block)
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

func (chain *Chain) Listen() consensus.Block {
	block := <-chain.deliverChan
	return *block
}

func (chain *Chain) Order(txn Transaction) error {
	return chain.node.consensus.SubmitRequest(txn.ToBytes())
}
