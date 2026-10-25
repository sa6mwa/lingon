package lifecycle

import (
	"strings"
	"testing"
)

func TestContainerPublicationSupportsPodmanWithoutRetryOption(t *testing.T) {
	w, f := testWorkflow(t)
	ctx := t.Context()
	if err := w.release(ctx, Options{Version: "1.2.3", Message: "feat(fixture): add behavior"}); err != nil {
		t.Fatal(err)
	}
	pushed := false
	f.mutate = func(c command) {
		if c.Name != "podman" || len(c.Args) < 2 || c.Args[0] != "manifest" || c.Args[1] != "push" {
			return
		}
		// Podman 5.4 supports manifest publication but rejects this option
		// before uploading anything. The fixture must enforce that boundary.
		for _, arg := range c.Args[2:] {
			if arg == "--retry" || strings.HasPrefix(arg, "--retry=") {
				t.Fatalf("manifest publication requires an unsupported Podman option: %s", arg)
			}
		}
		pushed = true
	}
	if err := w.publish(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := w.readReceipt()
	if err != nil {
		t.Fatal(err)
	}
	p, err := w.publication(r)
	if err != nil || !pushed || !p.Git || !p.GitHub || !p.Container {
		t.Fatalf("publication did not finish: pushed=%v state=%+v err=%v", pushed, p, err)
	}
}
