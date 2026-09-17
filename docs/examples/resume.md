---
layout: default
title: Resume
parent: Examples
nav_order: 2
permalink: /examples/resume/
---

# Resume

`abox.Resume` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Boots an existing `root.raw`. Empty id = latest session for this repo (`go run .` with no args). Pass a session id as `os.Args[1]`. The guest binary on that disk is whatever was cloned when the session was created. Protocol 4 is required; resume of a pre-rebuild disk returns `ErrGuestTooOld`.

## CLI

```bash
abox --resume                 # latest session for this repo
abox --resume <id>            # that session's root.raw
abox exec --resume --prompt "Summarize what we already did in this session."
```

Ids are directory names under `~/.abox/sessions/`. Resume boots the existing
`root.raw`; it does not re-copy the host git tree. Same protocol-4 rule as
the SDK: an old disk fails at start ([Errors]({{ '/examples/errors' | relative_url }})).

## SDK

```go
{% include examples/sdk-resume.go %}
```
