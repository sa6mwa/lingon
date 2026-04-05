//go:build !windows

package desktopnotify

import (
	"context"
	"os"

	"github.com/godbus/dbus/v5"
)

const (
	notificationAppName = "lingon"
	notificationSummary = "Lingon wall inactivity"
	notificationTimeout = int32(5000)
)

type sender interface {
	Notify(context.Context, Request) error
}

type notifier struct {
	getenv func(string) string
	sender sender
}

func newNotifier() Notifier {
	return &notifier{
		getenv: os.Getenv,
		sender: dbusSender{connect: dbus.SessionBus},
	}
}

func (n *notifier) Notify(ctx context.Context, req Request) error {
	if !desktopEnvLikely(n.getenv) {
		return nil
	}
	return n.sender.Notify(ctx, req)
}

type dbusSender struct {
	connect func() (*dbus.Conn, error)
}

func (s dbusSender) Notify(ctx context.Context, req Request) error {
	conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	call := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").CallWithContext(
		ctx,
		"org.freedesktop.Notifications.Notify",
		0,
		notificationAppName,
		uint32(0),
		"",
		req.Title,
		req.Body,
		[]string{},
		map[string]dbus.Variant{},
		notificationTimeout,
	)
	return call.Err
}
