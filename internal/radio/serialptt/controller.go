package serialptt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type Signal string

const (
	SignalRTS Signal = "rts"
	SignalDTR Signal = "dtr"
)

type LinePort interface {
	SetRTS(bool) error
	SetDTR(bool) error
	Close() error
}

type portOpener func(string) (LinePort, error)

type Controller struct {
	mu         sync.Mutex
	open       portOpener
	port       LinePort
	name       string
	signal     Signal
	activeHigh bool
}

func New() *Controller                                              { return &Controller{open: openPlatformPort} }
func NewWithOpener(open func(string) (LinePort, error)) *Controller { return &Controller{open: open} }

func (c *Controller) Set(portName string, signal Signal, enabled, activeHigh bool) error {
	if c == nil {
		return errors.New("serial PTT controller is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	signal = Signal(strings.ToLower(strings.TrimSpace(string(signal))))
	if signal != SignalRTS && signal != SignalDTR {
		return fmt.Errorf("unsupported serial PTT signal %q", signal)
	}
	portName = strings.TrimSpace(portName)
	if enabled && portName == "" {
		return errors.New("serial PTT port is empty")
	}
	if !enabled {
		return c.releaseLocked()
	}
	if c.port != nil && (c.name != portName || c.signal != signal || c.activeHigh != activeHigh) {
		if err := c.releaseLocked(); err != nil {
			return fmt.Errorf("release previous serial PTT port: %w", err)
		}
	}
	if c.port == nil {
		if c.open == nil {
			return errors.New("serial PTT is unsupported on this platform")
		}
		p, err := c.open(portName)
		if err != nil {
			return fmt.Errorf("open serial port %s: %w", portName, err)
		}
		c.port, c.name, c.signal, c.activeHigh = p, portName, signal, activeHigh
		inactive := !activeHigh
		if err := p.SetRTS(inactive); err != nil {
			_ = c.releaseLocked()
			return fmt.Errorf("initialize RTS inactive: %w", err)
		}
		if err := p.SetDTR(inactive); err != nil {
			_ = c.releaseLocked()
			return fmt.Errorf("initialize DTR inactive: %w", err)
		}
	}
	level := activeHigh
	var err error
	if signal == SignalRTS {
		err = c.port.SetRTS(level)
	} else {
		err = c.port.SetDTR(level)
	}
	if err != nil {
		_ = c.releaseLocked()
		return fmt.Errorf("assert %s PTT: %w", signal, err)
	}
	return nil
}

func (c *Controller) releaseLocked() error {
	if c.port == nil {
		return nil
	}
	inactive := !c.activeHigh
	var lineErr error
	if c.signal == SignalRTS {
		lineErr = c.port.SetRTS(inactive)
	} else {
		lineErr = c.port.SetDTR(inactive)
	}
	closeErr := c.port.Close()
	c.port, c.name, c.signal, c.activeHigh = nil, "", "", false
	if lineErr != nil && closeErr != nil {
		return errors.Join(lineErr, closeErr)
	}
	if lineErr != nil {
		return lineErr
	}
	return closeErr
}

func (c *Controller) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.releaseLocked()
}

// ListPorts returns a fresh system enumeration. Call this on startup and from
// the settings dialog's explicit Refresh action.
func ListPorts(ctx context.Context) ([]string, error) { return listPlatformPorts(ctx) }
