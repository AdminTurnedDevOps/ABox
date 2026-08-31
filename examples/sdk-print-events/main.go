package main

import (
	"context"
	"encoding/json"
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
	enc := json.NewEncoder(os.Stdout)
	_, err = sess.Turn(ctx, "List two files in the repo.", func(ev abox.Event) {
		_ = enc.Encode(ev)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
