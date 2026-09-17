---
layout: default
title: Run command
parent: Examples
nav_order: 7
permalink: /examples/run-command/
---

# Run command

`Session.RunCommand` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Guest `/bin/sh -c`. This is a supervisor RPC, not the model-tool approval gate. Default command: `uname -a && pwd && ls`. Extra args are the command (`go run . cat /etc/os-release`). Model-authored shell: [Approvals]({{ '/examples/approvals' | relative_url }}).

```go
{% include examples/sdk-run-command.go %}
```
