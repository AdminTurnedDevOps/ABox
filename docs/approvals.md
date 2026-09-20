---
layout: default
title: Approvals
nav_order: 7
permalink: /approvals/
---

# Approvals
{: .no_toc }

1. TOC
{:toc}

Model-authored `run_command` does not run until the host says so. Default
is **deny**. Protocol 4 is required.

An allowed command runs as the unprivileged guest user (UID/GID 1000), not as
the root-owned guest agent. Its dedicated process group is terminated when the
command returns or is canceled, so `allow_once` cannot leave a background shell
or replace `/usr/local/bin/abox-guest` for a later resume.

MCP tools, `apply_patch`, and the read-only builtins do not prompt. Host
import of a guest patch is not built yet.

## What is gated

| Action | Gate |
| --- | --- |
| Model calls `run_command` during `Turn` | Host approval (`allow_once` or `deny`) |
| `Session.RunCommand` / `abox --probe-vm` | None — supervisor RPC, not a model tool |
| MCP tool call | None yet |
| `apply_patch` / `list_files` / `read_file` / `search` | None |

The guest sends `request_run_command_approval` with the command, workdir,
and timeout. Anything other than `allow_once` (missing approver, error,
canceled turn, unknown decision) is deny.

## TUI

The TUI installs an approver that opens **APPROVAL REQUIRED**. Deny is
focused. ←/k deny, →/j allow once, Enter confirms, Esc denies. There is
no “remember for this session”.

Details: [CLI and TUI]({{ '/cli' | relative_url }}).

## Headless

`abox exec` uses the same agent and broker, but does **not** install an
approver. Model `run_command` is denied. Probe with `Session.RunCommand`
or `abox --probe-vm` if you need a supervisor shell in the guest.

## SDK

```go
err = sess.SetApprover(abox.ApproverFunc(func(ctx context.Context, req abox.ApprovalRequest) (abox.ApprovalDecision, error) {
    if req.Command == "uname -a" {
        return abox.ApprovalAllowOnce, nil
    }
    return abox.ApprovalDeny, nil
}))
```

`SetApprover(nil)` restores fail-closed. Cannot change the approver during
a turn. [Approvals example]({{ '/examples/approvals' | relative_url }}).

`ApprovalRequest` carries `Tool` (always `run_command` today), `ToolID`,
`Command`, `WorkDir`, and `TimeoutSec`. Empty workdir means `/work/repo`.
