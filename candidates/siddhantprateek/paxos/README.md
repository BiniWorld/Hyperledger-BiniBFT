# Paxos

Paxos is a consensus algorithm used in distributed systems to ensure that a group of nodes or processes can agree on a single value or decision. It involves three key roles: proposers, acceptors, and learners, each of which plays a specific part in the algorithm's execution.

> There are 3 roles in the Paxos algorithm – the proposer, the acceptors, and the learner.

1. `Proposers:` Proposers are responsible for initiating the consensus process by proposing a value. They communicate with other members of the group, typically the acceptors, to reach a consensus.

2. `Acceptors:` Acceptors are nodes or processes that accept proposed values. They play a crucial role in the decision-making process by either agreeing to a proposed value or rejecting it based on certain criteria.

3. `Learners:` Learners are responsible for tracking the progress of the consensus algorithm. They observe the messages exchanged between proposers and acceptors and determine when consensus has been reached.

The Paxos algorithm consists of two main phases:

## The Prepare Phase:

- Proposers select a proposal number and send a prepared request to the acceptors. It's essential that this request is sent to a majority (quorum) of acceptors for the algorithm to proceed.
- When an acceptor receives a prepared request, it compares the proposal number with the highest proposal number it has seen so far. If the incoming proposal number is higher, it accepts the proposal and sends a response to the proposer indicating this.
- If the acceptor has already accepted a proposal, it will inform the proposer of this fact, including the proposal number and value it has accepted.
- If the proposal has a lower number than what the acceptor has seen before, it will be ignored.

## The Accept Phase:

- If a proposer receives promises from a majority of acceptors, it checks if any of these promises include an accepted message. If so, the proposer can proceed to send an accept request to finalize the decision.
- If the proposer does not receive responses from most acceptors, it assumes its proposal number is not high enough and generates a higher proposal number to retry.
- When a proposer receives responses from a majority of acceptors for its accept request, it informs the learner that consensus has been reached.
- If an acceptor receives an accept request with a proposal number equal to what it has promised, it confirms the proposal's acceptance.
- If an acceptor receives an accept request with a lower proposal number than a previously promised one, it ignores the request.

On the learner's side, once it receives a sufficient number of accepted values, it recognizes that consensus has been achieved.

In the context of implementing the Paxos algorithm, one way to approach it is by using an actor system, where each actor (representing proposers, acceptors, and learners) encapsulates the logic and state related to its role. While Paxos involves constant changes in internal state, object-oriented programming can be used to manage and mutate the state within each actor instance. For example, an acceptor actor can maintain a mutable internal state like `max_id` and update it when receiving a prepared message with a higher ID number.


Paxos is a consensus algorithm that involves proposers, acceptors, and learners working together through two main phases (prepare and accept) to reach an agreement in a distributed system. Implementing it can be facilitated by using an actor-based approach, with each actor managing its state and interactions with others.