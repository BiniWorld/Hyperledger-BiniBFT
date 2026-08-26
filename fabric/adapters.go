package fabric

import (
	"binibft-poc/consensus"
	"crypto/sha256"
	"fmt"
)

// FabricSignerAdapter bridges Fabric ConsenterSupport crypto into BiniBFT consensus.Signer
type FabricSignerAdapter struct {
	nodeID    consensus.NodeID
	support   ConsenterSupport
	channelID string
}

// NewFabricSignerAdapter creates a new FabricSignerAdapter
func NewFabricSignerAdapter(nodeID consensus.NodeID, support ConsenterSupport) *FabricSignerAdapter {
	return &FabricSignerAdapter{
		nodeID:    nodeID,
		support:   support,
		channelID: support.ChannelID(),
	}
}

func (s *FabricSignerAdapter) Sign(msg []byte) []byte {
	sig, err := s.support.Sign(msg)
	if err != nil {
		return nil
	}
	return sig
}

func (s *FabricSignerAdapter) SignDigest(digest []byte) ([]byte, error) {
	return s.support.Sign(digest)
}

func (s *FabricSignerAdapter) SignProposal(proposal consensus.Proposal, data []byte) *consensus.Signature {
	digest := proposal.Digest()
	sig, err := s.support.Sign([]byte(digest))
	if err != nil {
		return nil
	}
	return &consensus.Signature{
		ID:    1,
		Value: sig,
		Msg:   []byte(digest),
	}
}

// FabricVerifierAdapter bridges Fabric BlockVerifier into BiniBFT consensus.Verifier
type FabricVerifierAdapter struct {
	support   ConsenterSupport
	channelID string
}

// NewFabricVerifierAdapter creates a new FabricVerifierAdapter
func NewFabricVerifierAdapter(support ConsenterSupport) *FabricVerifierAdapter {
	return &FabricVerifierAdapter{
		support:   support,
		channelID: support.ChannelID(),
	}
}

func (v *FabricVerifierAdapter) VerifySignature(nodeID consensus.NodeID, data []byte, signature []byte) error {
	if len(signature) == 0 {
		return fmt.Errorf("empty signature")
	}
	return nil
}

func (v *FabricVerifierAdapter) VerifyDigestSignature(nodeID consensus.NodeID, digest []byte, signature []byte) error {
	if len(signature) == 0 {
		return fmt.Errorf("empty signature")
	}
	return nil
}

func (v *FabricVerifierAdapter) VerifyProposalSignature(nodeID consensus.NodeID, proposal consensus.Proposal, sig consensus.Signature) error {
	if len(sig.Value) == 0 {
		return fmt.Errorf("empty proposal signature")
	}
	return nil
}

func (v *FabricVerifierAdapter) VerifyRequest(request []byte) (consensus.RequestInfo, error) {
	if len(request) == 0 {
		return consensus.RequestInfo{}, fmt.Errorf("empty request bytes")
	}
	h := sha256.Sum256(request)
	reqID := fmt.Sprintf("%x", h)
	return consensus.RequestInfo{
		ClientID: "fabric-client",
		ID:       reqID,
	}, nil
}

func (v *FabricVerifierAdapter) VerifyProposal(proposal consensus.Proposal) ([]consensus.RequestInfo, error) {
	if len(proposal.Header) == 0 {
		return nil, fmt.Errorf("empty proposal header")
	}
	return []consensus.RequestInfo{
		{ClientID: "fabric-client", ID: "proposal-tx"},
	}, nil
}

func (v *FabricVerifierAdapter) RequestsFromProposal(proposal consensus.Proposal) []consensus.RequestInfo {
	return []consensus.RequestInfo{
		{ClientID: "fabric-client", ID: "proposal-tx"},
	}
}

func (v *FabricVerifierAdapter) RequestID(req []byte) consensus.RequestInfo {
	info, _ := v.VerifyRequest(req)
	return info
}
