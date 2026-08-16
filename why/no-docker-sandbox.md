The thought of "why not just use Docker Sandbox instead?" came to mind when I was building this out.

Here are a few reasons why:

1. It spins up the Docker engine inside of the sandbox vs just using microvm
2. Its not a harness or an Agent, its the infrastructure runtime layer
3. You'd put your agent harness INSIDE of the Docker Agent

When thinking about these things, I began to imagine the constraint on resources locally and the extra hops it would take to hit your actual Agent vs having a Harness thats sandboxed-out-of-the-box (that sounds cool... I'll have to steal it from myself).

These aren't inherently "bad things". I'm just thinking to myself as I'm building out the solution "I want to be as close to the MicroVM as possible. I don't want additional hops where things can break out and go wrong. I want isolation at its purest form".
