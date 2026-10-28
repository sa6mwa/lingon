package clipboard

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.design/x/x11"
)

func authorityFile(t *testing.T, entries []x11.AuthEntry) string {
	t.Helper()
	var data []byte
	for _, e := range entries {
		data = binary.BigEndian.AppendUint16(data, e.Family)
		for _, field := range [][]byte{[]byte(e.Address), []byte(e.Number), []byte(e.Name), e.Data} {
			data = binary.BigEndian.AppendUint16(data, uint16(len(field)))
			data = append(data, field...)
		}
	}
	path := filepath.Join(t.TempDir(), "Xauthority")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClipboardTCPAuthorityPublication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	display, authority := startClipboardXvfb(ctx, t)
	backend, err := x11.ParseDisplay(display)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	forwardX11(ctx, t, listener, backend)
	number := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port - 6000)
	data, err := os.ReadFile(authority)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := x11.ParseXauthority(data)
	if err != nil {
		t.Fatal(err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	exact := x11.AuthEntry{Family: x11.FamilyInternet, Address: string([]byte{127, 0, 0, 1}), Number: number, Name: x11.MITCookie, Data: entries[0].Data}
	wrongLocal := x11.AuthEntry{Family: x11.FamilyLocal, Address: host, Number: number, Name: x11.MITCookie, Data: bytes.Repeat([]byte{9}, 16)}
	owner := NewX11("127.0.0.1:"+number, authorityFile(t, []x11.AuthEntry{wrongLocal, exact}))
	defer owner.Close()
	for _, mime := range []string{"image/png", "image/jpeg"} {
		img := testImage(t, mime, true)
		if err := owner.Set(ctx, img); err != nil {
			t.Fatal("unrelated local cookie masked TCP publication credential", err)
		}
		got, err := NewX11(display, authority).Read(ctx)
		if err != nil || got.MIME != mime || !bytes.Equal(got.Data, img.Data) {
			t.Fatal("authenticated publication changed image", err)
		}
	}
}

func TestClipboardAuthorityAddressMatching(t *testing.T) {
	entry := func(family uint16, address, number, data string) x11.AuthEntry {
		return x11.AuthEntry{Family: family, Address: address, Number: number, Name: x11.MITCookie, Data: []byte(data)}
	}
	v4 := net.ParseIP("203.0.113.8")
	v6 := net.ParseIP("2001:db8::8")
	local := entry(x11.FamilyLocal, "host", "7", "local")
	remote := entry(x11.FamilyInternet, string(v4.To4()), "7", "remote")
	remote6 := entry(6, string(v6.To16()), "7", "remote6")
	wild := entry(x11.FamilyWild, "", "7", "wild")
	for _, tc := range []struct {
		name    string
		peer    net.Addr
		entries []x11.AuthEntry
		want    string
	}{
		{"IPv4 remote beats local", &net.TCPAddr{IP: v4}, []x11.AuthEntry{local, remote}, "remote"},
		{"IPv4 mapped", &net.TCPAddr{IP: net.ParseIP("::ffff:203.0.113.8")}, []x11.AuthEntry{remote}, "remote"},
		{"IPv6 remote", &net.TCPAddr{IP: v6}, []x11.AuthEntry{local, remote, remote6}, "remote6"},
		{"Unix local", &net.UnixAddr{Name: "/owned/socket", Net: "unix"}, []x11.AuthEntry{remote, wild, local}, "local"},
		{"loopback local fallback", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}, []x11.AuthEntry{wild, local}, "local"},
		{"remote wildcard", &net.TCPAddr{IP: v4}, []x11.AuthEntry{local, wild}, "wild"},
		{"reject local on remote", &net.TCPAddr{IP: v4}, []x11.AuthEntry{local}, ""},
		{"reject different IP", &net.TCPAddr{IP: net.ParseIP("203.0.113.9")}, []x11.AuthEntry{remote}, ""},
		{"reject wrong family", &net.TCPAddr{IP: v6}, []x11.AuthEntry{remote}, ""},
		{"reject remote on Unix", &net.UnixAddr{Name: "/owned/socket", Net: "unix"}, []x11.AuthEntry{remote}, ""},
		{"reject wrong display", &net.TCPAddr{IP: v4}, []x11.AuthEntry{entry(x11.FamilyInternet, string(v4.To4()), "8", "wrong")}, ""},
		{"exact display priority", &net.TCPAddr{IP: v4}, []x11.AuthEntry{entry(x11.FamilyInternet, string(v4.To4()), "", "any-number"), remote}, "remote"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name, data := chooseCookie(tc.entries, 7, tc.peer, "host")
			if string(data) != tc.want || (tc.want == "" && name != "") || (tc.want != "" && name != x11.MITCookie) {
				t.Fatalf("wrong credential selected: name=%q data=%q", name, data)
			}
		})
	}
}

func TestClipboardTCPAuthorityIgnoresUnrelatedLocalCookie(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	endpoint, err := NewEndpoint(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	img := testImage(t, "image/png", false)
	if err := endpoint.Set(ctx, img); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(endpoint.Authority)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := x11.ParseXauthority(data)
	if err != nil {
		t.Fatal(err)
	}
	remote := entries[0]
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	local := x11.AuthEntry{Family: x11.FamilyLocal, Address: host, Number: remote.Number, Name: x11.MITCookie, Data: bytes.Repeat([]byte{9}, 16)}
	for _, records := range [][]x11.AuthEntry{{remote}, {local, remote}, {remote, local}} {
		got, err := NewX11(endpoint.Display, authorityFile(t, records)).Read(ctx)
		if err != nil || !bytes.Equal(got.Data, img.Data) {
			t.Fatal("TCP peer's valid cookie was masked by a local entry", err)
		}
	}
}
