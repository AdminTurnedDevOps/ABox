package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AdminTurnedDevOps/ABox/pkg/abox"
)

func main() {
	ctx := context.Background()
	wd, _ := os.Getwd()
	exe, _ := os.Executable()
	vmm := filepath.Join(filepath.Dir(exe), "abox-vmm")
	if _, err := os.Stat(vmm); err != nil {
		vmm = "abox-vmm"
	}
	sess, err := abox.Open(ctx, abox.Options{
		RepoPath:    wd,
		VCPU:        1,
		RAMMiB:      768,
		BootTimeout: 60 * time.Second,
		VMMPath:     vmm,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sess.Close()
	fmt.Println("session", sess.ID())
	paths, err := sess.ListFiles(ctx, ".", 2, 20)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, p := range paths {
		fmt.Println(p)
	}
}
