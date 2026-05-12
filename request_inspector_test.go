package main

import (
	"reflect"
	"testing"

	bft "github.com/hyperledger/binibft-poc/consensus/pkg/types"
)

func TestRequestID(t *testing.T) {
	node := &Node{}

	tests := []struct {
		name string
		req  []byte
		want bft.RequestInfo
	}{
		{
			name: "Case 1: Valid transaction bytes",
			req: Transaction{
				ClientID: "client-A",
				ID:       "id-A",
			}.ToBytes(),
			want: bft.RequestInfo{
				ClientID: "client-A",
				ID:       "id-A",
			},
		},
		{
			name: "Case 2: Valid transaction bytes with empty strings",
			req: Transaction{
				ClientID: "",
				ID:       "",
			}.ToBytes(),
			want: bft.RequestInfo{
				ClientID: "",
				ID:       "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := node.RequestID(tt.req)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Node.RequestID() = %v, want %v", got, tt.want)
			}
		})
	}
}
