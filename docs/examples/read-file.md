---
layout: default
title: Read file
parent: Examples
nav_order: 6
permalink: /examples/read-file/
---

# Read file

`Session.ReadFile` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Reads a guest path. Default `README.md`; pass another path as `os.Args[1]` (`go run . go.mod`).

```go
{% include examples/sdk-read-file.go %}
```
