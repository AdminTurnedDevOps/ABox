---
layout: default
title: Basic turn
parent: Examples
nav_order: 1
permalink: /examples/basic/
---

# Basic turn

`Open`, one `Turn`, stream `text`, `Close` on exit. SIGINT cancels the process
(protocol 2 also cancels the in-flight turn).

Repo: [`examples/sdk-basic`](https://github.com/AdminTurnedDevOps/ABox/tree/main/examples/sdk-basic)

```bash
export PATH="/path/to/ABox/bin:$PATH"
go run ./examples/sdk-basic
```

See [Quickstart]({{ '/quickstart' | relative_url }}) for the full listing.
