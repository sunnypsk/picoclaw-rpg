//go:build ignore

package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	workingDir, err := os.Getwd()
	if err != nil {
		fail("get working directory", err)
	}

	sourceRoot := filepath.Clean(filepath.Join(workingDir, "..", "..", "..", "..", "workspace"))
	targetRoot := filepath.Join(workingDir, "workspace")

	if err := os.RemoveAll(targetRoot); err != nil {
		fail("remove existing generated workspace", err)
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		fail("create generated workspace root", err)
	}

	err = filepath.WalkDir(sourceRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}
		if shouldSkipGeneratedPath(relPath, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		targetPath := filepath.Join(targetRoot, relPath)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, targetPath, info.Mode().Perm())
	})
	if err != nil {
		fail("copy workspace templates", err)
	}
}

func shouldSkipGeneratedPath(relPath string, isDir bool) bool {
	relPath = filepath.ToSlash(relPath)
	if strings.Contains(relPath, "/__pycache__/") || strings.HasPrefix(relPath, "__pycache__/") {
		return true
	}
	if isDir && filepath.Base(relPath) == "__pycache__" {
		return true
	}
	return strings.HasSuffix(relPath, ".pyc")
}

func copyFile(sourcePath, targetPath string, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	targetFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer targetFile.Close()

	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		return err
	}
	return targetFile.Close()
}

func fail(action string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", action, err)
	os.Exit(1)
}
