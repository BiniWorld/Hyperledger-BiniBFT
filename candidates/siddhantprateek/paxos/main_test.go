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

	for i := 0; i < numProposers; i++ {
		proposer := NewProposer(i, fmt.Sprintf("Value from Proposer %d", i), acceptors, learners)
		go proposer.Propose()
	}

	time.Sleep(1 * time.Second)
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
	proposer := NewProposer(0, "Test Value", []*Acceptor{acceptor}, []*Learner{learner})

	go proposer.Propose()
	time.Sleep(100 * time.Millisecond)

	if len(learner.accepted) != 1 {
		t.Errorf("Learner did not receive the accepted proposal.")
	}
	if !learner.accepted[0].Decided {
		t.Errorf("Learner received a proposal that was not marked as decided.")
	}
}
