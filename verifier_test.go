package main

import (
	"reflect"
	"testing"

	bft "github.com/hyperledger/binibft-poc/consensus/pkg/types"
)

func TestVerifyProposal(t *testing.T) {
	node := &Node{}

	tx1 := Transaction{ClientID: "client-1", ID: "tx-1"}
	tx2 := Transaction{ClientID: "client-2", ID: "tx-2"}

	blockData1 := BlockData{
		Transactions: [][]byte{tx1.ToBytes(), tx2.ToBytes()},
	}

	emptyBlockData := BlockData{Transactions: [][]byte{}}

	tests := []struct {
		name     string
		proposal bft.Proposal
		want     []bft.RequestInfo
		wantErr  bool
	}{
		{
			name: "Case 1: Proposal with multiple transactions",
			proposal: bft.Proposal{
				Payload: blockData1.ToBytes(),
			},
			want: []bft.RequestInfo{
				{ClientID: "client-1", ID: "tx-1"},
				{ClientID: "client-2", ID: "tx-2"},
			},
			wantErr: false,
		},
		{
			name: "Case 2: Proposal with no transactions",
			proposal: bft.Proposal{
				Payload: emptyBlockData.ToBytes(),
			},
			want:    []bft.RequestInfo{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := node.VerifyProposal(tt.proposal)
			if (err != nil) != tt.wantErr {
				t.Errorf("Node.VerifyProposal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Node.VerifyProposal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequestsFromProposal(t *testing.T) {
	node := &Node{}

	tx1 := Transaction{ClientID: "client-3", ID: "tx-3"}

	blockData1 := BlockData{
		Transactions: [][]byte{tx1.ToBytes()},
	}

	emptyBlockData := BlockData{Transactions: [][]byte{}}

	tests := []struct {
		name     string
		proposal bft.Proposal
		want     []bft.RequestInfo
	}{
		{
			name: "Case 1: Proposal with one transaction",
			proposal: bft.Proposal{
				Payload: blockData1.ToBytes(),
			},
			want: []bft.RequestInfo{
				{ClientID: "client-3", ID: "tx-3"},
			},
		},
		{
			name: "Case 2: Empty proposal payload",
			proposal: bft.Proposal{
				Payload: emptyBlockData.ToBytes(),
			},
			want: []bft.RequestInfo{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := node.RequestsFromProposal(tt.proposal)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Node.RequestsFromProposal() = %v, want %v", got, tt.want)
			}
		})
	}
}
