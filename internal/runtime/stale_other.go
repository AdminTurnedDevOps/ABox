//go:build !linux

package runtime

import "github.com/AdminTurnedDevOps/ABox/internal/session"

func recordHelper(*session.Session, int, string) error  { return nil }
func clearHelper(*session.Session) error                { return nil }
func cleanupStaleHelper(*session.Session, string) error { return nil }
