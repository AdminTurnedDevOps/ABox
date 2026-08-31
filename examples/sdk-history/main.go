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
	fmt.Println("hello history", len(sess.History()), "lines")
	_, err = sess.Turn(ctx, "Reply with the word ping.", func(ev abox.Event) {
		if ev.Kind == "text" {
			fmt.Print(ev.Text)
		}
	})
	fmt.Println()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for i, h := range sess.History() {
		fmt.Printf("%d %s %q\n", i, h.Kind, trunc(h.Text, 60))
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
