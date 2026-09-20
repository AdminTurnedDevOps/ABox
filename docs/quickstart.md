---
layout: default
title: Quickstart
nav_order: 2
permalink: /quickstart/
---

# Quickstart
{: .no_toc }

1. TOC
{:toc}

## Prerequisites

- Go 1.25+

The currently runnable host path requires an Apple Silicon Mac with
`kern.hv_support = 1`, Homebrew libkrun/libkrunfw, and Docker to pack the guest
root filesystem. Docker is never on the session execution path.

```bash
brew tap libkrun/krun
brew trust libkrun/krun
brew install libkrun libkrunfw
```

The Linux host/build port is implemented for amd64 and arm64, and Linux image
creation is rootless and Docker-free. Linux VMM execution, release support, and
KVM isolation remain **Planned** until separate Phase 0.5/18 evidence passes on
the pinned Arch and Fedora x86_64 baselines. WSL2 and containerized VMM
execution are unsupported. See [Platforms]({{ '/platforms' | relative_url }})
for package versions, build dependencies, and the exact boundary.

For the currently runnable macOS setup, from a clone of ABox:

```bash
make build
make image          # first time only; Docker
export PATH="$PWD/bin:$PATH"
```

Put a provider key on the **host** (the guest never receives it). In the TUI:

```text
/provider
```

That saves to the OS keystore first: macOS Keychain or Linux Secret Service.
If it is locked, absent, or times out, ABox warns and saves to
`~/.abox/credentials.env` (mode `0600`). Cloud stores (Vault, Azure Key Vault,
AWS Secrets Manager) use [`/credential`]({{ '/credentials' | relative_url }}).

You can also write `~/.abox/credentials.env` yourself with `XAI_API_KEY=…`
(or OpenAI / Anthropic).

## First program

One import: `github.com/AdminTurnedDevOps/ABox/pkg/abox`. Set `RepoPath` to any
path inside a Git worktree. The SDK discovers the repository root and snapshots
committed files plus any tracked modifications and non-ignored untracked files.
Git-ignored files, `.git` metadata, the active ABox state directory, symlinks,
and special files are excluded.

```bash
go get github.com/AdminTurnedDevOps/ABox@latest
```

Copy this into `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/AdminTurnedDevOps/ABox/pkg/abox"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	sess, err := abox.Open(ctx, abox.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sess.Close()

	fmt.Println("session", sess.ID(), "protocol", sess.Capabilities().Protocol)
	_, err = sess.Turn(ctx, "Say hello in one sentence.", func(ev abox.Event) {
		if ev.Kind == "text" {
			fmt.Print(ev.Text)
		}
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println()
}
```

```bash
export PATH="/path/to/abox-vmm-dir:$PATH"   # from the GitHub release archive
go run .
```

You should see `protocol 4` and a streamed sentence. `Close()` stops the VM.

A successful `Open` always speaks protocol 4. Older session disks return
`ErrGuestTooOld` — rebuild the guest (`make build && make image-update`) and
`Open` a **new** session. Do not resume the old id.

`abox-vmm` must be on `PATH`, or set `Options.VMMPath`. More methods:
[Examples]({{ '/examples' | relative_url }}).

## Terminal instead of Go

```bash
abox                  # TUI
abox --probe-vm       # boot + list files; no model key
abox exec --prompt "list the repository files"
```

[CLI and TUI]({{ '/cli' | relative_url }}) covers slash commands, resume, and
MCP login.

## Next

- [Concepts]({{ '/concepts' | relative_url }}) — host brokers, secretless config
- [Examples]({{ '/examples' | relative_url }}) — resume, cancel, probe, approvals, …
- [Platforms]({{ '/platforms' | relative_url }}) — host support, Linux builds, images
- [Troubleshooting]({{ '/troubleshooting' | relative_url }}) — images, loaders, KVM, codesign
