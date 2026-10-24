//go:build windows

package headless

import "os/exec"

func detach(_ *exec.Cmd) {}
