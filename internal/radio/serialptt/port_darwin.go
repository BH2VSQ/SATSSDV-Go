//go:build darwin

package serialptt

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	darwinTIOCMGET = 0x4004746a
	darwinTIOCMSET = 0x8004746d
	darwinTIOCMRTS = 0x004
	darwinTIOCMDTR = 0x002
)

type darwinPort struct{ file *os.File }

func openDarwinPort(name string) (LinePort, error) {
	f, e := os.OpenFile(name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if e != nil {
		return nil, e
	}
	return &darwinPort{file: f}, nil
}
func (p *darwinPort) SetRTS(v bool) error { return p.setBit(darwinTIOCMRTS, v) }
func (p *darwinPort) SetDTR(v bool) error { return p.setBit(darwinTIOCMDTR, v) }
func (p *darwinPort) setBit(mask int, v bool) error {
	if p == nil || p.file == nil {
		return os.ErrClosed
	}
	var bits int
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, p.file.Fd(), darwinTIOCMGET, uintptr(unsafe.Pointer(&bits)))
	if e != 0 {
		return fmt.Errorf("TIOCMGET: %w", e)
	}
	if v {
		bits |= mask
	} else {
		bits &^= mask
	}
	_, _, e = syscall.Syscall(syscall.SYS_IOCTL, p.file.Fd(), darwinTIOCMSET, uintptr(unsafe.Pointer(&bits)))
	if e != 0 {
		return fmt.Errorf("TIOCMSET: %w", e)
	}
	return nil
}
func (p *darwinPort) Close() error {
	if p == nil || p.file == nil {
		return nil
	}
	e := p.file.Close()
	p.file = nil
	return e
}
