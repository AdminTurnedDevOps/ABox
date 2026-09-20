// To run the Harness

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/AdminTurnedDevOps/ABox/internal/agent"
	"github.com/AdminTurnedDevOps/ABox/internal/config"
	"github.com/AdminTurnedDevOps/ABox/internal/credentials"
	"github.com/AdminTurnedDevOps/ABox/internal/credsource"
	"github.com/AdminTurnedDevOps/ABox/internal/hostbroker"
	"github.com/AdminTurnedDevOps/ABox/internal/mcpauth"
	"github.com/AdminTurnedDevOps/ABox/internal/repository"
	"github.com/AdminTurnedDevOps/ABox/internal/runtime"
	"github.com/AdminTurnedDevOps/ABox/internal/session"
	"github.com/AdminTurnedDevOps/ABox/internal/tui"
	"github.com/AdminTurnedDevOps/ABox/protocol"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), terminationSignals()...)
	go func() {
		<-ctx.Done()
		// Restore default handling so a second termination signal forces exit.
		stop()
	}()
	err := run(ctx)
	stop()
	if err != nil && !(ctx.Err() != nil && errors.Is(err, context.Canceled)) {
		fmt.Fprintf(os.Stderr, "abox: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if err := scrubLegacySessions(); err != nil {
		return err
	}
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		return runMCP(ctx, os.Args[2:])
	}
	if len(os.Args) > 1 && os.Args[1] == "creds" {
		return runCreds(ctx, os.Args[2:])
	}
	fs := flag.NewFlagSet("abox", flag.ContinueOnError)
	execFlag := fs.Bool("exec", false, "headless driver")
	prompt := fs.String("prompt", "", "prompt for exec mode")
	modelName := fs.String("model", "", "configured model profile name")
	probeVM := fs.Bool("probe-vm", false, "boot the guest and list files; no model call")
	resumeID := fs.String("resume", "", "resume the session with this id (same root.raw and conversation)")
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
	if len(fs.Args()) > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	cfg, cfgPath, err := config.Load()
	if err != nil {
		return err
	}
	resolver := credsource.NewResolver()
	defer resolver.Close()

	sel, ok := cfg.ModelNamed(*modelName)
	if !ok {
		return fmt.Errorf("no model profile %q (config %s)", *modelName, cfgPath)
	}

	if err := os.MkdirAll(config.SessionRoot(), 0o700); err != nil {
		return err
	}

	var sess *session.Session
	var archive []byte
	resuming := strings.TrimSpace(*resumeID) != ""
	if resuming && *probeVM {
		return fmt.Errorf("--probe-vm cannot resume a real session")
	}
	if resuming {
		loaded, err := session.Load(strings.TrimSpace(*resumeID))
		if err != nil {
			return err
		}
		sess = loaded
		fmt.Fprintf(os.Stderr, "abox: resuming session %s\n", sess.ID)
	} else {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		created, err := session.Create(wd)
		if err != nil {
			return err
		}
		snap, err := repository.OpenForSessionExcluding(wd, filepath.Join(created.Dir, "host-tree"), config.Dir())
		if err != nil {
			_ = os.RemoveAll(created.Dir)
			return err
		}
		archive, err = repository.ArchiveHEAD(snap.Root)
		if err != nil {
			_ = os.RemoveAll(created.Dir)
			return err
		}
		created.SourceDir = snap.HostSource
		created.RepoRoot = snap.HostSource
		created.HEAD = snap.HEAD
		if err := created.WriteMeta(); err != nil {
			_ = os.RemoveAll(created.Dir)
			return err
		}
		sess = created
		if snap.Ephemeral {
			fmt.Fprintln(os.Stderr, "abox: worktree has local changes; using a private Git snapshot")
		}
		fmt.Fprintf(os.Stderr, "abox: created session %s\n", sess.ID)
	}
	defer sess.ReleaseRuntimeLock()

	var sb *runtime.Sandbox
	var broker *hostbroker.Broker
	vmState := "not-started"
	image := cfg.Runtime.Image
	if image == "" {
		image = config.GuestImagePath()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var prepareErr error
	if *probeVM {
		prepareErr = runtime.PrepareProbe(sess, image, sel)
	} else {
		prepareErr = runtime.Prepare(sess, image, sel, resuming)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if prepareErr != nil {
		if execMode {
			return prepareErr
		}
		fmt.Fprintf(os.Stderr, "abox: vm prepare: %v\n", prepareErr)
		vmState = "unavailable"
	} else {
		if err := ctx.Err(); err != nil {
			return err
		}
		bootCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		vcpu, ram := cfg.Resources.Resolved()
		started, err := runtime.Start(bootCtx, sess, cfg.Runtime.VMMPath, vcpu, ram)
		cancel()
		if err != nil {
			if execMode && *prompt != "" {
				return fmt.Errorf("start vm: %w", err)
			}
			fmt.Fprintf(os.Stderr, "abox: vm start: %v\n", err)
			vmState = "failed"
		} else {
			if started.GuestProtocol < 2 {
				if resuming {
					started.Stop()
					return fmt.Errorf("cannot resume protocol-1 session %s after secretless config rewrite; rebuild the guest image and start a new session", sess.ID)
				}
				if !*probeVM {
					started.Stop()
					return fmt.Errorf("protocol-1 guest cannot use the secretless config; rebuild the guest image")
				}
			}
			if started.GuestProtocol < 4 && !*probeVM {
				started.Stop()
				return fmt.Errorf("guest protocol %d cannot enforce brokered MCP and command approvals; rebuild the guest image and start a new session", started.GuestProtocol)
			}
			sb = started
			vmState = "ready"
			defer sb.Stop()
			if *probeVM {
				if err := sb.TransferArchive(ctx, archive); err != nil {
					return fmt.Errorf("source transfer: %w", err)
				}
				var res protocol.ListFilesResult
				if err := sb.Call(ctx, "list_files", protocol.ListFilesParams{Path: ".", Depth: 4, Limit: 50}, &res); err != nil {
					return fmt.Errorf("guest list_files: %w", err)
				}
				fmt.Println("guest ready; files:")
				for _, p := range res.Paths {
					fmt.Println(p)
				}
				return nil
			}
			broker, err = brokerForMode(cfg, sel, resolver, execMode)
			if err != nil {
				return err
			}
			defer broker.Close()
			sb.SetGuestCallHandler(broker)
			if err := pushSecrets(ctx, sb, cfg, resolver, sel); err != nil {
				if execMode {
					return err
				}
				fmt.Fprintf(os.Stderr, "abox: %v\n", err)
			}
			if !resuming {
				if err := sb.TransferArchive(ctx, archive); err != nil {
					return fmt.Errorf("source transfer: %w", err)
				}
			}
		}
	}

	if *probeVM {
		return fmt.Errorf("vm not ready (%s)", vmState)
	}
	if execMode {
		return runExec(ctx, sb, *prompt)
	}
	var transcript []string
	if resuming {
		transcript = resumeLog(ctx, sess, sb)
		if len(transcript) > 0 {
			_ = session.WriteTranscript(sess.TranscriptPath(), transcript)
		}
	}
	return runTUI(ctx, cfg, sel, sb, broker, vmState, transcript, resolver, sess.TranscriptPath())
}

// Headless logs stream lifecycle; the TUI stays quiet so logs never paint into the UI.
func brokerForMode(cfg config.File, sel config.Model, resolver *credsource.Resolver, execMode bool) (*hostbroker.Broker, error) {
	b, err := hostbroker.New(cfg, sel, resolver)
	if err != nil {
		return nil, err
	}
	if execMode {
		b.SetLogger(log.Printf)
	}
	return b, nil
}

func pushSecrets(parent context.Context, sb *runtime.Sandbox, cfg config.File, resolver *credsource.Resolver, sel config.Model) error {
	if sb.GuestProtocol >= 3 {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	secrets, resolveErr := credsource.ResolveSelected(ctx, resolver, cfg, sel)
	pushErr := sb.PushSecrets(ctx, sel, secrets)
	var errs []error
	if resolveErr != nil {
		errs = append(errs, fmt.Errorf("resolve credentials: %w", resolveErr))
	}
	if pushErr != nil {
		errs = append(errs, fmt.Errorf("push resolved credentials: %w", pushErr))
	}
	return errors.Join(errs...)
}

func scrubLegacySessions() error {
	n, err := session.ScrubSecretsEverywhere()
	if n > 0 {
		fmt.Fprintf(os.Stderr, "abox: scrubbed plaintext secrets from %d old session(s)\n", n)
	}
	if err != nil {
		return fmt.Errorf("legacy session scrub incomplete; affected sessions may still contain plaintext secrets: %w", err)
	}
	return nil
}

func resumeLog(parent context.Context, sess *session.Session, sb *runtime.Sandbox) []string {
	if lines, err := session.ReadTranscript(sess.TranscriptPath()); err == nil && len(lines) > 0 {
		return lines
	}
	if sb == nil {
		return nil
	}
	if len(sb.History) > 0 {
		return tui.LogFromHistory(sb.History)
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	var got protocol.GetContextResult
	if err := sb.Call(ctx, "get_context", struct{}{}, &got); err == nil && len(got.History) > 0 {
		return tui.LogFromHistory(got.History)
	}
	var run protocol.RunCommandResult
	if err := sb.Call(ctx, "run_command", protocol.RunCommandParams{Command: "cat /var/lib/abox/context.json", Timeout: 5}, &run); err != nil || run.ExitCode != 0 {
		return nil
	}
	hist, err := agent.HistoryFromContextJSON([]byte(run.Stdout))
	if err != nil {
		return nil
	}
	return tui.LogFromHistory(hist)
}

func runExec(ctx context.Context, sb *runtime.Sandbox, prompt string) error {
	if prompt == "" {
		return fmt.Errorf("abox exec requires --prompt")
	}
	if sb == nil {
		return fmt.Errorf("agent runs only in the microVM")
	}
	enc := json.NewEncoder(os.Stdout)
	_, err := sb.UserTurnCtx(ctx, prompt, runtime.TurnOptions{RichEvents: true}, func(e protocol.AgentEvent) {
		_ = enc.Encode(e)
	})
	return err
}

func runTUI(ctx context.Context, cfg config.File, sel config.Model, sb *runtime.Sandbox, broker *hostbroker.Broker, vmState string, transcript []string, resolver *credsource.Resolver, transcriptPath string) error {
	return tui.Run(ctx, cfg, sel, sb, broker, vmState, transcript, resolver, transcriptPath)
}

func runMCP(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: abox mcp add --mode <direct|agentgateway> [--credential-env NAME] <name> <url>\n       abox mcp login <server-name>")
	}
	switch args[0] {
	case "add":
		return mcpAdd(args[1:])
	case "login":
		if len(args) < 2 {
			return fmt.Errorf("usage: abox mcp login <server-name>")
		}
		return mcpLogin(ctx, args[1])
	default:
		return fmt.Errorf("unknown mcp command %q\nusage: abox mcp add --mode <direct|agentgateway> <name> <url>", args[0])
	}
}

func mcpAdd(args []string) error {
	fs := flag.NewFlagSet("abox mcp add", flag.ContinueOnError)
	mode := fs.String("mode", "", "direct or agentgateway")
	cred := fs.String("credential-env", "", "optional env var holding a Bearer token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if strings.TrimSpace(*mode) == "" || len(rest) != 2 {
		return fmt.Errorf("usage: abox mcp add --mode <direct|agentgateway> [--credential-env NAME] <name> <url>")
	}
	cfg, path, err := config.Load()
	if err != nil {
		return err
	}
	srv := config.MCPServer{
		Name:          rest[0],
		URL:           rest[1],
		CredentialEnv: strings.TrimSpace(*cred),
	}
	if err := cfg.AddMCPServer(*mode, srv); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("added %s (%s) %s\n  config %s\n", srv.Name, *mode, srv.URL, path)
	if *mode == "direct" && srv.CredentialEnv != "" {
		fmt.Printf("  set %s or run: abox mcp login %s\n", srv.CredentialEnv, srv.Name)
	}
	return nil
}

func mcpLogin(ctx context.Context, name string) error {
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	if err := credentials.ApplyToEnv(); err != nil {
		return err
	}
	return mcpauth.LoginNamed(ctx, cfg, name)
}
