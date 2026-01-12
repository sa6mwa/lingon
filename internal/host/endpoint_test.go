package host

import "testing"

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
		wantErr  bool
	}{
		{
			name:     "https",
			endpoint: "https://localhost:12843/v1",
			want:     "wss://localhost:12843/v1",
		},
		{
			name:     "http",
			endpoint: "http://localhost:8080",
			want:     "ws://localhost:8080",
		},
		{
			name:     "wss",
			endpoint: "wss://relay.example/v1",
			want:     "wss://relay.example/v1",
		},
		{
			name:     "missing scheme",
			endpoint: "localhost:8080",
			wantErr:  true,
		},
		{
			name:     "unsupported scheme",
			endpoint: "ftp://example.com",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeEndpoint(tt.endpoint)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}
