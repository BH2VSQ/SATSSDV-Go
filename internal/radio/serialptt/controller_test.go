package serialptt

import (
	"errors"
	"reflect"
	"testing"
)

type fakePort struct {
	calls    []string
	closeErr error
}

func (f *fakePort) SetRTS(v bool) error { f.calls = append(f.calls, flag("RTS", v)); return nil }
func (f *fakePort) SetDTR(v bool) error { f.calls = append(f.calls, flag("DTR", v)); return nil }
func (f *fakePort) Close() error        { f.calls = append(f.calls, "CLOSE"); return f.closeErr }
func flag(s string, v bool) string {
	if v {
		return s + "=1"
	}
	return s + "=0"
}
func TestSerialPTTActiveHighAndRelease(t *testing.T) {
	p := &fakePort{}
	c := NewWithOpener(func(string) (LinePort, error) { return p, nil })
	if err := c.Set("COM3", SignalRTS, true, true); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("COM3", SignalRTS, false, true); err != nil {
		t.Fatal(err)
	}
	want := []string{"RTS=0", "DTR=0", "RTS=1", "RTS=0", "CLOSE"}
	if !reflect.DeepEqual(p.calls, want) {
		t.Fatalf("calls=%v want=%v", p.calls, want)
	}
}
func TestSerialPTTActiveLow(t *testing.T) {
	p := &fakePort{}
	c := NewWithOpener(func(string) (LinePort, error) { return p, nil })
	if err := c.Set("/dev/ttyUSB0", SignalDTR, true, false); err != nil {
		t.Fatal(err)
	}
	if err := c.Set("", SignalDTR, false, false); err != nil {
		t.Fatal(err)
	}
	want := []string{"RTS=1", "DTR=1", "DTR=0", "DTR=1", "CLOSE"}
	if !reflect.DeepEqual(p.calls, want) {
		t.Fatalf("calls=%v want=%v", p.calls, want)
	}
}
func TestRejectsInvalidSignalAndPort(t *testing.T) {
	c := New()
	if err := c.Set("COM1", Signal("cts"), true, true); err == nil {
		t.Fatal("expected signal error")
	}
	if err := c.Set("", SignalRTS, true, true); err == nil {
		t.Fatal("expected empty port error")
	}
	_ = c.Close()
}
func TestReleaseReportsLineAndCloseErrors(t *testing.T) {
	p := &fakePort{closeErr: errors.New("close failed")}
	c := NewWithOpener(func(string) (LinePort, error) { return p, nil })
	if err := c.Set("COM4", SignalRTS, true, true); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err == nil {
		t.Fatal("expected close error")
	}
}
