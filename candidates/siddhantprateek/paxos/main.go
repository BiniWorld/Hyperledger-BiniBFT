package main

import (
	"fmt"
	"sync"
)

type Proposal struct {
	Number  int
	Value   interface{}
	Decided bool
}

type Acceptor struct {
	mu           sync.Mutex
	promisedNum  int
	acceptedProp Proposal
}

type Learner struct {
	mu         sync.Mutex
	accepted   []Proposal
	quorumSize int
}

type Proposer struct {
	mu          sync.Mutex
	proposalNum int
	value       interface{}
	acceptors   []*Acceptor
	learners    []*Learner
}

func NewAcceptor() *Acceptor {
	return &Acceptor{}
}

func NewLearner(quorumSize int) *Learner {
	return &Learner{quorumSize: quorumSize}
}

func NewProposer(proposalNum int, value interface{}, acceptors []*Acceptor, learners []*Learner) *Proposer {
	return &Proposer{
		proposalNum: proposalNum,
		value:       value,
		acceptors:   acceptors,
		learners:    learners,
	}
}

func (a *Acceptor) ReceivePrepare(n int) (int, Proposal) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n > a.promisedNum {
		a.promisedNum = n
		return a.acceptedProp.Number, a.acceptedProp
	}
	return -1, Proposal{}
}

func (a *Acceptor) ReceiveAccept(n int, prop Proposal) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n >= a.promisedNum {
		a.promisedNum = n
		a.acceptedProp = prop
		return true
	}
	return false
}

func (l *Learner) ReceiveAccepted(prop Proposal) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.accepted = append(l.accepted, prop)
	if len(l.accepted) >= l.quorumSize {
		return true
	}
	return false
}

func (p *Proposer) Propose() {
	for {
		p.mu.Lock()
		n := p.proposalNum
		p.mu.Unlock()

		prepareResponses := make(map[int]Proposal)
		prepareResponsesCount := 0

		for _, acceptor := range p.acceptors {
			nPrime, prevAccepted := acceptor.ReceivePrepare(n)
			if nPrime != -1 {
				prepareResponsesCount++
				prepareResponses[nPrime] = prevAccepted
			}
		}

		if prepareResponsesCount >= len(p.acceptors)/2+1 {
			maxAccepted := Proposal{}
			for _, prevAccepted := range prepareResponses {
				if prevAccepted.Number > maxAccepted.Number {
					maxAccepted = prevAccepted
				}
			}

			if maxAccepted.Decided {
				p.mu.Lock()
				p.proposalNum++
				p.mu.Unlock()
			} else {
				p.mu.Lock()
				p.proposalNum++
				p.value = maxAccepted.Value
				p.mu.Unlock()

				acceptResponses := 0
				for _, acceptor := range p.acceptors {
					if acceptor.ReceiveAccept(n, Proposal{Number: n, Value: p.value}) {
						acceptResponses++
					}
				}

				if acceptResponses >= len(p.acceptors)/2+1 {
					// Consensus reached
					for _, learner := range p.learners {
						learner.ReceiveAccepted(Proposal{Number: n, Value: p.value, Decided: true})
					}
				}
			}
		}
	}
}

func main() {
	// Create acceptors and learners
	numAcceptors := 5
	numLearners := 3

	acceptors := make([]*Acceptor, numAcceptors)
	learners := make([]*Learner, numLearners)

	for i := 0; i < numAcceptors; i++ {
		acceptors[i] = NewAcceptor()
	}

	for i := 0; i < numLearners; i++ {
		learners[i] = NewLearner(numAcceptors/2 + 1)
	}

	// Create proposers and start them
	numProposers := 2

	for i := 0; i < numProposers; i++ {
		proposer := NewProposer(i, fmt.Sprintf("Value from Proposer %d", i), acceptors, learners)
		go proposer.Propose()
	}

	// Simulate external input triggering proposals
	// In a real system, this would be based on actual events or decisions
	// For simplicity, we'll just wait here
	select {}
}
