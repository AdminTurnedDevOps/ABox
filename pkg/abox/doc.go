// Package abox is the public Go SDK for embedding an ABox microVM agent session.
//
// Runtime requirements: macOS/arm64 with HVF or a supported Linux/amd64 KVM
// baseline, libkrun/libkrunfw, and a matching golden guest image (`make image`).
// Linux runtime support remains Planned until the documented hardware gates
// pass. Open and Resume require protocol 4 (host LLM/MCP
// brokers and run_command approval). Older guests return ErrGuestTooOld.
// Protocol 2 was the secret-push path; protocol 3 added the host provider
// broker. Credentials and MCP tokens stay on the host.
package abox
