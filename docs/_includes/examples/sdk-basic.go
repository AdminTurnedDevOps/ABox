package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/AdminTurnedDevOps/ABox/pkg/abox"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "sdk-basic: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	sess, err := abox.Open(ctx, abox.Options{})
	if err != nil {
		return err
	}
	defer sess.Close()
	fmt.Printf("session %s protocol %d\n", sess.ID(), sess.Capabilities().Protocol)
	res, err := sess.Turn(ctx, "Say hello in one sentence.", func(ev abox.Event) {
		switch ev.Kind {
		case "text":
			fmt.Print(ev.Text)
		case "tool":
			fmt.Printf("\n[tool %s %s]\n", ev.Tool, ev.Status)
		}
	})
	fmt.Println()
	if err != nil {
		return err
	}
	if res != nil && res.Usage != nil {
		fmt.Printf("usage in=%d out=%d stop=%s canceled=%v\n", res.Usage.InputTokens, res.Usage.OutputTokens, res.StopReason, res.Canceled)
	}
	return nil
}
