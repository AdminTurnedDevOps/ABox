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
	env := os.Getenv("ABOX_MCP_TOKEN_ENV")
	tok := os.Getenv("ABOX_MCP_TOKEN")
	if env == "" || tok == "" {
		fmt.Fprintln(os.Stderr, "set ABOX_MCP_TOKEN_ENV (guest env name) and ABOX_MCP_TOKEN")
		os.Exit(2)
	}
	if err := sess.SetMCPTokens(ctx, map[string]string{env: tok}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("mcp token set for", env)
	_, err = sess.Turn(ctx, "List your MCP tools by name.", func(ev abox.Event) {
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
