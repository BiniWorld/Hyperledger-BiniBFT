# Ragini Sharma

## BiniBFT consensus plugin for Hyperledger Fabric

[`binibft/`](./binibft) is a BiniBFT-based ordering service for Fabric v3.1.5. It is registered under Fabric's existing `BFT` consensus type in place of SmartBFT.

The code lives in the Fabric tree at `orderer/consensus/binibft/`; a working branch is at
[RaginiSharma01/fabric @ ragini/binibft-plugin](https://github.com/RaginiSharma01/fabric/tree/ragini/binibft-plugin).
It imports Fabric packages, so build and run it from inside that tree.

### How a block is ordered

1. The primary assembles a block from the request pool.
2. The BiniBFT engine finalizes it with pre-prepare / prepare / commit.
3. Every consenter signs the finalized block and broadcasts its signature.
4. A node writes the block once it holds a BFT quorum of signatures.

Step 3 is needed because peers verify blocks with Fabric's standard BFT block-validation policy, and the engine's own votes are not in that format.

### What's included

| Path | Contents |
| --- | --- |
| `binibft/*.go`, `binibft/engine/` | Plugin (chain, consenter, assembler, crypto, identity, network) and the BiniBFT engine |
| `binibft/plugin_test.go`, `binibft/testclient/` | Unit tests and a client that checks blocks with Fabric's own verifier |
| `binibft/testnet/` | 4-orderer localhost network and a one-command smoke test |
| `binibft/fullnet/` | Full network: N orderers (odd, default 5), 2 peer orgs, chaincode-as-a-service, Fabric Gateway client, `e2e-test.sh` |
| `binibft/TESTING.md` | How to run all of the above |

### Testing

`fullnet/e2e-test.sh` passes with 3, 5 and 7 orderers. It submits and queries transactions through the Fabric Gateway, stops one orderer, and checks that the network keeps ordering.

Testing turned up one bug. The Fabric Gateway sends each transaction to every orderer and needs a quorum of successes. The primary also receives forwarded copies of the same transaction, so it rejected the gateway's copy as a duplicate, and the gateway sometimes missed its quorum. Duplicates are now accepted as success.

### Known limitations

- No leader failover
- No catch-up for lagging or restarted nodes
- No live membership change
