package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/AdminTurnedDevOps/ABox/pkg/abox"
)

func main() {
	ctx := context.Background()
	_, err := abox.Open(ctx, abox.Options{
		Image:       "/nonexistent/abox-guest.raw",
		BootTimeout: 5 * time.Second,
	})
	if err == nil {
		fmt.Fprintln(os.Stderr, "expected missing image error")
		os.Exit(1)
	}
	fmt.Println("missing image:", err)

	sess, err := abox.Open(ctx, abox.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sess.Close()
	if sess.Capabilities().Protocol >= 2 {
		fmt.Println("guest is protocol 2; ErrGuestTooOld will not fire")
		return
	}
	_, err = sess.TurnOpts(ctx, "hi", abox.TurnOpts{MaxTurns: 2}, nil)
	if !errors.Is(err, abox.ErrGuestTooOld) {
		fmt.Println("expected ErrGuestTooOld, got", err)
		os.Exit(1)
	}
	fmt.Println("v1 guest:", err)
}
