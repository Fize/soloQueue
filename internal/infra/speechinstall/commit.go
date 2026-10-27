package speechinstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// replaceDirectory keeps the previous directory available for rollback until
// the new version is in place. Both paths must be on the same filesystem.
func replaceDirectory(stage, target string) error {
	backup := ""
	if _, err := os.Lstat(target); err == nil {
		backup, err = os.MkdirTemp(filepath.Dir(target), ".speech-backup-*")
		if err != nil {
			return err
		}
		if err = os.Remove(backup); err != nil {
			return err
		}
		if err = os.Rename(target, backup); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(stage, target); err != nil {
		if backup != "" {
			if rollback := os.Rename(backup, target); rollback != nil {
				return errors.Join(err, fmt.Errorf("restore previous installation: %w", rollback))
			}
		}
		return err
	}
	if backup != "" {
		_ = os.RemoveAll(backup)
	}
	return nil
}
