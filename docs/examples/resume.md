---
layout: default
title: Resume
parent: Examples
nav_order: 2
permalink: /examples/resume/
---

# Resume

Boots an existing `root.raw`. Empty id = latest session for this repo.

```bash
go run ./examples/sdk-resume
go run ./examples/sdk-resume -- fc519f5f063cc4d25c6f3f36c1e95152
```

Repo: [`examples/sdk-resume`](https://github.com/AdminTurnedDevOps/ABox/tree/main/examples/sdk-resume)

The guest binary on that disk is whatever was cloned when the session was
created. Protocol 2 features need a session opened after `make image-update`.
