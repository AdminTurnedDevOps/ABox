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

```go
{% include examples/sdk-errors.go %}
```
