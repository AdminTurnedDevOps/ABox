---
layout: default
title: Approvals
parent: Examples
nav_order: 17
permalink: /examples/approvals/
---

# Approvals

`Session.SetApprover` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Without an approver, model-authored `run_command` is denied. This sample
prints the command and allows it once. Host-initiated `RunCommand` does not
use this gate.

## CLI

The TUI is the CLI approver. When the model calls `run_command`:

```text
abox
# APPROVAL REQUIRED
# ← / k  deny
# → / j  allow once
# Enter  confirm
# Esc    deny
```

Deny is selected by default. There is no “remember for this session”.

Headless has no approver:

```bash
abox exec --prompt "Run uname -a in the guest."
```

Model `run_command` is **denied**. [Approvals]({{ '/approvals' | relative_url }}).

## SDK

```go
{% include examples/sdk-approvals.go %}
```
