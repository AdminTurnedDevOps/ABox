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

## CLI

The CLI has no supervisor `RunCommand` probe. `--probe-vm` only calls
`list_files`. Host-initiated shell is SDK-only.

Model-authored `run_command` is the [approvals]({{ '/examples/approvals' | relative_url }})
gate: the TUI prompts; `abox exec` denies.

```bash
abox --probe-vm
abox                                 # TUI; model shell prompts
abox exec --prompt "Run uname -a"    # denied (no approver)
```

## SDK

```go
{% include examples/sdk-run-command.go %}
```
