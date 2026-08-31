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
	paths, err := sess.ListFiles(ctx, ".", 4, 50)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("guest files:")
	for _, p := range paths {
		fmt.Println(p)
	}
}
