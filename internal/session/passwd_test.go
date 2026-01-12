package session

import (
	"strings"
	"testing"
)

func TestShellFromPasswdReader(t *testing.T) {
	data := strings.Join([]string{
		"root:x:0:0:root:/root:/bin/bash",
		"user:x:1000:1000:User:/home/user:/bin/zsh",
		"",
	}, "\n")
	shell, err := shellFromPasswdReader(strings.NewReader(data), "1000")
	if err != nil {
		t.Fatalf("shellFromPasswdReader: %v", err)
	}
	if shell != "/bin/zsh" {
		t.Fatalf("shell = %q, want /bin/zsh", shell)
	}
}
