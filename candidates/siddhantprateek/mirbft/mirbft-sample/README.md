# `Mir-BFT`

> Mir-BFT is a novel multi-leader consensus protocol designed to tackle scalability issues in existing Byzantine Fault Tolerant (BFT) protocols. These problems arise when operating in Wide Area Networks (WANs). To enhance throughput in such deployments, Mir-BFT introduces parallel execution. It effectively addresses concerns related to duplication and censorship attacks.

> In Mir-BFT, the protocol employs multiple leaders and employs a rotating hash assignment mechanism to counter duplication attacks. This mechanism involves distributing hash assignments among leaders to prevent Byzantine clients from submitting identical requests simultaneously. This safeguards against the manipulation of duplicated requests.

The protocol mitigates the problem of censorship attacks executed by Byzantine leaders, where client requests are intentionally delayed or discarded. By allowing parallel execution across multiple leaders, Mir-BFT enhances resistance against censorship, ensuring that the consensus process is not bottlenecked by the actions of a single malicious leader.

To optimize performance, Mir-BFT introduces client signature verification sharing. This strategy reduces computation bottlenecks, making the protocol more efficient. By implementing these innovative features, Mir-BFT stands out as a solution for achieving high throughput.

## Implementation

### `Hash(request Request) string`

- Calculates and returns the SHA-256 hash of the concatenated ClientID, Data, and Signature fields from a Request struct.

```go
func Hash(request Request) string {
	data := fmt.Sprintf("%d:%s:%s", request.ClientID, request.Data, request.Signature)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}
```

### `NewMirBFT(nodeID, numNodes int) *MirBFT`

- Creates and returns a new instance of the Mir-BFT algorithm for a node.
- Initializes the node's information, such as its ID, the total number of nodes, the list of leaders, pending proposals, committed status, and received proposal information.

```go
func NewMirBFT(nodeID, numNodes int) *MirBFT {
	mirBFT := &MirBFT{
		NodeID:    nodeID,
		NumNodes:  numNodes,
		Leaders:   []int{1, 2, 3, 4},
		Pending:   make(map[string]Proposal),
		Committed: make(map[string]bool),
		Received:  make(map[int]map[string]bool),
	}
	return mirBFT
}
```

### `ReceiveMessage(msg Message)`

- Handles incoming messages based on their content.
- If the content is of type Request, it initiates the proposal process.
- If the content is of type Proposal, it processes the received proposal.

```go
func (mb *MirBFT) ReceiveMessage(msg Message) {
	switch content := msg.Content.(type) {
	case Request:
		// Handle incoming client requests
		mb.Propose(content)
	case Proposal:
		// Handle incoming proposals
		mb.ProcessProposal(content)
	}
}

```

### `Propose(request Request)`

Creates a proposal based on the provided client request.
Broadcasts the proposal to other nodes for evaluation.

```go
func (mb *MirBFT) Propose(request Request) {
	hash := Hash(request) // Compute hash of the request
	proposal := Proposal{
		LeaderID: mb.Leaders[mb.NodeID%len(mb.Leaders)], // Rotate leader assignment
		Hash:     hash,
		Request:  request,
	}

	mb.Pending[hash] = proposal

	// Broadcast the proposal to other nodes
	for _, recipient := range mb.Leaders {
		if recipient != mb.NodeID {
			msg := Message{
				SenderID:  mb.NodeID,
				Recipient: recipient,
				Content:   proposal,
			}
			// Send the message to the recipient node
			SendMessage(msg)
		}
	}
}
```

### `ProcessProposal(proposal Proposal)`

- Processes the received proposal from a leader.
- Keeps track of received proposals and sends an acknowledgment to the leader if enough unique proposals are received.
- Initiates the commit process if acknowledgment conditions are met.

```go
func (mb *MirBFT) ProcessProposal(proposal Proposal) {
	if mb.Received[proposal.LeaderID] == nil {
		mb.Received[proposal.LeaderID] = make(map[string]bool)
	}
	mb.Received[proposal.LeaderID][proposal.Hash] = true

	// Check if there are enough unique proposals received from leaders
	if len(mb.Received[proposal.LeaderID]) >= (mb.NumNodes/2)+1 {
		// Send acknowledgment to the leader
		ackMsg := Message{
			SenderID:  mb.NodeID,
			Recipient: proposal.LeaderID,
			Content:   "ACK",
		}
		// Send the acknowledgment message
		SendMessage(ackMsg)

		// Commit the proposal
		mb.Commit(proposal.Hash)
	}
}
```

### `Commit(hash string)`

- Commits a proposal if it hasn't been committed already.
- Executes the request associated with the proposal.
- Broadcasts a commit message to all nodes.

```go
func (mb *MirBFT) Commit(hash string) {
	if _, ok := mb.Pending[hash]; ok && !mb.Committed[hash] {
		// Execute the request associated with the proposal
		request := mb.Pending[hash].Request
		ExecuteRequest(request)

		// Mark the proposal as committed
		mb.Committed[hash] = true

		// Broadcast the commit message to all nodes
		commitMsg := Message{
			SenderID:  mb.NodeID,
			Recipient: Broadcast,
			Content:   hash,
		}
		// Send the commit message to all nodes
		SendMessage(commitMsg)
	}
}
```

### `ExecuteRequest(request Request)`

- Simulates executing a client request by printing a message indicating the execution.

```go
func ExecuteRequest(request Request) {
	fmt.Printf("Executing request from Client %d: %s\n", request.ClientID, request.Data)
}
```

### `SendMessage(msg Message)`

- Simulates sending a message by printing a message indicating the sender, recipient, and content of the message.

```go
func SendMessage(msg Message) {
	fmt.Printf("Node %d sent a message to Node %d: %+v\n", msg.SenderID, msg.Recipient, msg.Content)
}
```

---