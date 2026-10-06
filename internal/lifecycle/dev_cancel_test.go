package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type cancelAfterPlayRunner struct {
	next             runner
	cancel           context.CancelFunc
	logged, tornDown bool
}

func (r *cancelAfterPlayRunner) run(ctx context.Context, c command) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out, err := r.next.run(ctx, c)
	if c.Name == "podman" && err == nil {
		switch {
		case strings.HasPrefix(strings.Join(c.Args, " "), "kube play "):
			r.cancel()
		case strings.HasPrefix(strings.Join(c.Args, " "), "pod logs "):
			r.logged = true
		case strings.HasPrefix(strings.Join(c.Args, " "), "kube down "):
			r.tornDown = true
		}
	}
	return out, err
}

func TestDevCancellationKeepsDiagnosticsAndOwnsTeardown(t *testing.T) {
	w, f := testWorkflow(t)
	template, err := os.ReadFile(filepath.Join("..", "..", "devenv.yaml.in"))
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(w.root, "devenv.yaml.in"), template)
	t.Setenv("LINGON_DEV_IMAGE", "localhost/owned:fixture")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &cancelAfterPlayRunner{next: f, cancel: cancel}
	w.r = r
	if err := w.dev(ctx, "dev-up"); err == nil {
		t.Fatal("canceled readiness accepted")
	}
	if !r.logged || !r.tornDown {
		t.Fatalf("cancellation lost diagnostics or cleanup: logs=%t down=%t", r.logged, r.tornDown)
	}
}
