package headless

import "testing"

func TestSocketPath(t *testing.T) {
	path, err := SocketPath("/tmp/lingon", "s.1")
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	want := "/tmp/lingon/headless/s.1.sock"
	if path != want {
		t.Fatalf("SocketPath = %q, want %q", path, want)
	}
}

func TestIsForegroundEnv(t *testing.T) {
	if !IsForegroundEnv("true") {
		t.Fatalf("expected true")
	}
	if IsForegroundEnv("false") {
		t.Fatalf("expected false")
	}
}

func TestIsRoutedStatusSender(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: RoutedStatusSenderConnected, want: true},
		{name: RoutedStatusSenderLost, want: true},
		{name: RoutedStatusSenderBackoff, want: true},
		{name: RoutedStatusSenderInfo, want: true},
		{name: RoutedStatusSenderError, want: true},
		{name: "broadcast-user", want: false},
		{name: "", want: false},
	}
	for _, tc := range tests {
		if got := IsRoutedStatusSender(tc.name); got != tc.want {
			t.Fatalf("IsRoutedStatusSender(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
