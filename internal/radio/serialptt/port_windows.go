//go:build windows

package serialptt

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	genericRead   = 0x80000000
	genericWrite  = 0x40000000
	openExisting  = 3
	setRTS        = 3
	clearRTS      = 4
	setDTR        = 5
	clearDTR      = 6
	invalidHandle = ^uintptr(0)
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procCreateFileW    = kernel32.NewProc("CreateFileW")
	procEscapeCommFunc = kernel32.NewProc("EscapeCommFunction")
	procCloseHandle    = kernel32.NewProc("CloseHandle")
)

type windowsPort struct{ handle syscall.Handle }

func openPlatformPort(name string) (LinePort, error) {
	name = strings.TrimSpace(name)
	if !strings.HasPrefix(strings.ToUpper(name), "COM") {
		return nil, errors.New("port must be named COMx")
	}
	if !strings.HasPrefix(name, `\\.\`) {
		name = `\\.\` + name
	}
	ptr, e := syscall.UTF16PtrFromString(name)
	if e != nil {
		return nil, e
	}
	r, _, ce := procCreateFileW.Call(uintptr(unsafe.Pointer(ptr)), genericRead|genericWrite, 0, 0, openExisting, 0, 0)
	if r == invalidHandle {
		if ce == syscall.Errno(0) {
			ce = errors.New("CreateFileW failed")
		}
		return nil, fmt.Errorf("open %s: %w", name, ce)
	}
	return &windowsPort{handle: syscall.Handle(r)}, nil
}
func (p *windowsPort) SetRTS(v bool) error {
	cmd := uintptr(clearRTS)
	if v {
		cmd = setRTS
	}
	return p.escape(cmd)
}
func (p *windowsPort) SetDTR(v bool) error {
	cmd := uintptr(clearDTR)
	if v {
		cmd = setDTR
	}
	return p.escape(cmd)
}
func (p *windowsPort) escape(cmd uintptr) error {
	if p == nil || p.handle == 0 {
		return errors.New("serial port is closed")
	}
	r, _, e := procEscapeCommFunc.Call(uintptr(p.handle), cmd)
	if r == 0 {
		if e == syscall.Errno(0) {
			e = errors.New("EscapeCommFunction failed")
		}
		return e
	}
	return nil
}
func (p *windowsPort) Close() error {
	if p == nil || p.handle == 0 {
		return nil
	}
	r, _, e := procCloseHandle.Call(uintptr(p.handle))
	p.handle = 0
	if r == 0 {
		if e == syscall.Errno(0) {
			e = errors.New("CloseHandle failed")
		}
		return e
	}
	return nil
}
