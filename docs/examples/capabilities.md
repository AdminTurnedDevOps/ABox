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

```go
{% include examples/sdk-capabilities.go %}
```
