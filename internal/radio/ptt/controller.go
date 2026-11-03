package ptt

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Mode string

const (
	ModeVOX    Mode = "vox"
	ModeHamlib Mode = "hamlib"
	ModeRTS    Mode = "rts"
	ModeDTR    Mode = "dtr"
)

type Controller interface {
	SetPTT(context.Context, bool) error
	Close() error
}

type Config struct {
	Mode          Mode   `json:"mode"`
	RigctlAddress string `json:"rigctlAddress"`
	SerialPort    string `json:"serialPort"`
	ActiveHigh    bool   `json:"activeHigh"`
}

func (c Config) Validate() error {
	switch Mode(strings.ToLower(strings.TrimSpace(string(c.Mode)))) {
	case ModeVOX:
		return nil
	case ModeHamlib:
		if strings.TrimSpace(c.RigctlAddress) == "" {
			return errors.New("Hamlib mode requires a rigctld address")
		}
	case ModeRTS, ModeDTR:
		if strings.TrimSpace(c.SerialPort) == "" {
			return errors.New("serial PTT mode requires a selected port")
		}
	default:
		return fmt.Errorf("unsupported PTT mode %q", c.Mode)
	}
	return nil
}

type VOXController struct{}

func NewVOXController() *VOXController                    { return &VOXController{} }
func (*VOXController) SetPTT(context.Context, bool) error { return nil }
func (*VOXController) Close() error                       { return nil }
func (*VOXController) RequiresPTTLeadTime() bool          { return false }
