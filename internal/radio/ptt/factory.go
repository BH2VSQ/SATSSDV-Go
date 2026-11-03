package ptt

import (
	"context"
	"fmt"

	"github.com/BH2VSQ/SATSSDV-Go/internal/radio/rigctl"
	"github.com/BH2VSQ/SATSSDV-Go/internal/radio/serialptt"
)

func NewController(cfg Config) (Controller, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	switch cfg.Mode {
	case ModeVOX:
		return NewVOXController(), nil
	case ModeHamlib:
		c := rigctl.New(cfg.RigctlAddress)
		return rigctl.NewPTTController(c)
	case ModeRTS, ModeDTR:
		signal := serialptt.SignalRTS
		if cfg.Mode == ModeDTR {
			signal = serialptt.SignalDTR
		}
		return &serialController{controller: serialptt.New(), port: cfg.SerialPort, signal: signal, activeHigh: cfg.ActiveHigh}, nil
	default:
		return nil, fmt.Errorf("unsupported PTT mode %q", cfg.Mode)
	}
}

type serialController struct {
	controller *serialptt.Controller
	port       string
	signal     serialptt.Signal
	activeHigh bool
}

func (c *serialController) SetPTT(_ context.Context, enabled bool) error {
	return c.controller.Set(c.port, c.signal, enabled, c.activeHigh)
}
func (c *serialController) Close() error { return c.controller.Close() }
