/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"encoding/json"
	"sync/atomic"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
)

// Assembler assembles BiniBFT proposals from batches of requests
type Assembler struct {
	RuntimeConfig   *atomic.Value
	VerificationSeq func() uint64
	Logger          *flogging.FabricLogger
}

// AssembleProposal assembles a proposal from metadata and batch of requests
func (a *Assembler) AssembleProposal(metadata []byte, batch []*BiniBFTRequest) BiniBFTProposal {
	// Serialize the batch
	batchData, err := json.Marshal(batch)
	if err != nil {
		a.Logger.Errorf("Failed to marshal batch: %v", err)
		batchData = []byte{}
	}

	// Create block header
	header := map[string]interface{}{
		"batchSize": len(batch),
		"timestamp": "now", // Would use actual timestamp
	}
	headerBytes, _ := json.Marshal(header)

	// Create proposal
	proposal := BiniBFTProposal{
		Payload:              batchData,
		Header:               headerBytes,
		Metadata:             metadata,
		VerificationSequence: int64(a.VerificationSeq()),
	}

	a.Logger.Debugf("Assembled proposal with %d requests", len(batch))

	return proposal
}

// DisassembleProposal disassembles a proposal back into its components
func (a *Assembler) DisassembleProposal(proposal BiniBFTProposal) ([]*BiniBFTRequest, error) {
	var batch []*BiniBFTRequest
	if err := json.Unmarshal(proposal.Payload, &batch); err != nil {
		a.Logger.Errorf("Failed to unmarshal proposal payload: %v", err)
		return nil, err
	}

	a.Logger.Debugf("Disassembled proposal into %d requests", len(batch))

	return batch, nil
}
