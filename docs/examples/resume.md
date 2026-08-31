---
layout: default
title: Resume
parent: Examples
nav_order: 2
permalink: /examples/resume/
---

# Resume

`abox.Resume` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Boots an existing `root.raw`. Empty id = latest session for this repo (`go run .` with no args). Pass a session id as `os.Args[1]`. The guest binary on that disk is whatever was cloned when the session was created. Protocol 2 features need a session opened after `make image-update`.

```go
{% include examples/sdk-resume.go %}
```
