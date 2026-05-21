package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type harnessConfig struct {
	Endpoint    string        `json:"endpoint"`
	CACertPath  string        `json:"ca_cert_path"`
	HostEchoLog string        `json:"host_echo_log"`
	HostCols    int           `json:"host_cols"`
	HostRows    int           `json:"host_rows"`
	Users       []harnessUser `json:"users"`
	GeneratedAt string        `json:"generated_at"`
}

type harnessUser struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	TOTPSecret string   `json:"totp_secret"`
	Sessions   []string `json:"sessions"`
	ViewToken  string   `json:"view_token"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "config-env":
		os.Exit(runConfigEnv(os.Args[2:]))
	case "test-times":
		os.Exit(runTestTimes(os.Args[2:]))
	case "test-names":
		os.Exit(runTestNames(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "lingon-android-tools commands:")
	fmt.Fprintln(os.Stderr, "  config-env --config <path>   emit shell assignments from harness JSON config")
	fmt.Fprintln(os.Stderr, "  test-times --dir <path>      emit Android JUnit test times sorted by descending duration")
	fmt.Fprintln(os.Stderr, "  test-names --file <path>     emit Kotlin @Test function names, one per line")
}

func runConfigEnv(args []string) int {
	fs := flag.NewFlagSet("config-env", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "harness config JSON path")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "config-env: %v\n", err)
		return 2
	}
	if strings.TrimSpace(*configPath) == "" {
		fmt.Fprintln(os.Stderr, "config-env: --config is required")
		return 2
	}

	data, err := os.ReadFile(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config-env: read config: %v\n", err)
		return 1
	}
	var cfg harnessConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config-env: parse config: %v\n", err)
		return 1
	}
	caPem, err := os.ReadFile(cfg.CACertPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config-env: read CA cert: %v\n", err)
		return 1
	}
	endpoint, port, err := splitEndpoint(cfg.Endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config-env: parse endpoint: %v\n", err)
		return 1
	}

	var buf bytes.Buffer
	writeEnv(&buf, "ENDPOINT", endpoint)
	writeEnv(&buf, "PORT", port)
	writeEnv(&buf, "USERNAME", userField(cfg.Users, 0, func(u harnessUser) string { return u.Username }))
	writeEnv(&buf, "PASSWORD", userField(cfg.Users, 0, func(u harnessUser) string { return u.Password }))
	writeEnv(&buf, "TOTP_SECRET", userField(cfg.Users, 0, func(u harnessUser) string { return u.TOTPSecret }))
	writeEnv(&buf, "CA_PATH", cfg.CACertPath)
	writeEnv(&buf, "CA_PEM_B64", base64.StdEncoding.EncodeToString(caPem))
	writeEnv(&buf, "SESSIONS", strings.Join(userFieldList(cfg.Users, 0, func(u harnessUser) []string { return u.Sessions }), ","))
	writeEnv(&buf, "VIEW_TOKEN", userField(cfg.Users, 0, func(u harnessUser) string { return u.ViewToken }))
	writeEnv(&buf, "USERNAME2", userField(cfg.Users, 1, func(u harnessUser) string { return u.Username }))
	writeEnv(&buf, "PASSWORD2", userField(cfg.Users, 1, func(u harnessUser) string { return u.Password }))
	writeEnv(&buf, "TOTP_SECRET2", userField(cfg.Users, 1, func(u harnessUser) string { return u.TOTPSecret }))
	writeEnv(&buf, "SESSIONS2", strings.Join(userFieldList(cfg.Users, 1, func(u harnessUser) []string { return u.Sessions }), ","))
	writeEnv(&buf, "VIEW_TOKEN2", userField(cfg.Users, 1, func(u harnessUser) string { return u.ViewToken }))
	writeEnv(&buf, "HOST_COLS", fmt.Sprintf("%d", cfg.HostCols))
	writeEnv(&buf, "HOST_ROWS", fmt.Sprintf("%d", cfg.HostRows))

	_, _ = os.Stdout.Write(buf.Bytes())
	return 0
}

func runTestNames(args []string) int {
	fs := flag.NewFlagSet("test-names", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	filePath := fs.String("file", "", "Kotlin test file path")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "test-names: %v\n", err)
		return 2
	}
	if strings.TrimSpace(*filePath) == "" {
		fmt.Fprintln(os.Stderr, "test-names: --file is required")
		return 2
	}

	f, err := os.Open(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "test-names: open file: %v\n", err)
		return 1
	}
	defer f.Close()

	names, err := extractTestNames(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "test-names: extract names: %v\n", err)
		return 1
	}
	for _, name := range names {
		fmt.Println(name)
	}
	return 0
}

type testTiming struct {
	ClassName string
	Name      string
	Seconds   float64
}

func runTestTimes(args []string) int {
	fs := flag.NewFlagSet("test-times", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dirPath := fs.String("dir", "", "Android JUnit XML result directory")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "test-times: %v\n", err)
		return 2
	}
	if strings.TrimSpace(*dirPath) == "" {
		fmt.Fprintln(os.Stderr, "test-times: --dir is required")
		return 2
	}
	timings, err := readJUnitTestTimings(*dirPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "test-times: %v\n", err)
		return 1
	}
	writeTestTimings(os.Stdout, timings)
	return 0
}

func readJUnitTestTimings(dir string) ([]testTiming, error) {
	var timings []testTiming
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasPrefix(filepath.Base(path), "TEST-") || filepath.Ext(path) != ".xml" {
			return nil
		}
		fileTimings, err := readJUnitTestTimingFile(path)
		if err != nil {
			return err
		}
		timings = append(timings, fileTimings...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(timings, func(i, j int) bool {
		if timings[i].Seconds == timings[j].Seconds {
			if timings[i].ClassName == timings[j].ClassName {
				return timings[i].Name < timings[j].Name
			}
			return timings[i].ClassName < timings[j].ClassName
		}
		return timings[i].Seconds > timings[j].Seconds
	})
	return timings, nil
}

func readJUnitTestTimingFile(path string) ([]testTiming, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	decoder := xml.NewDecoder(file)
	var timings []testTiming
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "testcase" {
			continue
		}
		var timing testTiming
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "classname":
				timing.ClassName = attr.Value
			case "name":
				timing.Name = attr.Value
			case "time":
				if _, err := fmt.Sscanf(attr.Value, "%f", &timing.Seconds); err != nil {
					return nil, fmt.Errorf("parse testcase time %q in %s: %w", attr.Value, path, err)
				}
			}
		}
		if timing.Name != "" {
			timings = append(timings, timing)
		}
	}
	return timings, nil
}

func writeTestTimings(w io.Writer, timings []testTiming) {
	fmt.Fprintln(w, "seconds\tclass\ttest")
	for _, timing := range timings {
		fmt.Fprintf(w, "%.3f\t%s\t%s\n", timing.Seconds, timing.ClassName, timing.Name)
	}
}

func extractTestNames(r io.Reader) ([]string, error) {
	var (
		out    []string
		scan   = bufio.NewScanner(r)
		expect bool
	)
	scan.Buffer(make([]byte, 0, 1024), 1024*1024)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}
		if line == "@Test" {
			expect = true
			continue
		}
		if expect {
			if strings.HasPrefix(line, "fun ") {
				if name := testNameFromFunLine(line); name != "" {
					out = append(out, name)
				}
			}
			expect = false
		}
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func testNameFromFunLine(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return ""
	}
	name := fields[1]
	if idx := strings.IndexByte(name, '('); idx >= 0 {
		name = name[:idx]
	}
	return name
}

func splitEndpoint(endpoint string) (string, string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", "", fmt.Errorf("empty endpoint")
	}
	idx := strings.LastIndex(endpoint, ":")
	if idx < 0 || idx+1 >= len(endpoint) {
		return "", "", fmt.Errorf("endpoint missing port")
	}
	port := endpoint[idx+1:]
	if slash := strings.IndexByte(port, '/'); slash >= 0 {
		port = port[:slash]
	}
	if port == "" {
		return "", "", fmt.Errorf("endpoint missing port")
	}
	return endpoint, port, nil
}

func writeEnv(w io.Writer, key, value string) {
	_, _ = fmt.Fprintf(w, "%s=%s\n", key, shellQuote(value))
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func userField(users []harnessUser, idx int, fn func(harnessUser) string) string {
	if idx < 0 || idx >= len(users) {
		return ""
	}
	return fn(users[idx])
}

func userFieldList(users []harnessUser, idx int, fn func(harnessUser) []string) []string {
	if idx < 0 || idx >= len(users) {
		return nil
	}
	return fn(users[idx])
}
