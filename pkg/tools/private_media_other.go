//go:build !linux

package tools

import (
	"fmt"
	"os/exec"
)

func startWithMediaProtection(cmd *exec.Cmd) error {
	protected := false
	privateMediaDirectories.Range(func(_, _ any) bool { protected = true; return false })
	if protected {
		return fmt.Errorf("private media protection requires Linux Landlock; exec blocked")
	}
	return cmd.Start()
}
