package main

import (
	"context"
	"fmt"
	"os"
	"time"

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
	res, err := sess.TurnOpts(ctx, "Inspect the repo, then stop.", abox.TurnOpts{
		MaxTurns: 4,
		Timeout:  2 * time.Minute,
	}, func(ev abox.Event) {
		if ev.Kind == "text" {
			fmt.Print(ev.Text)
		}
		if ev.Kind == "tool" {
			fmt.Printf("\n[tool %s %s]\n", ev.Tool, ev.Status)
		}
	})
	fmt.Println()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if res != nil && res.Usage != nil {
		fmt.Printf("usage in=%d out=%d stop=%s\n", res.Usage.InputTokens, res.Usage.OutputTokens, res.StopReason)
	}
}
