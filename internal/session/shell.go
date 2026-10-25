package session

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"

	"pkt.systems/lingon/internal/clipboard"
	"pkt.systems/lingon/internal/pty"
)

func resolveShellPath(shellOverride string) string {
	path := shellOverride
	if path == "" {
		if u, err := user.Current(); err == nil && u != nil && u.Uid != "" {
			if shell, err := shellFromPasswd(u.Uid); err == nil && shell != "" {
				path = shell
			}
		}
	}
	if path == "" {
		path = os.Getenv("SHELL")
	}
	if path == "" {
		path = "/bin/sh"
	}
	return path
}

func startShell(shellOverride, term string, endpoint *clipboard.Endpoint) (*os.File, *os.File, *exec.Cmd, error) {
	path := resolveShellPath(shellOverride)
	cmd := exec.Command(path)
	cmd.Env = os.Environ()
	if term != "" {
		cmd.Env = append(cmd.Env, "TERM="+term)
	}
	if endpoint != nil {
		cmd.Env = append(cmd.Env, "DISPLAY="+endpoint.Display, "XAUTHORITY="+endpoint.Authority)
	}
	ptyFile, ttyFile, err := pty.StartWithTTY(cmd)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("start shell: %w", err)
	}
	return ptyFile, ttyFile, cmd, nil
}
