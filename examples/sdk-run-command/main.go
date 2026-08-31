package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/AdminTurnedDevOps/ABox/pkg/abox"
)

func main() {
	cmd := "uname -a && pwd && ls"
	if len(os.Args) > 1 {
		cmd = strings.Join(os.Args[1:], " ")
	}
	ctx := context.Background()
	sess, err := abox.Open(ctx, abox.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sess.Close()
	res, err := sess.RunCommand(ctx, cmd, 15)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(res.Stdout)
	fmt.Fprint(os.Stderr, res.Stderr)
	fmt.Println("exit", res.ExitCode, "dur", res.Duration)
}
