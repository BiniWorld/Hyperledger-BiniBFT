package main

import (
	"reflect"
	"testing"

	bft "github.com/hyperledger/binibft-poc/consensus/pkg/types"
)

func TestVerifyProposal(t *testing.T) {
	node := &Node{}
	tests := []struct {
		name     string
		payload  BlockData
		expected []bft.RequestInfo
	}{
		{
			name:     "empty proposal payload",
			payload:  BlockData{Transactions: [][]byte{}},
			expected: []bft.RequestInfo{},
		},
		{
			name: "proposal with multiple transactions",
			payload: BlockData{Transactions: [][]byte{
				Transaction{ClientID: "client-a", TS: 1, ID: "tx-a", Data: "alpha"}.ToBytes(),
				Transaction{ClientID: "client-b", TS: 2, ID: "tx-b", Data: "beta"}.ToBytes(),
			}},
			expected: []bft.RequestInfo{
				{ClientID: "client-a", ID: "tx-a"},
				{ClientID: "client-b", ID: "tx-b"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proposal := bft.Proposal{Payload: tt.payload.ToBytes()}
			got, err := node.VerifyProposal(proposal)
			if err != nil {
				t.Fatalf("VerifyProposal returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("request info mismatch: got %+v want %+v", got, tt.expected)
			}
		})
	}
}

func TestRequestsFromProposal(t *testing.T) {
	node := &Node{}
	tests := []struct {
		name     string
		payload  BlockData
		expected []bft.RequestInfo
	}{
		{
			name:     "empty proposal payload",
			payload:  BlockData{Transactions: [][]byte{}},
			expected: []bft.RequestInfo{},
		},
		{
			name: "proposal with one transaction",
			payload: BlockData{Transactions: [][]byte{
				Transaction{ClientID: "client-z", TS: 7, ID: "tx-z", Data: "zeta"}.ToBytes(),
			}},
			expected: []bft.RequestInfo{
				{ClientID: "client-z", ID: "tx-z"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proposal := bft.Proposal{Payload: tt.payload.ToBytes()}
			got := node.RequestsFromProposal(proposal)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("request info mismatch: got %+v want %+v", got, tt.expected)
			}
		})
	}
}
