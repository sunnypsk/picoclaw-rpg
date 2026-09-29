package tools

import (
	"fmt"
	"path/filepath"
	"sync"
)

var privateMediaDirectories sync.Map

// ProtectMediaDirectory keeps managed media and its index out of ordinary file
// tools, even when a user configures a broad workspace or a path whitelist.
func ProtectMediaDirectory(dir string) error {
	path, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	privateMediaDirectories.Store(filepath.Clean(path), true)
	return nil
}

func checkPrivateMediaPath(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	resolved := absolute
	if real, err := filepath.EvalSymlinks(absolute); err == nil {
		resolved = real
	}
	blocked := false
	privateMediaDirectories.Range(func(key, value any) bool {
		root := key.(string)
		realRoot := root
		if real, err := filepath.EvalSymlinks(root); err == nil {
			realRoot = real
		}
		if isWithinWorkspace(absolute, root) || isWithinWorkspace(resolved, realRoot) {
			blocked = true
			return false
		}
		return true
	})
	if blocked {
		return fmt.Errorf("access denied: private managed media requires a scoped image reference")
	}
	return nil
}
