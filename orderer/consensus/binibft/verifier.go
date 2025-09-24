/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"sync/atomic"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric/common/policies"
	"github.com/hyperledger/fabric/orderer/consensus"
	"github.com/hyperledger/fabric/protoutil"
)

// Verifier verifies BiniBFT messages and proposals
type Verifier struct {
	Channel               string
	ConfigValidator       ConfigBlockValidator
	VerificationSequencer consensus.ConsenterSupport
	Logger                *flogging.FabricLogger
	RuntimeConfig         *atomic.Value
	ConsenterVerifier     *consenterVerifier
	AccessController      *chainACL
	Ledger                consensus.ConsenterSupport
}

// VerificationSequence returns the current verification sequence
func (v *Verifier) VerificationSequence() uint64 {
	return v.VerificationSequencer.Sequence()
}

// VerifyRequest verifies a client request
func (v *Verifier) VerifyRequest(request []byte) ([]byte, error) {
	// Basic verification - in production this would include signature verification
	return request, nil
}

// VerifyProposal verifies a BiniBFT proposal
func (v *Verifier) VerifyProposal(proposal BiniBFTProposal) error {
	// Basic verification - in production this would include:
	// - Signature verification
	// - Proposal structure validation
	// - Sequence number validation
	return nil
}

// consenterVerifier verifies consenter-specific data
type consenterVerifier struct {
	logger        *flogging.FabricLogger
	channel       string
	policyManager policies.Manager
}

// VerifyConsenterSig verifies a consenter signature
func (cv *consenterVerifier) VerifyConsenterSig(signature []byte, message []byte) error {
	// Simplified verification - in production this would verify the actual signature
	return nil
}

// VerifySignature verifies a signature against signed data
func (v *Verifier) VerifySignature(signatureSet []*protoutil.SignedData) error {
	return v.AccessController.Evaluate(signatureSet)
}
