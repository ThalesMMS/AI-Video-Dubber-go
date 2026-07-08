// Package atomicfile contains helpers for replacing completed temp files.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Replace atomically moves source to destination when the platform supports it.
// Callers should create source in the destination directory so the rename stays
// on one filesystem. On Windows, where os.Rename does not overwrite an existing
// file, Replace moves the old destination aside first and restores it if the
// final rename fails.
func Replace(source, destination string) error {
	renameErr := os.Rename(source, destination)
	if renameErr == nil {
		return nil
	}
	if runtime.GOOS != "windows" {
		return replaceError(source, destination, renameErr)
	}

	if _, err := os.Stat(destination); err != nil {
		return replaceError(source, destination, renameErr)
	}
	backup, err := reserveBackupName(filepath.Dir(destination))
	if err != nil {
		return err
	}
	if err := os.Rename(destination, backup); err != nil {
		_ = os.Remove(backup)
		return fmt.Errorf("move existing destination aside for replace %q: %w", destination, err)
	}
	if err := os.Rename(source, destination); err != nil {
		if restoreErr := os.Rename(backup, destination); restoreErr != nil {
			return fmt.Errorf("replace %q failed after moving original to %q: %w; restore failed: %v", destination, backup, err, restoreErr)
		}
		return replaceError(source, destination, err)
	}
	_ = os.Remove(backup)
	return nil
}

func reserveBackupName(dir string) (string, error) {
	file, err := os.CreateTemp(dir, ".atomic-replace-backup-*")
	if err != nil {
		return "", fmt.Errorf("reserve replace backup name: %w", err)
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("close replace backup placeholder: %w", err)
	}
	if err := os.Remove(name); err != nil {
		return "", fmt.Errorf("remove replace backup placeholder: %w", err)
	}
	return name, nil
}

func replaceError(source, destination string, err error) error {
	return fmt.Errorf("atomic replace %q with %q failed; source and destination must be on the same writable filesystem: %w", destination, source, err)
}
