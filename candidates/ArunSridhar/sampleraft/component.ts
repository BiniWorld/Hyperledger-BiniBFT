// const requestVote = (
//     configuration: NodeConfiguration,
//     rpc: RequestVoteRPC
// ): RequestVoteResponse => {
//     // If the RPC term is less than the current term, return a response with voteGranted set to false
//     if (rpc.term < configuration.currentTerm) {
//         return {
//             term: configuration.currentTerm,
//             voteGranted: false,
//         };
//     }

//     // If the RPC term is greater than the current term, update the current term and vote for the candidate
//     if (rpc.term > configuration.currentTerm) {
//         configuration.currentTerm = rpc.term;
//         configuration.votedFor = rpc.candidateId;
//     }

//     // If the voter has already voted for a candidate in this term, return a response with voteGranted set to false
//     if (configuration.votedFor && configuration.votedFor !== rpc.candidateId) {
//         return {
//             term: configuration.currentTerm,
//             voteGranted: false,
//         };
//     }

//     // If the candidate's log is not up-to-date, return a response with voteGranted set to false
//     const lastLogIndex = configuration.log.length - 1;
//     const lastLogTerm = configuration.log[lastLogIndex].term;
//     if (lastLogTerm > rpc.lastLogTerm || (lastLogTerm === rpc.lastLogTerm && lastLogIndex > rpc.lastLogIndex)) {
//         return {
//             term: configuration.currentTerm,
//             voteGranted: false,
//         };
//     }

//     // Otherwise, return a response with voteGranted set to true
//     configuration.votedFor = rpc.candidateId;
//     return {
//         term: configuration.currentTerm,
//         voteGranted: true,
//     };
// };