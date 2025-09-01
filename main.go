package main

import (
	"binibft-poc/consensus"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"os"
	"slices"
	"strconv"
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
	shardMap := make(map[consensus.ShardID]Shard)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: false,
		Level:     slog.LevelDebug,
	}))
	numNodes := 5

	networkOpts := NetworkOptions{
		NumNodes:     numNodes,
		BatchSize:    10,
		BatchTimeout: 2 * time.Second,
	}

	primary, shards := generateShardsWithRandomAssignment(numNodes, 2)
	for i, shard := range shards {
		shardID := consensus.ShardID(i + 1) // Assuming ShardID is int-based
		shardMap[shardID] = shard
	}
	fmt.Printf("primary: %v\n", primary)
	fmt.Printf("shards: %v\n", shards)
	clusterConfig := clusterConfig{
		primaryId: primary,
		shards:    shardMap,
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

func calculateSecondaryLeaderCount(totalNodes int, maxSecondaryLeaders int) int {
	remainingNodes := totalNodes - 1 // excluding primary

	idealSecondaryLeaders := int(math.Min(float64(remainingNodes/7), float64(maxSecondaryLeaders)))
	if idealSecondaryLeaders < 3 {
		idealSecondaryLeaders = 3
	}
	if idealSecondaryLeaders%2 == 0 {
		idealSecondaryLeaders++
	}

	const maxFollowersPerLeader = 9
	for idealSecondaryLeaders > 1 {
		availableFollowers := remainingNodes - idealSecondaryLeaders
		if availableFollowers/idealSecondaryLeaders > maxFollowersPerLeader {
			idealSecondaryLeaders--
		} else {
			break
		}
	}
	return idealSecondaryLeaders
}

// Distributes followers evenly across leaders
func distributeFollowers(numFollowers, numLeaders int) []int {
	followersPerLeader := make([]int, numLeaders)
	for i := range followersPerLeader {
		followersPerLeader[i] = numFollowers / numLeaders
	}
	for i := 0; i < numFollowers%numLeaders; i++ {
		followersPerLeader[i]++
	}
	return followersPerLeader
}

func generateRandomNodeIDs(total int) []consensus.NodeID {
	nodes := make([]consensus.NodeID, total)
	for i := 0; i < total; i++ {
		nodes[i] = consensus.NodeID(strconv.Itoa(i + 1))
	}
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })
	return nodes
}

func generateShardsWithRandomAssignment(totalNodes int, maxSecondaryLeaders int) (consensus.NodeID, []Shard) {
	if totalNodes < 4 {
		panic("Need at least 4 nodes (1 primary + 3 secondary leaders) to form shards")
	}
	if maxSecondaryLeaders > totalNodes-1 {
		maxSecondaryLeaders = totalNodes - 1
	}

	allNodes := generateRandomNodeIDs(totalNodes)
	primaryLeader := allNodes[0]
	remainingNodes := allNodes[1:]

	numSecondaryLeaders := calculateSecondaryLeaderCount(totalNodes, maxSecondaryLeaders)

	if numSecondaryLeaders >= len(remainingNodes) {
		panic("Not enough nodes to assign as secondary leaders")
	}

	secondaryLeaders := remainingNodes[:numSecondaryLeaders]
	followerPool := remainingNodes[numSecondaryLeaders:]
	numFollowers := len(followerPool)

	followersPerLeader := distributeFollowers(numFollowers, numSecondaryLeaders)

	shards := make([]Shard, numSecondaryLeaders)
	currentIndex := 0

	for i := 0; i < numSecondaryLeaders; i++ {
		numFollowers := followersPerLeader[i]
		endIndex := currentIndex + numFollowers
		if endIndex > len(followerPool) {
			panic(fmt.Sprintf("Trying to assign %d followers, but only %d available", numFollowers, len(followerPool)-currentIndex))
		}
		shards[i] = Shard{
			LeaderId:  secondaryLeaders[i],
			Followers: followerPool[currentIndex:endIndex],
		}
		currentIndex = endIndex
	}

	return primaryLeader, shards
}
