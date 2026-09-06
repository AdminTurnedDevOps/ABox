// Package abox is the public Go SDK for embedding an ABox microVM agent session.
//
// Runtime requirements: Apple Silicon, libkrun/libkrunfw, and a golden guest
// image (`make image`). Protocol-1 guests cannot consume current secretless
// session configuration and are rejected with ErrGuestTooOld. Protocol 2 is
// the legacy secret-push path; protocol 3 uses the host provider broker.
package abox
