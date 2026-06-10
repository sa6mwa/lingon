package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/lingon"
	"pkt.systems/lingon/internal/headless"
	"pkt.systems/lingon/internal/headlessd"
	"pkt.systems/lingon/internal/session"
	"pkt.systems/pslog"
)

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
	child := exec.Command(exePath, args...)
	configureDetachedProcess(child)
	child.Env = withEnv(os.Environ(), headless.EnvForeground, headless.ForegroundValue)
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

func runHeadlessForeground(cmd *cobra.Command, loader *lingon.Loader, configDir string, cfg lingon.Config) error {
	insecure, err := cmd.Flags().GetBool("insecure")
	if err != nil {
		return err
	}
	offlineValue, err := cmd.Flags().GetBool("offline")
	if err != nil {
		return err
	}
	authPath, err := cmd.Flags().GetString("auth-file")
	if err != nil {
		return err
	}
	if !cmd.Flags().Changed("auth-file") {
		authPath = cfg.Client.AuthFile
	}
	tokenValue, err := cmd.Flags().GetString("token")
	if err != nil {
		return err
	}
	endpointValue := ""
	if !offlineValue {
		endpointValue, err = cmd.Flags().GetString("endpoint")
		if err != nil {
			return err
		}
		endpointValue, err = resolveEndpointValue(cmd, loader, cfg.Client.Endpoint, endpointValue, authPath)
		if err != nil {
			return err
		}
		if endpointValue == "" {
			return fmt.Errorf("endpoint is required")
		}
		if !cmd.Flags().Changed("token") {
			resolved, resolveErr := resolveAccessToken(cmd.Context(), endpointValue, authPath, cfg.Server.TLS.Dir, insecure)
			if resolveErr != nil {
				ok, refreshErr := hasValidRefreshToken(endpointValue, authPath, timeNowUTC())
				if refreshErr != nil || !ok {
					if cmd.Flags().Changed("endpoint") || cmd.Flags().Changed("token") || cmd.Flags().Changed("auth-file") {
						return resolveErr
					}
					endpointValue = ""
					authPath = ""
					offlineValue = true
				}
			} else {
				tokenValue = resolved
			}
		}
	}
	if endpointValue != "" && tokenValue == "" && authPath == "" {
		return fmt.Errorf("access token is required")
	}
	if cmd.Flags().Changed("token") && !cmd.Flags().Changed("auth-file") {
		authPath = ""
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
		Endpoint:                endpointValue,
		Token:                   tokenValue,
		AuthFile:                authPath,
		SessionID:               sessionID,
		Cols:                    colsValue,
		Rows:                    rowsValue,
		Shell:                   shellPath,
		Term:                    termValue,
		Respawn:                 respawnValue,
		Offline:                 offlineValue,
		WallInactiveAfterLevels: wallInactiveAfterLevels,
		Publish:                 endpointValue != "",
		PublishControl:          true,
		HostnameOnly:            hostnameOnlyValue,
		ScrollbackLines:         scrollbackValue,
		TLSDir:                  cfg.Server.TLS.Dir,
		Insecure:                insecure,
		Logger:                  logger,
		Trace:                   traceWriter,
	})
	return daemon.Run(ctx)
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
