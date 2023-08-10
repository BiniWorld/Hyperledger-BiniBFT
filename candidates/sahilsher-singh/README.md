# Report

```js
Author: Sahilsher Singh Sandhu
Github: @Sandhu-Sahil
Email: sandhu.sahil2002@gmail.com
```

## Daily Entries

Available at [Daily progress report](https://github.com/Sandhu-Sahil/LFX-Hyperledger_progress-report)

## Week 1

```js
Date: July 3, 2023 - July 8, 2023
```

- Team Introduction: Introduce team members to each other, including their roles and responsibilities.
- Project Overview: Provide an overview of the project, its goals, and objectives.
Expectations and Goals: Set clear expectations for team members, clarify project goals, and ensure everyone is aligned.
- Setting up Communication Channels: Establish communication channels for the team to collaborate effectively (e.g., Slack, email, project management tools).
- Kick-off Meeting: Hold a kick-off meeting to discuss the project's scope, deliverables, and timeline.
- Project Planning: Initiate the project planning phase, including defining tasks, milestones, and dependencies.
- Start learning: The public blockchian and its consensus algorithms, as I don't have prior experience of it.

## Week 2 
```js
Date: July 10, 2023 - July 14, 2023
```

- Research and Background: Conduct any necessary research to gain a deeper understanding of the project domain. The team discussed the use case and identified the need for a consensus algorithm, specifically the Raft consensus algorithm. Raft Consensus Shortcomings: During discussions, the limitations of the Raft consensus algorithm were analyzed, highlighting potential areas for improvement.

- Onging journey of learning: Creating little projects to get fimilar with blockchain.

## Week 3

```js
Date: July 17, 2023 - July 21, 2023
```

- BFT Consensus Methods and Blockchain Interaction: Researched and examined existing Byzantine Fault Tolerant (BFT) consensus methods and their interaction with blockchain systems. Evaluated how BFT algorithms enhance the security and reliability of blockchain networks.

- Overview of Raft Consensus Algorithm: Provided a concise overview of the Raft consensus algorithm. Described its key components, including leader election, log replication, and safety properties. Understood the algorithm's role in achieving consensus in distributed systems.

- Raft Consensus Algorithm Limitations: Analyzed the limitations of the Raft consensus algorithm, including its inability to tolerate Byzantine faults. Identified potential areas for improvement.

- Hyperledger Labs BDLS: Explored Hyperledger Labs BDLS, a distributed ledger system designed to work with various consensus algorithms. Understood how it can be used to implement the Raft consensus algorithm.

- Architecture of Hyperledger Labs BDLS: Examined the architecture of Hyperledger Labs BDLS, including its components and their interactions. Understood how the Raft consensus algorithm is implemented in Hyperledger Labs BDLS (will be discussed in detail in the next week).

- Getting hands dirty: On the basis of my research, I started to create a little project to get a better understanding of the blockchain and its consensus algorithms. Also continuing my journey of further learning.

## Week 4

```js
Date: July 24, 2023 - July 28, 2023
```

- Public blockchain learning completed: : I have completed my learning of public blockchain and its consensus algorithms. I have also created a little project to get a better understanding of the blockchain and its consensus algorithms. Also created a little project to get a better understanding of the blockchain and its consensus algorithms.

- Start with Hyperledger Fabric: I have started to learn about Hyperledger Fabric and its consensus algorithms. Also continuing my journey of further learning.

- Byzantine: I have started to learn about Byzantine and its consensus algorithms. Byzantine fault tolerance is the dependability of a fault-tolerant computer system, particularly distributed computing systems, where components may fail and there is imperfect information on whether a component has failed. In a "Byzantine failure", a component such as a server can inconsistently appear both failed and functioning to failure-detection systems, presenting different symptoms to different observers. The term takes its name from an allegory, the "Byzantine Generals' Problem", developed to describe this behavior. 1/3 of the nodes can be faulty. 

- Raft: I have started to learn about Raft and its consensus algorithms. Raft is a consensus algorithm that is designed to be easy to understand. It's equivalent to Paxos in fault-tolerance and performance. The difference is that it's decomposed into relatively independent subproblems, and it cleanly addresses all major pieces needed for practical systems. We hope Raft will make consensus available to a wider audience, and that this wider audience will be able to develop a variety of higher quality consensus-based systems than are available today. It contains a leader, follower, and candidate. The leader is responsible for managing the log and replicating it to other nodes. The follower is responsible for responding to requests from the leader and forwarding requests to the leader. The candidate is responsible for requesting votes from other nodes.

- Paxos: I have started to learn about Paxos and its consensus algorithms. Paxos is a consensus algorithm that is used to achieve consensus in a network of unreliable processors. It was first introduced by Leslie Lamport in 1989. It is a two-phase protocol that allows a collection of machines to agree on a value even if some of the machines fail. It is a leaderless algorithm that works in rounds. In each round, every node proposes a value and votes for a value proposed by some node. The algorithm guarantees that a single value will be agreed upon by all the non-faulty nodes. It contains three roles: proposer, acceptor, and learner. The proposer proposes a value, the acceptor accepts a value, after the decision all are converted into the learner learns the value.

- Corda: I have started to learn about Cordo and its consensus algorithms. In this the transaction is not broadcasted to all the nodes, instead it is shared with the nodes which are involved in the transaction and the nortary node. Nortary node is the node which validates the transaction and then broadcast it to the other nodes.

## Week 5

```js
Date: July 31, 2023 - August 4, 2023
```

- Hyperledger Fabric: I have started to learn about Hyperledger Fabric and its consensus algorithms. Hyperledger Fabric is a permissioned blockchain infrastructure, originally contributed by IBM and Digital Asset, providing a modular architecture with a delineation of roles between the nodes in the infrastructure, execution of Smart Contracts (called "chaincode" in Fabric) and configurable consensus and membership services. It contains a leader, follower, and candidate. The leader is responsible for managing the log and replicating it to other nodes. The follower is responsible for responding to requests from the leader and forwarding requests to the leader. The candidate is responsible for requesting votes from other nodes.

- KBA course completed: I have completed my KBA course. I have learned about the basics of blockchain and its consensus algorithms. I have also created a little project to get a better understanding of the blockchain and its consensus algorithms. 

- Mango tracking system: I have started to learn about the mango tracking system. I have also created a little project to get a better understanding of the blockchain and its consensus algorithms. Also created a little project to get a better understanding of the blockchain and its consensus algorithms.

## Week 6

```js
Date: August 7, 2023 - August 11, 2023
```

- https://github.com/Sandhu-Sahil/bootstrapping-hyperledger: Bootstrapping Hyperledger Fabric Network.

- https://github.com/Sandhu-Sahil/mango-tracking-sys: I have started to learn about the mango tracking system. I have also created a little project to get a better understanding of the blockchain and its consensus algorithms.

- https://hyperledger-fabric.readthedocs.io/en/latest/test_network.html: Read the docs of Hyperledger Fabric. Created a network using the test network for deep learning of Raft Fabric.