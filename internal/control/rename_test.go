package control

import (
	"strings"
	"testing"
	"time"

	"pkt.systems/lingon/internal/clock"
	"pkt.systems/lingon/internal/sessionmanage"
)

func TestRenamePromptNeverRendersPastedControlCharacters(t *testing.T) {
	var prompt RenamePrompt
	prompt.Begin("immutable", clock.NewMock())
	for _, b := range []byte("safe\u009b2J\u202etext") {
		prompt.Feed(b)
	}
	if !strings.HasSuffix(prompt.Label(), "safe2Jtext") {
		t.Fatalf("unsafe prompt: %q", prompt.Label())
	}
	name, _ := prompt.Feed('\r')
	if sessionmanage.ValidateName(name) == nil {
		t.Fatal("render sanitization silently accepted an invalid name")
	}
}

func TestRenamePromptCancellationPreservesFollowingInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cancel byte
		input  string
		want   string
	}{
		{"ctrl-c SS3 prefix", 0x03, "Ohello", "Ohello"},
		{"ctrl-c CSI prefix", 0x03, "[hello", "[hello"},
		{"escape CSI tail", 0x1b, "[Ahello", "hello"},
		{"escape SS3 tail", 0x1b, "OAhello", "hello"},
		{"escape ordinary input", 0x1b, "hello", "hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var prompt RenamePrompt
			prompt.Begin("immutable", clock.NewMock())
			name, done := prompt.Feed(tc.cancel)
			if name != "" || !done || prompt.Active {
				t.Fatal("cancellation did not close the prompt")
			}
			var forwarded strings.Builder
			for _, b := range []byte(tc.input) {
				if !prompt.DiscardEscapeTail(b) {
					forwarded.WriteByte(b)
				}
			}
			if forwarded.String() != tc.want {
				t.Fatalf("forwarded %q, want %q", forwarded.String(), tc.want)
			}
		})
	}
}

func TestRenamePrefixAndPromptUnicode(t *testing.T) {
	var prefix Prefix
	prefix.Feed(CtrlL)
	if action, _ := prefix.Feed('e'); action != ActionRename {
		t.Fatal("rename keymap missing")
	}
	var prompt RenamePrompt
	prompt.Begin("immutable", clock.NewMock())
	for _, b := range []byte("日本🦊") {
		prompt.Feed(b)
	}
	prompt.Feed(0x7f)
	for _, b := range []byte("語") {
		prompt.Feed(b)
	}
	name, done := prompt.Feed('\r')
	if name != "日本語" || !done || prompt.Active || prompt.SessionID != "immutable" {
		t.Fatalf("prompt=%+v name=%q", prompt, name)
	}
	prompt.Begin("immutable", clock.NewMock())
	name, done = prompt.Feed(0x1b)
	if !prompt.DiscardEscapeTail('[') || !prompt.DiscardEscapeTail('A') || prompt.DiscardEscapeTail('x') {
		t.Fatal("escape tail leaked to the shell")
	}
	if name != "" || !done || prompt.Active {
		t.Fatal("escape did not cancel")
	}
}

func TestRenameEscapeTailExpiresBeforeDelayedInput(t *testing.T) {
	for _, prefix := range []string{"", "[", "O"} {
		for _, input := range []string{"OMP_NUM_THREADS=1\n", "[hello"} {
			t.Run(prefix+"/"+input, func(t *testing.T) {
				clk := clock.NewMock()
				var prompt RenamePrompt
				prompt.Begin("immutable", clk)
				prompt.Feed(0x1b)
				for _, b := range []byte(prefix) {
					if !prompt.DiscardEscapeTail(b) {
						t.Fatal("immediate escape introducer leaked")
					}
				}
				clk.Add(time.Second)
				var forwarded strings.Builder
				for _, b := range []byte(input) {
					if !prompt.DiscardEscapeTail(b) {
						forwarded.WriteByte(b)
					}
				}
				if forwarded.String() != input {
					t.Fatalf("delayed input = %q, want %q", forwarded.String(), input)
				}
			})
		}
	}
}

func TestRenameEscapeTailRecognitionBoundary(t *testing.T) {
	for _, tc := range []struct {
		delay       time.Duration
		input, want string
	}{
		{99 * time.Millisecond, "[A", ""},
		{99 * time.Millisecond, "OA", ""},
		{100 * time.Millisecond, "OMP_NUM_THREADS=1", "OMP_NUM_THREADS=1"},
		{100 * time.Millisecond, "[hello", "[hello"},
	} {
		t.Run(tc.input+"/"+tc.delay.String(), func(t *testing.T) {
			clk := clock.NewMock()
			var prompt RenamePrompt
			prompt.Begin("immutable", clk)
			prompt.Feed(0x1b)
			clk.Add(tc.delay)
			var forwarded strings.Builder
			for _, b := range []byte(tc.input) {
				if !prompt.DiscardEscapeTail(b) {
					forwarded.WriteByte(b)
				}
			}
			if forwarded.String() != tc.want {
				t.Fatalf("forwarded %q, want %q", forwarded.String(), tc.want)
			}
		})
	}
}
