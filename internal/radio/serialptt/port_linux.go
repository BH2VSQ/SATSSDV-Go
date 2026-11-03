//go:build linux

package serialptt

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	linuxTIOCMGET = 0x5415
	linuxTIOCMSET = 0x5418
	linuxTIOCMRTS = 0x004
	linuxTIOCMDTR = 0x002
)

type unixPort struct{ file *os.File }

func openUnixPort(name string) (LinePort, error) {
	f, e := os.OpenFile(name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if e != nil {
		return nil, e
	}
	return &unixPort{file: f}, nil
}
func (p *unixPort) SetRTS(v bool) error { return p.setBit(linuxTIOCMRTS, v) }
func (p *unixPort) SetDTR(v bool) error { return p.setBit(linuxTIOCMDTR, v) }
func (p *unixPort) setBit(mask int, v bool) error {
	if p == nil || p.file == nil {
		return os.ErrClosed
	}
	var bits int
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, p.file.Fd(), linuxTIOCMGET, uintptr(unsafe.Pointer(&bits)))
	if errno != 0 {
		return fmt.Errorf("TIOCMGET: %w", errno)
	}
	if v {
		bits |= mask
	} else {
		bits &^= mask
	}
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, p.file.Fd(), linuxTIOCMSET, uintptr(unsafe.Pointer(&bits)))
	if errno != 0 {
		return fmt.Errorf("TIOCMSET: %w", errno)
	}
	return nil
}
func (p *unixPort) Close() error {
	if p == nil || p.file == nil {
		return nil
	}
	e := p.file.Close()
	p.file = nil
	return e
}
