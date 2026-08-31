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

Put a provider key in the host (the SDK loads `~/.abox/credentials.env` the
same way the CLI does):

```text
# in the ABox TUI
/provider
```

or write `~/.abox/credentials.env` (mode `0600`) with `XAI_API_KEY=…` (or
OpenAI / Anthropic).

## First program

```bash
cd /path/to/your/git/repo    # SDK snapshots this tree into the guest
go run github.com/AdminTurnedDevOps/ABox/examples/sdk-basic@latest
```

Or copy [`examples/sdk-basic`](https://github.com/AdminTurnedDevOps/ABox/tree/main/examples/sdk-basic):

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
export PATH="/path/to/ABox/bin:$PATH"   # abox-vmm
go run .
```

You should see `protocol 2` and a streamed sentence. `Close()` stops the VM.

## Module import

```bash
go get github.com/AdminTurnedDevOps/ABox/pkg/abox
```

`abox-vmm` must be on `PATH`, or set `Options.VMMPath`.

## Next

- [Examples]({{ '/examples' | relative_url }}) — resume, cancel, probe, patch export, …
- [Troubleshooting]({{ '/troubleshooting' | relative_url }}) — missing image, v1 guest, codesign
