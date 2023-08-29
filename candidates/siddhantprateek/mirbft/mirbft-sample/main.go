package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// Broadcast value for sending messages to all nodes
const Broadcast = -1

type Request struct {
	ClientID  int
	Data      string
	Signature string
}

type Proposal struct {
	LeaderID int
	Hash     string
	Request  Request
}

type Message struct {
	SenderID  int
	Recipient int
	Content   interface{}
}

type MirBFT struct {
	NodeID    int
	NumNodes  int
	Leaders   []int
	Pending   map[string]Proposal
	Committed map[string]bool
	Received  map[int]map[string]bool
	Mutex     sync.Mutex
}

func Hash(request Request) string {
	data := fmt.Sprintf("%d:%s:%s", request.ClientID, request.Data, request.Signature)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

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

func ExecuteRequest(request Request) {
	fmt.Printf("Executing request from Client %d: %s\n", request.ClientID, request.Data)
}

func SendMessage(msg Message) {
	fmt.Printf("Node %d sent a message to Node %d: %+v\n", msg.SenderID, msg.Recipient, msg.Content)
}

func main() {
	numNodes := 4 // Number of nodes in the network

	// Initialize Mir-BFT instances for each node
	nodes := make([]*MirBFT, numNodes)
	for i := 0; i < numNodes; i++ {
		nodes[i] = NewMirBFT(i, numNodes)
	}

	// Simulate sending messages and proposing requests
	request := Request{ClientID: 1, Data: "Transaction data", Signature: "Client signature"}
	nodes[0].Propose(request)
}
