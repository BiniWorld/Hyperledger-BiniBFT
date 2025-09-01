package main

import (
	"binibft-poc/consensus"
	"binibft-poc/consensus/protos"
	"fmt"

	"github.com/golang/protobuf/proto"
)

// AssembleProposal creates a block proposal from transaction requests
func (n *Node) AssembleProposal(metadata []byte, requests [][]byte) consensus.Proposal {
	n.logger.Info("Node assembling proposal", "nodeID", n.id, "requestCount", len(requests))

	blockData := BlockData{
		Transactions: requests,
	}.ToBytes()

	md := &protos.ViewMetadata{}
	if err := proto.Unmarshal(metadata, md); err != nil {
		panic(fmt.Sprintf("Unable to unmarshal metadata, error: %v", err))
	}
	return consensus.Proposal{
		Header: BlockHeader{
			PrevHash: n.prevHash,
			DataHash: computeDigest(blockData),
			Sequence: int64(md.LatestSequence),
		}.ToBytes(),
		Payload:  BlockData{Transactions: requests}.ToBytes(),
		Metadata: metadata,
	}
}
