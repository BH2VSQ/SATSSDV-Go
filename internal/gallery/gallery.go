package gallery

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Asset struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	AddedAt time.Time `json:"addedAt"`
	Width   int       `json:"width,omitempty"`
	Height  int       `json:"height,omitempty"`
}
type Index struct {
	Assets []Asset `json:"assets"`
}
type Store struct{ Root string }

func (s Store) Import(src string) (Asset, error) {
	if s.Root == "" {
		return Asset{}, errors.New("gallery root is empty")
	}
	f, e := os.Open(src)
	if e != nil {
		return Asset{}, e
	}
	defer f.Close()
	if e = os.MkdirAll(s.Root, 0o755); e != nil {
		return Asset{}, e
	}
	id, e := newID()
	if e != nil {
		return Asset{}, e
	}
	ext := strings.ToLower(filepath.Ext(src))
	if len(ext) > 8 {
		ext = ""
	}
	name := filepath.Base(src)
	dst := filepath.Join(s.Root, id+ext)
	out, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if e != nil {
		return Asset{}, e
	}
	if _, e = io.Copy(out, f); e != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return Asset{}, e
	}
	if e = out.Sync(); e != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return Asset{}, e
	}
	if e = out.Close(); e != nil {
		_ = os.Remove(dst)
		return Asset{}, e
	}
	return Asset{ID: id, Name: name, Path: dst, AddedAt: time.Now().UTC()}, nil
}
func (s Store) Delete(a Asset) error {
	if a.Path == "" {
		return errors.New("asset path is empty")
	}
	root, e := filepath.Abs(s.Root)
	if e != nil {
		return e
	}
	target, e := filepath.Abs(a.Path)
	if e != nil {
		return e
	}
	rel, e := filepath.Rel(root, target)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("asset path escapes gallery root")
	}
	return os.Remove(target)
}
func newID() (string, error) {
	b := make([]byte, 12)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}
