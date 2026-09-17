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

	sess, err := abox.Open(ctx, abox.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sess.Close()

	err = sess.SetApprover(abox.ApproverFunc(func(_ context.Context, req abox.ApprovalRequest) (abox.ApprovalDecision, error) {
		fmt.Fprintf(os.Stderr, "approve %s: %s\n", req.Tool, req.Command)
		return abox.ApprovalAllowOnce, nil
	}))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = sess.Turn(ctx, "Run uname -a in the guest.", func(ev abox.Event) {
		if ev.Kind == "text" {
			fmt.Print(ev.Text)
		}
		if ev.Kind == "tool" {
			fmt.Printf("\n[tool %s %s]\n", ev.Tool, ev.Status)
		}
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println()
}
