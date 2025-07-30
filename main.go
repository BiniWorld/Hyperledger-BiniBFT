package main

import (
	"binibft-poc/consensus"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"
)

type Shard struct {
	LeaderId  consensus.NodeID
	Followers []consensus.NodeID
}

type clusterConfig struct {
	primaryId consensus.NodeID
	shards    map[consensus.ShardID]Shard
}

func main() {

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	numNodes := 5

	networkOpts := NetworkOptions{
		NumNodes:     numNodes,
		BatchSize:    10,
		BatchTimeout: 2 * time.Second,
	}

	clusterConfig := clusterConfig{
		primaryId: "1", // Node 1 is the primary leader coordinating all shards
		shards: map[consensus.ShardID]Shard{
			1: {
				LeaderId:  "2", // Node 2 leads shard 0
				Followers: []consensus.NodeID{"3", "4", "5"},
			},
			// 1: {
			// 	LeaderId:  "4", // Node 4 leads shard 1
			// 	Followers: []consensus.NodeID{"5"},
			// },
		},
	}

	chains := make(map[int]*Chain)

	mapNodes := make(map[string]*NodeInfo)
	for id := 1; id <= networkOpts.NumNodes; id++ {
		mapNodes[fmt.Sprintf("%d", id)] = &NodeInfo{
			ID:         fmt.Sprintf("%d", id),
			Address:    fmt.Sprintf("localhost:%d", 10000+id),
			OpsAddress: fmt.Sprintf("localhost:%d", 20000+id),
		}
	}

	for id := 1; id <= networkOpts.NumNodes; id++ {
		logger.Info("Initializing node", "nodeId", id)
		address := mapNodes[fmt.Sprintf("%d", id)].Address
		opsAddress := mapNodes[fmt.Sprintf("%d", id)].OpsAddress

		walDir := fmt.Sprintf("./data/node%d", id)
		blocksDir := fmt.Sprintf("./blocks/node%d", id)

		var role consensus.NodeRole
		var shardId consensus.ShardID
		var shardLeaderId consensus.NodeID
		var followers []consensus.NodeID

		nodeIDStr := consensus.NodeID(fmt.Sprintf("%d", id))

		if nodeIDStr == clusterConfig.primaryId {
			// Primary leader - assign to a special shard or manage all shards
			shardId = 0 // Primary leader can be associated with shard 0 for simplicity
			role = consensus.RolePrimaryLeader
			followers = []consensus.NodeID{} // Primary leader doesn't have direct followers
			shardLeaderId = clusterConfig.primaryId
		} else {
			// Check if this node is a shard leader
			found := false
			for sid, shard := range clusterConfig.shards {
				if nodeIDStr == shard.LeaderId {
					shardId = sid
					role = consensus.RoleShardLeader
					followers = shard.Followers
					shardLeaderId = shard.LeaderId
					found = true
					break
				}
			}

			// If not a shard leader, check if it's a follower
			if !found {
				for sid, shard := range clusterConfig.shards {
					if slices.Contains(shard.Followers, nodeIDStr) {
						shardId = sid
						role = consensus.RoleShardFollower
						followers = []consensus.NodeID{} // Followers don't have followers
						shardLeaderId = shard.LeaderId
						found = true
						break
					}
				}
			}

			// If node is not found in any shard configuration, log an error
			if !found {
				logger.Error("Node not found in cluster configuration", "nodeId", id)
				continue
			}
		}

		chain := NewChain(
			fmt.Sprintf("%d", id),
			address,
			opsAddress,
			mapNodes,
			logger.With("nodeId", id).With("address", address),
			networkOpts,
			walDir,
			blocksDir,
			shardId,
			shardLeaderId,
			followers,
			role,
			clusterConfig.primaryId,
			clusterConfig, // Pass the entire cluster configuration
		)
		go func() {
			nodeID := id
			_ = nodeID
			for {
				block := chain.Listen()
				// _ = block
				logger.Info(fmt.Sprintf("Node: %d block: %v", nodeID, block))
			}
		}()
		chains[id] = chain
	}
	select {}
}
