package main

import (
	"binibft-poc/consensus"
	"binibft-poc/consensus/protos"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"
	"github.com/quic-go/quic-go/http3"
)

type Node struct {
	clock       *time.Ticker
	secondClock *time.Ticker
	stopChan    chan struct{}
	doneWG      sync.WaitGroup
	prevHash    string
	id          consensus.NodeID
	deliverChan chan<- *consensus.Block
	consensus   *consensus.Consensus
	address     string
	in          Ingress
	// db                *leveldb.DB
	opsAddress        string
	config            clusterConfig
	shardId           consensus.ShardID
	followers         []consensus.NodeID
	role              consensus.NodeRole
	roleUpdateChan    chan consensus.NodeRole // Channel to receive role updates
	logger            consensus.Logger
	cachedHttpClients map[string]*http.Client
	storage           consensus.BlockStorage
	comm              *Communicator
	primaryId         consensus.NodeID
	shardLeaderId     consensus.NodeID
	mapNodes          map[string]*NodeInfo

	lastHeartbeat          time.Time      // Last received heartbeat from primary
	heartbeatTicker        *time.Ticker   // For sending heartbeat (if primary)
	heartbeatStop          chan struct{}  // Stop signal for heartbeat loop
	heartbeatWG            sync.WaitGroup // WaitGroup for cleanup
	lastHeartbeats         map[consensus.NodeID]time.Time
	heartbeatMonitorTicker *time.Ticker
	heartbeatMonitorWG     sync.WaitGroup
	heartbeatSeen          map[consensus.NodeID]bool
	heartbeatMutex         sync.RWMutex // Mutex for heartbeat maps

	signer   *ECDSASigner
	verifier *ECDSAVerifier
	privKey  *ecdsa.PrivateKey
}

type Metrix struct {
	StartTime time.Time
	EndTime   time.Time
	Duriation time.Duration
}

var metrix map[string]*Metrix

func NewNode(
	id consensus.NodeID,
	address string,
	opsAddress string,
	mapNodes map[string]*NodeInfo,
	deliverChan chan<- *consensus.Block,
	logger consensus.Logger,
	opts NetworkOptions,
	nodeDir string,
	blocksDir string,
	shardId consensus.ShardID,
	shardLeaderId consensus.NodeID,
	followers []consensus.NodeID,
	role consensus.NodeRole,
	primaryId consensus.NodeID,
	clusterConfig clusterConfig, // Add cluster config parameter

) *Node {

	metrix = map[string]*Metrix{}
	logger.Info("Node initialized WAL")
	walstorage, err := consensus.NewLevelDBStorage(nodeDir)
	if err != nil {
		panic(fmt.Sprintf("Failed to create consensus storage: %v", err))
	}

	comm := &Communicator{
		nodeId:            id,
		mapNodes:          mapNodes,
		cachedHttpClients: map[consensus.NodeID]*http.Client{},
		logger:            logger,
		handler:           nil,
	}

	// Create storage for consensus
	storage, err := consensus.NewLevelDBStorage(blocksDir)
	if err != nil {
		panic(fmt.Sprintf("Failed to create consensus storage: %v", err))
	}

	privKey, err := GenerateKeyPair()
	if err != nil {
		logger.Error("Failed to generate ECDSA key for node", "error", err)
	}
	channelID := "default-channel"
	signer := NewECDSASigner(id, privKey, channelID)
	verifier := NewECDSAVerifier(channelID)
	if privKey != nil {
		verifier.RegisterPublicKey(id, &privKey.PublicKey)
	}

	node := &Node{
		// db:          db,
		address:     address,
		opsAddress:  opsAddress,
		clock:       time.NewTicker(time.Second),
		secondClock: time.NewTicker(time.Second),
		id:          id,
		deliverChan: deliverChan,
		stopChan:    make(chan struct{}),
		logger:      logger,

		shardId: shardId,
		storage: storage,

		config:         clusterConfig,
		mapNodes:       mapNodes,
		lastHeartbeats: make(map[consensus.NodeID]time.Time),
		shardLeaderId:  shardLeaderId,
		role:           role,
		primaryId:      clusterConfig.primaryId,
		heartbeatSeen:  make(map[consensus.NodeID]bool),
		roleUpdateChan: make(chan consensus.NodeRole, 1),
		comm:           comm,

		signer:   signer,
		verifier: verifier,
		privKey:  privKey,
	}
	metadata := &protos.ViewMetadata{
		LatestSequence: 0,
		ViewId:         0,
	}
	block, err := storage.GetLatestBlock()
	if err == nil {
		err = proto.Unmarshal(block.Metadata, metadata)
		if err != nil {
			logger.Info(fmt.Sprintf("Unable to unmarshal metadata, error: %v", err))
		}
		// Set prevHash from the latest block
		node.prevHash = fmt.Sprintf("%x", sha256.Sum256(block.ToBytes()))
		logger.Info("Node %d found latest block with sequence %v, prevHash: %s", id, metadata.LatestSequence, node.prevHash)
	} else {
		// Genesis block case - no previous hash
		node.prevHash = ""
		logger.Info("Node %d starting from genesis, no previous hash", id)
	}

	builder := consensus.NewConsensusBuilder()
	builder.WithNetwork(comm)
	builder.WithLogger(logger)
	builder.WithStorage(walstorage, storage) // Add storage configuration
	builder.WithPrimaryLeader(primaryId)
	builder.WithNode(id, shardId, role)
	builder.WithBatchingConfig(int(opts.BatchSize), opts.BatchTimeout)
	builder.WithViewMetaData(metadata)
	builder.WitAssembler(node)
	builder.WithRequestInspector(node)
	builder.WithApplication(node) // Use the node as the application delivery interface
	builder.WithSigner(node)      // Use the node as the signer
	builder.WithVerifier(verifier)
	builder.WithChannelID(channelID)

	// Configure ALL shards for cross-shard coordination using the cluster config
	// This is needed so the primary leader knows about all shard leaders
	for shardID, shard := range clusterConfig.shards {
		builder.WithShard(shardID, shard.LeaderId, shard.Followers)
		logger.Debug("Configured shard", "shardID", shardID, "leader", shard.LeaderId, "followers", shard.Followers)
	}
	node.consensus, err = builder.Build()

	// Set node reference in consensus config for role updates
	if node.consensus != nil {
		node.consensus.SetNodeReference(node)
	}
	if err != nil {
		panic("error building consensus")
	}
	node.lastHeartbeats[node.primaryId] = time.Time{}
	node.lastHeartbeats[node.shardLeaderId] = time.Time{}

	node.heartbeatSeen[node.primaryId] = false
	node.heartbeatSeen[node.shardLeaderId] = false
	node.consensus.Start(context.Background())

	node.Start()

	return node
}

// unmarshalConsensusMessage properly unmarshals a consensus message with correct payload types
func (c *Node) unmarshalConsensusMessage(data []byte) (*consensus.Message, error) {
	// First, unmarshal into a temporary struct to get the message type
	var tempMsg struct {
		Type      consensus.MessageType `json:"Type"`
		From      consensus.NodeID      `json:"From"`
		To        consensus.NodeID      `json:"To"`
		ShardID   consensus.ShardID     `json:"ShardID"`
		Timestamp time.Time             `json:"Timestamp"`
		Payload   json.RawMessage       `json:"Payload"`
	}

	err := json.Unmarshal(data, &tempMsg)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	// Create the final message
	message := &consensus.Message{
		Type:      tempMsg.Type,
		From:      tempMsg.From,
		To:        tempMsg.To,
		ShardID:   tempMsg.ShardID,
		Timestamp: tempMsg.Timestamp,
	}

	// Convert payload based on message type
	switch tempMsg.Type {
	case consensus.MsgLeaderElection:
		var payload consensus.LeaderElectionMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal LeaderElectionMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgElectionAck:
		var payload consensus.ElectionAckMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ElectionAckMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgShardAssignment:
		var payload consensus.ShardAssignmentMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ShardAssignmentMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgLeaderAnnouncement:
		var payload consensus.LeaderAnnouncementMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal LeaderAnnouncementMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgHeartbeat:
		var payload consensus.HeartbeatMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal HeartbeatMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgRequest:
		var payload consensus.RequestMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal RequestMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgPrePrep:
		var payload consensus.PrePrepMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal PrePrepMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgPreparePhase:
		var payload consensus.PreparePhaseMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal PreparePhaseMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgPrepare:
		var payload consensus.PrepMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal PrepMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgCommitRequest:
		var payload consensus.CommitRequestMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal CommitRequestMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgCommitPhase:
		var payload consensus.CommitMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal CommitMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgViewChange:
		var payload consensus.ViewChangeRequest
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ViewChangeRequest: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgCrossShardRequest:
		var payload consensus.CrossShardRequestMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal CrossShardRequestMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgShardAck:
		var payload consensus.ShardAckMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ShardAckMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgFinalizedBlock:
		var payload consensus.FinalizedBlockMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal FinalizedBlockMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgIntraShardVote:
		var payload consensus.IntraShardVoteMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal IntraShardVoteMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgIntraShardVoteResponse:
		var payload consensus.IntraShardVoteResponse
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal IntraShardVoteResponse: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgSyncRequest:
		var payload consensus.SyncRequestMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal SyncRequestMessage: %w", err)
		}
		message.Payload = &payload

	case consensus.MsgSyncResponse:
		var payload consensus.SyncResponseMessage
		if err := json.Unmarshal(tempMsg.Payload, &payload); err != nil {
			return nil, fmt.Errorf("failed to unmarshal SyncResponseMessage: %w", err)
		}
		message.Payload = &payload

	default:
		// For unknown message types, keep as raw JSON
		message.Payload = tempMsg.Payload
	}

	return message, nil
}

func (c *Node) getCommServer() *http3.Server {
	tlsCert, err := generateSelfSignedCert()
	if err != nil {
		c.logger.Error("failed to generate self-signed certificate", "error", err)
		panic(err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"quic-echo-example"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/transaction", func(w http.ResponseWriter, r *http.Request) {
		request, err := io.ReadAll(r.Body)
		if err != nil {
			c.logger.Error("Error reading request body", "error", err)
			w.WriteHeader(500)
			return
		}

		if err := c.consensus.SubmitRequest(request); err != nil {
			http.Error(w, fmt.Sprintf("Failed to handle message: %v", err), http.StatusInternalServerError)
			return
		}

		c.logger.Info("Received transaction", "request", string(request))
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("/consensus", func(w http.ResponseWriter, r *http.Request) {
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			c.logger.Error("Error reading request body", "error", err)
			w.WriteHeader(500)
			return
		}

		// Parse the consensus message with proper payload type conversion
		message, err := c.unmarshalConsensusMessage(requestBody)
		if err != nil {
			c.logger.Error("Error unmarshaling consensus message", "error", err)
			w.WriteHeader(500)
			return
		}

		c.logger.Debug("Node received consensus message", "nodeId", c.id, "from", message.From, "type", message.Type)

		// Forward the message to the consensus system
		err = c.consensus.HandleMessage(message.From, *message)
		if err != nil {
			c.logger.Error("Error handling consensus message", "error", err, "from", message.From, "type", message.Type)
			w.WriteHeader(500)
			return
		}

		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
	return &http3.Server{
		Addr:      c.address,
		TLSConfig: tlsConfig,
		Handler:   mux,
	}
}

func (n *Node) getOperationsServer() *http.Server {
	muxOps := gin.Default()
	muxOps.GET("/stop", func(ctx *gin.Context) {
		// TODO: Implement proper consensus stop
		n.logger.Info("Stop endpoint called", "nodeID", n.id)
		ctx.JSON(200, gin.H{
			"message": "Node stop requested",
			"nodeID":  n.id,
			"status":  "pending",
		})
	})
	muxOps.GET("/start", func(ctx *gin.Context) {
		// TODO: Implement proper consensus start
		n.logger.Info("Start endpoint called", "nodeID", n.id)
		ctx.JSON(200, gin.H{
			"message": "Node start requested",
			"nodeID":  n.id,
			"status":  "active",
		})
	})
	muxOps.GET("/status", func(ctx *gin.Context) {
		ownID := n.id
		height := 0
		block, err := n.storage.GetLatestBlock()
		if err == nil {
			height = int(block.Sequence)
		}

		// Get consensus status for additional information
		consensusStatus := n.consensus.GetStatus()

		// Determine shard leader from cluster configuration or consensus config
		var shardLeader consensus.NodeID
		var isShardLeader bool

		if shard, exists := n.config.shards[n.shardId]; exists {
			shardLeader = shard.LeaderId
			isShardLeader = ownID == shardLeader
		} else if n.shardId == 0 {
			shardLeader = n.config.primaryId
			isShardLeader = ownID == shardLeader
		} else {
			shardLeader = "unknown"
			isShardLeader = false
		}

		var avg time.Duration
		var droppedTxns int
		for k, v := range metrix {
			if v.EndTime.IsZero() {
				fmt.Printf("txn: %s is not in block\n", k)
				droppedTxns++
			}
			v.Duriation = v.EndTime.Sub(v.StartTime)
			avg += v.Duriation
		}
		count := len(metrix) - droppedTxns
		avg = time.Duration(float64(avg) / float64(count))

		ctx.JSON(200, gin.H{
			"nodeID":        ownID,
			"height":        height,
			"shardID":       n.shardId,
			"shardLeader":   shardLeader,
			"isShardLeader": isShardLeader,
			"role":          consensusStatus.Role.String(),
			"isActive":      consensusStatus.IsActive,
			"currentView":   consensusStatus.CurrentView,
			"isPrimary":     consensusStatus.IsPrimary,
			"avgDuriation":  avg.String(),
			"dropped":       droppedTxns,
		})
	})

	muxOps.GET("/metrics", func(ctx *gin.Context) {
		// Get consensus metrics
		metrics := n.consensus.GetMetrics()

		// Add node-specific information
		metrics["timestamp"] = time.Now().Unix()
		metrics["node_address"] = n.address
		metrics["ops_address"] = n.opsAddress

		ctx.JSON(200, gin.H{
			"success": true,
			"data":    metrics,
		})
	})
	muxOps.POST("/tx", func(c *gin.Context) {
		var txInput TXInput
		err := c.BindJSON(&txInput)
		if err != nil {
			c.JSON(500, gin.H{
				"message": fmt.Sprintf("Error binding JSON: %v", err),
			})
			return
		}
		// generate uuid
		txID := uuid.New().String()
		tx := Transaction{
			ClientID: txInput.ClientID,
			Data:     txInput.Data,
			TS:       int(time.Now().UnixNano() / 1000000),
			ID:       txID,
		}
		metrix[txID] = &Metrix{StartTime: time.Now()}
		err = n.consensus.SubmitRequest(tx.ToBytes())
		if err != nil {
			c.JSON(500, gin.H{
				"message": fmt.Sprintf("Error submitting request: %v", err),
			})
			return
		}
		c.JSON(200, gin.H{
			"message": "Request submitted for consensus",
			"txID":    tx.ID,
		})
	})

	muxOps.GET("/height", func(ctx *gin.Context) {
		block, err := n.storage.GetLatestBlock()
		if err != nil {
			n.logger.Error("Error getting latest block", "error", err)
			ctx.JSON(500, gin.H{"error": "No blocks found", "height": 0})
			return
		}
		ctx.JSON(200, gin.H{"height": block.Sequence})
	})

	muxOps.GET("/blocks/:blockNumber", func(ctx *gin.Context) {
		blockNumberString := ctx.Param("blockNumber")
		blockNumber, err := strconv.ParseUint(blockNumberString, 10, 64)
		if err != nil {
			n.logger.Error("Error converting string to uint64", "error", err)
			ctx.JSON(500, gin.H{"error": err})
			return
		}
		// blockBytes, err := n.db.Get([]byte(strconv.FormatUint(blockNumber, 10)), &opt.ReadOptions{})
		block, err := n.storage.GetBlockByHeight(blockNumber)
		if err != nil {
			n.logger.Error("Error getting block", "blockNumber", blockNumber, "error", err)
			ctx.JSON(500, gin.H{"error": err})
			return
		}
		ctx.JSON(200, gin.H{"block": block})
	})

	return &http.Server{
		Addr:    n.opsAddress,
		Handler: muxOps,
	}
}

func (c *Node) Start() {
	// Create the HTTP server for operations
	httpServer := c.getOperationsServer()
	go func() {
		err := httpServer.ListenAndServe()
		if err != nil {
			c.logger.Error("failed to serve HTTP operations server", "error", err)
			panic(err)
		}
	}()
	// Create the HTTP/3 server for communication between the nodes
	commServer := c.getCommServer()
	go func() {
		err := commServer.ListenAndServe()
		if err != nil {
			c.logger.Error("failed to serve HTTP/3 communication server", "error", err)
			panic(err)
		}
	}()

	// Start heartbeat services
	c.startHeartbeatSender(5 * time.Second)
	c.startHeartbeatMonitor(20 * time.Second)

	// Start role update listener
	go c.listenForRoleUpdates()

	return
}

func (c *Node) Stop() {
	select {
	case <-c.stopChan:
		break
	default:
		close(c.stopChan)
	}
	close(c.roleUpdateChan)
	c.clock.Stop()
	c.doneWG.Wait()
	// n.consensus.Stop()
}

// listenForRoleUpdates listens for role updates and restarts heartbeat monitor accordingly
func (c *Node) listenForRoleUpdates() {
	for {
		select {
		case newRole := <-c.roleUpdateChan:
			c.logger.Info("Role update received", "oldRole", c.role.String(), "newRole", newRole.String())
			c.updateRole(newRole)
		case <-c.stopChan:
			return
		}
	}
}

// updateRole updates the node's role and restarts heartbeat monitor if necessary
func (c *Node) updateRole(newRole consensus.NodeRole) {
	oldRole := c.role
	c.role = newRole

	// Update primary and shard leader IDs based on new role
	if c.consensus != nil {
		status := c.consensus.GetStatus()
		// Primary ID is already set from cluster config, no need to update from status
		if status.ShardID != 0 {
			if shardLeaders := c.consensus.GetShardLeaders(); shardLeaders != nil {
				if leader, exists := shardLeaders[status.ShardID]; exists {
					c.shardLeaderId = leader
				}
			}
		}
	}

	c.logger.Info("Node role updated",
		"nodeID", c.id,
		"oldRole", oldRole.String(),
		"newRole", newRole.String(),
		"primaryId", c.primaryId,
		"shardLeaderId", c.shardLeaderId)

	// Restart heartbeat monitor with new role
	c.restartHeartbeatMonitor()
}

// restartHeartbeatMonitor stops the current heartbeat monitor and starts a new one
func (c *Node) restartHeartbeatMonitor() {
	c.logger.Info("Restarting heartbeat monitor with new role", "role", c.role.String())

	// Stop current heartbeat monitor
	c.stopHeartbeatMonitor()

	// Start new heartbeat monitor with updated role
	c.startHeartbeatMonitor(20 * time.Second)

	c.logger.Info("Heartbeat monitor restarted", "role", c.role.String())
}

// UpdateNodeRole allows external components to update the node's role
func (c *Node) UpdateNodeRole(newRole consensus.NodeRole) {
	select {
	case c.roleUpdateChan <- newRole:
		// Role update sent successfully
	default:
		c.logger.Error("Role update channel is full, dropping update", "newRole", newRole.String())
	}
}

// UpdateNodeConfig updates the node's configuration after leader election
func (c *Node) UpdateNodeConfig(primaryId consensus.NodeID, shardLeaders map[consensus.ShardID]consensus.NodeID, shardNodes map[consensus.ShardID][]consensus.NodeID) {
	c.primaryId = primaryId

	// Update cluster config
	c.config.primaryId = primaryId
	for shardID, leaderID := range shardLeaders {
		if nodes, exists := shardNodes[shardID]; exists {
			c.config.shards[shardID] = Shard{
				LeaderId:  leaderID,
				Followers: nodes[1:], // First is leader, rest are followers
			}
		}
	}

	// Update this node's specific fields
	if c.id == primaryId {
		c.role = consensus.RolePrimaryLeader
		c.shardId = 0
		c.shardLeaderId = primaryId
		c.followers = []consensus.NodeID{}
		// Update config fields
		c.role = consensus.RolePrimaryLeader

	} else {
		// Find which shard this node belongs to
		for shardID, nodes := range shardNodes {
			for i, nodeID := range nodes {
				if nodeID == c.id {
					c.shardId = shardID
					if i == 0 {
						c.role = consensus.RoleShardLeader
						c.shardLeaderId = c.id
						c.followers = nodes[1:]
						// Update config fields

						c.shardId = shardID
					} else {
						c.role = consensus.RoleShardFollower
						c.shardLeaderId = nodes[0]
						c.followers = []consensus.NodeID{}
						// Update config fields

						c.shardId = shardID
					}
					break
				}
			}
		}
	}

	c.logger.Info("Node configuration updated after election",
		"nodeID", c.id,
		"role", c.role.String(),
		"shardID", c.shardId,
		"shardLeaderId", c.shardLeaderId,
		"primaryId", c.primaryId,
		"followers", c.followers)

	// Restart heartbeat monitor with new configuration
	c.restartHeartbeatMonitor()
}

// ReceiveHeartbeat implements ApplicationDelivery interface
func (n *Node) ReceiveHeartbeat(from consensus.NodeID, timestamp time.Time) {
	n.heartbeatMutex.Lock()
	n.lastHeartbeats[from] = timestamp
	n.heartbeatSeen[from] = true
	n.heartbeatMutex.Unlock()
	n.logger.Info("[Heartbeat] Received heartbeat from %s at %s", from, timestamp.Format(time.RFC3339))
}

func computeDigest(rawBytes []byte) string {
	h := sha256.New()
	h.Write(rawBytes)
	digest := h.Sum(nil)
	return hex.EncodeToString(digest)
}

func generateSelfSignedCert() (tls.Certificate, error) {
	// Generate a new private key
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	// Set up a certificate template
	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour) // 1 year validity

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	certTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Your Organization"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		DNSNames:              []string{"localhost"},
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add IP addresses to the certificate if needed
	certTemplate.IPAddresses = append(certTemplate.IPAddresses, net.ParseIP("127.0.0.1"))

	// Create the certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &certTemplate, &certTemplate, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	// Encode the private key and the certificate
	cert := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}

	return cert, nil
}
