# ABox

Terminal-native coding-agent harness. The agent — prompts, model calls, and
tools — runs inside a libkrun microVM. The host is the TUI and VMM.

![](img/aboxlogo.jpg)

**Status:** experimental. Isolation is **Planned** until hardware acceptance
tests pass. This tree is a first runnable increment: TUI, fail-closed tools,
and a real libkrun boot path on Apple Silicon.

## Prerequisites

- Apple Silicon Mac
- Go 1.24+
- Docker (to pack the guest disk)
- libkrun and libkrunfw

```bash
brew tap libkrun/krun
brew trust libkrun/krun
brew install libkrun libkrunfw
```

Confirm `kern.hv_support` is `1`.

## Quickstart

From this repo (or any directory). If Git is missing, dirty, or has no
commits, ABox copies the files into a private snapshot and leaves your
host Git alone.

```bash
make build
make image
export PATH="$PWD/bin:$PATH"
export XAI_API_KEY=...          # or OPENAI_API_KEY / ANTHROPIC_API_KEY
abox
```

- `enter` sends a prompt (`shift+enter` / `alt+enter` for a new line)
- `/provider` sets Grok, OpenAI, or Anthropic API keys
- `ctrl+c` quits
- The agent runs only inside the guest

## Test

Open `abox`

Send any prompt.

Example:

![](img/lol.png)

## Choose Provider

Currently, Grok, OpenAI, and Anthropic are supported.

![](img/prov1.png)
![](img/prov2.png)

## Without Key

Prove the microVM without a model key:

```bash
abox --probe-vm
```

Headless agent loop:

```bash
abox exec --prompt "list the repository files"
```

Skip the VM (tools fail closed; useful for UI-only checks):

```bash
abox --no-vm
```

## What is not done yet

MCP, compaction, checkpoint/rollback/fork, agentgateway, and resource
acceptance. See `PLAN.md`.

## Security

Do not describe this build as verified isolation. The device plan is
allowlisted (no guest NIC, no host-path virtio-fs, TSI flags zero). Claims
stay Planned until the hardware suite in `PLAN.md` §21.4 passes.
