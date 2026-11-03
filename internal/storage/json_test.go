package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveJSONReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.json")
	if err := SaveJSON(path, map[string]string{"version": "old"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveJSON(path, map[string]string{"version": "new"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := LoadJSON(path, &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != "new" {
		t.Fatalf("saved version = %q, want new", got["version"])
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file remains: %v", err)
	}
}

func TestSaveJSONRecoversInterruptedReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "album.json")
	backup := path + ".bak"
	if err := os.WriteFile(backup, []byte(`{"version":"previous"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveJSON(path, map[string]string{"version": "current"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := LoadJSON(path, &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != "current" {
		t.Fatalf("saved version = %q, want current", got["version"])
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup was not cleaned up: %v", err)
	}
}
