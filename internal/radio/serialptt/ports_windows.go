//go:build windows

package serialptt

import (
	"context"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

var comPattern = regexp.MustCompile(`(?i)\bCOM[0-9]+\b`)

func listPlatformPorts(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "reg.exe", "query", `HKLM\HARDWARE\DEVICEMAP\SERIALCOMM`)
	b, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	matches := comPattern.FindAllString(string(b), -1)
	seen := map[string]bool{}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		m = strings.ToUpper(m)
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return comNumber(out[i]) < comNumber(out[j]) })
	return out, nil
}
func comNumber(s string) int {
	n := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		}
	}
	return n
}
