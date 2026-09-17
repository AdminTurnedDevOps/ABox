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
	if errors.Is(err, abox.ErrGuestTooOld) {
		fmt.Println("rebuild guest: make build && make image-update, then Open a new session")
		fmt.Println(err)
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sess.Close()
	fmt.Printf("guest is protocol %d; Open already rejected older disks\n", sess.Capabilities().Protocol)
}
