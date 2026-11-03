package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func UserDataDir(appName string) (string, error) {
	if appName == "" {
		return "", errors.New("application name is empty")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
func SaveJSON(path string, v any) error {
	if path == "" {
		return errors.New("output path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// os.Rename does not replace an existing destination on Windows. Preserve
	// the old file as a backup while installing the fully-written new index.
	backup := path + ".bak"
	_, targetErr := os.Stat(path)
	_, backupErr := os.Stat(backup)
	if os.IsNotExist(targetErr) && backupErr == nil {
		// Recover a previous file if the process stopped after moving the old
		// destination aside but before installing the replacement.
		if err := os.Rename(backup, path); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("restore interrupted backup %s: %w", backup, err)
		}
		targetErr = nil
		backupErr = os.ErrNotExist
	}
	if backupErr == nil {
		if err := os.Remove(backup); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("remove stale backup %s: %w", backup, err)
		}
	} else if !os.IsNotExist(backupErr) {
		_ = os.Remove(tmp)
		return fmt.Errorf("inspect backup %s: %w", backup, backupErr)
	}
	if targetErr == nil {
		if err := os.Rename(path, backup); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("backup previous %s: %w", path, err)
		}
	} else if !os.IsNotExist(targetErr) {
		_ = os.Remove(tmp)
		return fmt.Errorf("inspect previous %s: %w", path, targetErr)
	}
	if err := os.Rename(tmp, path); err != nil {
		if _, statErr := os.Stat(backup); statErr == nil {
			if restoreErr := os.Rename(backup, path); restoreErr != nil {
				_ = os.Remove(tmp)
				return fmt.Errorf("install %s: %w; restore previous file: %v", path, err, restoreErr)
			}
		}
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		// The new index is already installed. A stale backup is harmless and
		// will be discarded during the next successful write.
	}
	return nil
}
func LoadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}
