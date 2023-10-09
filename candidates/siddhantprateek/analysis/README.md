# Analysis 

## `Mir-BFT`

### Requirments

- It can tolerate nodes that may have arbitarily, including malicious behavior. As a result it requires a level of redundancy to achieve the robustness, often involving a large number of nodes compared to algorithm like Raft.
- Mir-BFT can be resource intensive, in terms of CPU and network bandwidth while dealing with nodes and high volumes of messages.
- For security and integrity of communication among Mir-BFT nodes it uses message encryption and authentication techniques.

### Drawbacks

- This protocol generates high-volume of message for consensus, which lead to increased in network traffic and potentially impact system performance.
- It may not be much scalable as compared other algorithms present.
- Achieving consensus in a Byzantine fault-tolerant manner can introduce additional latency, especially in scenarios where there are a significant number of faulty nodes or where cryptographic operations are required.


## `Raft`

### Requirments

- Majority of Nodes Must Be Accessible
- The quorum size in Raft is typically set to a majority of nodes
- Raft requires sufficient disk space and persistence to store its log entries and configuration data.
- Raft is designed to handle node failures gracefully, complex failure scenarios, such as network partitions or multiple simultaneous failures

### Drawbacks

* Raft's design centers around a leader node that handles all client requests and log replication. This can limit its scalability, especially for write-intensive workloads, as all writes must go through the leader.
* Raft's single leader approach simplifies the consensus process but can introduce latency and potential bottlenecks
* Raft can be resource-intensive, particularly in terms of memory and disk usage, due to the need to maintain logs and configuration data.


## `Paxos`

## Requirement
- More than half of the nodes in the cluster must be functioning correctly and accessible for the system to make progress and ensure the safety of decisions.
- Paxos assumes that nodes have reasonably synchronized
- Significant clock drift between nodes can lead to issues in reaching consensus.

## Drawback

- Paxos can be slow in certain cases, especially when dealing with network partitions or when nodes fail frequently
-  Achieving consensus often requires multiple rounds of communication
- Some variants of Paxos involve a single leader, which can introduce a single point of failure and a potential bottleneck 
- Changing the membership of a Paxos cluster, such as adding or removing nodes, can be challenging
- Paxos typically requires a majority quorum, which means you need an odd number of nodes in your cluster (e.g., 3, 5, 7, etc.) to ensure a clear majority. 

## `Smart-BFT`


## System Requirements

* _To be Discussed_

## Protocol Workflow / Designs

* _To be Discussed_
