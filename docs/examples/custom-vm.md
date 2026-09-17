---
layout: default
title: Custom VM
parent: Examples
nav_order: 12
permalink: /examples/custom-vm/
---

# Custom VM

`abox.Options` on the one SDK, [`pkg/abox`]({{ '/api' | relative_url }}).

Sets vCPU, RAM, boot timeout, and `VMMPath`. Lists files (no model).

## CLI

No `--vcpu` / `--ram` flags. Edit `~/.abox/config.yaml` (`ABOX_HOME`
overrides the home):

```yaml
resources:
  vcpu: 1
  ram_mib: 768
runtime:
  isolation: microvm
  backend: libkrun
  network: deny-by-default
  # image: /path/to/abox-guest.raw
  # vmm_path: /path/to/abox-vmm
```

Then:

```bash
abox --probe-vm
```

CLI boot timeout is 45s (not a flag). SDK `Options.BootTimeout` / `VCPU` /
`RAMMiB` / `Image` / `VMMPath` override the file for that process.

## SDK

```go
{% include examples/sdk-custom-vm.go %}
```
