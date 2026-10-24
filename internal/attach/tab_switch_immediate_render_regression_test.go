package attach_test

import (
	"testing"

	"pkt.systems/lingon/internal/ptytest"
)

func TestAttachRemoteTabSwitchRendersWithoutExtraInput(t *testing.T) {
	h := newHarness(t)
	tokenA := "ATTACH_SWITCH_TOKEN_A_991"
	tokenB := "ATTACH_SWITCH_TOKEN_B_427"
	startTabSwitchHost(t, h, "attach-switch-a", "sw-a", tokenA)
	startTabSwitchHost(t, h, "attach-switch-b", "sw-b", tokenB)

	attach := h.StartMultiAttach(ptytest.MultiAttachOptions{
		SessionID: "attach-switch-a",
		Cols:      100,
		Rows:      30,
	})
	t.Cleanup(attach.Cancel)

	// Every switch is sent once and acknowledged by its rendered frame.
	// Observing a socket or a callback is not proof of the visible tab, and
	// retrying a switch can toggle back after the first request completes.
	waitTabSwitchFrame(t, attach, tokenA+"_20", tokenB)
	attach.Send("\x0cn")
	waitTabSwitchFrame(t, attach, tokenB+"_20", tokenA)
	attach.Send("\x0cn")
	waitTabSwitchFrame(t, attach, tokenA+"_20", tokenB)

	// Switch to tab B and require immediate framebuffer swap without extra input.
	attach.Send("\x0cn")
	waitTabSwitchFrame(t, attach, tokenB+"_20", tokenA)
}
