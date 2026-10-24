package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"pkt.systems/lingon/internal/lifecycle"
)

func main() {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(os.Stderr, "local release orchestration requires Linux; artifact targets include FreeBSD and Darwin")
		os.Exit(1)
	}
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: make help (or lingon-lifecycle <operation> from repository root)")
		os.Exit(1)
	}
	root, err := os.Getwd()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err == nil {
		err = lifecycle.Run(ctx, lifecycle.Options{
			Root: root, Operation: os.Args[1], Version: os.Getenv("LINGON_RELEASE_VERSION"), Message: os.Getenv("LINGON_RELEASE_MESSAGE"),
		})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Lifecycle stopped:", err)
		os.Exit(1)
	}
}
