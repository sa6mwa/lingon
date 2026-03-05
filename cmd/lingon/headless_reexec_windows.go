//go:build windows

package main

import "os/exec"

func configureDetachedProcess(_ *exec.Cmd) {}
