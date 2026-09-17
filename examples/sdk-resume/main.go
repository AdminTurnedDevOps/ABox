package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/AdminTurnedDevOps/ABox/pkg/abox"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: sdk-resume <session-id>")
		os.Exit(2)
	}
	sess, err := abox.Resume(ctx, os.Args[1], abox.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sess.Close()
	fmt.Println("resumed", sess.ID(), "protocol", sess.Capabilities().Protocol)
	_, err = sess.Turn(ctx, "Summarize what we already did in this session.", func(ev abox.Event) {
		if ev.Kind == "text" {
			fmt.Print(ev.Text)
		}
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println()
}
