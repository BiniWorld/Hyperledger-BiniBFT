# `Mir-BFT`

> Mir-BFT is a novel multi-leader consensus protocol designed to tackle scalability issues in existing Byzantine Fault Tolerant (BFT) protocols. These problems arise when operating in Wide Area Networks (WANs). To enhance throughput in such deployments, Mir-BFT introduces parallel execution. It effectively addresses concerns related to duplication and censorship attacks.

> In Mir-BFT, the protocol employs multiple leaders and employs a rotating hash assignment mechanism to counter duplication attacks. This mechanism involves distributing hash assignments among leaders to prevent Byzantine clients from submitting identical requests simultaneously. This safeguards against the manipulation of duplicated requests.

The protocol mitigates the problem of censorship attacks executed by Byzantine leaders, where client requests are intentionally delayed or discarded. By allowing parallel execution across multiple leaders, Mir-BFT enhances resistance against censorship, ensuring that the consensus process is not bottlenecked by the actions of a single malicious leader.

To optimize performance, Mir-BFT introduces client signature verification sharing. This strategy reduces computation bottlenecks, making the protocol more efficient. By implementing these innovative features, Mir-BFT stands out as a solution for achieving high throughput.

## Setting up development environment

- Resolve all dependencies
```bash
go mod tidy
```

- Run the application by
```bash
go run main.go > output.log 2>&1
```