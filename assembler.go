package main

import (
	"binibft-poc/consensus"
	"binibft-poc/consensus/protos"

	"github.com/golang/protobuf/proto"
)

// AssembleProposal creates a block proposal from transaction requests safely
func (n *Node) AssembleProposal(metadata []byte, requests [][]byte) consensus.Proposal {
	n.logger.Info("Node assembling proposal", "nodeID", n.id, "requestCount", len(requests))

	blockData := BlockData{
		Transactions: requests,
	}.ToBytes()

	var latestSeq uint64 = 0
	if len(metadata) > 0 {
		md := &protos.ViewMetadata{}
		if err := proto.Unmarshal(metadata, md); err == nil {
			latestSeq = md.LatestSequence
		} else {
			n.logger.Error("Failed to unmarshal metadata in AssembleProposal", "error", err)
		}
	}

	header := BlockHeader{
		PrevHash: n.prevHash,
		DataHash: computeDigest(blockData),
		Sequence: int64(latestSeq),
	}.ToBytes()

	return consensus.Proposal{
		Header:   header,
		Payload:  blockData,
		Metadata: metadata,
	}
}
