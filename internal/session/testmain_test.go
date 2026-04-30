package session

import (
	"os"
	"testing"

	"pkt.systems/lingon/internal/testpty"
)

func TestMain(m *testing.M) {
	if handled, code, err := testpty.MaybeReexecOwnedPTY(); handled {
		if err != nil {
			_, _ = os.Stderr.WriteString(err.Error() + "\n")
			if code == 0 {
				code = 1
			}
		}
		os.Exit(code)
	}
	code := m.Run()
	os.Exit(code)
}
