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

- Apple Silicon Mac (`kern.hv_support` = 1)
- Go 1.25+
- Docker once, to pack the golden root filesystem (`make image`)
- Homebrew libkrun + libkrunfw

```bash
brew tap libkrun/krun
brew trust libkrun/krun
brew install libkrun libkrunfw
```

From a clone of ABox:

```bash
make build
make image          # first time only; Docker
export PATH="$PWD/bin:$PATH"
```

Put a provider key on the **host** (the guest never receives it). In the TUI:

```text
/provider
```

That saves to the macOS keychain first, then `~/.abox/credentials.env` (mode
`0600`) if the keychain is locked. Cloud stores (Vault, Azure Key Vault, AWS
Secrets Manager) use [`/credential`]({{ '/credentials' | relative_url }}).

You can also write `~/.abox/credentials.env` yourself with `XAI_API_KEY=…`
(or OpenAI / Anthropic).

## First program

One import: `github.com/AdminTurnedDevOps/ABox/pkg/abox`. Work from any git
repo (the SDK snapshots that tree into the guest).

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
- [Troubleshooting]({{ '/troubleshooting' | relative_url }}) — missing image, old guest, codesign
