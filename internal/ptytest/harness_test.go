package ptytest

import (
	"context"
	"testing"

	"pkt.systems/lingon/internal/desktopnotify"
)

type recordingNotifier struct{}

func (recordingNotifier) Notify(context.Context, desktopnotify.Request) error {
	return nil
}

func TestEffectiveHostDesktopNotificationConfigDefaultsToNoopNotifier(t *testing.T) {
	t.Parallel()

	disabled, notifier := effectiveHostDesktopNotificationConfig(HostOptions{})
	if disabled {
		t.Fatal("expected desktop notifications to stay logically enabled with noop notifier")
	}
	if notifier == nil {
		t.Fatal("expected noop notifier by default")
	}
	if _, ok := notifier.(noopNotifier); !ok {
		t.Fatalf("expected noop notifier by default, got %T", notifier)
	}
}

func TestEffectiveHostDesktopNotificationConfigPreservesExplicitNotifier(t *testing.T) {
	t.Parallel()

	want := recordingNotifier{}
	disabled, notifier := effectiveHostDesktopNotificationConfig(HostOptions{
		DesktopNotifier: want,
	})
	if disabled {
		t.Fatal("expected explicit notifier to keep notifications enabled")
	}
	if notifier != want {
		t.Fatalf("expected explicit notifier to be preserved, got %T", notifier)
	}
}

func TestEffectiveHostDesktopNotificationConfigRespectsDisableFlag(t *testing.T) {
	t.Parallel()

	want := recordingNotifier{}
	disabled, notifier := effectiveHostDesktopNotificationConfig(HostOptions{
		DisableDesktopNotifications: true,
		DesktopNotifier:             want,
	})
	if !disabled {
		t.Fatal("expected disable flag to be preserved")
	}
	if notifier != want {
		t.Fatalf("expected notifier to be preserved when disabled, got %T", notifier)
	}
}

var _ desktopnotify.Notifier = recordingNotifier{}
