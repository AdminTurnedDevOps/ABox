---
layout: default
title: Errors
parent: Examples
nav_order: 13
permalink: /examples/errors/
---

# Errors

`abox.ErrGuestTooOld` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Demonstrates a missing golden image, then `ErrGuestTooOld` when `Open` hits a guest older than protocol 4.

## CLI

Missing golden image:

```bash
abox --probe-vm
# guest image missing … (run: make image)
```

Old session disk (guest older than protocol 4):

```bash
abox --resume <id>
# guest protocol N cannot enforce brokered MCP and command approvals; rebuild the guest image and start a new session
```

```bash
make build && make image-update
abox                          # new session; do not resume the old id
```

`--probe-vm` can still list files on that old disk.
[Troubleshooting]({{ '/troubleshooting' | relative_url }}).

## SDK

```go
{% include examples/sdk-errors.go %}
```
