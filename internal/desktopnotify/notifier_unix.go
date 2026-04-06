//go:build !windows

package desktopnotify

import (
	"context"
	"errors"
	"os"
	"sync"

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

type dbusConnection interface {
	Object(dest string, path dbus.ObjectPath) dbusObject
}

type dbusObject interface {
	CallWithContext(context.Context, string, dbus.Flags, ...interface{}) *dbus.Call
}

type sessionBusConnection struct {
	conn *dbus.Conn
}

func (c sessionBusConnection) Object(dest string, path dbus.ObjectPath) dbusObject {
	return sessionBusObject{obj: c.conn.Object(dest, path)}
}

type sessionBusObject struct {
	obj dbus.BusObject
}

func (o sessionBusObject) CallWithContext(ctx context.Context, method string, flags dbus.Flags, args ...interface{}) *dbus.Call {
	return o.obj.CallWithContext(ctx, method, flags, args...)
}

type notifier struct {
	getenv func(string) string
	sender sender
}

func newNotifier() Notifier {
	return &notifier{
		getenv: os.Getenv,
		sender: &dbusSender{connect: func() (dbusConnection, error) {
			conn, err := dbus.SessionBus()
			if err != nil {
				return nil, err
			}
			return sessionBusConnection{conn: conn}, nil
		}},
	}
}

func (n *notifier) Notify(ctx context.Context, req Request) error {
	if !desktopEnvLikely(n.getenv) {
		return nil
	}
	return n.sender.Notify(ctx, req)
}

type dbusSender struct {
	connect func() (dbusConnection, error)
	mu      sync.Mutex
	conn    dbusConnection
}

func (s *dbusSender) Notify(ctx context.Context, req Request) error {
	conn, err := s.connection()
	if err != nil {
		return err
	}

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

func (s *dbusSender) connection() (dbusConnection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		return s.conn, nil
	}
	if s.connect == nil {
		return nil, errors.New("desktop notifier connection unavailable")
	}
	conn, err := s.connect()
	if err != nil {
		return nil, err
	}
	s.conn = conn
	return conn, nil
}
