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

```go
{% include examples/sdk-approvals.go %}
```
