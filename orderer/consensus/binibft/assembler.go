/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"crypto/sha256"
	"encoding/asn1"
	"sync/atomic"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	cb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric/protoutil"
)

// Assembler assembles BiniBFT proposals from batches of requests
type Assembler struct {
	RuntimeConfig   *atomic.Value
	VerificationSeq func() uint64
	Logger          *flogging.FabricLogger
}

// AssembleProposal assembles a proposal from metadata and batch of requests (same format as SmartBFT)
func (a *Assembler) AssembleProposal(metadata []byte, requests [][]byte) BiniBFTProposal {
	// Convert BiniBFT requests to raw bytes if needed
	var requestBytes [][]byte
	for _, req := range requests {
		requestBytes = append(requestBytes, req)
	}

	// Create a block exactly like SmartBFT
	rtc := a.RuntimeConfig.Load().(RuntimeConfig)

	lastConfigBlockNum := rtc.LastConfigBlock.Header.Number
	lastBlock := rtc.LastBlock

	if len(requests) == 0 {
		a.Logger.Panicf("Programming error, no requests in proposal")
	}

	// Process requests the same way as SmartBFT
	batchedRequests := a.singleConfigTxOrSeveralNonConfigTx(requestBytes, a.Logger)

	block := protoutil.NewBlock(lastBlock.Header.Number+1, protoutil.BlockHeaderHash(lastBlock.Header))
	block.Data = &cb.BlockData{Data: batchedRequests}
	block.Header.DataHash = protoutil.ComputeBlockDataHash(block.Data)

	if protoutil.IsConfigBlock(block) {
		lastConfigBlockNum = block.Header.Number
	}

	// Set metadata exactly like SmartBFT
	block.Metadata.Metadata[cb.BlockMetadataIndex_LAST_CONFIG] = protoutil.MarshalOrPanic(&cb.Metadata{
		Value: protoutil.MarshalOrPanic(&cb.LastConfig{Index: lastConfigBlockNum}),
	})
	block.Metadata.Metadata[cb.BlockMetadataIndex_SIGNATURES] = protoutil.MarshalOrPanic(&cb.Metadata{
		Value: protoutil.MarshalOrPanic(&cb.OrdererBlockMetadata{
			ConsenterMetadata: metadata,
			LastConfig: &cb.LastConfig{
				Index: lastConfigBlockNum,
			},
		}),
	})

	tuple := &ByteBufferTuple{
		A: protoutil.MarshalOrPanic(block.Data),
		B: protoutil.MarshalOrPanic(block.Metadata),
	}

	// Create proposal exactly like SmartBFT
	prop := BiniBFTProposal{
		Header:               protoutil.BlockHeaderBytes(block.Header),
		Payload:              tuple.ToBytes(),
		Metadata:             metadata,
		VerificationSequence: int64(a.VerificationSeq()),
	}

	a.Logger.Infof("Assembled proposal with %d requests, block number %d, dataHash %x",
		len(requests), block.Header.Number, block.Header.DataHash)

	// Log detailed block information for debugging
	a.Logger.Infof("Block metadata: LAST_CONFIG=%d, has SIGNATURES=%t",
		lastConfigBlockNum, len(block.Metadata.Metadata[cb.BlockMetadataIndex_SIGNATURES]) > 0)

	// COMPREHENSIVE REQUEST COMPARISON LOGGING
	a.Logger.Infof("=== BINIBFT REQUEST INPUT COMPARISON ===")
	a.Logger.Infof("Original requests: %d, Batched requests: %d", len(requests), len(batchedRequests))
	for i, req := range batchedRequests {
		reqHash := sha256.Sum256(req)
		a.Logger.Infof("Request[%d] Hash: %x, Size: %d bytes", i, reqHash[:8], len(req))

		// Try to parse request to get transaction ID
		if envelope, err := protoutil.UnmarshalEnvelope(req); err == nil {
			if payload, err := protoutil.UnmarshalPayload(envelope.Payload); err == nil {
				if chdr, err := protoutil.UnmarshalChannelHeader(payload.Header.ChannelHeader); err == nil {
					a.Logger.Infof("Request[%d] TxID: %s, Type: %s", i, chdr.TxId, cb.HeaderType_name[chdr.Type])
				}
			}
		}
	}
	a.Logger.Infof("=== END BINIBFT REQUEST INPUT ===")

	return prop
}

// singleConfigTxOrSeveralNonConfigTx processes requests the same way as SmartBFT
func (a *Assembler) singleConfigTxOrSeveralNonConfigTx(requests [][]byte, logger *flogging.FabricLogger) [][]byte {
	// Scan until a config transaction is found
	var batchedRequests [][]byte
	var i int
	for i < len(requests) {
		currentRequest := requests[i]
		envelope, err := protoutil.UnmarshalEnvelope(currentRequest)
		if err != nil {
			logger.Panicf("Programming error, received bad envelope but should have validated it: %v", err)
			continue
		}

		// If we saw a config transaction, we cannot add any more transactions to the batch.
		if protoutil.IsConfigTransaction(envelope) {
			break
		}

		// Else, it's not a config transaction, so add it to the batch.
		batchedRequests = append(batchedRequests, currentRequest)
		i++
	}

	// If we don't have any transaction in the batch, it is safe to assume we only
	// saw a single transaction which is a config transaction.
	if len(batchedRequests) == 0 {
		batchedRequests = [][]byte{requests[0]}
	}

	// At this point, batchedRequests contains either a single config transaction, or a few non config transactions.
	return batchedRequests
}

// DisassembleProposal disassembles a proposal back into its components (same format as SmartBFT)
func (a *Assembler) DisassembleProposal(proposal BiniBFTProposal) ([][]byte, error) {
	block, err := ProposalToBlock(proposal)
	if err != nil {
		a.Logger.Errorf("Failed to convert proposal to block: %v", err)
		return nil, err
	}

	requests := block.Data.Data
	a.Logger.Debugf("Disassembled proposal into %d requests", len(requests))

	return requests, nil
}

// ByteBufferTuple is the byte slice tuple
type ByteBufferTuple struct {
	A []byte
	B []byte
}

// ToBytes marshals the buffer tuple to bytes
func (bbt *ByteBufferTuple) ToBytes() []byte {
	bytes, err := asn1.Marshal(*bbt)
	if err != nil {
		panic(err)
	}
	return bytes
}

// FromBytes unmarshals bytes to a buffer tuple
func (bbt *ByteBufferTuple) FromBytes(bytes []byte) error {
	_, err := asn1.Unmarshal(bytes, bbt)
	return err
}
