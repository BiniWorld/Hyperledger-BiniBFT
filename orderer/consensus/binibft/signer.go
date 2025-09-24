/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"encoding/json"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	cb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric/protoutil"
)

// Signer signs messages for BiniBFT consensus
type Signer struct {
	ID               uint64
	Logger           *flogging.FabricLogger
	SignerSerializer SignerSerializer
}

// Signature represents a BiniBFT signature
type Signature struct {
	IdentifierHeader     []byte
	BlockHeader          []byte
	OrdererBlockMetadata []byte
}

// Marshal serializes the signature
func (s *Signature) Marshal() []byte {
	data, _ := json.Marshal(s)
	return data
}

// Unmarshal deserializes the signature
func (s *Signature) Unmarshal(data []byte) error {
	return json.Unmarshal(data, s)
}

// Sign signs a message and returns the signature
func (s *Signer) Sign(message []byte) ([]byte, error) {
	return s.SignerSerializer.Sign(message)
}

// SignProposal signs a BiniBFT proposal
func (s *Signer) SignProposal(proposal BiniBFTProposal) (*Signature, error) {
	// Create identifier header
	idHeader := &cb.IdentifierHeader{
		Identifier: uint32(s.ID),
	}

	idHeaderBytes := protoutil.MarshalOrPanic(idHeader)

	// Create signature
	sig := &Signature{
		IdentifierHeader:     idHeaderBytes,
		BlockHeader:          proposal.Header,
		OrdererBlockMetadata: proposal.Metadata,
	}

	return sig, nil
}
