package control

import (
	"strings"
	"testing"

	"pkt.systems/lingon/internal/sessionmanage"
)

func TestRenamePromptNeverRendersPastedControlCharacters(t *testing.T) {
	var prompt RenamePrompt
	prompt.Begin("immutable")
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

func TestRenamePrefixAndPromptUnicode(t *testing.T) {
	var prefix Prefix
	prefix.Feed(CtrlL)
	if action, _ := prefix.Feed('e'); action != ActionRename {
		t.Fatal("rename keymap missing")
	}
	var prompt RenamePrompt
	prompt.Begin("immutable")
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
	prompt.Begin("immutable")
	name, done = prompt.Feed(0x1b)
	if !prompt.DiscardEscapeTail('[') || !prompt.DiscardEscapeTail('A') || prompt.DiscardEscapeTail('x') {
		t.Fatal("escape tail leaked to the shell")
	}
	if name != "" || !done || prompt.Active {
		t.Fatal("escape did not cancel")
	}
}
