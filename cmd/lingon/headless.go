package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/headlessd"
	"pkt.systems/lingon/internal/session"
	"pkt.systems/pslog"
)

const (
	headlessStartupStatusEnv     = "__LINGON__HEADLESS_STARTUP_STATUS_FD"
	headlessStartupStatusFD      = 3
	headlessStartupStatusTimeout = 10 * time.Second
)

type headlessStartupStatus struct {
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Session string `json:"session,omitempty"`
	Socket  string `json:"socket,omitempty"`
}

type headlessStartupReporter struct {
	file *os.File
}

func headlessAliasEnabled(argv0 string) bool {
	name := strings.TrimSpace(filepath.Base(argv0))
	return strings.EqualFold(name, "lingonx")
}

func configDirForLoader(loader *lingon.Loader) string {
	if loader == nil {
		return lingon.DefaultConfigDir()
	}
	used := strings.TrimSpace(loader.ConfigFileUsed())
	if used == "" {
		return lingon.DefaultConfigDir()
	}
	return filepath.Dir(used)
}

func startHeadlessReexec(cmd *cobra.Command, configDir string) error {
	if err := validateHeadlessReexecFlags(cmd); err != nil {
		return err
	}
	if err := os.MkdirAll(headless.BaseDir(configDir), 0o700); err != nil {
		return err
	}
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	args := append([]string(nil), os.Args[1:]...)
	sessionID, err := cmd.Flags().GetString("session")
	if err != nil {
		return err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		generated, _ := session.DefaultSessionIdentity()
		sessionID, err = headless.NormalizeSessionID(generated)
		if err != nil {
			return err
		}
		args = append(args, "--session", sessionID)
	}
	socketPath, err := headless.SocketPath(configDir, sessionID)
	if err != nil {
		return err
	}
	statusR, statusW, err := os.Pipe()
	if err != nil {
		return err
	}
	defer func() {
		_ = statusR.Close()
	}()
	defer func() {
		_ = statusW.Close()
	}()
	child := exec.Command(exePath, args...)
	configureDetachedProcess(child)
	child.ExtraFiles = []*os.File{statusW}
	child.Env = withEnv(withEnv(os.Environ(), headless.EnvForeground, headless.ForegroundValue), headlessStartupStatusEnv, strconv.Itoa(headlessStartupStatusFD))
	nullIn, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer func() {
		_ = nullIn.Close()
	}()
	nullOut, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() {
		_ = nullOut.Close()
	}()
	child.Stdin = nullIn
	child.Stdout = nullOut
	child.Stderr = nullOut
	if err := child.Start(); err != nil {
		return err
	}
	_ = statusW.Close()
	status, err := waitForHeadlessStartupStatus(statusR, child, logPathForHeadlessStartup(cmd))
	if err != nil {
		return err
	}
	if status.Status != "ready" {
		if strings.TrimSpace(status.Error) == "" {
			return fmt.Errorf("headless startup failed before reporting readiness")
		}
		return fmt.Errorf("headless startup failed: %s", status.Error)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "headless session starting in background (session=%s pid=%d socket=%s)\n", sessionID, child.Process.Pid, socketPath)
	return nil
}

func validateHeadlessReexecFlags(cmd *cobra.Command) error {
	if cmd == nil || !cmd.Flags().Changed(geometryFlagName) {
		return nil
	}
	raw, err := cmd.Flags().GetString(geometryFlagName)
	if err != nil {
		return err
	}
	_, _, err = parseGeometry(raw)
	return err
}

func runHeadlessForeground(cmd *cobra.Command, loader *lingon.Loader, configDir string, cfg lingon.Config) (retErr error) {
	startupReporter := headlessStartupReporterFromEnv()
	if startupReporter != nil {
		_ = os.Unsetenv(headlessStartupStatusEnv)
	}
	startupReady := false
	defer func() {
		if startupReporter == nil {
			return
		}
		defer startupReporter.Close()
		if retErr != nil && !startupReady {
			_ = startupReporter.Failed(retErr)
		}
	}()
	insecure, err := cmd.Flags().GetBool("insecure")
	if err != nil {
		return err
	}
	relay, err := resolveHeadlessRelayConfig(cmd, loader, cfg, insecure)
	if err != nil {
		return err
	}
	sessionID, err := cmd.Flags().GetString("session")
	if err != nil {
		return err
	}
	colsValue, rowsValue, err := resolveHeadlessSize(cmd)
	if err != nil {
		return err
	}
	wallInactiveAfterLevels, err := resolveHeadlessWallInactiveAfterLevels(cmd, cfg)
	if err != nil {
		return err
	}
	shellPath, err := cmd.Flags().GetString("shell")
	if err != nil {
		return err
	}
	termValue, err := cmd.Flags().GetString("term")
	if err != nil {
		return err
	}
	if !cmd.Flags().Changed("term") {
		termValue = cfg.Terminal.Term
	}
	respawnValue, err := cmd.Flags().GetBool("respawn")
	if err != nil {
		return err
	}
	if !cmd.Flags().Changed("respawn") {
		respawnValue = cfg.Terminal.Respawn
	}
	hostnameOnlyValue, err := cmd.Flags().GetBool("hostname-only")
	if err != nil {
		return err
	}
	if !cmd.Flags().Changed("hostname-only") {
		hostnameOnlyValue = cfg.Terminal.HostnameOnly
	}
	scrollbackValue := cfg.Terminal.ScrollbackLines
	if cmd.Flags().Changed("scrollback-lines") {
		value, getErr := cmd.Flags().GetInt("scrollback-lines")
		if getErr != nil {
			return getErr
		}
		scrollbackValue = value
	}
	logPath, err := cmd.Flags().GetString("log-file")
	if err != nil {
		return err
	}
	if !cmd.Flags().Changed("log-file") {
		logPath = cfg.Client.LogFile
	}
	if logPath == "" {
		logPath = lingon.DefaultLogPath()
	}
	logger, closer, err := openClientLogger(logPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = closer.Close()
	}()
	ctx := pslog.ContextWithLogger(cmd.Context(), logger)
	traceEnabled, err := cmd.Flags().GetBool("trace")
	if err != nil {
		return err
	}
	tracePath, err := cmd.Flags().GetString("trace-file")
	if err != nil {
		return err
	}
	traceWriter, outPath, err := setupTrace(traceEnabled, tracePath)
	if err != nil {
		return err
	}
	if traceWriter != nil {
		defer func() {
			_ = traceWriter.Close()
			fmt.Fprintln(cmd.OutOrStdout(), formatItalicGray(fmt.Sprintf("-- trace saved to %s --", outPath)))
		}()
	}
	daemon := headlessd.New(headlessd.Options{
		ConfigDir:               configDir,
		Endpoint:                relay.Endpoint,
		Token:                   relay.Token,
		AuthFile:                relay.AuthPath,
		SessionID:               sessionID,
		Cols:                    colsValue,
		Rows:                    rowsValue,
		Shell:                   shellPath,
		Term:                    termValue,
		Respawn:                 respawnValue,
		Offline:                 relay.Offline,
		WallInactiveAfterLevels: wallInactiveAfterLevels,
		Publish:                 relay.Endpoint != "",
		PublishControl:          true,
		HostnameOnly:            hostnameOnlyValue,
		ScrollbackLines:         scrollbackValue,
		TLSDir:                  cfg.Server.TLS.Dir,
		Insecure:                insecure,
		Logger:                  logger,
		Trace:                   traceWriter,
		OnStartupReady: func(ready headlessd.StartupReady) {
			startupReady = true
			if startupReporter != nil {
				_ = startupReporter.Ready(ready)
				startupReporter.Close()
				startupReporter = nil
			}
		},
	})
	return daemon.Run(ctx)
}

func headlessStartupReporterFromEnv() *headlessStartupReporter {
	raw := strings.TrimSpace(os.Getenv(headlessStartupStatusEnv))
	if raw == "" {
		return nil
	}
	fd, err := strconv.Atoi(raw)
	if err != nil || fd < 0 {
		return nil
	}
	return &headlessStartupReporter{file: os.NewFile(uintptr(fd), "headless-startup-status")}
}

func (r *headlessStartupReporter) Ready(ready headlessd.StartupReady) error {
	if r == nil || r.file == nil {
		return nil
	}
	return json.NewEncoder(r.file).Encode(headlessStartupStatus{
		Status:  "ready",
		Session: ready.SessionID,
		Socket:  ready.SocketPath,
	})
}

func (r *headlessStartupReporter) Failed(err error) error {
	if r == nil || r.file == nil || err == nil {
		return nil
	}
	return json.NewEncoder(r.file).Encode(headlessStartupStatus{
		Status: "error",
		Error:  headlessStartupErrorMessage(err),
	})
}

func (r *headlessStartupReporter) Close() {
	if r == nil || r.file == nil {
		return
	}
	_ = r.file.Close()
	r.file = nil
}

func waitForHeadlessStartupStatus(r io.Reader, child *exec.Cmd, logPath string) (headlessStartupStatus, error) {
	statusCh := make(chan headlessStartupStatus, 1)
	errCh := make(chan error, 1)
	go func() {
		var status headlessStartupStatus
		err := json.NewDecoder(bufio.NewReader(r)).Decode(&status)
		if err != nil {
			errCh <- err
			return
		}
		statusCh <- status
	}()
	timer := time.NewTimer(headlessStartupStatusTimeout)
	defer timer.Stop()
	select {
	case status := <-statusCh:
		return status, nil
	case err := <-errCh:
		if errors.Is(err, io.EOF) {
			return headlessStartupStatus{}, fmt.Errorf("headless startup failed before reporting readiness")
		}
		return headlessStartupStatus{}, fmt.Errorf("read headless startup status: %w", err)
	case <-timer.C:
		if child != nil && child.Process != nil {
			_ = child.Process.Kill()
		}
		if strings.TrimSpace(logPath) != "" {
			return headlessStartupStatus{}, fmt.Errorf("headless startup did not report readiness within %s; see %s", headlessStartupStatusTimeout, logPath)
		}
		return headlessStartupStatus{}, fmt.Errorf("headless startup did not report readiness within %s", headlessStartupStatusTimeout)
	}
}

func headlessStartupErrorMessage(err error) string {
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "unknown error"
	}
	if strings.Contains(msg, "auth file not found") && !strings.Contains(msg, "--offline") {
		msg += "; use `--offline` for local-only headless startup"
	}
	return msg
}

func logPathForHeadlessStartup(cmd *cobra.Command) string {
	if cmd == nil {
		return lingon.DefaultLogPath()
	}
	value, err := cmd.Flags().GetString("log-file")
	if err != nil || strings.TrimSpace(value) == "" {
		return lingon.DefaultLogPath()
	}
	return value
}

type headlessRelayConfig struct {
	Endpoint string
	Token    string
	AuthPath string
	Offline  bool
}

func resolveHeadlessRelayConfig(cmd *cobra.Command, loader *lingon.Loader, cfg lingon.Config, insecure bool) (headlessRelayConfig, error) {
	offlineValue, err := cmd.Flags().GetBool("offline")
	if err != nil {
		return headlessRelayConfig{}, err
	}
	authPath, err := cmd.Flags().GetString("auth-file")
	if err != nil {
		return headlessRelayConfig{}, err
	}
	if !cmd.Flags().Changed("auth-file") {
		authPath = cfg.Client.AuthFile
	}
	tokenValue, err := cmd.Flags().GetString("token")
	if err != nil {
		return headlessRelayConfig{}, err
	}
	endpointFlag, err := cmd.Flags().GetString("endpoint")
	if err != nil {
		return headlessRelayConfig{}, err
	}
	endpointValue, err := resolveEndpointValue(cmd, loader, cfg.Client.Endpoint, endpointFlag, authPath)
	if err != nil {
		if headlessLocalOnlyOfflineRequested(cmd, loader, cfg, offlineValue) && errors.Is(err, errEndpointAmbiguous) {
			return headlessRelayConfig{
				Offline: true,
			}, nil
		}
		return headlessRelayConfig{}, err
	}
	if endpointValue == "" {
		return headlessRelayConfig{}, fmt.Errorf("endpoint is required")
	}
	if !cmd.Flags().Changed("token") {
		resolved, resolveErr := resolveAccessToken(cmd.Context(), endpointValue, authPath, cfg.Server.TLS.Dir, insecure)
		if resolveErr != nil {
			ok, refreshErr := hasValidRefreshToken(endpointValue, authPath, timeNowUTC())
			if refreshErr != nil || !ok {
				if headlessRelayExplicit(cmd, loader, cfg) {
					return headlessRelayConfig{}, resolveErr
				}
				endpointValue = ""
				authPath = ""
				offlineValue = true
			}
		} else {
			tokenValue = resolved
		}
	}
	if endpointValue != "" && tokenValue == "" && authPath == "" {
		return headlessRelayConfig{}, fmt.Errorf("access token is required")
	}
	if cmd.Flags().Changed("token") && !cmd.Flags().Changed("auth-file") {
		authPath = ""
	}
	return headlessRelayConfig{
		Endpoint: endpointValue,
		Token:    tokenValue,
		AuthPath: authPath,
		Offline:  offlineValue,
	}, nil
}

func headlessRelayExplicit(cmd *cobra.Command, loader *lingon.Loader, cfg lingon.Config) bool {
	if cmd.Flags().Changed("endpoint") || cmd.Flags().Changed("token") || cmd.Flags().Changed("auth-file") {
		return true
	}
	return endpointExplicitlyConfigured(loader) && strings.TrimSpace(cfg.Client.Endpoint) != ""
}

func headlessLocalOnlyOfflineRequested(cmd *cobra.Command, loader *lingon.Loader, cfg lingon.Config, offline bool) bool {
	return offline && !headlessRelayExplicit(cmd, loader, cfg)
}

func resolveHeadlessWallInactiveAfterLevels(cmd *cobra.Command, cfg lingon.Config) ([]time.Duration, error) {
	raw := strings.TrimSpace(cfg.Terminal.WallInactiveAfter)
	if raw == "" {
		raw = lingon.DefaultWallInactiveAfterCSV
	}
	if cmd != nil && cmd.Flags().Changed("wall-inactive-after") {
		value, err := cmd.Flags().GetString("wall-inactive-after")
		if err != nil {
			return nil, err
		}
		raw = strings.TrimSpace(value)
	}
	levels, err := lingon.ParseWallInactiveAfterLevels(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid wall inactive levels %q: %w", raw, err)
	}
	return levels, nil
}

func timeNowUTC() time.Time {
	return time.Now().UTC()
}

func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			continue
		}
		out = append(out, entry)
	}
	out = append(out, prefix+value)
	return out
}
