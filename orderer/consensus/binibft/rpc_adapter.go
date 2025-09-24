/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package binibft

import (
	"time"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric/orderer/common/cluster"
)

// RPCAdapter adapts cluster.RPC to RPC interface for BiniBFT
type RPCAdapter struct {
	*cluster.RPC
}

// NewRPCAdapter creates a new RPC adapter using cluster.RPC
func NewRPCAdapter(channel string, comm cluster.Communicator, logger *flogging.FabricLogger) *RPCAdapter {
	return &RPCAdapter{
		RPC: &cluster.RPC{
			Logger:        logger,
			Channel:       channel,
			StreamsByType: cluster.NewStreamsByType(),
			Comm:          comm,
			Timeout:       5 * time.Minute,
		},
	}
}
