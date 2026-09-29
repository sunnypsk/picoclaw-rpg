//go:build linux

package tools

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPrivateMediaShellProtection(t *testing.T) {
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 || abi < 3 {
		t.Skip("kernel lacks Landlock ABI 3")
	}
	home := t.TempDir()
	private := filepath.Join(home, ".picoclaw", "private-media", "images")
	if err := os.MkdirAll(private, 0700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(private, "index.json")
	if err := os.WriteFile(secret, []byte("private-image-index"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ProtectMediaDirectory(private); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { privateMediaDirectories.Delete(private) })
	workspace := t.TempDir()
	for _, script := range []string{
		"cat ~/.picoclaw/private-media/images/index.json",
		"cat \"$HOME/.picoclaw/private-media/images/index.json\"",
		"p=$HOME; p=$p/.picoclaw/private-media/images/index.json; cat \"$p\"",
		"cp ~/.picoclaw/private-media/images/index.json ./copy.json",
		"echo changed > ~/.picoclaw/private-media/images/index.json",
	} {
		cmd := exec.Command("sh", "-c", script)
		cmd.Dir = workspace
		cmd.Env = append(os.Environ(), "HOME="+home)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		if err := startWithMediaProtection(cmd); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Wait(); err == nil {
			t.Fatalf("protected access succeeded: %s", script)
		}
		if bytes.Contains(output.Bytes(), []byte("private-image-index")) {
			t.Fatal("private bytes leaked")
		}
	}
	cmd := exec.Command("sh", "-c", "echo allowed > normal.txt; cat normal.txt")
	cmd.Dir = workspace
	if err := startWithMediaProtection(cmd); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal("workspace operation failed:", err)
	}
	// The gateway thread must retain access after the protected child exits.
	data, err := os.ReadFile(secret)
	if err != nil || string(data) != "private-image-index" {
		t.Fatalf("gateway affected: %s %v", data, err)
	}
}
