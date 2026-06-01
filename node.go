package main

import (
    "crypto/ecdsa"
    "crypto/elliptic"
    "crypto/rand"
    "crypto/sha256"
    "crypto/tls"
    "crypto/x509"
    "crypto/x509/pkix"
    "encoding/hex"
    "fmt"
    "io"
    "math/big"
    "net"
    "net/http"
    "strconv"
    "sync"
    "time"

    smart "github.com/hyperledger/binibft-poc/consensus/pkg/api"
    smartbft "github.com/hyperledger/binibft-poc/consensus/pkg/consensus"
    bft "github.com/hyperledger/binibft-poc/consensus/pkg/types"
    "github.com/hyperledger/binibft-poc/consensus/pkg/wal"
    "github.com/hyperledger/binibft-poc/consensus/binibftprotos"

    "github.com/gin-gonic/gin"
    "github.com/golang/protobuf/proto"
    "github.com/google/uuid"
    "github.com/quic-go/quic-go"
    "github.com/quic-go/quic-go/http3"
    log "github.com/sirupsen/logrus"
    "github.com/syndtr/goleveldb/leveldb"
    "github.com/syndtr/goleveldb/leveldb/opt"
)

type Node struct {
    clock       *time.Ticker
    secondClock *time.Ticker
    stopChan    chan struct{}
    doneWG      sync.WaitGroup
    prevHash    string
    id          uint64
    deliverChan chan<- *Block
    consensus   *smartbft.Consensus
    address     string
    in          Ingress
    db          *leveldb.DB
    opsAddress  string
}

func NewNode(
    id uint64,
    address string,
    opsAddress string,
    mapNodes map[uint64]*NodeInfo,
    deliverChan chan<- *Block,
    logger smart.Logger,
    walmet *wal.Metrics,
    bftmet *smart.Metrics,
    opts NetworkOptions,
    nodeDir string,
    blocksDir string,
) *Node {

    writeAheadLog, _, err := wal.InitializeAndReadAll(logger, nodeDir, &wal.Options{Metrics: walmet.With("label1", "val1")})
    if err != nil {
        logger.Panicf("Failed to initialize a WAL for chain at %s: %v", nodeDir, err)
    }

    logger.Infof("Node %d initialized WAL", id)
    // Open the LevelDB database
    db, err := leveldb.OpenFile(blocksDir, nil)
    if err != nil {
        log.Fatal(err)
    }

    // Test LevelDB

    // Put value
    err = db.Put([]byte("key"), []byte("value"), nil)
    if err != nil {
        log.Panicf("Error storing block: %v", err)
    }

    // Get value
    data, err := db.Get([]byte("key"), nil)
    if err != nil {
        panic(err)
    }

    fmt.Println("Key: key")
    fmt.Println("Value:", string(data))

    node := &Node{
        db:          db,
        address:     address,
        opsAddress:  opsAddress,
        clock:       time.NewTicker(time.Second),
        secondClock: time.NewTicker(time.Second),
        id:          id,
        deliverChan: deliverChan,
        stopChan:    make(chan struct{}),
    }

    config := bft.Configuration{
        RequestBatchMaxCount:          100,
        RequestBatchMaxBytes:          10 * 1024 * 1024,
        RequestBatchMaxInterval:       50 * time.Millisecond,
        IncomingMessageBufferSize:     200,
        RequestPoolSize:               400,
        RequestForwardTimeout:         2 * time.Second,
        RequestComplainTimeout:        20 * time.Second,
        RequestAutoRemoveTimeout:      3 * time.Minute,
        ViewChangeResendInterval:      5 * time.Second,
        ViewChangeTimeout:             20 * time.Second,
        LeaderHeartbeatTimeout:        time.Minute,
        LeaderHeartbeatCount:          10,
        NumOfTicksBehindBeforeSyncing: 10,
        CollectTimeout:                time.Second,
        SyncOnStart:                   false,
        SpeedUpViewChange:             false,
        LeaderRotation:                false,
        DecisionsPerLeader:            3,
        RequestMaxBytes:               10 * 1024,
        RequestPoolSubmitTimeout:      5 * time.Second,
    }
    config.SelfID = id
    config.RequestBatchMaxInterval = opts.BatchTimeout
    config.RequestBatchMaxCount = opts.BatchSize
    comm := Communicator{
        nodeId:            id,
        mapNodes:          mapNodes,
        cachedHttpClients: make(map[uint64]*http.Client),
    }
    metadata := &binibftprotos.ViewMetadata{
        LatestSequence: 0,
        ViewId:         0,
    }
    latestBlock, err := db.Get([]byte("__latest_block"), &opt.ReadOptions{})
    if err == nil {
        block := BlockFromBytes(latestBlock)
        err = proto.Unmarshal(block.Metadata, metadata)
        if err != nil {
            log.Warnf("Unable to unmarshal metadata, error: %v", err)
        }
        // Initialize prevHash with the hash of the latest block
        node.prevHash = computeDigest(block.ToBytes())
        log.Infof("Node %d found latest block with sequence %v, prevHash: %s", id, metadata.LatestSequence, node.prevHash)
    } else {
        // No previous block exists, initialize with empty hash (genesis block)
        node.prevHash = ""
        log.Infof("Node %d starting with genesis block, prevHash: empty", id)
    }

    node.consensus = &smartbft.Consensus{
        Config:            config,
        ViewChangerTicker: node.secondClock.C,
        Scheduler:         node.clock.C,
        Logger:            logger,
        Metrics:           bftmet,
        Comm:              comm,
        Signer:            node,
        Verifier:          node,
        Application:       node,
        Assembler:         node,
        RequestInspector:  node,
        Synchronizer:      node,
        WAL:               writeAheadLog,
        Metadata:          metadata,
    }
    if err = node.consensus.Start(); err != nil {
        panic(fmt.Sprintf("error on consensus start: %v", err))
    }
    node.Start()

    return node
}
func (n *Node) getCommServer() *http3.Server {
    // generate self-signed certificate for HTTP/3 server
    tlsCert, err := generateSelfSignedCert()
    if err != nil {
        log.Fatalf("failed to generate self-signed certificate: %v", err)
    }
    tlsConfig := &tls.Config{
        Certificates: []tls.Certificate{tlsCert},
        NextProtos:   []string{"quic-echo-example"},
    }

    mux := http.NewServeMux()
    mux.HandleFunc("/transaction", func(w http.ResponseWriter, r *http.Request) {
        request, err := io.ReadAll(r.Body)
        if err != nil {
            log.Warnf("Error reading request body: %v", err)
            w.WriteHeader(500)
            return
        }
        // bytes to proto.message
        msg := &FwdMessage{}
        err = proto.Unmarshal(request, msg)
        if err != nil {
            log.Warnf("Error unmarshaling proto.message: %v", err)
            w.WriteHeader(500)
            return
        }
        err = n.consensus.SubmitRequest(msg.Payload)
        if err != nil {
            log.Warnf("Error submitting request: %v", err)
            w.WriteHeader(500)
            return
        }
        log.Infof("Node %d received transaction: %s", n.id, string(request))
        w.WriteHeader(200)
        w.Write([]byte("ok"))
    })
    mux.HandleFunc("/consensus", func(w http.ResponseWriter, r *http.Request) {
        request, err := io.ReadAll(r.Body)
        if err != nil {
            log.Warnf("Error reading request body: %v", err)
            w.WriteHeader(500)
            return
        }
        // parse to binibftprotos.Message
        msg := &binibftprotos.Message{}
        err = proto.Unmarshal(request, msg)
        if err != nil {
            log.Warnf("Error unmarshaling proto.message: %v", err)
            w.WriteHeader(500)
            return
        }
        // get query variable "id" from request
        strId := r.URL.Query().Get("id")
        num, err := strconv.ParseUint(strId, 10, 64)
        if err != nil {
            log.Warnf("Error converting string to uint64: %v", err)
            w.WriteHeader(500)
            return
        }
        n.consensus.HandleMessage(num, msg)
        log.Debugf("Node %d received consensus from node %d", n.id, num)
        w.WriteHeader(200)
        w.Write([]byte("ok"))
    })
    return &http3.Server{
        Addr:       n.address,
        TLSConfig:  tlsConfig,
        QuicConfig: &quic.Config{},
        Handler:    mux,
    }
}
func (n *Node) getOperationsServer() *http.Server {
    muxOps := gin.Default()
    muxOps.GET("/stop", func(ctx *gin.Context) {
        n.consensus.Stop()
        ctx.JSON(200, gin.H{"message": "Node stopped"})
    })
    muxOps.GET("/start", func(ctx *gin.Context) {
        err := n.consensus.Start()
        if err != nil {
            ctx.JSON(500, gin.H{"error": err})
            return
        }
        ctx.JSON(200, gin.H{"message": "Node started"})
    })
    muxOps.GET("/status", func(ctx *gin.Context) {
        leaderID := n.consensus.GetLeaderID()
        ownID := n.id
        height := 0
        blockBytes, err := n.db.Get([]byte("__latest_block"), &opt.ReadOptions{})
        if err == nil {
            block := BlockFromBytes(blockBytes)
            height = int(block.Sequence)
        }
        ctx.JSON(200, gin.H{
            "height":   height,
            "leaderID": leaderID,
            "nodeID":   ownID,
            "isLeader": leaderID == ownID,
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
                "message": fmt.Sprintf("Error ordering transaction: %v", err),
            })
            return
        }
        c.JSON(200, gin.H{
            "message": "Transaction ordered",
            "txID":    tx.ID,
        })
    })

    muxOps.GET("/height", func(ctx *gin.Context) {
        blockBytes, err := n.db.Get([]byte("__latest_block"), &opt.ReadOptions{})
        if err != nil {
            log.Warnf("Error getting latest block: %v", err)
            ctx.JSON(500, gin.H{"error": err})
            return
        }
        block := BlockFromBytes(blockBytes)
        ctx.JSON(200, gin.H{"height": block.Sequence})
        return
    })
    muxOps.GET("/blocks/:blockNumber", func(ctx *gin.Context) {
        blockNumberString := ctx.Param("blockNumber")
        blockNumber, err := strconv.ParseUint(blockNumberString, 10, 64)
        if err != nil {
            log.Warnf("Error converting string to uint64: %v", err)
            ctx.JSON(500, gin.H{"error": err})
            return
        }
        blockBytes, err := n.db.Get([]byte(strconv.FormatUint(blockNumber, 10)), &opt.ReadOptions{})
        if err != nil {
            log.Warnf("Error getting block %d: %v", blockNumber, err)
            ctx.JSON(500, gin.H{"error": err})
            return
        }
        block := BlockFromBytes(blockBytes)
        ctx.JSON(200, gin.H{"block": block})
        return
    })

    return &http.Server{
        Addr:    n.opsAddress,
        Handler: muxOps,
    }
}
func (n *Node) Start() {
    // Create the HTTP server for operations
    httpServer := n.getOperationsServer()
    go func() {
        err := httpServer.ListenAndServe()
        if err != nil {
            log.Fatalf("failed to serve: %v", err)
        }
    }()
    // Create the HTTP/3 server for communication between the nodes
    commServer := n.getCommServer()
    go func() {
        err := commServer.ListenAndServe()
        if err != nil {
            log.Fatalf("failed to serve: %v", err)
        }
    }()
    return
}

func (n *Node) Stop() {
    select {
    case <-n.stopChan:
        break
    default:
        close(n.stopChan)
    }
    n.clock.Stop()
    n.doneWG.Wait()
    n.consensus.Stop()
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