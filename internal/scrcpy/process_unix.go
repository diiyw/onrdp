//go:build !windows

package scrcpy

import "os/exec"

func prepareCommand(*exec.Cmd) {}
