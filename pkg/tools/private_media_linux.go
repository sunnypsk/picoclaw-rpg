//go:build linux

package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// startWithMediaProtection applies a Landlock layer on a disposable OS thread.
// Only that thread and the child inherit it; the gateway remains unrestricted.
// Never UnlockOSThread: Go destroys the locked thread when the goroutine exits.
func startWithMediaProtection(cmd *exec.Cmd) error {
	var roots []string
	privateMediaDirectories.Range(func(key, value any) bool { roots = append(roots, key.(string)); return true })
	if len(roots) == 0 {
		return cmd.Start()
	}
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := restrictPrivateMedia(roots); err != nil {
			done <- fmt.Errorf("private media protection unavailable; exec blocked: %w", err)
			return
		}
		done <- cmd.Start()
	}()
	return <-done
}

func restrictPrivateMedia(roots []string) error {
	abi, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return errno
	}
	// ABI 3 covers truncate as well as reads, writes and cross-directory moves.
	if abi < 3 {
		return fmt.Errorf("Landlock ABI 3 or later required")
	}
	for i, path := range roots {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		roots[i] = resolved
	}
	handled := uint64((1 << 15) - 1)
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, uintptr(unsafe.Pointer(&handled)), unsafe.Sizeof(handled), 0)
	if errno != 0 {
		return errno
	}
	defer unix.Close(int(fd))
	var allowTree func(string) error
	allowTree = func(path string) error {
		ancestor := false
		for _, root := range roots {
			if isWithinWorkspace(path, root) {
				return nil
			}
			if isWithinWorkspace(root, path) {
				ancestor = true
			}
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		// Do not grant an alternate symlink path to a protected inode. Its canonical
		// target already has the appropriate rule from the real hierarchy traversal.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if ancestor {
			entries, err := os.ReadDir(path)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if err := allowTree(filepath.Join(path, entry.Name())); err != nil {
					return err
				}
			}
			return nil
		}
		access := handled
		if !info.IsDir() {
			access = unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_TRUNCATE
		}
		parent, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		defer unix.Close(parent)
		attr := struct {
			Access uint64
			Parent int32
		}{access, int32(parent)}
		_, _, errno := unix.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd, unix.LANDLOCK_RULE_PATH_BENEATH, uintptr(unsafe.Pointer(&attr)), 0, 0, 0)
		if errno != 0 {
			return errno
		}
		return nil
	}
	if err := allowTree("/"); err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	_, _, errno = unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
