# Hyperledger BiniBFT

> **High-Throughput Sharded Byzantine Fault Tolerant (BFT) Consensus Engine for Hyperledger Fabric**

BiniBFT is a scalable, hierarchical Byzantine Fault Tolerant consensus algorithm designed specifically for enterprise blockchains. It divides orderer nodes into concurrent execution shards to achieve horizontal scaling while maintaining strict cryptographic safety, deterministic block delivery, and Byzantine fault tolerance.

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Core Protocol Guarantees](#core-protocol-guarantees)
3. [Live Demo Guide (How to Present BiniBFT)](#live-demo-guide-how-to-present-binibft)
   - [Demo Option A: Standalone Real-Time Consensus Visualizer](#demo-option-a-standalone-real-time-consensus-visualizer)
   - [Demo Option B: Full Hyperledger Fabric Multi-Node Docker Network](#demo-option-b-full-hyperledger-fabric-multi-node-docker-network)
4. [Deployment & Network Operations](#deployment--network-operations)
5. [Fabric In-Tree Consenter Integration](#fabric-in-tree-consenter-integration)
6. [Automated Test Suite](#automated-test-suite)
7. [Repository Structure](#repository-structure)

---

## Architecture Overview

BiniBFT organizes orderer nodes into a two-tier hierarchical structure:

```
                          ┌───────────────────────┐
                          │     Client / Peer     │
                          └───────────┬───────────┘
                                      │ Transactions
                                      ▼
                          ┌───────────────────────┐
                          │    Primary Leader     │
                          │   (Global Proposer)   │
                          └───────┬───────┬───────┘
                     Pre-Prepare  │       │  Pre-Prepare
                    ┌─────────────┘       └─────────────┐
                    ▼                                   ▼
        ┌───────────────────────┐           ┌───────────────────────┐
        │    Shard 1 Leader     │           │    Shard 2 Leader     │
        └───┬───────────────┬───┘           └───┬───────────────┬───┘
            │ Intra-Vote    │                   │ Intra-Vote    │
            ▼               ▼                   ▼               ▼
      ┌───────────┐   ┌───────────┐       ┌───────────┐   ┌───────────┐
      │ Follower  │   │ Follower  │       │ Follower  │   │ Follower  │
      │  Node 1   │   │  Node 2   │       │  Node 3   │   │  Node 4   │
      └───────────┘   └───────────┘       └───────────┘   └───────────┘
            │               │                   │               │
            └───────┬───────┘                   └───────┬───────┘
                    │ Shard 1 QC                        │ Shard 2 QC
                    └─────────────┐           ┌─────────┘
                                  ▼           ▼
                          ┌───────────────────────┐
                          │ Cross-Shard Commit QC │
                          │   (2f + 1 Quorum)     │
                          └───────────┬───────────┘
                                      │ Final Block
                                      ▼
                          ┌───────────────────────┐
                          │   Ledger Delivery     │
                          └───────────────────────┘
```

### Protocol Phases:
1. **Batching & Pre-Prepare**: Client transactions are hashed with deterministic IDs and assembled into proposals by the Primary Leader.
2. **Intra-Shard Consensus**: Each shard leader distributes proposals to its followers and collects $Q_{intra} = 2f_{shard} + 1$ cryptographic signatures into a `ShardQC`.
3. **Cross-Shard Coordination**: Shard QCs are submitted to the Primary Leader. When $Q_{cross} = 2f_{shards} + 1$ shard quorums are collected, a cryptographic `CommitQC` is assembled.
4. **Block Finalization & Delivery**: The finalized block with embedded `CommitQC` metadata is written to the ledger and streamed to peers.

---

## Core Protocol Guarantees

| Invariant | Specification | Formula |
| :--- | :--- | :--- |
| **Intra-Shard Byzantine Quorum** | Max faulty nodes $f$, required signatures $Q$ | $f = \lfloor \frac{n-1}{3} \rfloor, \quad Q_{intra} = 2f + 1$ |
| **Cross-Shard Byzantine Quorum** | Max faulty shards $f$, required shard QCs $Q$ | $f = \lfloor \frac{s-1}{3} \rfloor, \quad Q_{cross} = 2f + 1$ |
| **Leader Election with VRF** | P-256 Elliptic Curve VRF proof generation and zero-knowledge verification | $\text{Score} = \text{SHA256}(\text{VRFOutput})$ |
| **Domain Separation** | Cryptographic separation preventing cross-channel / cross-phase replay | Canonical prefixes (`BINIBFT_PREPREP_ACK_v1`, etc.) |
| **State Catch-Up Engine** | Chunked block synchronization with continuous hash validation | $b_n.\text{PreviousHash} == \text{SHA256}(b_{n-1})$ |

---

## Live Demo Guide (How to Present BiniBFT)

You can demonstrate BiniBFT in two distinct modes depending on your audience:

---

### Demo Option A: Standalone Real-Time Consensus Visualizer
*Best for fast interactive demonstrations of the consensus engine, quorum validation, and leader election.*

```bash
cd /media/vin/data7/forks/lfx/Hyperledger-BiniBFT

# 1. Run a 4-node cluster with 1 shard, submitting 5 transactions:
go run . --nodes=4 --shards=1 --txs=5 --tx-interval=500ms

# 2. Or run a larger 7-node cluster with 2 shards, submitting 10 transactions:
go run . --nodes=7 --shards=2 --txs=10 --tx-interval=300ms
```

#### What to Highlight in the Output:
1. **Dynamic Topology Assignment**: Note the logged `primaryLeader` and `shardCount`.
2. **Cryptographic Key Registration**: Note `All nodes initialized and public keys registered` showing ECDSA P-256 key establishment.
3. **Transaction Ingestion**: Note `Submitted transaction to consensus pool with txID: ...`.
4. **Proposal Assembly & Pre-Prepare**: Note `Primary leader starting consensus for proposal view: 0 sequence: 1`.
5. **Intra-Shard Quorum**: Note `Checking follower majority for pre-prep ackCount: ... required: ...`.
6. **Block Commitment**: Note `Delivered consensus block nodeID: ... sequence: 1 txCount: ...`.

---

### Demo Option B: Full Hyperledger Fabric Multi-Node Docker Network
*Best for demonstrating production enterprise blockchain integration with real Fabric orderers, peers, and CLI.*

#### Step 1: Ensure Containers are Running
```bash
cd /media/vin/data7/forks/lfx/Hyperledger-BiniBFT/deployment
docker ps --filter "network=binibft-net"
```
You will see 5 Orderer nodes (`orderer1` to `orderer5`), 2 Peer nodes (`peer0.org1`, `peer0.org2`), and `cli`.

#### Step 2: Show Live Blockchain Ledger Height
Query the peer nodes to prove the blockchain is active and maintaining block state:
```bash
docker exec cli peer channel getinfo -c binibft-channel
```
**Expected Output:**
```json
Blockchain info: {"height":1,"currentBlockHash":"qT964BJiRCWEUjlULYH/AVbnce2XnpQ86Na37eRgvPk="}
```

#### Step 3: Show Channel Participation Across All 5 Orderers
Query the orderer administrative endpoints:
```bash
/home/vin/fabric-samples/bin/osnadmin channel list -o localhost:9443
/home/vin/fabric-samples/bin/osnadmin channel list -o localhost:9444
/home/vin/fabric-samples/bin/osnadmin channel list -o localhost:9445
/home/vin/fabric-samples/bin/osnadmin channel list -o localhost:9446
/home/vin/fabric-samples/bin/osnadmin channel list -o localhost:9447
```
**Expected Output:** All 5 orderers report `Status: 200` with `"name": "binibft-channel"`.

#### Step 4: Fetch & Decode a Block from the Orderer over TLS
Demonstrate fetching blocks from the orderer cluster and decoding them into JSON:
```bash
# Fetch Block 0 over gRPC mTLS
docker exec cli peer channel fetch 0 ./block0.pb -c binibft-channel \
  -o orderer1.example.com:7050 --tls \
  --cafile /opt/gopath/src/github.com/hyperledger/fabric/peer/crypto/ordererOrganizations/example.com/orderers/orderer1.example.com/tls/ca.crt

# Decode Protobuf block into human-readable JSON
docker exec cli configtxlator proto_decode --type common.Block --input ./block0.pb --output ./block0.json

# Inspect decoded block structure
docker exec cli head -n 30 ./block0.json
```

---

## Deployment & Network Operations

The `deployment/` directory contains all artifacts for managing the multi-node Fabric network:

### Topology & Ports

| Container | Host Port | Role | Description |
| :--- | :--- | :--- | :--- |
| `orderer1.example.com` | `7050` / `9443` | Primary Leader | Global proposer & batch coordinator |
| `orderer2.example.com` | `7056` / `9444` | Shard 1 Leader | Shard 1 intra-consensus coordinator |
| `orderer3.example.com` | `7052` / `9445` | Shard 1 Follower | Shard 1 validator node |
| `orderer4.example.com` | `7053` / `9446` | Shard 2 Leader | Shard 2 intra-consensus coordinator |
| `orderer5.example.com` | `7054` / `9447` | Shard 2 Follower | Shard 2 validator node |
| `peer0.org1.example.com` | `7051` | Org 1 Peer | Endorsing and committing peer |
| `peer0.org2.example.com` | `9051` | Org 2 Peer | Endorsing and committing peer |
| `cli` | N/A | Admin CLI | Tooling container for channel commands |

### Managing the Network Lifecycle

```bash
cd /media/vin/data7/forks/lfx/Hyperledger-BiniBFT/deployment

# Generate crypto material and channel genesis block:
PATH=/home/vin/fabric-samples/bin:$PATH ./network.sh generate

# Start all containers in the background:
/bin/docker-compose -f docker-compose-binibft.yaml up -d

# Stop and clean up all containers and volumes:
/bin/docker-compose -f docker-compose-binibft.yaml down --volumes --remove-orphans
```

---

## Fabric In-Tree Consenter Integration

BiniBFT is wired directly into the official `hyperledger/fabric` source repository at [`/media/vin/data7/forks/lfx/fabric`](file:///media/vin/data7/forks/lfx/fabric):

### Key Integration Points:
- **Consenter Factory**: [`fabric/orderer/consensus/binibft/consenter.go`](file:///media/vin/data7/forks/lfx/fabric/orderer/consensus/binibft/consenter.go) implements `consensus.Consenter` and `consensus.Chain`.
- **Server Registration**: [`fabric/orderer/common/server/main.go`](file:///media/vin/data7/forks/lfx/fabric/orderer/common/server/main.go#L640) registers `consenters["binibft"]`.
- **Binary & Docker Build**:
  ```bash
  cd /media/vin/data7/forks/lfx/fabric

  # Compile native orderer binary
  go build -mod=mod -o ./build/bin/orderer ./cmd/orderer

  # Build Docker image
  docker build -t hyperledger/fabric-orderer:binibft -t hyperledger/fabric-orderer:2.5 -f Dockerfile.binibft-orderer .
  ```

---

## Automated Test Suite

Run the full automated test suite containing **41 unit, integration, crypto, VRF, and Byzantine chaos tests**:

```bash
cd /media/vin/data7/forks/lfx/Hyperledger-BiniBFT
go test -v ./...
```

### Breakdown of Test Suites:
- **`binibft-poc` (12 tests)**:
  - ECDSA P-256 key generation, signing, and verification.
  - Canonical domain separation digest computation.
  - Safe transaction deserialization and deduplication.
  - Mutual TLS (mTLS) transport security.
- **`binibft-poc/consensus` (20 tests)**:
  - Shard and Cross-Shard Byzantine Quorum calculations ($Q_{intra}$, $Q_{cross}$).
  - P-256 ECVRF proof generation, verification, and tamper detection.
  - Leader election term monotonicity and VRF score ranking.
  - State synchronization catch-up engine with hash chain validation.
  - Byzantine fault injection (invalid signatures, double-proposing equivocation rejection, network partition recovery).
- **`binibft-poc/fabric` (9 tests)**:
  - Fabric Consenter and Chain life cycle (`Order`, `Configure`, `Deliver`).
  - Genesis block #0 to #N streaming with hash chain verification.
  - Dynamic channel configuration updates (Config Blocks).
  - Multi-channel state isolation.

---

## Repository Structure

```
Hyperledger-BiniBFT/
├── README.md                     # Comprehensive documentation & demo guide
├── main.go                       # Standalone multi-node cluster runner
├── node.go                       # Node networking & HTTP/3 consensus service
├── chain.go                      # Local chain abstraction
├── crypto.go                     # ECDSA P-256 cryptographic primitives
├── signer.go / verifier.go       # Cryptographic Signer and Verifier interfaces
├── request_inspector.go          # Transaction ID & payload inspector
├── assembler.go                  # Batch proposal assembler
├── communicator.go               # Secure mTLS transport layer
├── consensus/
│   ├── view.go                   # Core 3-phase consensus state machine
│   ├── quorum.go                 # Byzantine quorum calculations & QC validation
│   ├── vrf.go                    # Verifiable Random Function (VRF) engine
│   ├── leader_election.go        # VRF-ranked leader election engine
│   ├── sync.go                   # State sync & catch-up engine
│   ├── storage.go                # WAL & LevelDB block storage
│   ├── digest.go                 # Domain separation hashing
│   ├── messages.go               # Protocol message definitions
│   └── fault_injection_test.go   # Byzantine chaos & partition test suite
├── fabric/
│   ├── consenter.go              # Fabric Consenter plugin factory
│   ├── chain.go                  # Fabric Chain implementation
│   ├── config.go                 # configtx.yaml metadata schema & parser
│   ├── adapters.go               # Fabric MSP identity adapters
│   ├── types.go                  # Fabric ConsenterSupport contracts
│   └── lifecycle_test.go         # Full Tier 1 Fabric lifecycle test suite
└── deployment/
    ├── crypto-config.yaml        # 5 Orderers + 2 Peer Orgs identity spec
    ├── configtx.yaml             # Channel & consensus genesis profile
    ├── docker-compose-binibft.yaml # Multi-orderer, multi-peer Docker network
    └── network.sh                # Network orchestration script
```
