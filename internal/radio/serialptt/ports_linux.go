//go:build linux

package serialptt

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func listPlatformPorts(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	patterns := []string{"/dev/ttyUSB*", "/dev/ttyACM*", "/dev/ttyS*", "/dev/serial/by-id/*"}
	set := map[string]struct{}{}
	for _, p := range patterns {
		matches, _ := filepath.Glob(p)
		for _, m := range matches {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if _, e := os.Stat(m); e == nil {
				set[m] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func openPlatformPort(name string) (LinePort, error) {
	if !strings.HasPrefix(name, "/dev/") {
		return nil, os.ErrNotExist
	}
	return openUnixPort(name)
}
