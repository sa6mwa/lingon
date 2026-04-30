//go:build integration
// +build integration

package integrationptyattach_test

import (
	"os"
	"testing"
)

var attachIntegrationTempRoot string

func TestMain(m *testing.M) {
	code := 0
	root, err := os.MkdirTemp("", "lingon-integrationptyattach-")
	if err != nil {
		_, _ = os.Stderr.WriteString("mktemp integrationptyattach: " + err.Error() + "\n")
		code = 1
	} else {
		attachIntegrationTempRoot = root
	}
	if code == 0 {
		code = m.Run()
	}
	if attachIntegrationTempRoot != "" {
		_ = os.RemoveAll(attachIntegrationTempRoot)
	}
	os.Exit(code)
}
