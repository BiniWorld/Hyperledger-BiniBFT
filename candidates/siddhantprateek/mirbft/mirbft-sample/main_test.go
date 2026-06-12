package main

import (
	"fmt"
	"sync"
	"testing"
)

func TestMirBFTConcurrency(t *testing.T) {
	numNodes := 4
	mb := NewMirBFT(0, numNodes)

	// We will spawn multiple goroutines to propose requests concurrently
	// and verify that map access is fully synchronized without causing panic.
	var wg sync.WaitGroup
	numConcurrent := 50

	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(clientId int) {
			defer wg.Done()
			req := Request{
				ClientID:  clientId,
				Data:      fmt.Sprintf("Transaction data %d", clientId),
				Signature: fmt.Sprintf("sig-%d", clientId),
			}
			mb.Propose(req)
		}(i)
	}

	wg.Wait()

	mb.Mutex.Lock()
	pendingLen := len(mb.Pending)
	mb.Mutex.Unlock()

	if pendingLen != numConcurrent {
		t.Errorf("Expected %d pending proposals, got %d", numConcurrent, pendingLen)
	}
}

func TestMirBFTProcessProposalConcurrency(t *testing.T) {
	numNodes := 4
	mb := NewMirBFT(0, numNodes)

	// Pre-fill pending proposals
	req := Request{ClientID: 1, Data: "data", Signature: "sig"}
	hash := Hash(req)
	mb.Pending[hash] = Proposal{LeaderID: 1, Hash: hash, Request: req}

	var wg sync.WaitGroup
	numConcurrent := 50

	// Simulating multiple concurrent processing calls for proposals from different leaders
	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(leaderId int) {
			defer wg.Done()
			prop := Proposal{
				LeaderID: leaderId,
				Hash:     hash,
				Request:  req,
			}
			mb.ProcessProposal(prop)
		}(i % numNodes)
	}

	wg.Wait()
}
