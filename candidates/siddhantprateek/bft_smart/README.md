# Research: `BFT-Smart`

`Author: Siddhant Prateek Mahanayak`


## About Hyperledger `Sawtooth`

Hyperledger Sawtooth is an open-source blockchain platform developed by the Linux Foundation's Hyperledger project. It was designed to facilitate the creation, deployment, and management of distributed ledger applications. Sawtooth aims to provide a flexible and modular framework that allows developers to implement various blockchain applications with different business logic while also supporting multiple consensus mechanisms.

Key Features of Hyperledger Sawtooth:

1. Modular Architecture: Sawtooth follows a modular architecture, which means different components of the system are implemented as pluggable modules. This design allows for easy customization and adaptability to different use cases.

2. Smart Contracts: Sawtooth supports the execution of smart contracts written in various programming languages such as Python, JavaScript, Go, and Rust. It provides a transaction processor interface for deploying and managing these smart contracts.

3. Consensus Mechanisms: As mentioned in the question, Sawtooth provides multiple consensus mechanisms to validate and agree on the state of the blockchain network. The three primary consensus mechanisms are:

   a. Proof of Elapsed Time (PoET): PoET is a unique consensus algorithm developed by Intel. It is based on a lottery-based mechanism where each validator node waits for a random amount of time, and the node that waits the least becomes the leader for creating the next block. This process is more energy-efficient compared to traditional proof-of-work (PoW) mechanisms.

   b. Practical Byzantine Fault Tolerance (PBFT): PBFT is a well-known consensus algorithm that works well in distributed systems with Byzantine faults (malicious nodes). It ensures that if a supermajority of nodes (two-thirds) agree on a block, it is considered valid.

   c. Raft: Raft is a consensus algorithm designed for simplicity and ease of understanding. It is often used in systems requiring fault-tolerance and strong consistency guarantees. In Sawtooth, Raft can be used to create a private blockchain network with a known set of validators.

4. Transaction Families: Sawtooth introduces the concept of "Transaction Families," which allows developers to organize related smart contracts and transactions. Each transaction family can define its own rules and permissions, making it easier to manage complex blockchain applications.

5. Permissioning and Privacy: Sawtooth provides various features for managing permissions and privacy, enabling enterprises to deploy private and consortium blockchains with controlled access.


## Setting up Sample Application

```bash
docker-compose -f docker-compose-sawtooth-pbft up -d
```

This Compose file creates five Sawtooth nodes named validator-# (numbered from 0 to 4). Note the container names for the Sawtooth components on each node:

`validator-0`:
```bash
sawtooth-validator-default-0
sawtooth-rest-api-default-0
sawtooth-pbft-engine-default-0 or sawtooth-poet-engine-0
sawtooth-settings-tp-default-0
sawtooth-intkey-tp-python-default-0
sawtooth-xo-tp-python-default-0
(PoET only) sawtooth-poet-validator-registry-tp-0
```
`validator-1`:

```bash
sawtooth-validator-default-1
sawtooth-rest-api-default-1
sawtooth-pbft-engine-default-1 or sawtooth-poet-engine-1
sawtooth-settings-tp-default-1
sawtooth-intkey-tp-python-default-1
sawtooth-xo-tp-python-default-1
(PoET only) sawtooth-poet-validator-registry-tp-1
```
... and so on.