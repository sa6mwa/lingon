//go:build windows

package desktopnotify

import "context"

type noopNotifier struct{}

func newNotifier() Notifier {
	return noopNotifier{}
}

func (noopNotifier) Notify(context.Context, Request) error {
	return nil
}
