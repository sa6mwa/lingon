package attach

import "testing"

func TestNormalizeEndpointAssumesHTTPSWhenSchemeOmitted(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantWS   string
		wantHTTP string
		wantErr  bool
	}{
		{
			name:     "host port path",
			input:    "localhost:1234/v1",
			wantWS:   "wss://localhost:1234/v1",
			wantHTTP: "https://localhost:1234/v1",
		},
		{
			name:     "domain path",
			input:    "pkt.systems/lingon/v1",
			wantWS:   "wss://pkt.systems/lingon/v1",
			wantHTTP: "https://pkt.systems/lingon/v1",
		},
		{
			name:     "ws translates to http",
			input:    "ws://relay.example/v1",
			wantWS:   "ws://relay.example/v1",
			wantHTTP: "http://relay.example/v1",
		},
		{
			name:    "unsupported scheme",
			input:   "ftp://relay.example/v1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotWS, gotHTTP, err := normalizeEndpoint(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeEndpoint(%q): expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeEndpoint(%q): %v", tt.input, err)
			}
			if gotWS != tt.wantWS {
				t.Fatalf("normalizeEndpoint(%q) ws = %q, want %q", tt.input, gotWS, tt.wantWS)
			}
			if gotHTTP != tt.wantHTTP {
				t.Fatalf("normalizeEndpoint(%q) http = %q, want %q", tt.input, gotHTTP, tt.wantHTTP)
			}
		})
	}
}
