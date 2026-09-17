---
layout: default
title: Resume
parent: Examples
nav_order: 2
permalink: /examples/resume/
---

# Resume

`abox.Resume` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Boots an existing `root.raw` by session id (`go run . <id>`). The guest binary
on that disk is whatever was cloned when the session was created. Protocol 4
is required; resume of a pre-rebuild disk returns `ErrGuestTooOld`.

## CLI

```bash
abox --resume <id>            # that session's root.raw
abox exec --resume <id> --prompt "Summarize what we already did in this session."
```

Ids are directory names under `~/.abox/sessions/`. Resume boots the existing
`root.raw`; it does not re-copy the host source directory. Same protocol-4 rule as
the SDK: an old disk fails at start ([Errors]({{ '/examples/errors' | relative_url }})).

## SDK

```go
{% include examples/sdk-resume.go %}
```
