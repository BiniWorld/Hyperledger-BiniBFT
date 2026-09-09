# Hyperledger BiniBFT

BiniBFT is a Go prototype of a hierarchical, sharded Byzantine fault-tolerant consensus algorithm for Hyperledger Fabric-style ordering.

This repository currently provides two ways to demonstrate the protocol:

1. A standalone multi-node local demo. This is the recommended demo.
2. A Fabric-oriented Docker configuration and adapter tests. This is useful for showing the intended integration shape, but it is not a production Fabric deployment by itself.

## What the standalone demo does

The standalone runner creates a configurable number of nodes in one Go process. Each node has:

- Its own node ID.
- Its own consensus and block-storage directories.
- Its own operations HTTP port.
- Its own HTTP/3 consensus port.
- A role: primary leader, shard leader, or shard follower.

The runner creates a random topology, registers the nodes' public keys, submits transactions to the primary leader, and prints block-delivery events.

The protocol flow shown by the demo is:

```text
client request
      |
      v
batching at primary leader
      |
      v
Pre-Prepare -> shard leaders/followers
      |
      v
intra-shard votes and shard quorum
      |
      v
Prepare -> shard leaders/followers
      |
      v
Commit -> shard leaders/followers
      |
      v
block delivery and local storage
```

## Requirements

- Go 1.24 or newer, as specified by `go.mod`.
- `curl` for the optional HTTP inspection commands.
- Docker and Docker Compose only if using the Fabric-oriented demo.

Check the local tools:

```bash
go version
curl --version
docker --version
docker compose version
```

## Build and test

Run these commands from the repository root:

```bash
cd /media/vin/data7/forks/lfx/Hyperledger-BiniBFT

go mod download
go test ./...
go test -v ./...
go build -o binibft-demo .
```

The tests cover cryptographic signing, digest/domain separation, request validation, batching, quorum checks, VRF proof handling, state synchronization, transport configuration, and the local Fabric adapter contracts.

## Recommended standalone demo

### Start a small cluster

Four nodes and one shard is the easiest topology to explain:

```bash
cd /media/vin/data7/forks/lfx/Hyperledger-BiniBFT
go run . --nodes=4 --shards=1 --txs=5 --tx-interval=500ms
```

### Start a larger sharded cluster

Seven nodes and two shards make the shard hierarchy more visible:

```bash
cd /media/vin/data7/forks/lfx/Hyperledger-BiniBFT
go run . --nodes=7 --shards=2 --txs=20 --tx-interval=500ms
```

The available command-line flags are:

```text
--nodes         Total nodes in the local cluster. Default: 7
--shards        Number of shards. Default: 2
--txs           Number of automatically submitted transactions. Default: 10
--tx-interval   Delay between automatically submitted transactions. Default: 500ms
```

For example:

```bash
go run . --nodes=10 --shards=3 --txs=50 --tx-interval=200ms
```

Use at least four nodes for a normal BFT demonstration. The implementation has special quorum handling for small clusters, but a small cluster does not demonstrate meaningful Byzantine tolerance.

## What to show during the demo

At startup, point out the log entries containing:

```text
Starting BiniBFT Consensus Cluster
Assigned cluster topology
All nodes initialized and public keys registered
```

Record the randomly selected primary leader from the `primaryLeader` field. Then point out:

```text
Submitting client transactions to Primary Leader
Primary leader starting consensus for proposal
pre-prepare
prepare
commit
Delivered consensus block
```

The exact wording can vary with the log level and code path, so search the output by phase names if necessary:

```bash
go run . --nodes=7 --shards=2 --txs=20 --tx-interval=500ms 2>&1 | tee /tmp/binibft-demo.log
rg -n "primaryLeader|shard|PrePrep|Prepare|Commit|quorum|Delivered|transaction" /tmp/binibft-demo.log
```

## Live node inspection

The local runner assigns operations ports beginning at `20001` and consensus ports beginning at `10001`:

```text
node 1 -> operations http://127.0.0.1:20001, consensus https://127.0.0.1:10001
node 2 -> operations http://127.0.0.1:20002, consensus https://127.0.0.1:10002
node N -> operations http://127.0.0.1:2000N, consensus https://127.0.0.1:1000N
```

Query a node's status:

```bash
curl -s http://127.0.0.1:20001/status | jq .
```

If `jq` is not installed:

```bash
curl http://127.0.0.1:20001/status
```

Query metrics:

```bash
curl -s http://127.0.0.1:20001/metrics | jq .
```

Query the latest stored height:

```bash
curl -s http://127.0.0.1:20001/height
```

Fetch a stored block:

```bash
curl -s http://127.0.0.1:20001/blocks/1 | jq .
```

Inspect several nodes and compare their roles and heights:

```bash
for port in 20001 20002 20003 20004 20005 20006 20007; do
  echo "===== node port ${port} ====="
  curl -s "http://127.0.0.1:${port}/status"
  echo
done
```

## Submit transactions manually

The operations API accepts JSON with `clientID` and `data` fields:

```bash
curl -s -X POST http://127.0.0.1:20001/tx \
  -H 'Content-Type: application/json' \
  -d '{"clientID":"demo-client","data":"hello from the live demo"}'
```

Submit multiple transactions:

```bash
for i in $(seq 1 10); do
  curl -s -X POST http://127.0.0.1:20001/tx \
    -H 'Content-Type: application/json' \
    -d "{\"clientID\":\"demo-client\",\"data\":\"demo transaction ${i}\"}"
  echo
  sleep 0.5
done
```

Send transactions to the primary leader's operations port. The primary is selected randomly at startup, so read the startup log first.

## Demonstrating heartbeats and failure handling

Heartbeat sending and monitoring are implemented in `heartbeat.go`. However, the current standalone runner places all nodes in one process, so killing the process kills every node.

The current `/start` and `/stop` operations endpoints are informational placeholders. They do not actually stop or start an individual node:

```bash
curl http://127.0.0.1:20001/stop
curl http://127.0.0.1:20001/start
```

For that reason, do not present these endpoints as a working node-failure control. To demonstrate heartbeat failure today, use the unit tests:

```bash
go test -v ./consensus -run 'TestNetworkPartitionRecovery|TestByzantine'
```

A useful future demo improvement is to run each node as a separate process and add real administrative commands for stopping, starting, and faulting one node.

## Dynamic node membership: current status

There is currently no live node-join or node-leave command.

The following command changes the initial cluster size only; it does not add a node to an already running cluster:

```bash
go run . --nodes=8 --shards=2 --txs=20
```

The current topology is created once at startup. Adding a real node would require all of the following:

1. Start the new node independently.
2. Authenticate and admit its identity.
3. Exchange its public key with existing nodes.
4. Assign it to a shard.
5. Commit a membership/configuration change.
6. Recalculate quorum membership safely.
7. Synchronize missing blocks.
8. Broadcast the new topology to every existing node.

The repository has configuration and state-sync building blocks, but it does not yet implement this complete live membership workflow.

## Restarting with a different topology

Stop the demo with `Ctrl-C`, then start a new topology:

```bash
go run . --nodes=4 --shards=1 --txs=5
```

For a clean demonstration, remove only this project's generated demo directories before restarting:

```bash
rm -rf ./data ./blocks
```

This removes local demo state and is not required if you want to demonstrate restart/catch-up behavior. Do not run that command from a directory containing unrelated data.

## Fabric-oriented Docker configuration

The `deployment/` directory contains a static Docker Compose configuration, crypto configuration, and channel configuration for a Fabric-oriented demonstration.

Inspect the files first:

```bash
cd /media/vin/data7/forks/lfx/Hyperledger-BiniBFT/deployment
ls -la
sed -n '1,240p' docker-compose-binibft.yaml
sed -n '1,240p' configtx.yaml
```

The helper script supports these operations:

```bash
./network.sh generate
./network.sh up
./network.sh down
./network.sh restart
```

Before running it, make sure Fabric binaries such as `cryptogen` and `configtxgen` are available:

```bash
command -v cryptogen
command -v configtxgen
command -v docker-compose || command -v docker
```

Start the configured network:

```bash
cd /media/vin/data7/forks/lfx/Hyperledger-BiniBFT/deployment
./network.sh up
docker ps --filter network=binibft-net
```

Stop it:

```bash
./network.sh down
```

The Docker topology is fixed by `docker-compose-binibft.yaml`. It is not a live node-membership demonstration.

## Repository layout

```text
main.go                       standalone cluster runner
node.go                       node HTTP/HTTP3 servers and operations API
communicator.go               node-to-node transport
heartbeat.go                  heartbeat sender and monitor
consensus/view.go             Pre-Prepare, Prepare, and Commit state machine
consensus/quorum.go           shard and cross-shard quorum/QC helpers
consensus/leader_election.go  leader election and shard assignment
consensus/vrf.go              VRF proof generation and verification primitives
consensus/sync.go             state synchronization and catch-up engine
consensus/storage.go          LevelDB block storage
fabric/                       Fabric-shaped adapter and lifecycle tests
deployment/                   static Docker/Fabric-oriented configuration
```

## What this demo proves

The standalone demo is suitable for showing:

- Batching and request ingestion.
- Hierarchical message flow.
- Shard roles and shard assignments.
- Quorum calculations.
- Cryptographic signing and validation paths.
- Block delivery and local persistence.
- State-sync and fault-injection behavior through tests.

It does not, by itself, prove:

- Production Byzantine fault tolerance.
- Live membership changes.
- Independent process failure recovery.
- Full authenticated Fabric orderer integration.
- Production-grade network security or performance.

For a presentation, describe it accurately as a live multi-node BiniBFT protocol prototype with static membership.
