package main

import (
	"context"
	"fmt"
	"os"

	"github.com/AdminTurnedDevOps/ABox/pkg/abox"
)

func main() {
	path := "README.md"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	ctx := context.Background()
	sess, err := abox.Open(ctx, abox.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sess.Close()
	got, err := sess.ReadFile(ctx, path, 8<<10)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if got.Binary {
		fmt.Println("binary file")
		return
	}
	fmt.Print(got.Content)
	if got.Trunc {
		fmt.Fprintln(os.Stderr, "(truncated)")
	}
}
