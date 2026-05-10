package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestExtractTestNames(t *testing.T) {
	const kotlin = `
package example

import org.junit.Test

class ExampleTest {
    @Test
    fun first_test() {
    }

    @Test
    fun secondTest() {
    }
}
`
	names, err := extractTestNames(strings.NewReader(kotlin))
	if err != nil {
		t.Fatalf("extractTestNames: %v", err)
	}
	if want, got := []string{"first_test", "secondTest"}, names; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("extractTestNames = %v, want %v", got, want)
	}
}

func TestReadJUnitTestTimingsSortsByDuration(t *testing.T) {
	dir := t.TempDir()
	xmlPath := filepath.Join(dir, "TEST-emulator.xml")
	const xml = `<?xml version='1.0' encoding='UTF-8'?>
<testsuite name="systems.pkt.lingon.EndToEndTest" tests="3" failures="0" errors="0" skipped="0" time="7.0">
  <testcase name="fast_test" classname="systems.pkt.lingon.EndToEndTest" time="0.250" />
  <testcase name="slow_test" classname="systems.pkt.lingon.EndToEndTest" time="4.500" />
  <testcase name="medium_test" classname="systems.pkt.lingon.EndToEndTest" time="1.250" />
</testsuite>`
	if err := os.WriteFile(xmlPath, []byte(xml), 0o600); err != nil {
		t.Fatalf("write junit xml: %v", err)
	}

	timings, err := readJUnitTestTimings(dir)
	if err != nil {
		t.Fatalf("readJUnitTestTimings: %v", err)
	}
	got := make([]string, 0, len(timings))
	for _, timing := range timings {
		got = append(got, timing.Name)
	}
	want := []string{"slow_test", "medium_test", "fast_test"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("timing order = %v, want %v", got, want)
	}

	var out bytes.Buffer
	writeTestTimings(&out, timings)
	for _, wantLine := range []string{
		"seconds\tclass\ttest",
		"4.500\tsystems.pkt.lingon.EndToEndTest\tslow_test",
		"1.250\tsystems.pkt.lingon.EndToEndTest\tmedium_test",
		"0.250\tsystems.pkt.lingon.EndToEndTest\tfast_test",
	} {
		if !strings.Contains(out.String(), wantLine) {
			t.Fatalf("timing output missing %q:\n%s", wantLine, out.String())
		}
	}
}

func TestRunConfigEnv(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caPath, []byte("cert-bytes"), 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	cfgPath := filepath.Join(dir, "config.json")
	cfgJSON := `{
  "endpoint": "https://127.0.0.1:12345/v1",
  "ca_cert_path": ` + strconv.Quote(caPath) + `,
  "host_echo_log": "/tmp/log",
  "host_cols": 120,
  "host_rows": 40,
  "users": [
    {
      "username": "alice",
      "password": "secret",
      "totp_secret": "totp1",
      "sessions": ["session-a", "session-b"],
      "view_token": "view1"
    },
    {
      "username": "bob",
      "password": "secret2",
      "totp_secret": "totp2",
      "sessions": ["session-c"],
      "view_token": "view2"
    }
  ]
}`
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan struct{})
	go func() {
		_, _ = stdout.ReadFrom(r)
		close(done)
	}()
	if code := runConfigEnv([]string{"--config", cfgPath}); code != 0 {
		t.Fatalf("runConfigEnv exit code = %d", code)
	}
	_ = w.Close()
	os.Stdout = oldStdout
	<-done

	out := stdout.String()
	for _, want := range []string{
		"ENDPOINT='https://127.0.0.1:12345/v1'",
		"PORT='12345'",
		"USERNAME='alice'",
		"PASSWORD='secret'",
		"TOTP_SECRET='totp1'",
		"CA_PATH='",
		"CA_PEM_B64='",
		"SESSIONS='session-a,session-b'",
		"VIEW_TOKEN='view1'",
		"USERNAME2='bob'",
		"PASSWORD2='secret2'",
		"TOTP_SECRET2='totp2'",
		"SESSIONS2='session-c'",
		"VIEW_TOKEN2='view2'",
		"HOST_COLS='120'",
		"HOST_ROWS='40'",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}
