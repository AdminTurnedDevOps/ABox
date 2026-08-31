package main

import (
	"context"
	"fmt"
	"os"

	"github.com/AdminTurnedDevOps/ABox/pkg/abox"
)

func main() {
	ctx := context.Background()
	sess, err := abox.Open(ctx, abox.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sess.Close()
	c := sess.Capabilities()
	fmt.Printf("protocol=%d cancel=%v rich=%v turn_opts=%v\n", c.Protocol, c.Cancel, c.RichEvents, c.TurnOptions)
	if c.Protocol < 2 {
		fmt.Fprintln(os.Stderr, "rebuild guest: make build && make image-update, then Open a new session")
		os.Exit(1)
	}
}
