type Term = number;
type LogIndex = number;
// type index= number;

enum NodeState {
    FOLLOWER = "follower",
    CANDIDATE = "candidate",
    LEADER = "leader",
}

type NodeId = string;

type LogEntry = {
    term: Term;
    command: any;
};

type Log = LogEntry[];

type StateMachine = {
    applyCommand: (command: any) => void;
};

type NodeConfiguration = {
    id: NodeId;
    stateMachine: StateMachine;
    currentTerm: Term;
    votedFor: NodeId | null;    
    log: Log;
    state: string;
    commitIndex: number;
};

type RequestVoteRPC = {
    term: Term;
    candidateId: NodeId;
    lastLogIndex: LogIndex;
    lastLogTerm: Term;
};

type RequestVoteResponse = {
    term: Term;
    voteGranted: boolean;
};

type AppendEntriesRPC = {
    term: Term;
    leaderId: NodeId;
    prevLogIndex: LogIndex;
    prevLogTerm: Term;
    entries: LogEntry[];
    leaderCommit: LogIndex;
};

type AppendEntriesResponse = {
    term: Term;
    success: boolean;
    index : number;
};



const requestVote = (
    configuration: NodeConfiguration,
    rpc: RequestVoteRPC
): RequestVoteResponse => {
    // If the RPC term is less than the current term, return a response with voteGranted set to false
    if (rpc.term < configuration.currentTerm) {
        return {
            term: configuration.currentTerm,
            voteGranted: false,
        };
    }

    // If the RPC term is greater than the current term, update the current term and vote for the candidate
    if (rpc.term > configuration.currentTerm) {
        configuration.currentTerm = rpc.term;
        configuration.votedFor = rpc.candidateId;
    }

    // If the voter has already voted for a candidate in this term, return a response with voteGranted set to false
    if (configuration.votedFor && configuration.votedFor !== rpc.candidateId) {
        return {
            term: configuration.currentTerm,
            voteGranted: false,
        };
    }

    // If the candidate's log is not up-to-date, return a response with voteGranted set to false
    const lastLogIndex = configuration.log.length - 1;
    const lastLogTerm = configuration.log[lastLogIndex].term;
    if (lastLogTerm > rpc.lastLogTerm || (lastLogTerm === rpc.lastLogTerm && lastLogIndex > rpc.lastLogIndex)) {
        return {
            term: configuration.currentTerm,
            voteGranted: false,
        };
    }

    // Otherwise, return a response with voteGranted set to true
    configuration.votedFor = rpc.candidateId;
    return {
        term: configuration.currentTerm,
        voteGranted: true,
    };
};

const appendEntries = (
    configuration: NodeConfiguration,
    rpc: AppendEntriesRPC
): AppendEntriesResponse => {
    // If the RPC term is less than the current term, return a response with success set to false
    if (rpc.term < configuration.currentTerm) {
        return {
            term: configuration.currentTerm,
            success: false,
        };
    }

    // If the RPC term is greater than the current term, update the current term and set the node's state to follower
    if (rpc.term > configuration.currentTerm) {
        configuration.currentTerm = rpc.term;
        configuration.state = NodeState.FOLLOWER;
    }

    // If the previous log index and term don't match the node's log, return a response with success set to false
    const prevLogIndex = rpc.prevLogIndex;
    const prevLogTerm = rpc.prevLogTerm;
    if (configuration.log[prevLogIndex]?.term !== prevLogTerm) {
        return {
            term: configuration.currentTerm,
            success: false,
        };
    }

    // Otherwise, append the new entries to the log and return a response with success set to true
    configuration.log = [...configuration.log.slice(0, prevLogIndex + 1), ...rpc.entries];
    configuration.commitIndex = Math.min(rpc.leaderCommit, configuration.log.length - 1);
    return {
        term: configuration.currentTerm,
        success: true,
    };
};


const startElection = (configuration: NodeConfiguration): void => {
    // Increment the current term and set the node's state to candidate
    configuration.currentTerm++;
    configuration.state = NodeState.CANDIDATE;

    // Reset the votedFor field
    configuration.votedFor = null;

    // Request votes from other nodes
    sendRequestVoteRPC(configuration);
};

const sendRequestVoteRPC = (configuration: NodeConfiguration): void => {
    // Implementation omitted for brevity
    // Sends a RequestVoteRPC to other nodes in the cluster
};


const handleRequestVoteRPC = (
    configuration: NodeConfiguration,
    rpc: RequestVoteRPC
): RequestVoteResponse => {
    // If the RPC term is less than the current term, return a response with voteGranted set to false
    if (rpc.term < configuration.currentTerm) {
        return {
            term: configuration.currentTerm,
            voteGranted: false,
        };
    }

    // If the RPC term is greater than the current term, update the current term and set the node's state to follower
    if (rpc.term > configuration.currentTerm) {
        configuration.currentTerm = rpc.term;
        configuration.state = NodeState.FOLLOWER;
    }

    // If the node is already a leader or a candidate, return a response with voteGranted set to false
    if (configuration.state === NodeState.LEADER || configuration.state === NodeState.CANDIDATE) {
        return {
            term: configuration.currentTerm,
            voteGranted: false,
        };
    }

    // If the node has already voted for another candidate in this term, return a response with voteGranted set to false
    if (configuration.votedFor && configuration.votedFor !== rpc.candidateId) {
        return {
            term: configuration.currentTerm,
            voteGranted: false,
        };
    }

    // Otherwise, return the result of the requestVote function
    return requestVote(configuration, rpc);
};

const handleAppendEntriesRPC = (
    configuration: NodeConfiguration,
    rpc: AppendEntriesRPC
): AppendEntriesResponse => {
    // If the RPC term is less than the current term, return a response with success set to false
    if (rpc.term < configuration.currentTerm) {
        return {
            term: configuration.currentTerm,
            success: false,
            index:1
        };
    }

    // If the RPC term is greater than the current term, update the current term and set the node's state to follower
    if (rpc.term > configuration.currentTerm) {
        configuration.currentTerm = rpc.term;
        configuration.state = NodeState.FOLLOWER;
    }

    // If the node is a leader, return a response with success set to false
    if (configuration.state === NodeState.LEADER) {
        return {
            term: configuration.currentTerm,
            success: false,
            index: 1
        };
    }

    // Otherwise, return the result of the appendEntries function
    return appendEntries(configuration, rpc);
};


const advanceCommitIndex = (
    configuration: NodeConfiguration,
    responses: AppendEntriesResponse[]
): void => {
    // Sort the responses by term and index
    responses.sort((a, b) => a.term !== b.term ? a.term - b.term : a.index - b.index);

    // Find the highest index that is included in a majority of responses
    const majority = Math.floor(responses.length / 2) + 1;
    let commitIndex = 0;
    for (let i = 0; i < responses.length; i++) {
        if (responses.slice(0, i + 1).filter((r) => r.success).length >= majority) {
            commitIndex = responses[i].index;
        }
    }

    // Set the commit index to the highest index that is included in a majority of responses
    configuration.commitIndex = commitIndex;
};