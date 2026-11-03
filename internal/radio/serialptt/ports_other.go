//go:build !windows && !linux && !darwin

package serialptt

import (
	"context"
	"errors"
)

func listPlatformPorts(context.Context) ([]string, error) {
	return nil, errors.New("serial port enumeration is unsupported on this operating system")
}
func openPlatformPort(string) (LinePort, error) {
	return nil, errors.New("serial PTT is unsupported on this operating system")
}
