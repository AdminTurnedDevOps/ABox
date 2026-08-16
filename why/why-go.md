If you look at Harnesses, they're written in a few different languages:

- Go
- TS
- Rust

and when thinking about what language to write a Harness in, you should think about:

- Harness is I/O-bound glue. Every meaningful latency cost is the model generating tokens over the wire. Whether your event loop dispatches a tool call in 5ms or 50ms is invisible next to a 4-second streaming response
- Distribution of the Harness. A Go or Rust binary means no runtime, no version conflicts, and it drops cleanly into containers, CI runners, and locked-down environments.
- Startup time
- Concurrency for paralell sub-agents
- Fast startup that doesn't undo Firecracker's boot time.
- Goroutines that fit process supervision and fan-out across VMs.

Because of the above, Go or Rust are naturally great languages. Because I like Go, I went with Go. No other reason as it would've been perfectly suitable in Rust. I might even have an Agent do a Rust version for comparison at some point.
