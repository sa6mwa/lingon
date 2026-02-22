package main

import (
	"context"
	"fmt"
	"os"

	"pkt.systems/lingon"
	"pkt.systems/psi"
	"pkt.systems/pslog"
)

func main() {
	psi.Run(submain)
}

func submain(ctx context.Context) int {
	loader := lingon.NewLoader()
	root := NewRootCommand(loader)
	logger := pslog.LoggerFromEnv(ctx, pslog.WithEnvWriter(os.Stdout)).With("app", "lingon")
	root.SetContext(pslog.ContextWithLogger(ctx, logger))
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
