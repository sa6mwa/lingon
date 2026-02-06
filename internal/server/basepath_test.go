package server

import "testing"

func TestNormalizeBasePath(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"empty", "", "", false},
		{"root", "/", "", false},
		{"trim", "  /  ", "", false},
		{"simple", "foo", "/foo", false},
		{"nested", "foo/bar", "/foo/bar", false},
		{"leading", "/foo/bar", "/foo/bar", false},
		{"trailing", "/foo/bar/", "/foo/bar", false},
		{"double", "foo//bar", "/foo/bar", false},
		{"dot", "/./", "", true},
		{"dotdot", "/../", "", true},
		{"withdot", "/foo/../bar", "", true},
		{"scheme", "http://example", "", true},
		{"query", "/foo?bar", "", true},
		{"fragment", "/foo#bar", "", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeBasePath(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeBasePath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
