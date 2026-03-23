// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

// Package fileutil provides file manipulation utilities.
package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// WriteFileAtomic atomically writes data to a file using a temp file + rename pattern.
//
// This guarantees that the target file is either:
// - Completely written with the new data
// - Unchanged (if any step fails before rename)
//
// The function:
// 1. Creates a temp file in the same directory (original untouched)
// 2. Writes data to temp file
// 3. Syncs data to disk (critical for SD cards/flash storage)
// 4. Sets file permissions
// 5. Syncs directory metadata (ensures rename is durable)
// 6. Atomically renames temp file to target path
//
// Safety guarantees:
// - Original file is NEVER modified until successful rename
// - Temp file is always cleaned up on error
// - Data is flushed to physical storage before rename
// - Directory entry is synced to prevent orphaned inodes
//
// Parameters:
//   - path: Target file path
//   - data: Data to write
//   - perm: File permission mode (e.g., 0o600 for secure, 0o644 for readable)
//
// Returns:
//   - Error if any step fails, nil on success
//
// Example:
//
//	// Secure config file (owner read/write only)
//	err := utils.WriteFileAtomic("config.json", data, 0o600)
//
//	// Public readable file
//	err := utils.WriteFileAtomic("public.txt", data, 0o644)
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create temp file in the same directory (ensures atomic rename works)
	// Using a hidden prefix (.tmp-) to avoid issues with some tools
	tmpFile, err := os.OpenFile(
		filepath.Join(dir, fmt.Sprintf(".tmp-%d-%d", os.Getpid(), time.Now().UnixNano())),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		perm,
	)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	tmpPath := tmpFile.Name()
	cleanup := true

	defer func() {
		if cleanup {
			tmpFile.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	// Write data to temp file
	// Note: Original file is untouched at this point
	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// CRITICAL: Force sync to storage medium before any other operations.
	// This ensures data is physically written to disk, not just cached.
	// Essential for SD cards, eMMC, and other flash storage on edge devices.
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	// Set file permissions before closing
	if err := tmpFile.Chmod(perm); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	// Close file before rename (required on Windows)
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename: temp file becomes the target
	// On POSIX: rename() is atomic
	// On Windows: Rename() is atomic for files
	if err := renameWithRetry(tmpPath, path); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Sync directory to ensure rename is durable
	// This prevents the renamed file from disappearing after a crash
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		dirFile.Close()
	}

	// Success: skip cleanup (file was renamed, no temp to remove)
	cleanup = false
	return nil
}

func renameWithRetry(from, to string) error {
	const (
		windowsRenameRetries = 20
		windowsRenameDelay   = 15 * time.Millisecond
	)

	attempts := 1
	if runtime.GOOS == "windows" {
		attempts = windowsRenameRetries
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := os.Rename(from, to); err != nil {
			lastErr = err
			if !shouldRetryRename(err) || i == attempts-1 {
				return err
			}
			time.Sleep(windowsRenameDelay)
			continue
		}
		return nil
	}

	return lastErr
}

func shouldRetryRename(err error) bool {
	if err == nil || runtime.GOOS != "windows" {
		return false
	}
	if os.IsPermission(err) {
		return true
	}

	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "access is denied") || strings.Contains(lower, "used by another process")
}
