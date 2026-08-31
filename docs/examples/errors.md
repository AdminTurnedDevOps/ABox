---
layout: default
title: Errors
parent: Examples
nav_order: 13
permalink: /examples/errors/
---

# Errors

`abox.ErrGuestTooOld` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Demonstrates a missing golden image, then `ErrGuestTooOld` when the guest is v1 and `TurnOpts` is used.

```go
{% include examples/sdk-errors.go %}
```
