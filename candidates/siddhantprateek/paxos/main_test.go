package main

import (
	"fmt"
	"testing"
	"time"
)

func TestPaxosConsensus(t *testing.T) {
	numAcceptors := 5
	numLearners := 3
	numProposers := 2

	acceptors := make([]*Acceptor, numAcceptors)
	learners := make([]*Learner, numLearners)
	for i := 0; i < numAcceptors; i++ {
		acceptors[i] = NewAcceptor()
	}

	for i := 0; i < numLearners; i++ {
		learners[i] = NewLearner(numAcceptors/2 + 1)
	}

	proposers := make([]*Proposer, numProposers)
	for i := 0; i < numProposers; i++ {
		// Initialize proposers starting at proposalNum >= 1 to exceed acceptor's promisedNum (0)
		proposers[i] = NewProposer(i+1, fmt.Sprintf("Value from Proposer %d", i), acceptors, learners)
		go proposers[i].Propose()
	}

	// Wait for all learners to decide using event channel with a timeout
	for i, learner := range learners {
		select {
		case <-learner.decided:
			// Consensus reached for this learner
		case <-time.After(2 * time.Second):
			t.Fatalf("Timeout waiting for learner %d to decide", i)
		}
	}

	// Clean up proposers
	for _, proposer := range proposers {
		proposer.Stop()
	}

	for _, learner := range learners {
		if len(learner.accepted) == 0 {
			t.Errorf("Learner did not receive any accepted proposals.")
		}
		for _, prop := range learner.accepted {
			if !prop.Decided {
				t.Errorf("Learner received a proposal that was not marked as decided.")
			}
		}
	}
}

func TestPaxosProposer(t *testing.T) {
	acceptor := NewAcceptor()
	learner := NewLearner(1)
	// Proposer proposalNum must start at >= 1 to be accepted by the acceptor (promisedNum starts at 0)
	proposer := NewProposer(1, "Test Value", []*Acceptor{acceptor}, []*Learner{learner})

	go proposer.Propose()
	defer proposer.Stop()

	// Wait for learner to decide
	select {
	case <-learner.decided:
		// Decided successfully
	case <-time.After(2 * time.Second):
		t.Fatalf("Timeout waiting for learner to decide")
	}

	if len(learner.accepted) != 1 {
		t.Errorf("Learner did not receive the accepted proposal.")
	}
	if !learner.accepted[0].Decided {
		t.Errorf("Learner received a proposal that was not marked as decided.")
	}
}
