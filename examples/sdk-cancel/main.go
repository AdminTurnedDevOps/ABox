package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/AdminTurnedDevOps/ABox/pkg/abox"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	sess, err := abox.Open(ctx, abox.Options{BootTimeout: 60 * time.Second})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sess.Close()
	if !sess.Capabilities().Cancel {
		fmt.Fprintln(os.Stderr, "guest cannot cancel (protocol", sess.Capabilities().Protocol, ")")
		os.Exit(1)
	}
	turnCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	res, err := sess.Turn(turnCtx, "Count slowly from 1 to 50 in words.", func(ev abox.Event) {
		if ev.Kind == "text" {
			fmt.Print(ev.Text)
		}
	})
	fmt.Println()
	if res != nil {
		fmt.Println("canceled", res.Canceled)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
