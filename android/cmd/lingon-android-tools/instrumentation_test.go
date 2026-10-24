package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func instrumentStatus(method string, code int, stack string) string {
	return fmt.Sprintf("INSTRUMENTATION_STATUS: id=AndroidJUnitRunner\nINSTRUMENTATION_STATUS: numtests=1\nINSTRUMENTATION_STATUS: class=example.Tests\nINSTRUMENTATION_STATUS: test=%s\nINSTRUMENTATION_STATUS: stack=%s\nINSTRUMENTATION_STATUS_CODE: %d\n", method, stack, code)
}

func TestInstrumentationResultsFailClosed(t *testing.T) {
	start := instrumentStatus("works", 1, "")
	pass := instrumentStatus("works", 0, "")
	final := "INSTRUMENTATION_RESULT: stream=done\nINSTRUMENTATION_CODE: -1\n"
	for _, tc := range []struct {
		name, raw   string
		allowed, ok bool
	}{
		{"success", start + pass + final, false, true},
		{"missing final", start + pass, false, false},
		{"unfinished", start + final, false, false},
		{"duplicate", start + pass + start + pass + final, false, false},
		{"unexpected", instrumentStatus("other", 1, "") + pass + final, false, false},
		{"without start", pass + final, false, false},
		{"test failure", start + instrumentStatus("works", -2, "error\nsecond line & <detail>") + final, false, false},
		{"test error", start + instrumentStatus("works", -1, "error") + final, false, false},
		{"unapproved skip", start + instrumentStatus("works", -3, "") + final, false, false},
		{"approved assumption", start + instrumentStatus("works", -4, "assumption") + final, true, true},
		{"crash", start + "INSTRUMENTATION_FAILED: process crashed\n", false, false},
		{"crash after tests", start + pass + "INSTRUMENTATION_RESULT: shortMsg=Process crashed\nINSTRUMENTATION_CODE: -1\n", false, false},
		{"cancelled run", start + pass + "INSTRUMENTATION_CODE: 0\n", false, false},
		{"wrong count", strings.ReplaceAll(start+pass+final, "numtests=1", "numtests=2"), false, false},
		{"unknown status", start + instrumentStatus("works", 7, "") + final, false, false},
		{"duplicate field", strings.Replace(start+pass+final, "INSTRUMENTATION_STATUS: test=works", "INSTRUMENTATION_STATUS: test=works\nINSTRUMENTATION_STATUS: test=works", 1), false, false},
		{"data after final", start + pass + final + pass, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allowed := map[string]bool{}
			if tc.allowed {
				allowed["example.Tests#works"] = true
			}
			_, err := parseInstrumentation(strings.NewReader(tc.raw), map[string]bool{"example.Tests#works": true}, allowed, io.Discard)
			if (err == nil) != tc.ok {
				t.Fatalf("result %v, wanted success=%v", err, tc.ok)
			}
		})
	}
}

func TestInstrumentationPreservesMultilineFailure(t *testing.T) {
	raw := instrumentStatus("works", 1, "") + instrumentStatus("works", -2, "line one\nline two <&>") + "INSTRUMENTATION_CODE: -1\n"
	suite, err := parseInstrumentation(strings.NewReader(raw), map[string]bool{"example.Tests#works": true}, nil, io.Discard)
	if err == nil || len(suite.Cases) != 1 || suite.Cases[0].Failure.Text != "line one\nline two <&>" {
		t.Fatalf("lost failure: %+v / %v", suite, err)
	}
}

func TestInstrumentationAccountsEverySelectedMethod(t *testing.T) {
	expected := map[string]bool{"example.Tests#first": true, "example.Tests#second": true}
	result := func(method string) string {
		return strings.ReplaceAll(instrumentStatus(method, 1, "")+instrumentStatus(method, 0, ""), "numtests=1", "numtests=2")
	}
	final := "INSTRUMENTATION_CODE: -1\n"
	for _, tc := range []struct {
		name, raw string
		ok        bool
	}{
		{"out of selection order", result("second") + result("first") + final, true},
		{"omitted method", result("first") + final, false},
		{"repeated method", result("first") + result("first") + final, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			suite, err := parseInstrumentation(strings.NewReader(tc.raw), expected, nil, io.Discard)
			if (err == nil) != tc.ok || tc.ok && suite.Tests != 2 {
				t.Fatalf("coverage: %+v / %v; wanted success=%v", suite, err, tc.ok)
			}
		})
	}
}

func TestInstrumentationQuotesRemoteArguments(t *testing.T) {
	if got := shellQuote("password ' with $HOME; command"); got != "'password '\"'\"' with $HOME; command'" {
		t.Fatalf("unsafe remote quoting: %s", got)
	}
}

func TestInstrumentCommandRequiresProtocolAndADBSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, raw  string
		exit, want int
	}{
		{"success", instrumentStatus("works", 1, "") + instrumentStatus("works", 0, "") + "INSTRUMENTATION_CODE: -1\n", 0, 0},
		{"adb exits zero after crash", "INSTRUMENTATION_RESULT: shortMsg=Process crashed\nINSTRUMENTATION_CODE: 0\n", 0, 1},
		{"adb fails after valid results", instrumentStatus("works", 1, "") + instrumentStatus("works", 0, "") + "INSTRUMENTATION_CODE: -1\n", 7, 1},
		{"adb exits zero with test failure", instrumentStatus("works", 1, "") + instrumentStatus("works", -2, "failed <&>") + "INSTRUMENTATION_CODE: -1\n", 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ca := filepath.Join(dir, "ca.pem")
			cfgPath := filepath.Join(dir, "config.json")
			rawPath := filepath.Join(dir, "protocol")
			trace := filepath.Join(dir, "arguments")
			adb := filepath.Join(dir, "adb")
			report := filepath.Join(dir, "reports", "TEST-native.xml")
			cfg := harnessConfig{Endpoint: "https://127.0.0.1:1234/v1", CACertPath: ca, HostCols: 120, HostRows: 40, Users: []harnessUser{{Username: "user", Password: "secret ' $value", TOTPSecret: "otp", Sessions: []string{"a", "b"}, ViewToken: "v", ControlToken: "c"}, {Username: "other", Password: "pw2", TOTPSecret: "otp2", Sessions: []string{"d"}, ViewToken: "v2"}}}
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{ca: []byte("test CA"), cfgPath: data, rawPath: []byte(tc.raw), adb: []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$NATIVE_TRACE\"\ncat \"$NATIVE_PROTOCOL\"\nexit " + fmt.Sprint(tc.exit) + "\n")}
			for path, contents := range files {
				if err := os.WriteFile(path, contents, 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("NATIVE_TRACE", trace)
			t.Setenv("NATIVE_PROTOCOL", rawPath)
			args := []string{"--adb", adb, "--serial", "emulator-owned", "--config", cfgPath, "--classes", "example.Tests#works", "--report", report}
			if code := runInstrument(args); code != tc.want {
				t.Fatalf("exit=%d want=%d", code, tc.want)
			}
			evidence, err := os.ReadFile(report + ".raw")
			if err != nil || string(evidence) != tc.raw {
				t.Fatalf("raw evidence: %q / %v", evidence, err)
			}
			data, err = os.ReadFile(report)
			if err != nil {
				t.Fatal(err)
			}
			var suite instrumentSuite
			if err := xml.Unmarshal(data, &suite); err != nil {
				t.Fatal(err)
			}
			if tc.want == 0 && (suite.Tests != 1 || suite.Errors != 0 || suite.Failures != 0 || suite.Cases[0].Name != "works") {
				t.Fatalf("invalid success report: %+v", suite)
			}
			if tc.want != 0 && suite.Errors == 0 {
				t.Fatal("unsuccessful run reported no errors")
			}
			invocation, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range []string{"-s\nemulator-owned\nshell\nam\ninstrument\n-w\n-r\n", "'endpoint'\n'https://localhost:1234/v1'", "'password'\n" + shellQuote(cfg.Users[0].Password), "'sessions'\n'a,b'", "'username2'\n'other'", "'host_cols'\n'120'", "'host_rows'\n'40'", "systems.pkt.lingon.test/androidx.test.runner.AndroidJUnitRunner"} {
				if !strings.Contains(string(invocation), value) {
					t.Fatalf("lost argument %q: %s", value, invocation)
				}
			}
			// Evidence is immutable even when a caller accidentally repeats a batch.
			if code := runInstrument(args); code == 0 {
				t.Fatal("overwrote existing evidence")
			}
		})
	}
}
