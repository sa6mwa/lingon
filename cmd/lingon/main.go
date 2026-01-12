package main

import (
	"context"
	"fmt"
	"os"

	"pkt.systems/lingon"
	"pkt.systems/pslog"
)

func main() {
	loader := lingon.NewLoader()
	root := NewRootCommand(loader)
	logger := pslog.LoggerFromEnv(pslog.WithEnvWriter(os.Stdout))
	root.SetContext(pslog.ContextWithLogger(context.Background(), logger))
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
