package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credentials"
	"github.com/AdminTurnedDevOps/ABox/internal/mcpauth"
	"github.com/AdminTurnedDevOps/ABox/internal/repository"
	"github.com/AdminTurnedDevOps/ABox/internal/runtime"
	"github.com/AdminTurnedDevOps/ABox/internal/session"
	"github.com/AdminTurnedDevOps/ABox/internal/tui"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "abox: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		return runMCP(os.Args[2:])
	}
	fs := flag.NewFlagSet("abox", flag.ContinueOnError)
	execFlag := fs.Bool("exec", false, "headless driver")
	prompt := fs.String("prompt", "", "prompt for exec mode")
	modelName := fs.String("model", "", "configured model profile name")
	noVM := fs.Bool("no-vm", false, "do not start a microVM; tools fail closed")
	probeVM := fs.Bool("probe-vm", false, "boot the guest and list files; no model call")
	args := os.Args[1:]
	execMode := false
	if len(args) > 0 && args[0] == "exec" {
		execMode = true
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *execFlag {
		execMode = true
	}

	cfg, cfgPath, err := config.Load()
	if err != nil {
		return err
	}
	if err := credentials.ApplyToEnv(); err != nil {
		return err
	}
	sel, ok := cfg.ModelNamed(*modelName)
	if !ok {
		return fmt.Errorf("no model profile %q (config %s)", *modelName, cfgPath)
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.SessionRoot(), 0o700); err != nil {
		return err
	}

	sess, err := session.Create(wd, "pending")
	if err != nil {
		return err
	}
	snap, err := repository.OpenForSession(wd, filepath.Join(sess.Dir, "host-tree"))
	if err != nil {
		return err
	}
	sess.RepoRoot = snap.Root
	sess.HEAD = snap.HEAD
	if err := sess.WriteMeta(); err != nil {
		return err
	}
	if snap.Ephemeral {
		fmt.Fprintf(os.Stderr, "abox: no clean committed worktree; using an ephemeral snapshot. host git is unchanged.\n")
	}

	var sb *runtime.Sandbox
	vmState := "not-started"
	if !*noVM {
		image := cfg.Runtime.Image
		if image == "" {
			image = filepath.Join(config.ImageDir(), "abox-guest.raw")
		}
		mcpServers, err := cfg.ResolvedMCPServers()
		if err != nil {
			return err
		}
		if err := runtime.Prepare(sess, image, sel, currentSecrets(cfg), mcpServers); err != nil {
			if execMode {
				return err
			}
			fmt.Fprintf(os.Stderr, "abox: vm prepare: %v\n", err)
			vmState = "unavailable"
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			vcpu, ram := cfg.Resources.VCPU, cfg.Resources.RAMMiB
			if vcpu == 0 {
				vcpu = 1
			}
			if ram == 0 {
				ram = 768
			}
			started, err := runtime.Start(ctx, sess, cfg.Runtime.VMMPath, vcpu, ram)
			if err != nil {
				if execMode && *prompt != "" {
					return fmt.Errorf("start vm: %w", err)
				}
				fmt.Fprintf(os.Stderr, "abox: vm start: %v\n", err)
				vmState = "failed"
			} else {
				sb = started
				vmState = "ready"
				defer sb.Stop()
				archive, err := repository.ArchiveHEAD(snap.Root)
				if err != nil {
					return err
				}
				if err := sb.TransferArchive(context.Background(), archive); err != nil {
					fmt.Fprintf(os.Stderr, "abox: repo transfer: %v\n", err)
				}
			}
		}
	}

	if *probeVM {
		if sb == nil {
			return fmt.Errorf("vm not ready (%s)", vmState)
		}
		var res protocol.ListFilesResult
		if err := sb.Call(context.Background(), "list_files", protocol.ListFilesParams{Path: ".", Depth: 4, Limit: 50}, &res); err != nil {
			return fmt.Errorf("guest list_files: %w", err)
		}
		fmt.Println("guest ready; files:")
		for _, p := range res.Paths {
			fmt.Println(p)
		}
		return nil
	}
	if execMode {
		return runExec(sb, *prompt)
	}
	return tui.Run(cfg, sel, sb, vmState)
}

func runExec(sb *runtime.Sandbox, prompt string) error {
	if prompt == "" {
		return fmt.Errorf("abox exec requires --prompt")
	}
	if sb == nil {
		return fmt.Errorf("agent runs only in the microVM")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	enc := json.NewEncoder(os.Stdout)
	return sb.UserTurn(ctx, prompt, func(e protocol.AgentEvent) {
		_ = enc.Encode(e)
	})
}

func currentSecrets(cfg config.File) map[string]string {
	out := map[string]string{}
	for _, name := range []string{"XAI_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
		if v := os.Getenv(name); v != "" {
			out[name] = v
		}
	}
	servers, err := cfg.ResolvedMCPServers()
	if err != nil {
		return out
	}
	for _, s := range servers {
		name := config.TokenEnv(s)
		if v := os.Getenv(name); v != "" {
			out[name] = v
		}
	}
	return out
}

func runMCP(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: abox mcp login <server-name>")
	}
	switch args[0] {
	case "login":
		if len(args) < 2 {
			return fmt.Errorf("usage: abox mcp login <server-name>")
		}
		return mcpLogin(args[1])
	default:
		return fmt.Errorf("unknown mcp command %q", args[0])
	}
}

func mcpLogin(name string) error {
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	if err := credentials.ApplyToEnv(); err != nil {
		return err
	}
	return mcpauth.LoginNamed(context.Background(), cfg, name)
}
