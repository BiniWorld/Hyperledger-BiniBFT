package main

import (
	"binibft-poc/consensus"
	"strconv"
)

func (*Node) Sign(msg []byte) []byte {
	return nil
}

func (n *Node) SignProposal(consensus.Proposal, []byte) *consensus.Signature {
	nodeId, _ := strconv.ParseUint(string(n.id), 10, 64)
	return &consensus.Signature{
		ID: nodeId,
	}
}
