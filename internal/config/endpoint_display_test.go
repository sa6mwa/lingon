package config

import "testing"

func TestEndpointDisplay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		endpoint     string
		hostnameOnly bool
		want         string
	}{
		{
			name:         "full endpoint when disabled",
			endpoint:     "https://localhost:12843/v1",
			hostnameOnly: false,
			want:         "https://localhost:12843/v1",
		},
		{
			name:         "hostname from https endpoint",
			endpoint:     "https://localhost:12843/v1",
			hostnameOnly: true,
			want:         "localhost",
		},
		{
			name:         "hostname without scheme",
			endpoint:     "relay.example.com:12843/v1",
			hostnameOnly: true,
			want:         "relay.example.com",
		},
		{
			name:         "hostname from ws endpoint",
			endpoint:     "wss://relay.example.com/v1",
			hostnameOnly: true,
			want:         "relay.example.com",
		},
		{
			name:         "malformed fallback",
			endpoint:     "://bad",
			hostnameOnly: true,
			want:         "://bad",
		},
		{
			name:         "blank input",
			endpoint:     "   ",
			hostnameOnly: true,
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EndpointDisplay(tt.endpoint, tt.hostnameOnly)
			if got != tt.want {
				t.Fatalf("EndpointDisplay(%q, %v) = %q, want %q", tt.endpoint, tt.hostnameOnly, got, tt.want)
			}
		})
	}
}
