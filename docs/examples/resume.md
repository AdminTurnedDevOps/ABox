---
layout: default
title: Resume
parent: Examples
nav_order: 2
permalink: /examples/resume/
---

# Resume

Boots an existing `root.raw`. Empty id = latest session for this repo. The guest binary on that disk is whatever was cloned when the session was created. Protocol 2 features need a session opened after `make image-update`.

```bash
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-resume@latest
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-resume@latest SESSION_ID
```

```go
{% include examples/sdk-resume.go %}
```
