package main

import (
	"reflect"
	"testing"

	bft "github.com/hyperledger/binibft-poc/consensus/pkg/types"
)

func TestRequestID(t *testing.T) {
	node := &Node{}
	tests := []struct {
		name     string
		input    Transaction
		expected bft.RequestInfo
	}{
		{
			name: "extract request info from simple transaction",
			input: Transaction{
				ClientID: "client-1",
				TS:       10,
				ID:       "tx-10",
				Data:     "payload",
			},
			expected: bft.RequestInfo{ClientID: "client-1", ID: "tx-10"},
		},
		{
			name: "extract request info from different client",
			input: Transaction{
				ClientID: "client-2",
				TS:       20,
				ID:       "tx-20",
				Data:     "payload-2",
			},
			expected: bft.RequestInfo{ClientID: "client-2", ID: "tx-20"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := node.RequestID(tt.input.ToBytes())
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("request info mismatch: got %+v want %+v", got, tt.expected)
			}
		})
	}
}
