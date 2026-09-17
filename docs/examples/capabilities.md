---
layout: default
title: Capabilities
parent: Examples
nav_order: 11
permalink: /examples/capabilities/
---

# Capabilities

`Session.Capabilities` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Prints protocol and feature flags. Exit 1 if the guest is below protocol 4 (`Open` already rejects that).

## CLI

The CLI does not print capability flags. A TUI or `exec` session already
requires protocol 4; older disks fail at start.

`--probe-vm` is the exception: it still lists files on a pre-protocol-4
guest.

```bash
abox --probe-vm               # list files even on an old disk
abox                          # fails if guest < protocol 4
abox --resume <old-id>        # same; rebuild then Open a new session
```

Rebuild: `make build && make image-update`. [Errors]({{ '/examples/errors' | relative_url }}).

## SDK

```go
{% include examples/sdk-capabilities.go %}
```
