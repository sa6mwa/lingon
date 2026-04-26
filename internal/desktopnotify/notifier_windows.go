//go:build windows

package desktopnotify

func newNotifier() Notifier {
	return noopNotifier{}
}
