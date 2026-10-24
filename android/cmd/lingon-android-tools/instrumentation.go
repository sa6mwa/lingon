package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type instrumentCase struct {
	Class   string            `xml:"classname,attr"`
	Name    string            `xml:"name,attr"`
	Seconds string            `xml:"time,attr"`
	Failure *instrumentDetail `xml:"failure,omitempty"`
	Error   *instrumentDetail `xml:"error,omitempty"`
	Skipped *instrumentDetail `xml:"skipped,omitempty"`
}

type instrumentDetail struct {
	Text string `xml:",chardata"`
}
type instrumentSuite struct {
	XMLName  xml.Name         `xml:"testsuite"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Errors   int              `xml:"errors,attr"`
	Skipped  int              `xml:"skipped,attr"`
	Cases    []instrumentCase `xml:"testcase"`
}

// AndroidJUnitRunner sends start=1, success=0, error=-1, failure=-2,
// ignored=-3 and assumption-failure=-4. The final am result must be -1.
// Parse incrementally; never infer success from adb's exit status alone.
func parseInstrumentation(input io.Reader, expected, allowedSkip map[string]bool, progress io.Writer) (instrumentSuite, error) {
	suite := instrumentSuite{}
	seen := map[string]bool{}
	bundle := map[string]string{}
	field, active := "", ""
	var started time.Time
	finished := false
	scan := bufio.NewScanner(input)
	scan.Buffer(make([]byte, 4096), 1024*1024)
	for scan.Scan() {
		line := strings.TrimSuffix(scan.Text(), "\r")
		if finished && strings.HasPrefix(line, "INSTRUMENTATION_") {
			return suite, errors.New("instrumentation data after final result")
		}
		switch {
		case strings.HasPrefix(line, "INSTRUMENTATION_STATUS: "):
			key, value, ok := strings.Cut(strings.TrimPrefix(line, "INSTRUMENTATION_STATUS: "), "=")
			if !ok || key == "" {
				return suite, errors.New("malformed instrumentation status")
			}
			if _, exists := bundle[key]; exists {
				return suite, fmt.Errorf("duplicate status field %q", key)
			}
			bundle[key], field = value, key
		case strings.HasPrefix(line, "INSTRUMENTATION_STATUS_CODE: "):
			code, err := strconv.Atoi(strings.TrimPrefix(line, "INSTRUMENTATION_STATUS_CODE: "))
			if err != nil {
				return suite, err
			}
			key := bundle["class"] + "#" + bundle["test"]
			if !expected[key] || seen[key] {
				return suite, fmt.Errorf("unexpected or duplicate instrumentation test: %s", key)
			}
			if bundle["id"] != "AndroidJUnitRunner" {
				return suite, errors.New("unexpected instrumentation reporter")
			}
			n, err := strconv.Atoi(bundle["numtests"])
			if err != nil || n != len(expected) {
				return suite, errors.New("instrumentation selection count mismatch")
			}
			if code == 1 {
				if active != "" {
					return suite, errors.New("overlapping instrumentation starts")
				}
				active, started = key, time.Now()
			} else {
				if active != key {
					return suite, fmt.Errorf("completion without matching start: %s", key)
				}
				c := instrumentCase{Class: bundle["class"], Name: bundle["test"], Seconds: fmt.Sprintf("%.6f", time.Since(started).Seconds())}
				switch code {
				case 0:
				case -1:
					c.Error = &instrumentDetail{Text: bundle["stack"]}
					suite.Errors++
				case -2:
					c.Failure = &instrumentDetail{Text: bundle["stack"]}
					suite.Failures++
				case -3, -4:
					c.Skipped = &instrumentDetail{Text: bundle["stack"]}
					suite.Skipped++
				default:
					return suite, fmt.Errorf("unsupported instrumentation status code %d", code)
				}
				seen[key], active = true, ""
				suite.Cases = append(suite.Cases, c)
				suite.Tests++
				fmt.Fprintf(progress, "Android result: %s (status %d)\n", key, code)
			}
			bundle = map[string]string{}
			field = ""
		case strings.HasPrefix(line, "INSTRUMENTATION_FAILED:") || strings.HasPrefix(line, "INSTRUMENTATION_ABORTED:"):
			return suite, fmt.Errorf("instrumentation aborted: %s", line)
		case strings.HasPrefix(line, "INSTRUMENTATION_CODE: "):
			if strings.TrimPrefix(line, "INSTRUMENTATION_CODE: ") != "-1" || active != "" || len(bundle) > 0 {
				return suite, errors.New("instrumentation did not finish normally")
			}
			finished = true
			field = ""
		case strings.HasPrefix(line, "INSTRUMENTATION_RESULT:"):
			if strings.HasPrefix(line, "INSTRUMENTATION_RESULT: shortMsg=") || strings.HasPrefix(line, "INSTRUMENTATION_RESULT: longMsg=") {
				return suite, fmt.Errorf("instrumentation run error: %s", line)
			}
			field = ""
		default:
			if field != "" {
				if len(bundle[field])+len(line) > 1024*1024 {
					return suite, errors.New("instrumentation status exceeds bound")
				}
				bundle[field] += "\n" + line
			}
		}
	}
	if err := scan.Err(); err != nil {
		return suite, err
	}
	if !finished || active != "" || len(seen) != len(expected) {
		return suite, errors.New("missing final or required instrumentation results")
	}
	for _, c := range suite.Cases {
		if c.Error != nil || c.Failure != nil {
			return suite, fmt.Errorf("android test failed: %s#%s", c.Class, c.Name)
		}
		if c.Skipped != nil && !allowedSkip[c.Class+"#"+c.Name] {
			return suite, fmt.Errorf("required Android test skipped: %s#%s", c.Class, c.Name)
		}
	}
	return suite, nil
}

func runInstrument(args []string) int {
	fs := flag.NewFlagSet("instrument", flag.ContinueOnError)
	adb := fs.String("adb", "", "adb executable")
	serial := fs.String("serial", "", "owned device serial")
	config := fs.String("config", "", "harness configuration")
	classes := fs.String("classes", "", "comma-separated class#method selection")
	report := fs.String("report", "", "JUnit report path")
	skip := fs.String("allow-skip", "", "one permitted class#method skip")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *adb == "" || *serial == "" || *config == "" || *classes == "" || *report == "" {
		fmt.Fprintln(os.Stderr, "instrument: adb, serial, config, classes and report are required")
		return 2
	}
	expected := map[string]bool{}
	for _, key := range strings.Split(*classes, ",") {
		class, method, ok := strings.Cut(key, "#")
		if !ok || class == "" || method == "" || expected[key] {
			fmt.Fprintln(os.Stderr, "instrument: invalid or duplicate method selection")
			return 2
		}
		expected[key] = true
	}
	allowed := map[string]bool{}
	if *skip != "" {
		if !expected[*skip] {
			fmt.Fprintln(os.Stderr, "instrument: permitted skip is not selected")
			return 2
		}
		allowed[*skip] = true
	}
	data, err := os.ReadFile(*config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var cfg harnessConfig
	if err = json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	env, err := instrumentArguments(cfg, *classes)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signalCtx, 45*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, *adb, append([]string{"-s", *serial, "shell", "am", "instrument", "-w", "-r"}, env...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 10 * time.Second
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err = os.MkdirAll(filepath.Dir(*report), 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	raw, err := os.OpenFile(*report+".raw", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer raw.Close()
	// Keep stderr separate: concurrent writes must not corrupt the raw protocol.
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	suite, parseErr := parseInstrumentation(io.TeeReader(pipe, raw), expected, allowed, os.Stdout)
	if parseErr != nil {
		cancel()
	}
	// Drain remaining output even after a protocol error so adb cannot block.
	_, drainErr := io.Copy(raw, pipe)
	waitErr := cmd.Wait()
	if parseErr != nil || waitErr != nil || drainErr != nil {
		suite.Errors++
	}
	f, err := os.OpenFile(*report, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		encoder := xml.NewEncoder(f)
		err = encoder.Encode(suite)
		err = errors.Join(err, f.Close())
	}
	if err = errors.Join(parseErr, drainErr, waitErr, err); err != nil {
		fmt.Fprintln(os.Stderr, "instrument:", err)
		return 1
	}
	return 0
}

func instrumentArguments(cfg harnessConfig, classes string) ([]string, error) {
	endpoint, _, err := splitEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	endpoint = strings.Replace(endpoint, "127.0.0.1", "localhost", 1)
	ca, err := os.ReadFile(cfg.CACertPath)
	if err != nil {
		return nil, err
	}
	values := [][2]string{
		{"class", classes}, {"endpoint", endpoint}, {"username", userField(cfg.Users, 0, func(u harnessUser) string { return u.Username })},
		{"password", userField(cfg.Users, 0, func(u harnessUser) string { return u.Password })}, {"totp_secret", userField(cfg.Users, 0, func(u harnessUser) string { return u.TOTPSecret })},
		{"ca_pem_b64", base64.StdEncoding.EncodeToString(ca)}, {"sessions", strings.Join(userFieldList(cfg.Users, 0, func(u harnessUser) []string { return u.Sessions }), ",")},
		{"view_token", userField(cfg.Users, 0, func(u harnessUser) string { return u.ViewToken })}, {"control_token", userField(cfg.Users, 0, func(u harnessUser) string { return u.ControlToken })},
		{"username2", userField(cfg.Users, 1, func(u harnessUser) string { return u.Username })}, {"password2", userField(cfg.Users, 1, func(u harnessUser) string { return u.Password })},
		{"totp_secret2", userField(cfg.Users, 1, func(u harnessUser) string { return u.TOTPSecret })}, {"sessions2", strings.Join(userFieldList(cfg.Users, 1, func(u harnessUser) []string { return u.Sessions }), ",")},
		{"view_token2", userField(cfg.Users, 1, func(u harnessUser) string { return u.ViewToken })}, {"host_cols", strconv.Itoa(cfg.HostCols)}, {"host_rows", strconv.Itoa(cfg.HostRows)},
	}
	args := []string{}
	for _, entry := range values {
		args = append(args, "-e", shellQuote(entry[0]), shellQuote(entry[1]))
	}
	return append(args, "systems.pkt.lingon.test/androidx.test.runner.AndroidJUnitRunner"), nil
}
