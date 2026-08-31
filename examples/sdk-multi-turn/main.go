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
	print := func(ev abox.Event) {
		if ev.Kind == "text" {
			fmt.Print(ev.Text)
		}
	}
	if _, err := sess.Turn(ctx, "Remember the codeword: cedar. Reply ok.", print); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println()
	if _, err := sess.Turn(ctx, "What was the codeword?", print); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println()
}
