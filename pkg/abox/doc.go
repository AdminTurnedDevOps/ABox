// Package abox is the public Go SDK for embedding an ABox microVM agent session.
//
// Runtime requirements: Apple Silicon, libkrun/libkrunfw, and a golden guest
// image (`make image`). Open and Resume require protocol 4 (host LLM/MCP
// brokers and run_command approval). Older guests return ErrGuestTooOld.
// Protocol 2 was the secret-push path; protocol 3 added the host provider
// broker. Credentials and MCP tokens stay on the host.
package abox
