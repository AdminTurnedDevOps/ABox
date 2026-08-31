// Package abox is the public Go SDK for embedding an ABox microVM agent session.
//
// Runtime requirements: Apple Silicon, libkrun/libkrunfw, and a golden guest
// image (`make image`). Resume of a session created before a guest rebuild
// may speak protocol v1; Capabilities() reports what that guest supports.
// Turn options, rich events, and mid-turn cancel require protocol 2.
package abox
