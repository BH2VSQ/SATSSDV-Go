//go:build !malgo || !cgo

package audio

func NewRuntime(captureDeviceID, playbackDeviceID string, sampleRate int) (Runtime, error) {
	return nil, ErrRuntimeUnavailable
}
