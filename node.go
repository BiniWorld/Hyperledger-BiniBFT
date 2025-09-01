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
	primaryId         consensus.NodeID
	logger            consensus.Logger
	mapNodes          map[string]*NodeInfo
	cachedHttpClients map[string]*http.Client
	storage           consensus.BlockStorage
}

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
		mapNodes:    mapNodes,
		shardId:     shardId,
		storage:     storage,
		config:      clusterConfig,
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

	// Configure ALL shards for cross-shard coordination using the cluster config
	// This is needed so the primary leader knows about all shard leaders
	for shardID, shard := range clusterConfig.shards {
		builder.WithShard(shardID, shard.LeaderId, shard.Followers)
		logger.Debug("Configured shard", "shardID", shardID, "leader", shard.LeaderId, "followers", shard.Followers)
	}

	node.consensus, err = builder.Build()
	if err != nil {
		panic("error building consensus")
	}

	node.consensus.Start(context.Background())

	node.Start()

	return node
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

		// Parse the consensus message
		var message consensus.Message
		err = json.Unmarshal(requestBody, &message)
		if err != nil {
			c.logger.Error("Error unmarshaling consensus message", "error", err)
			w.WriteHeader(500)
			return
		}

		c.logger.Debug("Node received consensus message", "nodeId", c.id, "from", message.From, "type", message.Type)

		// Forward the message to the consensus system
		err = c.consensus.HandleMessage(message.From, message)
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

		// Determine shard leader from cluster configuration
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
	return
}

func (c *Node) Stop() {
	select {
	case <-c.stopChan:
		break
	default:
		close(c.stopChan)
	}
	c.clock.Stop()
	c.doneWG.Wait()
	// n.consensus.Stop()
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
