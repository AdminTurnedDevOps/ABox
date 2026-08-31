---
layout: default
title: Resume
parent: Examples
nav_order: 2
permalink: /examples/resume/
---

# Resume

One SDK: [`pkg/abox`]({{ '/api' | relative_url }}). This page is a sample program that calls `abox.Resume`.

Boots an existing `root.raw`. Empty id = latest session for this repo. The guest binary on that disk is whatever was cloned when the session was created. Protocol 2 features need a session opened after `make image-update`.

Copy into your own `main.go` (`go get github.com/AdminTurnedDevOps/ABox@latest`):

```go
{% include examples/sdk-resume.go %}
```

Optional: run this sample (same module as every other sample, not a second SDK):

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-resume@latest
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-resume@latest SESSION_ID
```
