package main

import (
	"binibft-poc/consensus"
	"flag"
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
	var (
		numNodes   int
		numShards  int
		numTxs     int
		txInterval time.Duration
	)

	flag.IntVar(&numNodes, "nodes", 7, "Total number of consensus nodes in cluster")
	flag.IntVar(&numShards, "shards", 2, "Number of consensus shards")
	flag.IntVar(&numTxs, "txs", 10, "Number of transactions to submit for consensus")
	flag.DurationVar(&txInterval, "tx-interval", 500*time.Millisecond, "Interval between submitted transactions")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: false,
		Level:     slog.LevelInfo,
	}))

	logger.Info("Starting BiniBFT Consensus Cluster",
		"nodes", numNodes,
		"shards", numShards,
		"txs", numTxs)

	shardMap := make(map[consensus.ShardID]Shard)
	networkOpts := NetworkOptions{
		NumNodes:     numNodes,
		BatchSize:    5,
		BatchTimeout: 1 * time.Second,
	}

	primary, shards := generateShardsWithRandomAssignment(numNodes, numShards)
	for i, shard := range shards {
		shardID := consensus.ShardID(i + 1)
		shardMap[shardID] = shard
	}

	logger.Info("Assigned cluster topology",
		"primaryLeader", primary,
		"shardCount", len(shards))

	clusterConfig := clusterConfig{
		primaryId: primary,
		shards:    shardMap,
	}

	chains := make(map[int]*Chain)
	mapNodes := make(map[string]*NodeInfo)
	for id := 1; id <= networkOpts.NumNodes; id++ {
		mapNodes[fmt.Sprintf("%d", id)] = &NodeInfo{
			ID:         fmt.Sprintf("%d", id),
			Address:    fmt.Sprintf("127.0.0.1:%d", 10000+id),
			OpsAddress: fmt.Sprintf("127.0.0.1:%d", 20000+id),
		}
	}

	for id := 1; id <= networkOpts.NumNodes; id++ {
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
			shardId = 0
			role = consensus.RolePrimaryLeader
			followers = []consensus.NodeID{}
			shardLeaderId = clusterConfig.primaryId
		} else {
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

			if !found {
				for sid, shard := range clusterConfig.shards {
					if slices.Contains(shard.Followers, nodeIDStr) {
						shardId = sid
						role = consensus.RoleShardFollower
						followers = []consensus.NodeID{}
						shardLeaderId = shard.LeaderId
						found = true
						break
					}
				}
			}
		}

		chain := NewChain(
			fmt.Sprintf("%d", id),
			address,
			opsAddress,
			mapNodes,
			logger,
			networkOpts,
			walDir,
			blocksDir,
			shardId,
			shardLeaderId,
			followers,
			role,
			clusterConfig.primaryId,
			clusterConfig,
		)

		go func(nodeID int, ch *Chain) {
			for {
				block := ch.Listen()
				logger.Info("Delivered consensus block",
					"nodeID", nodeID,
					"sequence", block.Sequence,
					"txCount", len(block.Transactions))
			}
		}(id, chain)

		chains[id] = chain
	}

	// Cross-register ECDSA public keys across all nodes
	for _, c1 := range chains {
		for _, c2 := range chains {
			if c2.node != nil && c2.node.privKey != nil && c1.node != nil && c1.node.verifier != nil {
				c1.node.verifier.RegisterPublicKey(c2.node.id, &c2.node.privKey.PublicKey)
			}
		}
	}

	logger.Info("All nodes initialized and public keys registered")

	// Wait 1 second for cluster to settle
	time.Sleep(1 * time.Second)

	// Ingest transactions to primary leader
	primaryIDInt, _ := strconv.Atoi(string(primary))
	primaryChain := chains[primaryIDInt]

	if primaryChain != nil && primaryChain.node != nil {
		go func() {
			logger.Info("Submitting client transactions to Primary Leader", "primary", primary, "totalTxs", numTxs)
			for i := 1; i <= numTxs; i++ {
				now := int(time.Now().UnixNano())
				data := fmt.Sprintf("TransferAsset(asset-%d, 100)", i)
				txID := consensus.ComputeTransactionDigest("default-channel", "client-demo", now, data)
				tx := Transaction{
					ClientID: "client-demo",
					TS:       now,
					ID:       txID,
					Data:     data,
				}
				rawTx := tx.ToBytes()
				if err := primaryChain.node.consensus.SubmitRequest(rawTx); err != nil {
					logger.Error("Failed to submit request", "txID", tx.ID, "error", err)
				} else {
					logger.Info("Submitted transaction to consensus pool", "txID", tx.ID)
				}
				time.Sleep(txInterval)
			}
			logger.Info("Finished submitting all client transactions")
		}()
	}

	// Run indefinitely or until interrupted
	select {}
}

func calculateSecondaryLeaderCount(totalNodes int, maxSecondaryLeaders int) int {
	remainingNodes := totalNodes - 1
	idealSecondaryLeaders := int(math.Min(float64(remainingNodes/3), float64(maxSecondaryLeaders)))
	if idealSecondaryLeaders < 1 {
		idealSecondaryLeaders = 1
	}
	return idealSecondaryLeaders
}

func distributeFollowers(numFollowers, numLeaders int) []int {
	if numLeaders <= 0 {
		return []int{}
	}
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
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })
	return nodes
}

func generateShardsWithRandomAssignment(totalNodes int, maxSecondaryLeaders int) (consensus.NodeID, []Shard) {
	if totalNodes < 3 {
		panic("Need at least 3 nodes to form BiniBFT cluster")
	}
	if maxSecondaryLeaders > totalNodes-1 {
		maxSecondaryLeaders = totalNodes - 1
	}

	allNodes := generateRandomNodeIDs(totalNodes)
	primaryLeader := allNodes[0]
	remainingNodes := allNodes[1:]

	numSecondaryLeaders := calculateSecondaryLeaderCount(totalNodes, maxSecondaryLeaders)
	if numSecondaryLeaders >= len(remainingNodes) {
		numSecondaryLeaders = len(remainingNodes)
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
			endIndex = len(followerPool)
		}
		shards[i] = Shard{
			LeaderId:  secondaryLeaders[i],
			Followers: followerPool[currentIndex:endIndex],
		}
		currentIndex = endIndex
	}

	return primaryLeader, shards
}
