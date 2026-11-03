//go:build darwin

package serialptt

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func listPlatformPorts(ctx context.Context) ([]string, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	matches, _ := filepath.Glob("/dev/cu.*")
	out := make([]string, 0, len(matches))
	for _, p := range matches {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if _, e := os.Stat(p); e == nil {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}
func openPlatformPort(name string) (LinePort, error) {
	if !strings.HasPrefix(name, "/dev/cu.") {
		return nil, os.ErrNotExist
	}
	return openDarwinPort(name)
}
