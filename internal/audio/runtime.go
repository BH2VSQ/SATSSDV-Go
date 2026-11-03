package audio

import "errors"

type DeviceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

type Runtime interface {
	CaptureStream
	PlaybackStream
	DeviceLists() (inputs, outputs []DeviceInfo, err error)
	Close() error
}

var ErrRuntimeUnavailable = errors.New("audio runtime is unavailable; build with CGO and the malgo tag")
