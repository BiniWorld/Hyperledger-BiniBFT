// const appendEntries = (
//     configuration: NodeConfiguration,
//     rpc: AppendEntriesRPC
// ): AppendEntriesResponse => {
//     // If the RPC term is less than the current term, return a response with success set to false
//     if (rpc.term < configuration.currentTerm) {
//         return {
//             term: configuration.currentTerm,
//             success: false,
//         };
//     }

//     // If the RPC term is greater than the current term, update the current term and set the node's state to follower
//     if (rpc.term > configuration.currentTerm) {
//         configuration.currentTerm = rpc.term;
//         configuration.state = NodeState.FOLLOWER;
//     }

//     // If the previous log index and term don't match the node's log, return a response with success set to false
//     const prevLogIndex = rpc.prevLogIndex;
//     const prevLogTerm = rpc.prevLogTerm;
//     if (configuration.log[prevLogIndex]?.term !== prevLogTerm) {
//         return {
//             term: configuration.currentTerm,
//             success: false,
//         };
//     }

//     // Otherwise, append the new entries to the log and return a response with success set to true
//     configuration.log = [...configuration.log.slice(0, prevLogIndex + 1), ...rpc.entries];
//     configuration.commitIndex = Math.min(rpc.leaderCommit, configuration.log.length - 1);
//     return {
//         term: configuration.currentTerm,
//         success: true,
//     };
// };