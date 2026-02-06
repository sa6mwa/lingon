package attach

import (
	"io"
	"os"
	"testing"
)

func TestIsTerminalWriterNilFile(t *testing.T) {
	var file *os.File
	var writer io.Writer = file
	if isTerminalWriter(writer) {
		t.Fatalf("expected nil file writer to be non-terminal")
	}
}
