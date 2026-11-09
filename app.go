package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	runtimedebug "runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/album"
	"github.com/BH2VSQ/SATSSDV-Go/internal/audio"
	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
	"github.com/BH2VSQ/SATSSDV-Go/internal/gallery"
	"github.com/BH2VSQ/SATSSDV-Go/internal/gifmode"
	"github.com/BH2VSQ/SATSSDV-Go/internal/i18n"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
	"github.com/BH2VSQ/SATSSDV-Go/internal/radio/ptt"
	"github.com/BH2VSQ/SATSSDV-Go/internal/radio/serialptt"
	rxengine "github.com/BH2VSQ/SATSSDV-Go/internal/rx"
	"github.com/BH2VSQ/SATSSDV-Go/internal/storage"
	templatepkg "github.com/BH2VSQ/SATSSDV-Go/internal/template"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
	"github.com/BH2VSQ/SATSSDV-Go/internal/tximage"
	"github.com/BH2VSQ/SATSSDV-Go/internal/txqueue"
)

type rxRuntimeStats struct {
	audioBlocks          atomic.Uint64
	audioSamples         atomic.Uint64
	inputDiscontinuities atomic.Uint64
	decoderResets        atomic.Uint64
	queueDrops           atomic.Uint64
	decoderBlocks        atomic.Uint64
	decoderPackets       atomic.Uint64
	decoderErrors        atomic.Uint64
	decoderPanics        atomic.Uint64
	managerSubmitDrops   atomic.Uint64
	lastErrorMu          sync.RWMutex
	lastError            string
	lastErrorAt          time.Time
}

type App struct {
	mu                 sync.Mutex
	txGate             sync.Mutex
	txReserved         bool
	runtimeChanging    bool
	ctx                context.Context
	dataDir            string
	settings           Settings
	ports              []string
	keyer              ptt.Controller
	pttTest            *ptt.TestController
	pttError           string
	audioRuntime       audio.Runtime
	audioDuplex        *audio.Duplex
	audioError         string
	rxSpectrumMu       sync.Mutex
	rxPreviewMu        sync.RWMutex
	rxPreviewPNG       []byte
	rxPreviewGIF       []byte
	rxSpectrumWindow   []float32
	rxSpectrumLastEmit time.Time
	rxSNRMu            sync.Mutex
	rxSNRTracker       dsp.BandSNRTracker
	rxSNRProfile       string
	rxSNRSampleRate    int
	rxModemLocked      atomic.Bool
	rxPacketValidated  atomic.Bool
	rxMarySNRDB        atomic.Uint64 // float64 bits from the synchronized M-ary EVM estimator.
	rxMarySNRValid     atomic.Bool
	txSpectrumMu       sync.Mutex
	txSpectrumLastEmit time.Time
	txProgressMu       sync.Mutex
	txProgressLastEmit time.Time
	rxQueue            chan []float32
	rxQueueMu          sync.Mutex
	rxGapPending       bool
	rxCancel           context.CancelFunc
	rxDone             chan struct{}
	rxManager          *rxengine.Manager
	rxStats            rxRuntimeStats
	events             *EventBroker
	albumMu            sync.Mutex
	txCancel           context.CancelFunc
	txDone             chan struct{}
	txImageID          uint8
}

func NewApp() *App {
	a := &App{settings: DefaultSettings(), events: NewEventBroker()}
	a.rxManager = rxengine.NewManager(a.handleRXImageUpdate)
	return a
}
func (a *App) startup(ctx context.Context) {
	// Keep the Go-managed heap bounded so large image reconstructions and
	// accumulated sessions do not starve other desktop applications. This is
	// a soft Go heap limit; it does not cap native Qt or audio allocations.
	runtimedebug.SetMemoryLimit(640 << 20)
	runtimedebug.SetGCPercent(80)
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
	dir, err := storage.UserDataDir("SATSSDV-Go")
	if err != nil {
		a.setPTTError(err)
		return
	}
	a.mu.Lock()
	a.dataDir = dir
	a.mu.Unlock()
	var cfg Settings
	if err := storage.LoadJSON(filepath.Join(dir, "settings.json"), &cfg); err == nil {
		a.settings = normalizeSettings(cfg)
	} else if !errors.Is(err, os.ErrNotExist) {
		a.setPTTError(err)
	}
	if err := a.migrateLegacyTemplateBackground(); err != nil {
		a.setPTTError(fmt.Errorf("migrate shared template background: %w", err))
	}
	_, _ = a.RefreshSerialPorts()
	if err := a.rebuildPTT(); err != nil {
		a.setPTTError(err)
		return
	}
	if err := a.initializeAudio(); err != nil {
		a.setAudioError(err)
	}
}
func (a *App) shutdown(context.Context) {
	_ = a.StopPTTTest()
	a.StopTX()
	a.mu.Lock()
	txDone := a.txDone
	a.mu.Unlock()
	if txDone != nil {
		<-txDone
	}
	if err := a.closeAudio(); err != nil {
		a.setAudioError(err)
	}
	a.mu.Lock()
	runner := a.pttTest
	a.pttTest = nil
	a.keyer = nil
	a.mu.Unlock()
	if runner != nil {
		if err := runner.Close(); err != nil {
			a.setPTTError(err)
		}
	}
	if a.rxManager != nil {
		a.rxManager.Close()
	}
}
func (a *App) setPTTError(err error) {
	if err == nil {
		return
	}
	a.mu.Lock()
	a.pttError = err.Error()
	a.mu.Unlock()
	a.emit("ptt:error", err.Error())
}
func (a *App) rebuildPTT() error {
	a.mu.Lock()
	cfg := a.settings.PTT
	old := a.pttTest
	a.pttTest = nil
	a.keyer = nil
	a.mu.Unlock()
	if old != nil {
		if err := old.Close(); err != nil {
			return fmt.Errorf("close previous PTT controller: %w", err)
		}
	}
	keyer, err := ptt.NewController(cfg)
	if err != nil {
		return err
	}
	runner, err := ptt.NewTestController(keyer, func(s ptt.TestState) {
		a.emit("ptt:test-state", s)
	})
	if err != nil {
		_ = keyer.Close()
		return err
	}
	a.mu.Lock()
	a.keyer = keyer
	a.pttTest = runner
	a.pttError = ""
	a.mu.Unlock()
	return nil
}
func (a *App) GetProfiles() []domain.Profile { return domain.Profiles() }
func (a *App) GetSettings() Settings         { a.mu.Lock(); defer a.mu.Unlock(); return a.settings }
func (a *App) SaveSettings(s Settings) error {
	s = normalizeSettings(s)
	if err := domain.ValidateIndependentModes(s.RXProfile, s.TXProfile); err != nil {
		return err
	}
	if err := s.PTT.Validate(); err != nil {
		return err
	}
	if s.PTTLeadMS < 0 || s.PTTLeadMS > 10000 || s.PTTTailMS < 0 || s.PTTTailMS > 10000 {
		return errors.New("PTT lead/tail must be between 0 and 10000 ms")
	}
	a.mu.Lock()
	old, dir := a.settings, a.dataDir
	a.mu.Unlock()
	if dir == "" {
		return errors.New("application data directory is not initialized")
	}
	// Locale, profile, image quality and station-identity changes are live
	// settings. Do not reset RX merely because one of those values changed.
	rebuildRuntime := old.PTT != s.PTT ||
		old.AudioSampleRate != s.AudioSampleRate ||
		old.RXInputDevice != s.RXInputDevice ||
		old.TXOutputDevice != s.TXOutputDevice
	if !rebuildRuntime {
		if err := storage.SaveJSON(filepath.Join(dir, "settings.json"), s); err != nil {
			return err
		}
		a.mu.Lock()
		a.settings = s
		a.mu.Unlock()
		return nil
	}

	a.txGate.Lock()
	if a.runtimeChanging || a.txReserved {
		a.txGate.Unlock()
		return errors.New("stop the active TX transmission before applying audio/PTT settings")
	}
	a.runtimeChanging = true
	a.txGate.Unlock()
	defer func() {
		a.txGate.Lock()
		a.runtimeChanging = false
		a.txGate.Unlock()
	}()
	if err := storage.SaveJSON(filepath.Join(dir, "settings.json"), s); err != nil {
		return err
	}

	var resultErr error
	if err := a.StopPTTTest(); err != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("stop PTT test before applying settings: %w", err))
	}
	if err := a.closeAudio(); err != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("close previous audio runtime: %w", err))
	}
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	if err := a.rebuildPTT(); err != nil {
		return errors.Join(resultErr, fmt.Errorf("rebuild PTT controller: %w", err))
	}
	if err := a.initializeAudio(); err != nil {
		a.setAudioError(err)
		resultErr = errors.Join(resultErr, fmt.Errorf("reinitialize audio runtime: %w", err))
	}
	return resultErr
}

type AudioState struct {
	Connected  bool   `json:"connected"`
	RXActive   bool   `json:"rxActive"`
	TXActive   bool   `json:"txActive"`
	SampleRate int    `json:"sampleRate"`
	Error      string `json:"error,omitempty"`
}
type AudioDeviceLists struct {
	Inputs  []audio.DeviceInfo `json:"inputs"`
	Outputs []audio.DeviceInfo `json:"outputs"`
}

func (a *App) initializeAudio() error {
	a.mu.Lock()
	cfg := a.settings
	keyer := a.keyer
	ctx := a.ctx
	a.mu.Unlock()
	if keyer == nil {
		return errors.New("PTT controller is not initialized")
	}
	runtimeAudio, err := audio.NewRuntime(cfg.RXInputDevice, cfg.TXOutputDevice, cfg.AudioSampleRate)
	if err != nil {
		return err
	}
	duplex, err := audio.NewDuplex(runtimeAudio, runtimeAudio, keyer, cfg.AudioSampleRate, time.Duration(cfg.PTTLeadMS)*time.Millisecond, time.Duration(cfg.PTTTailMS)*time.Millisecond)
	if err != nil {
		_ = runtimeAudio.Close()
		return err
	}
	workerParent := ctx
	if workerParent == nil {
		workerParent = context.Background()
	}
	workerCtx, workerCancel := context.WithCancel(workerParent)
	queue := make(chan []float32, 512)
	workerDone := make(chan struct{})
	a.rxQueueMu.Lock()
	a.rxGapPending = false
	a.rxQueueMu.Unlock()
	a.mu.Lock()
	a.audioRuntime, a.audioDuplex = runtimeAudio, duplex
	a.audioError = ""
	a.rxSpectrumWindow = nil
	a.rxQueue, a.rxCancel, a.rxDone = queue, workerCancel, workerDone
	a.mu.Unlock()
	go func() {
		defer close(workerDone)
		a.runRXDecodeWorker(workerCtx, queue)
	}()
	if err := duplex.StartRX(workerCtx, a.handleRXAudio); err != nil {
		workerCancel()
		<-workerDone
		a.mu.Lock()
		a.rxQueue, a.rxCancel, a.rxDone = nil, nil, nil
		a.audioRuntime, a.audioDuplex = nil, nil
		a.mu.Unlock()
		_ = duplex.Close()
		_ = runtimeAudio.Close()
		return fmt.Errorf("start automatic RX: %w", err)
	}
	return nil
}

func (a *App) closeAudio() error {
	a.mu.Lock()
	duplex, runtimeAudio := a.audioDuplex, a.audioRuntime
	rxCancel, rxDone := a.rxCancel, a.rxDone
	a.audioDuplex, a.audioRuntime = nil, nil
	a.rxQueue, a.rxCancel, a.rxDone = nil, nil, nil
	a.mu.Unlock()
	a.rxQueueMu.Lock()
	a.rxGapPending = false
	a.rxQueueMu.Unlock()
	if rxCancel != nil {
		rxCancel()
	}
	var errs []error
	if duplex != nil {
		if err := duplex.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if runtimeAudio != nil {
		if err := runtimeAudio.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if rxDone != nil {
		<-rxDone
	}
	return errors.Join(errs...)
}

func (a *App) setAudioError(err error) {
	if err == nil {
		return
	}
	a.mu.Lock()
	a.audioError = err.Error()
	a.mu.Unlock()
	a.emit("audio:error", err.Error())
}

func (a *App) GetAudioState() AudioState {
	a.mu.Lock()
	d := a.audioDuplex
	rate := a.settings.AudioSampleRate
	audioErr := a.audioError
	ready := a.audioRuntime != nil
	a.mu.Unlock()

	// txReserved remains true for the full TX task, including the scheduler's
	// inter-cycle countdown when Duplex.State().TXActive is temporarily false.
	a.txGate.Lock()
	txReserved := a.txReserved
	a.txGate.Unlock()

	state := AudioState{Connected: ready, SampleRate: rate, Error: audioErr}
	if d != nil {
		s := d.State()
		state.RXActive = s.RXActive
		state.TXActive = s.TXActive || txReserved
	} else {
		state.TXActive = txReserved
	}
	return state
}

func (a *App) GetAudioDevices() (AudioDeviceLists, error) {
	a.mu.Lock()
	r := a.audioRuntime
	a.mu.Unlock()
	if r == nil {
		return AudioDeviceLists{}, audio.ErrRuntimeUnavailable
	}
	in, out, err := r.DeviceLists()
	return AudioDeviceLists{Inputs: in, Outputs: out}, err
}

func (a *App) StartRX() error {
	a.resetRXAudioSNRTracker()
	a.mu.Lock()
	d, ctx := a.audioDuplex, a.ctx
	profileID, rate := a.settings.RXProfile, a.settings.AudioSampleRate
	a.mu.Unlock()
	if d == nil {
		return audio.ErrRuntimeUnavailable
	}
	if profileID == domain.AutoReceiveProfile && rate < 44100 {
		return errors.New("automatic RX detection requires an audio sample rate of at least 44100 Hz; use 48000 Hz")
	}
	if profile, err := domain.ParseProfile(profileID); err == nil && profile.LinkLayer == "CCSDS-MARY" && rate < 44100 {
		return errors.New("QPSK/8PSK/16QAM 1600-symbol RX requires an audio sample rate of at least 44100 Hz; use 48000 Hz or higher")
	}
	if err := d.StartRX(ctx, a.handleRXAudio); err != nil {
		a.setAudioError(err)
		return err
	}
	return nil
}
func (a *App) StopRX() error {
	a.resetRXAudioSNRTracker()
	a.mu.Lock()
	d := a.audioDuplex
	a.mu.Unlock()
	if d == nil {
		return nil
	}
	if err := d.StopRX(); err != nil {
		a.setAudioError(err)
		return err
	}
	return nil
}
func (a *App) StopTX() {
	a.mu.Lock()
	d, cancel := a.audioDuplex, a.txCancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if d != nil {
		d.StopTX()
	}
}

// Start1500HzTestTone transmits a windowed 1500 Hz tone for at most 60 seconds
// through the configured TX audio/PTT path. A backend timeout cancels it even
// when the UI remains open or its event loop is stalled.
func (a *App) Start1500HzTestTone() error {
	a.txGate.Lock()
	a.mu.Lock()
	duplex, parent, rate, runner := a.audioDuplex, a.ctx, a.settings.AudioSampleRate, a.pttTest
	a.mu.Unlock()
	if duplex == nil {
		a.txGate.Unlock()
		return audio.ErrRuntimeUnavailable
	}
	if parent == nil {
		parent = context.Background()
	}
	if a.runtimeChanging || a.txReserved || (runner != nil && runner.State().Active) || duplex.State().TXActive {
		a.txGate.Unlock()
		return errors.New("TX is already active or PTT test is running")
	}
	if rate < 8000 || rate > 192000 {
		a.txGate.Unlock()
		return fmt.Errorf("invalid TX sample rate %d", rate)
	}
	ctx, cancel := context.WithTimeout(parent, 75*time.Second)
	done := make(chan struct{})
	a.txReserved = true
	a.mu.Lock()
	a.txCancel, a.txDone = cancel, done
	a.mu.Unlock()
	a.txGate.Unlock()
	a.emit("tx:tone-state", map[string]any{"active": true})
	a.emit("tx:state", map[string]any{"active": true})
	go func() {
		defer close(done)
		defer func() {
			cancel()
			a.txGate.Lock()
			a.txReserved = false
			a.mu.Lock()
			if a.txDone == done {
				a.txCancel, a.txDone = nil, nil
			}
			a.mu.Unlock()
			a.emitTXResetTelemetry()
			a.emit("tx:tone-state", map[string]any{"active": false})
			a.emit("tx:state", map[string]any{"active": false})
			a.txGate.Unlock()
		}()
		const duration = 60.0
		samples := make([]float32, int(float64(rate)*duration))
		const frequency = 1500.0
		ramp := int(float64(rate) * 0.01)
		for i := range samples {
			// Higher level improves VOX-trigger margin while retaining 3 dBFS headroom.
			gain := 0.70
			if ramp > 0 && i < ramp {
				gain *= float64(i) / float64(ramp)
			}
			if ramp > 0 && len(samples)-i <= ramp {
				gain *= float64(len(samples)-i-1) / float64(ramp)
			}
			samples[i] = float32(gain * math.Sin(2*math.Pi*frequency*float64(i)/float64(rate)))
		}
		err := duplex.Transmit(ctx, samples, func(progress float64) {
			a.emitTXProgress(samples, rate, progress)
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			a.emit("tx:error", err.Error())
		}
	}()
	return nil
}

type RenderedTXImage struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	PNGBase64        string       `json:"pngBase64"`
	GIFBase64        string       `json:"gifBase64,omitempty"`
	OverlayPNGBase64 string       `json:"overlayPNGBase64,omitempty"`
	GIFRect          gifmode.Rect `json:"gifRect,omitempty"`
}

// StartTXRendered transmits the currently selected template as one image.
// Repeat mode resends this same rendered image at the configured interval.
func (a *App) StartTXRendered(image RenderedTXImage, repeat bool, intervalSeconds int, ax25To string) error {
	if intervalSeconds < 0 || intervalSeconds > 86400 {
		return errors.New("TX interval must be between 0 and 86400 seconds")
	}
	if image.ID == "" {
		image.ID = "selected-template"
	}
	if image.PNGBase64 == "" {
		return errors.New("rendered TX template is empty")
	}
	data, err := base64.StdEncoding.DecodeString(image.PNGBase64)
	if err != nil {
		return fmt.Errorf("decode rendered TX template: %w", err)
	}
	if len(data) > 8*1024*1024 {
		return errors.New("rendered TX template exceeds the 8 MiB safety limit")
	}
	if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		return errors.New("rendered TX template is not a PNG")
	}
	a.mu.Lock()
	profile, quality, callsign, ax25SSID, audioRate := a.settings.TXProfile, a.settings.Quality, a.settings.Callsign, a.settings.AX25SSID, a.settings.AudioSampleRate
	a.mu.Unlock()
	ax25To = strings.ToUpper(strings.TrimSpace(ax25To))
	if ax25To == "" {
		ax25To = "CQ"
	}
	parsedProfile, err := domain.ParseProfile(profile)
	if err != nil {
		return err
	}
	if parsedProfile.LinkLayer == "CCSDS-MARY" && audioRate < 44100 {
		return errors.New("QPSK/8PSK/16QAM 1600-symbol TX requires an audio sample rate of at least 44100 Hz; use 48000 Hz or higher")
	}
	if strings.TrimSpace(image.GIFBase64) != "" {
		if parsedProfile.ImageCodec != "SSDV" {
			return errors.New("GIF template components require an SSDV TX profile")
		}
		return a.startTransmissionGIF(image, data, profile, quality, callsign, ax25SSID, audioRate, ax25To, repeat, intervalSeconds)
	}
	job := txqueue.Job{ID: image.ID, ImageName: image.Name, ImageDataBase64: image.PNGBase64, Profile: profile, Quality: quality, AX25SSID: ax25SSID, AX25To: ax25To}
	return a.startTransmission(job, repeat, intervalSeconds)
}

// startTransmissionGIF runs the attachment's private SATSSDV-Go incremental
// GIF transport through the existing radio/audio path. It is intentionally
// limited to SSDV profiles; PCSI has a different packet format.
func (a *App) startTransmissionGIF(rendered RenderedTXImage, basePNG []byte, profileID, quality, callsign string, ax25SSID, audioRate int, ax25To string, repeat bool, intervalSeconds int) error {
	a.mu.Lock()
	maryFECProfile := a.settings.MARYFECProfile
	a.mu.Unlock()
	gifData, err := base64.StdEncoding.DecodeString(rendered.GIFBase64)
	if err != nil || len(gifData) == 0 || len(gifData) > 8*1024*1024 {
		return errors.New("template GIF must be valid and no larger than 8 MiB")
	}
	baseImage, err := png.Decode(bytes.NewReader(basePNG))
	if err != nil {
		return fmt.Errorf("decode template base PNG: %w", err)
	}
	base := image.NewRGBA(image.Rect(0, 0, baseImage.Bounds().Dx(), baseImage.Bounds().Dy()))
	draw.Draw(base, base.Bounds(), baseImage, baseImage.Bounds().Min, draw.Src)
	var overlay *image.RGBA
	if rendered.OverlayPNGBase64 != "" {
		overlayData, decodeErr := base64.StdEncoding.DecodeString(rendered.OverlayPNGBase64)
		if decodeErr != nil || len(overlayData) == 0 || len(overlayData) > 8*1024*1024 {
			return errors.New("template GIF overlay PNG is invalid or exceeds 8 MiB")
		}
		overlayImage, imageErr := png.Decode(bytes.NewReader(overlayData))
		if imageErr != nil {
			return fmt.Errorf("decode template overlay PNG: %w", imageErr)
		}
		if overlayImage.Bounds().Dx() != base.Bounds().Dx() || overlayImage.Bounds().Dy() != base.Bounds().Dy() {
			return errors.New("template GIF overlay dimensions do not match the base canvas")
		}
		overlay = image.NewRGBA(base.Bounds())
		draw.Draw(overlay, overlay.Bounds(), overlayImage, overlayImage.Bounds().Min, draw.Src)
	} else {
		overlay = image.NewRGBA(base.Bounds())
	}
	parsedProfile, err := domain.ParseProfile(profileID)
	if err != nil {
		return err
	}
	if parsedProfile.ImageCodec != "SSDV" {
		return errors.New("GIF template components require an SSDV TX profile")
	}
	if parsedProfile.LinkLayer == "CCSDS-MARY" && audioRate < 44100 {
		return errors.New("QPSK/8PSK/16QAM 1600-symbol TX requires an audio sample rate of at least 44100 Hz")
	}
	if intervalSeconds < 0 || intervalSeconds > 86400 {
		return errors.New("TX interval must be between 0 and 86400 seconds")
	}
	if strings.TrimSpace(ax25To) == "" {
		ax25To = "CQ"
	}
	if quality == "" {
		quality = "medium"
	}

	jobID, imageName, gifRect := rendered.ID, rendered.Name, rendered.GIFRect
	job := txqueue.Job{ID: jobID, ImageName: imageName, Profile: profileID, Quality: quality, AX25SSID: ax25SSID, AX25To: ax25To}
	// The GIF can be expensive to decode/packetize. Prepare it in the TX worker,
	// not on the Qt event thread, then reuse the same burst for each repeat cycle.
	prepared := false
	var prepareErr error
	var frames []gifmode.Frame
	var archiveGIF []byte
	var archiveWidth, archiveHeight int
	var stats gifmode.Stats
	var waveformRate int
	var packets [][]byte
	var encoder *transport.Encoder
	var estimatedSamples int64
	var wireFrameCount int
	var duplex *audio.Duplex
	var currentCallsign string
	var baseImageID uint8
	sender := func(ctx context.Context, _ txqueue.Job) error {
		if !prepared {
			frames, prepareErr = gifmode.DecodeTemplate(gifData, quality, base, overlay, gifRect)
			if prepareErr != nil {
				prepared = true
				return prepareErr
			}
			if err := ctx.Err(); err != nil {
				prepared = true
				return err
			}
			archiveWidth = frames[0].Image.Bounds().Dx()
			archiveHeight = frames[0].Image.Bounds().Dy()
			archiveGIF, prepareErr = gifmode.EncodeGIF(frames)
			if prepareErr != nil {
				prepared = true
				return fmt.Errorf("prepare TX GIF album preview: %w", prepareErr)
			}
			if len(archiveGIF) == 0 || len(archiveGIF) > 16*1024*1024 {
				prepared = true
				return errors.New("transmitted GIF preview is empty or exceeds the 16 MiB album limit")
			}
			a.mu.Lock()
			currentCallsign = strings.TrimSpace(a.settings.Callsign)
			if currentCallsign == "" {
				currentCallsign = callsign
			}
			a.txImageID++
			baseImageID = a.txImageID
			if len(frames) > 1 {
				a.txImageID += uint8(len(frames) - 1)
			}
			duplex = a.audioDuplex
			a.mu.Unlock()
			if duplex == nil {
				prepareErr = audio.ErrRuntimeUnavailable
				prepared = true
				return prepareErr
			}
			encodedPackets, packetStats, encodeErr := gifmode.EncodeContextV2(ctx, frames, currentCallsign, baseImageID, ssdv.DefaultJPEGQuality)
			if encodeErr != nil {
				prepareErr = encodeErr
				prepared = true
				return prepareErr
			}
			packets = encodedPackets
			stats = packetStats
			// The compressed album animation is now prepared and packetization is
			// complete. Do not retain every full-resolution RGBA frame or the source
			// GIF/template layers for the duration of a potentially long TX burst.
			frames = nil
			gifData, base, overlay = nil, nil, nil
			transportConfig := transport.Config{Profile: domain.TransportMode(profileID), AudioSampleRate: audioRate, MARYFECProfile: maryFECProfileValue(maryFECProfile)}
			if parsedProfile.LinkLayer == "AX.25 UI / HDLC" {
				transportConfig.Source = ax25.Address{Callsign: currentCallsign, SSID: uint8(ax25SSID)}
				transportConfig.Destination = ax25.Address{Callsign: ax25To}
			}
			var createErr error
			encoder, createErr = transport.NewEncoder(transportConfig)
			if createErr != nil {
				prepareErr = createErr
				prepared = true
				return prepareErr
			}
			estimatedSamples, prepareErr = encoder.EstimateOutputSamplesForPackets(packets)
			if prepareErr != nil {
				prepared = true
				return fmt.Errorf("estimate GIF template stream: %w", prepareErr)
			}
			var countErr error
			wireFrameCount, countErr = encoder.EstimateWirePacketCount(packets)
			if countErr != nil {
				prepared = true
				return fmt.Errorf("estimate GIF physical data frames: %w", countErr)
			}
			// Discard temporary frame and image buffers before opening the bounded
			// PCM stream. The encoded 256-byte packet set is retained for repeat TX.
			runtimedebug.FreeOSMemory()
			waveformRate, prepareErr = encoder.SampleRate()
			if prepareErr != nil {
				prepared = true
				return fmt.Errorf("prepare GIF template stream: %w", prepareErr)
			}
			prepared = true
			airtime := durationForSamples(estimatedSamples, waveformRate)
			a.emit("tx:gif-stats", map[string]any{
				"frames": stats.Frames, "keyframes": stats.Keyframes,
				"dataPackets": stats.DataPackets, "totalPackets": stats.TotalPackets,
				"wirePackets": wireFrameCount,
				"changedMCUs": stats.ChangedMCUs, "totalMCUs": stats.TotalMCUs,
				"motionVectors": stats.MotionVectors, "predictedMCUs": stats.PredictedMCUs,
				"estimatedSeconds": int(airtime.Seconds()),
			})
		}
		if prepareErr != nil {
			return prepareErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var startedAt time.Time
		estimatedDuration := durationForSamples(estimatedSamples, waveformRate)
		totalDataFrames := wireFrameCount
		producer := func(streamCtx context.Context, emit func([]float32) error) error {
			streamedSamples, streamErr := encoder.StreamPackets(streamCtx, packets, func(block []float32) error {
				if emitErr := emit(block); emitErr != nil {
					return emitErr
				}
				a.emitTXStreamSpectrum(block, waveformRate)
				return nil
			})
			if streamErr == nil && streamedSamples != estimatedSamples {
				return fmt.Errorf("GIF stream encoder produced %d samples, estimated %d", streamedSamples, estimatedSamples)
			}
			return streamErr
		}
		if err := duplex.TransmitStreamWithStart(ctx, estimatedSamples, producer, func() {
			startedAt = time.Now().UTC()
			a.emitTXMetrics(startedAt, estimatedDuration, totalDataFrames, 0)
		}, func(progress float64) {
			a.emitTXProgress(nil, waveformRate, progress)
			a.emitTXMetrics(startedAt, estimatedDuration, totalDataFrames, progress)
		}); err != nil {
			return err
		}
		if err := a.archiveTXGIFData(archiveGIF, archiveWidth, archiveHeight, profileID, currentCallsign, baseImageID, stats, startedAt); err != nil {
			return fmt.Errorf("archive transmitted GIF: %w", err)
		}
		a.emit("tx:gif-sent", map[string]any{"fileName": imageName, "frames": stats.Frames, "packetsSent": stats.TotalPackets, "wirePacketsSent": totalDataFrames, "startedAt": startedAt})
		return nil
	}
	return a.startTransmissionWithSender(job, repeat, intervalSeconds, sender)
}

func (a *App) startTransmission(job txqueue.Job, repeat bool, intervalSeconds int) error {
	return a.startTransmissionWithSender(job, repeat, intervalSeconds, func(ctx context.Context, item txqueue.Job) error {
		return a.sendImageJob(ctx, item)
	})
}

func (a *App) startTransmissionWithSender(job txqueue.Job, repeat bool, intervalSeconds int, sender txqueue.Sender) error {
	if sender == nil {
		return errors.New("TX sender is required")
	}
	a.mu.Lock()
	parent, duplex, test := a.ctx, a.audioDuplex, a.pttTest
	a.mu.Unlock()
	if duplex == nil {
		return audio.ErrRuntimeUnavailable
	}
	if parent == nil {
		parent = context.Background()
	}
	a.txGate.Lock()
	if a.runtimeChanging || a.txReserved || (test != nil && test.State().Active) || duplex.State().TXActive {
		a.txGate.Unlock()
		return errors.New("TX is already active or PTT test is running")
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	a.txReserved = true
	a.mu.Lock()
	a.txCancel, a.txDone = cancel, done
	a.mu.Unlock()
	a.txGate.Unlock()

	go func() {
		defer close(done)
		defer func() {
			cancel()
			a.txGate.Lock()
			a.txReserved = false
			a.mu.Lock()
			a.txCancel, a.txDone = nil, nil
			a.mu.Unlock()
			a.emitTXResetTelemetry()
			a.emit("tx:state", map[string]any{"active": false})
			a.txGate.Unlock()
		}()
		if ctx.Err() == nil {
			a.emit("tx:state", map[string]any{"active": true})
		}
		err := txqueue.Run(ctx, []txqueue.Job{job}, txqueue.Config{Repeat: repeat, Interval: time.Duration(intervalSeconds) * time.Second}, sender, func(event txqueue.Event) {
			a.emit("tx:status", event)
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			a.emit("tx:error", err.Error())
		}
	}()
	return nil
}

func (a *App) getContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}

func maryFECProfileValue(value string) int {
	if strings.EqualFold(strings.TrimSpace(value), "robust") {
		return transport.MARYFECRobust
	}
	return transport.MARYFECBalanced
}

func (a *App) sendImageJob(ctx context.Context, job txqueue.Job) error {
	if _, err := domain.ParseProfile(job.Profile); err != nil {
		return err
	}
	var source image.Image
	if job.ImageDataBase64 != "" {
		data, decodeErr := base64.StdEncoding.DecodeString(job.ImageDataBase64)
		if decodeErr != nil {
			return fmt.Errorf("decode rendered TX image data: %w", decodeErr)
		}
		var format string
		source, format, decodeErr = image.Decode(bytes.NewReader(data))
		if decodeErr != nil {
			return fmt.Errorf("decode rendered TX image (%s): %w", format, decodeErr)
		}
	} else {
		file, openErr := os.Open(job.ImagePath)
		if openErr != nil {
			return openErr
		}
		var decodeErr error
		source, _, decodeErr = image.Decode(file)
		closeErr := file.Close()
		if decodeErr != nil {
			return fmt.Errorf("decode source image: %w", decodeErr)
		}
		if closeErr != nil {
			return closeErr
		}
	}
	a.mu.Lock()
	callsign, rate, duplex := a.settings.Callsign, a.settings.AudioSampleRate, a.audioDuplex
	maryFECProfile := a.settings.MARYFECProfile
	a.txImageID++
	imageID := a.txImageID
	a.mu.Unlock()
	if duplex == nil {
		return audio.ErrRuntimeUnavailable
	}
	prepared, err := tximage.PrepareImagePacketsWithAX25FEC(source, job.Profile, job.Quality, callsign, imageID, rate, uint8(job.AX25SSID), job.AX25To, maryFECProfileValue(maryFECProfile))
	if err != nil {
		return err
	}
	source = nil // the prepared/resized image and encoded packets are now sufficient
	estimatedSamples, err := prepared.Encoder.EstimateOutputSamplesForPackets(prepared.Packets)
	if err != nil {
		return err
	}
	a.emit("tx:progress", 0.0)
	var transmissionStartedAt time.Time
	totalDataFrames, err := prepared.Encoder.EstimateWirePacketCount(prepared.Packets)
	if err != nil {
		return err
	}
	estimatedDuration := durationForSamples(estimatedSamples, prepared.SampleRate)
	producer := func(streamCtx context.Context, emit func([]float32) error) error {
		streamedSamples, streamErr := prepared.Encoder.StreamPackets(streamCtx, prepared.Packets, func(block []float32) error {
			if emitErr := emit(block); emitErr != nil {
				return emitErr
			}
			a.emitTXStreamSpectrum(block, prepared.SampleRate)
			return nil
		})
		if streamErr == nil && streamedSamples != estimatedSamples {
			return fmt.Errorf("stream encoder produced %d samples, estimated %d", streamedSamples, estimatedSamples)
		}
		return streamErr
	}
	if err := duplex.TransmitStreamWithStart(ctx, estimatedSamples, producer, func() {
		transmissionStartedAt = time.Now().UTC()
		a.emitTXMetrics(transmissionStartedAt, estimatedDuration, totalDataFrames, 0)
	}, func(progress float64) {
		a.emitTXProgress(nil, prepared.SampleRate, progress)
		a.emitTXMetrics(transmissionStartedAt, estimatedDuration, totalDataFrames, progress)
	}); err != nil {
		return err
	}
	prepared.Packets = nil
	return a.archiveTXImage(prepared.Image, job, callsign, imageID, prepared.PacketCount, transmissionStartedAt)
}

func (a *App) archiveTXImage(img image.Image, job txqueue.Job, callsign string, imageID uint8, packetCount int, transmissionStartedAt time.Time) error {
	dir, err := a.requireDataDir()
	if err != nil {
		return err
	}
	a.albumMu.Lock()
	defer a.albumMu.Unlock()
	imageDir := filepath.Join(dir, "album", "images")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		return err
	}
	created := transmissionStartedAt.UTC()
	if created.IsZero() {
		created = time.Now().UTC()
	}
	name := fmt.Sprintf("TX_%s_%03d.png", created.Format("20060102T150405.000Z"), imageID)
	path := filepath.Join(imageDir, name)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	var index album.Index
	if err := storage.LoadJSON(filepath.Join(dir, "album.json"), &index); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(path)
		return err
	}
	index.Records = append(index.Records, album.Record{
		ID:   fmt.Sprintf("tx-%s-%d", created.Format("20060102150405.000000000"), imageID),
		Type: "TX", ImagePath: path, CreatedAt: created, Profile: job.Profile,
		Callsign: callsign, ImageID: uint16(imageID), PacketsReceived: packetCount,
		PacketsExpected: packetCount, Quality: job.Quality, Status: "sent",
	})
	if err := storage.SaveJSON(filepath.Join(dir, "album.json"), index); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func (a *App) archiveTXGIFData(data []byte, width, height int, profileID, callsign string, imageID uint8, stats gifmode.Stats, transmissionStartedAt time.Time) error {
	if len(data) == 0 || len(data) > 16*1024*1024 {
		return errors.New("transmitted GIF preview is empty or exceeds the 16 MiB album limit")
	}
	if width <= 0 || height <= 0 {
		return errors.New("transmitted GIF preview has invalid dimensions")
	}
	dir, err := a.requireDataDir()
	if err != nil {
		return err
	}
	a.albumMu.Lock()
	defer a.albumMu.Unlock()
	imageDir := filepath.Join(dir, "album", "images")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		return err
	}
	created := transmissionStartedAt.UTC()
	if created.IsZero() {
		created = time.Now().UTC()
	}
	name := fmt.Sprintf("TX_%s_%s_%03d.gif", created.Format("20060102T150405.000000000Z"), safeAlbumName(callsign), imageID)
	path := filepath.Join(imageDir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	var index album.Index
	if err := storage.LoadJSON(filepath.Join(dir, "album.json"), &index); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(path)
		return err
	}
	index.Records = append(index.Records, album.Record{
		ID:   fmt.Sprintf("tx-gif-%s-%d", created.Format("20060102150405.000000000"), imageID),
		Type: "TX", ImagePath: path, ImageWidth: width, ImageHeight: height,
		CreatedAt: created, Profile: profileID, Callsign: callsign, ImageID: uint16(imageID),
		PacketsReceived: stats.TotalPackets, PacketsExpected: stats.TotalPackets,
		Quality: fmt.Sprintf("SSDV GIF · %d frames", stats.Frames), Status: "sent",
	})
	if err := storage.SaveJSON(filepath.Join(dir, "album.json"), index); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// durationForSamples converts an exact PCM sample estimate to a wall-clock
// duration without including preparation time or PTT lead/tail delays.
func durationForSamples(samples int64, sampleRate int) time.Duration {
	if samples <= 0 || sampleRate <= 0 {
		return 0
	}
	return time.Duration(float64(samples) / float64(sampleRate) * float64(time.Second))
}

// buildTXMetrics creates the UI payload. Data-frame progress is derived from
// the played PCM fraction; all SSDV transport frames have a fixed 256-byte
// packet size, so this is a monotonic estimate of frames that have reached the
// audio output. GIF control markers are included because they are also full
// 256-byte transport frames; AX.25 bit stuffing may make individual boundaries vary.
func buildTXMetrics(startedAt, now time.Time, estimated time.Duration, totalFrames int, progress float64) map[string]any {
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	if totalFrames < 0 {
		totalFrames = 0
	}
	elapsed := time.Duration(0)
	if !startedAt.IsZero() && now.After(startedAt) {
		elapsed = now.Sub(startedAt)
	}
	remaining := estimated - elapsed
	if remaining < 0 || progress >= 1 {
		remaining = 0
	}
	sentFrames := int(math.Floor(float64(totalFrames) * progress))
	if progress >= 1 {
		sentFrames = totalFrames
	}
	secondsCeil := func(d time.Duration) int {
		if d <= 0 {
			return 0
		}
		return int((d + time.Second - 1) / time.Second)
	}
	return map[string]any{
		"estimatedSeconds": secondsCeil(estimated),
		"elapsedSeconds":   int(elapsed / time.Second),
		"remainingSeconds": secondsCeil(remaining),
		"totalDataFrames":  totalFrames,
		"sentDataFrames":   sentFrames,
		"progress":         progress,
	}
}

func emptyTXMetrics() map[string]any {
	return map[string]any{
		"estimatedSeconds": 0, "elapsedSeconds": 0, "remainingSeconds": 0,
		"totalDataFrames": 0, "sentDataFrames": 0, "progress": 0.0,
	}
}

func (a *App) emitTXResetTelemetry() {
	a.emit("tx:progress", 0.0)
	a.emit("tx:metrics", emptyTXMetrics())
}

func (a *App) emitTXMetrics(startedAt time.Time, estimated time.Duration, totalFrames int, progress float64) {
	if startedAt.IsZero() {
		return
	}
	a.emit("tx:metrics", buildTXMetrics(startedAt, time.Now(), estimated, totalFrames, progress))
}

func (a *App) emitTXProgress(samples []float32, sampleRate int, progress float64) {
	now := time.Now()
	a.txProgressMu.Lock()
	shouldEmitProgress := now.Sub(a.txProgressLastEmit) >= time.Second/15 || progress >= 1
	if shouldEmitProgress {
		a.txProgressLastEmit = now
	}
	a.txProgressMu.Unlock()
	if shouldEmitProgress {
		a.emit("tx:progress", progress)
	}
	if len(samples) < 4096 {
		return
	}
	// Audio playback callbacks can arrive much faster than the display row
	// rate. Limit FFT work and native Qt event traffic to 15 Hz; the waterfall
	// renderer cannot display more rows than this anyway.
	now = time.Now()
	a.txSpectrumMu.Lock()
	if now.Sub(a.txSpectrumLastEmit) < time.Second/15 {
		a.txSpectrumMu.Unlock()
		return
	}
	a.txSpectrumLastEmit = now
	a.txSpectrumMu.Unlock()
	end := int(progress * float64(len(samples)-1))
	const spectrumWindow = 4096
	start := end - spectrumWindow
	if start < 0 {
		start = 0
	}
	if start+spectrumWindow > len(samples) {
		start = len(samples) - spectrumWindow
	}
	bins, err := dsp.Spectrum(samples[start:start+spectrumWindow], sampleRate)
	if err == nil {
		a.emit("tx:spectrum", bins)
	}
}

// emitTXStreamSpectrum consumes the most recent streamed PCM block. Unlike the
// old batch path it never indexes into a full-duration waveform. FFT work and UI
// events remain limited to 15 Hz to match waterfall row cadence.
func (a *App) emitTXStreamSpectrum(samples []float32, sampleRate int) {
	const spectrumWindow = 4096
	if len(samples) < spectrumWindow {
		return
	}
	now := time.Now()
	a.txSpectrumMu.Lock()
	if now.Sub(a.txSpectrumLastEmit) < time.Second/15 {
		a.txSpectrumMu.Unlock()
		return
	}
	a.txSpectrumLastEmit = now
	a.txSpectrumMu.Unlock()
	window := samples[len(samples)-spectrumWindow:]
	bins, err := dsp.Spectrum(window, sampleRate)
	if err == nil {
		a.emit("tx:spectrum", bins)
	}
}

// TransmitPCM accepts mono float32 samples from the Go image/modem pipeline.
// Native clients should normally invoke a backend image-send method rather than
// transferring large PCM arrays through the local IPC channel.
func (a *App) TransmitPCM(samples []float32, waveformSampleRate int) error {
	a.txGate.Lock()
	a.mu.Lock()
	d, ctx, targetRate, runner := a.audioDuplex, a.ctx, a.settings.AudioSampleRate, a.pttTest
	a.mu.Unlock()
	if d == nil {
		a.txGate.Unlock()
		return audio.ErrRuntimeUnavailable
	}
	if a.runtimeChanging || a.txReserved || (runner != nil && runner.State().Active) {
		a.txGate.Unlock()
		return errors.New("TX is unavailable while a PTT test is active")
	}
	a.txReserved = true
	a.txGate.Unlock()
	defer func() { a.txGate.Lock(); a.txReserved = false; a.txGate.Unlock() }()
	if waveformSampleRate <= 0 || targetRate <= 0 {
		return errors.New("PCM sample rates must be positive")
	}
	estimated := int64(math.Round(float64(len(samples)) * float64(targetRate) / float64(waveformSampleRate)))
	if estimated <= 0 {
		return errors.New("PCM waveform is empty")
	}
	producer := func(streamCtx context.Context, emit func([]float32) error) error {
		var resampler *dsp.StreamResampler
		if waveformSampleRate != targetRate {
			var createErr error
			resampler, createErr = dsp.NewStreamResampler(waveformSampleRate, targetRate)
			if createErr != nil {
				return createErr
			}
		}
		const chunkSamples = 4096
		for offset := 0; offset < len(samples); offset += chunkSamples {
			if err := streamCtx.Err(); err != nil {
				return err
			}
			end := min(offset+chunkSamples, len(samples))
			block := samples[offset:end]
			if resampler != nil {
				block = resampler.Feed(block)
			}
			if len(block) > 0 {
				if err := emit(block); err != nil {
					return err
				}
				a.emitTXStreamSpectrum(block, targetRate)
			}
		}
		if resampler != nil {
			if tail := resampler.Flush(); len(tail) > 0 {
				if err := emit(tail); err != nil {
					return err
				}
				a.emitTXStreamSpectrum(tail, targetRate)
			}
		}
		return nil
	}
	return d.TransmitStream(ctx, estimated, producer, func(progress float64) {
		a.emitTXProgress(nil, targetRate, progress)
	})
}

func estimateRXAudioSNR(bins []dsp.SpectrumBin, profileID string, sampleRate int) dsp.BandSNR {
	if sampleRate <= 0 {
		return dsp.BandSNR{}
	}
	nyquist := float64(sampleRate) / 2
	// Keep the occupied modem band tight and estimate the local noise spectrum
	// from guard bands on both sides. A single distant 4.5..9 kHz reference is
	// not representative of the noise level around a 1.2/1.6/2.4 kHz modem
	// signal after receiver and sound-card filtering.
	signalLow, signalHigh := 650.0, 3100.0
	guards := []dsp.FrequencyBand{{LowHz: 300, HighHz: 550}, {LowHz: 3300, HighHz: 4300}}
	switch {
	case profileID == domain.AutoReceiveProfile:
		signalLow, signalHigh = 500, 3800
		guards = []dsp.FrequencyBand{{LowHz: 200, HighHz: 400}, {LowHz: 4000, HighHz: 5200}}
	case strings.Contains(profileID, "1600"):
		// QPSK/8PSK/16QAM use a 1.6 kHz real audio carrier. Exclude the
		// low-frequency rumble and upper audio noise outside the useful sidebands.
		signalLow, signalHigh = 450, 3000
		guards = []dsp.FrequencyBand{{LowHz: 250, HighHz: 400}, {LowHz: 3150, HighHz: 4400}}
	case strings.Contains(profileID, "2400"):
		// CCSDS 2400 GMSK is centred near 1.8 kHz and occupies a wider band.
		signalLow, signalHigh = 450, 3900
		guards = []dsp.FrequencyBand{{LowHz: 200, HighHz: 350}, {LowHz: 4100, HighHz: 5600}}
	}
	// Clamp each interval to Nyquist while preserving a usable guard width.
	clamped := make([]dsp.FrequencyBand, 0, len(guards))
	for _, band := range guards {
		band.HighHz = math.Min(band.HighHz, nyquist-50)
		if band.HighHz-band.LowHz >= 100 {
			clamped = append(clamped, band)
		}
	}
	signalHigh = math.Min(signalHigh, nyquist-100)

	// AFSK 1200 must show both separated mark/space tone groups. M-ary and
	// GMSK instead need a wider occupied cluster; one narrow whistle is not a
	// valid modem signal. Auto mode accepts either recognized spectral shape.
	isAFSK1200 := profileID == string(domain.SSDVAX25) ||
		profileID == string(domain.SSDVCCSDS1200) || profileID == string(domain.PCSICCSDS1200)
	if isAFSK1200 {
		return dsp.EstimateBandSNRWithTonePairEvidence(bins, signalLow, signalHigh, clamped, 300)
	}
	wideband := dsp.EstimateBandSNRWithGuards(bins, signalLow, signalHigh, clamped)
	if wideband.Valid || profileID != domain.AutoReceiveProfile {
		return wideband
	}
	return dsp.EstimateBandSNRWithTonePairEvidence(bins, signalLow, signalHigh, clamped, 300)
}

func rxAudioSNRPassband(profileID string, sampleRate int) (float64, float64) {
	if sampleRate <= 0 {
		return 0, 0
	}
	nyquist := float64(sampleRate) / 2
	low, high := 650.0, 3100.0
	switch {
	case profileID == domain.AutoReceiveProfile:
		low, high = 500, 3800
	case strings.Contains(profileID, "1600"):
		low, high = 450, 3000
	case strings.Contains(profileID, "2400"):
		low, high = 450, 3900
	}
	high = math.Min(high, nyquist-100)
	if high <= low {
		return 0, 0
	}
	return low, high
}

func (a *App) estimateTrackedRXAudioSNR(bins []dsp.SpectrumBin, profileID string, sampleRate int) dsp.BandSNR {
	// AFSK1200 leaves separated mark/space clusters and intervening guard
	// bins. Wideband M-ary/GMSK spectra do not leave a dependable in-band noise
	// reference; return unavailable rather than extrapolate from a stale idle
	// profile and display an implausibly high number.
	if profileID == domain.AutoReceiveProfile || strings.Contains(profileID, "1600") || strings.Contains(profileID, "2400") {
		return dsp.BandSNR{}
	}
	if profileID != string(domain.SSDVAX25) && profileID != string(domain.SSDVCCSDS1200) && profileID != string(domain.PCSICCSDS1200) {
		return dsp.BandSNR{}
	}
	low, high := rxAudioSNRPassband(profileID, sampleRate)
	if high <= low {
		return dsp.BandSNR{}
	}
	// The stateless sideband estimator is used only as a warm-up signal hint.
	// The displayed SNR itself comes from the per-bin idle-noise profile learnt
	// in the same modem passband over preceding noise-only windows.
	signalHint := estimateRXAudioSNR(bins, profileID, sampleRate).Valid
	a.rxSNRMu.Lock()
	defer a.rxSNRMu.Unlock()
	if a.rxSNRProfile != profileID || a.rxSNRSampleRate != sampleRate {
		a.rxSNRTracker.Reset()
		a.rxSNRProfile = profileID
		a.rxSNRSampleRate = sampleRate
	}
	return a.rxSNRTracker.EstimateWithEvidence(bins, low, high, signalHint, a.rxModemLocked.Load(), a.rxPacketValidated.Load())
}

func (a *App) resetRXAudioSNRTracker() {
	if a == nil {
		return
	}
	a.rxSNRMu.Lock()
	a.rxSNRTracker.Reset()
	a.rxModemLocked.Store(false)
	a.rxPacketValidated.Store(false)
	a.rxMarySNRDB.Store(0)
	a.rxMarySNRValid.Store(false)
	a.rxSNRProfile = ""
	a.rxSNRSampleRate = 0
	a.rxSNRMu.Unlock()
}

func (a *App) handleRXAudio(samples []float32) {
	a.rxStats.audioBlocks.Add(1)
	if samples == nil {
		a.rxStats.inputDiscontinuities.Add(1)
		a.resetRXAudioSNRTracker()
		a.rxSpectrumMu.Lock()
		a.rxSpectrumWindow = nil
		a.rxSpectrumLastEmit = time.Time{}
		a.rxSpectrumMu.Unlock()
		a.enqueueRXSamples(nil)
		return
	}
	if len(samples) == 0 {
		return
	}
	a.rxStats.audioSamples.Add(uint64(len(samples)))
	a.mu.Lock()
	rate := a.settings.AudioSampleRate
	profileID := a.settings.RXProfile
	a.mu.Unlock()
	now := time.Now()
	a.rxSpectrumMu.Lock()
	a.rxSpectrumWindow = append(a.rxSpectrumWindow, samples...)
	const spectrumWindow = 4096
	if len(a.rxSpectrumWindow) > spectrumWindow {
		a.rxSpectrumWindow = append([]float32(nil), a.rxSpectrumWindow[len(a.rxSpectrumWindow)-spectrumWindow:]...)
	}
	shouldEmit := len(a.rxSpectrumWindow) >= spectrumWindow && now.Sub(a.rxSpectrumLastEmit) >= time.Second/15
	var window []float32
	if shouldEmit {
		window = append([]float32(nil), a.rxSpectrumWindow[len(a.rxSpectrumWindow)-spectrumWindow:]...)
		a.rxSpectrumLastEmit = now
	}
	a.rxSpectrumMu.Unlock()
	if shouldEmit {
		var snr dsp.BandSNR
		snrReason := ""
		bins, err := dsp.Spectrum(window, rate)
		if err == nil {
			a.emit("rx:spectrum", bins)
			if strings.Contains(profileID, "1600") || profileID == domain.AutoReceiveProfile {
				// M-ary occupies nearly the complete useful audio channel, so a
				// spectrum-only noise reference is not independent. Use EVM from
				// the live synchronized symbol decisions instead, including Auto RX.
				if a.rxMarySNRValid.Load() {
					snr = dsp.BandSNR{SNRDB: math.Float64frombits(a.rxMarySNRDB.Load()), Valid: true}
					snrReason = "mary-evm"
				} else if profileID == domain.AutoReceiveProfile {
					// Auto RX may be testing several unrelated demodulators in parallel.
					// Do not publish a generic spectrum-only number before a candidate has
					// synchronized: it is not comparable across AFSK, GMSK and M-ary.
					snrReason = "auto-awaiting-sync"
				} else {
					snrReason = "mary-awaiting-sync"
				}
			} else {
				snr = a.estimateTrackedRXAudioSNR(bins, profileID, rate)
				if strings.Contains(profileID, "2400") {
					snrReason = "wideband-estimator-unavailable"
				}
			}
		}
		var sum float64
		peak := float32(0)
		for _, v := range window {
			x := float64(v)
			sum += x * x
			if float32(math.Abs(x)) > peak {
				peak = float32(math.Abs(x))
			}
		}
		rms := math.Sqrt(sum / float64(len(window)))
		db := 20 * math.Log10(math.Max(rms, 1e-12))
		a.emit("rx:level", map[string]interface{}{
			"rmsDbfs": db, "peak": float64(peak),
			"snrDb": snr.SNRDB, "snrValid": snr.Valid, "snrReason": snrReason,
			"noiseFloorDbfs": snr.NoiseFloorDBFS, "signalBandDbfs": snr.SignalBandDBFS,
			"snrEvidenceBins": snr.EvidenceBins,
		})
	}
	// Always forward every capture block to the ordered decoder queue. Only
	// display telemetry is throttled; demodulation input is never rate-limited.
	a.enqueueRXSamples(samples)
}

func (a *App) enqueueRXSamples(samples []float32) {
	// Keep the capture callback non-blocking. If the decoder falls behind,
	// preserve all audio already queued and remember a single discontinuity.
	// The old implementation drained the entire queue on overflow, potentially
	// throwing away several seconds of a valid image transmission at once.
	a.rxQueueMu.Lock()
	defer a.rxQueueMu.Unlock()
	a.mu.Lock()
	queue := a.rxQueue
	a.mu.Unlock()
	if queue == nil {
		return
	}
	if samples == nil {
		select {
		case queue <- nil:
			a.rxGapPending = false
		default:
			a.rxGapPending = true
		}
		return
	}
	if a.rxGapPending {
		// Reserve two queue slots atomically from this producer's point of
		// view: one for the discontinuity marker and one for the current block.
		// Sending only the marker would create a reset on every callback while
		// continuously dropping the corresponding audio block.
		if cap(queue)-len(queue) < 2 {
			a.rxStats.queueDrops.Add(1)
			return
		}
		queue <- nil
		a.rxGapPending = false
	}
	block := append([]float32(nil), samples...)
	select {
	case queue <- block:
	default:
		a.rxGapPending = true
		a.rxStats.queueDrops.Add(1)
	}
}

func safeFeedRXDecoder(decoder interface {
	Feed([]float32) ([]transport.ReceivedPacket, error)
}, block []float32) (packets []transport.ReceivedPacket, err error, panicMessage, panicStack string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicMessage = fmt.Sprintf("%v", recovered)
			panicStack = string(runtimedebug.Stack())
			err = fmt.Errorf("RX decoder panic: %v", recovered)
			packets = nil
		}
	}()
	packets, err = decoder.Feed(block)
	return
}

func (a *App) recordRXError(stage string, err error) {
	if a == nil || err == nil {
		return
	}
	a.rxStats.lastErrorMu.Lock()
	a.rxStats.lastError = stage + ": " + err.Error()
	a.rxStats.lastErrorAt = time.Now()
	a.rxStats.lastErrorMu.Unlock()
}

func rxDecoderHasModemLock(decoder interface{}) bool {
	switch value := decoder.(type) {
	case interface {
		Diagnostics() transport.DecoderDiagnostics
	}:
		d := value.Diagnostics()
		return d.MARYLocked || d.FSKLocked || d.GMSKLocked
	case interface {
		Diagnostics() transport.AutoDecoderDiagnostics
	}:
		d := value.Diagnostics()
		if d.FSK1200Locked || d.GMSK2400Locked || d.LockedProfile != "" {
			return true
		}
		for _, candidate := range d.MARYCandidates {
			if candidate.MARYLocked {
				return true
			}
		}
	}
	return false
}

func rxDecoderHasRecentValidPacket(decoder interface{}) bool {
	switch value := decoder.(type) {
	case interface {
		Diagnostics() transport.DecoderDiagnostics
	}:
		return value.Diagnostics().ValidPacketRecent
	case interface {
		Diagnostics() transport.AutoDecoderDiagnostics
	}:
		return value.Diagnostics().ValidPacketRecent
	}
	return false
}

// rxDecoderHasFreshValidPacket is stricter than the UI's general "recent"
// packet indicator. SNR display is only permitted briefly after a packet has
// passed its parser/CRC checks; a packet from several seconds ago must not
// validate a new noise/interference window.
func rxDecoderHasFreshValidPacket(decoder interface{}) bool {
	const maxAgeMS = int64(1200)
	switch value := decoder.(type) {
	case interface {
		Diagnostics() transport.DecoderDiagnostics
	}:
		age := value.Diagnostics().LastPacketAgeMS
		return value.Diagnostics().ValidPacketRecent && age >= 0 && age <= maxAgeMS
	case interface {
		Diagnostics() transport.AutoDecoderDiagnostics
	}:
		age := value.Diagnostics().LastPacketAgeMS
		return value.Diagnostics().ValidPacketRecent && age >= 0 && age <= maxAgeMS
	}
	return false
}

func rxDecoderDiagnostics(decoder interface{}) interface{} {
	switch value := decoder.(type) {
	case interface {
		Diagnostics() transport.DecoderDiagnostics
	}:
		return value.Diagnostics()
	case interface {
		Diagnostics() transport.AutoDecoderDiagnostics
	}:
		return value.Diagnostics()
	default:
		return nil
	}
}

func (a *App) emitRXDiagnostics(decoder interface{}, profile string, rate int, queue <-chan []float32) {
	if a == nil {
		return
	}
	lastError := ""
	lastErrorAt := time.Time{}
	a.rxStats.lastErrorMu.RLock()
	lastError, lastErrorAt = a.rxStats.lastError, a.rxStats.lastErrorAt
	a.rxStats.lastErrorMu.RUnlock()
	payload := map[string]interface{}{
		"profile": profile, "sampleRate": rate,
		"audioBlocks":          a.rxStats.audioBlocks.Load(),
		"audioSamples":         a.rxStats.audioSamples.Load(),
		"inputDiscontinuities": a.rxStats.inputDiscontinuities.Load(),
		"decoderResets":        a.rxStats.decoderResets.Load(),
		"queueDepth":           len(queue), "queueCapacity": cap(queue),
		"queueDrops":         a.rxStats.queueDrops.Load(),
		"decoderBlocks":      a.rxStats.decoderBlocks.Load(),
		"decoderPackets":     a.rxStats.decoderPackets.Load(),
		"decoderErrors":      a.rxStats.decoderErrors.Load(),
		"decoderPanics":      a.rxStats.decoderPanics.Load(),
		"managerSubmitDrops": a.rxStats.managerSubmitDrops.Load(),
		"decoder":            rxDecoderDiagnostics(decoder),
		"lastError":          lastError,
	}
	if !lastErrorAt.IsZero() {
		payload["lastErrorAgeMs"] = time.Since(lastErrorAt).Milliseconds()
	}
	if a.rxManager != nil {
		payload["manager"] = a.rxManager.Diagnostics()
	}
	a.emit("rx:diagnostics", payload)
}

func rxDecoderMARYSNR(decoder interface{}) (float64, bool) {
	switch value := decoder.(type) {
	case interface {
		Diagnostics() transport.DecoderDiagnostics
	}:
		d := value.Diagnostics()
		if !d.MARYLocked || !d.MARYSNRValid {
			return 0, false
		}
		return d.MARYSNRDB, true
	case interface {
		Diagnostics() transport.AutoDecoderDiagnostics
	}:
		d := value.Diagnostics()
		for _, candidate := range d.MARYCandidates {
			if candidate.MARYLocked && candidate.MARYSNRValid {
				return candidate.MARYSNRDB, true
			}
		}
	}
	return 0, false
}

func (a *App) runRXDecodeWorker(ctx context.Context, queue <-chan []float32) {
	var decoder interface {
		Feed([]float32) ([]transport.ReceivedPacket, error)
	}
	activeProfile := ""
	activeRate := 0
	diagnosticsTicker := time.NewTicker(time.Second)
	defer diagnosticsTicker.Stop()
	defer func() { a.emitRXDiagnostics(decoder, activeProfile, activeRate, queue) }()
	for {
		select {
		case <-ctx.Done():
			return
		case <-diagnosticsTicker.C:
			a.emitRXDiagnostics(decoder, activeProfile, activeRate, queue)
		case block, ok := <-queue:
			if !ok {
				return
			}
			a.rxStats.decoderBlocks.Add(1)
			if block == nil {
				a.rxStats.decoderResets.Add(1)
				a.rxModemLocked.Store(false)
				a.rxPacketValidated.Store(false)
				a.rxMarySNRValid.Store(false)
			}
			a.mu.Lock()
			profileID, rate := a.settings.RXProfile, a.settings.AudioSampleRate
			a.mu.Unlock()
			if profileID != activeProfile || rate != activeRate {
				a.rxModemLocked.Store(false)
				a.rxPacketValidated.Store(false)
				a.rxMarySNRValid.Store(false)
				activeProfile, activeRate = profileID, rate
				var err error
				if profileID == domain.AutoReceiveProfile {
					decoder, err = transport.NewAutoStreamDecoder(rate)
				} else {
					decoder, err = transport.NewStreamDecoder(domain.TransportMode(profileID), rate)
				}
				if err != nil {
					a.setAudioError(fmt.Errorf("initialize %s streaming decoder: %w", profileID, err))
					a.recordRXError("decoder-init", err)
					a.rxStats.decoderErrors.Add(1)
					decoder = nil
					a.emitRXDiagnostics(decoder, activeProfile, activeRate, queue)
					continue
				}
			}
			if decoder == nil {
				continue
			}
			packets, err, panicMessage, panicStack := safeFeedRXDecoder(decoder, block)
			if err == nil {
				a.rxModemLocked.Store(rxDecoderHasModemLock(decoder))
				a.rxPacketValidated.Store(rxDecoderHasFreshValidPacket(decoder))
				snrDB, snrValid := rxDecoderMARYSNR(decoder)
				a.rxMarySNRDB.Store(math.Float64bits(snrDB))
				a.rxMarySNRValid.Store(snrValid)
			} else {
				a.rxModemLocked.Store(false)
				a.rxPacketValidated.Store(false)
				a.rxMarySNRValid.Store(false)
			}
			if err != nil {
				a.rxStats.decoderErrors.Add(1)
				a.recordRXError("decoder-feed", err)
				if panicMessage != "" {
					a.rxStats.decoderPanics.Add(1)
					// The decoder may have partially mutated state before the panic.
					// Reset it so the worker remains alive and can acquire a later burst.
					if resetter, ok := decoder.(interface{ Reset() }); ok {
						resetter.Reset()
					}
					a.emit("rx:decoder-error", map[string]interface{}{"stage": "feed", "error": err.Error(), "panic": true, "stack": panicStack})
				}
			} else if len(packets) > 0 {
				a.rxStats.lastErrorMu.Lock()
				a.rxStats.lastError = ""
				a.rxStats.lastErrorAt = time.Time{}
				a.rxStats.lastErrorMu.Unlock()
			}
			a.rxStats.decoderPackets.Add(uint64(len(packets)))
			for _, packet := range packets {
				if a.rxManager != nil && !a.rxManager.Submit(packet) {
					a.rxStats.managerSubmitDrops.Add(1)
				}
			}
			if err != nil || len(packets) > 0 {
				a.emitRXDiagnostics(decoder, activeProfile, activeRate, queue)
			}
		}
	}
}

func (a *App) handleRXImageUpdate(update rxengine.Update) {
	// GIF frames produce progressive PNG previews for the live RX pane, but
	// the album stores one reconstructed animated GIF per completed stream.
	var previewPNG []byte
	if update.PNGBase64 != "" {
		data, err := decodeRXPreviewPNG(update.PNGBase64)
		if err != nil {
			if update.Error == "" {
				update.Error = err.Error()
			}
		} else {
			previewPNG = data
			update.HasPreview = true
			a.setLatestRXPreview(data)
		}
	}

	if update.Codec == "SSDV-GIF" {
		// Persist a final GIF immediately, or checkpoint a partial GIF after an
		// RX idle interval. Avoid re-encoding/writing the entire growing animation
		// after every frame, which would create O(n^2) CPU and disk traffic.
		if update.GIFEnd || update.GIFPartialArchive {
			if err := a.archiveRXGIF(update); err != nil {
				update.GIFSaveError = err.Error()
			} else {
				update.GIFSaved = true
				a.rxManager.MarkGIFSnapshotArchived(update.SessionID)
				if update.GIFEnd && !update.GIFStreamIncomplete {
					// Keep incomplete frame packet buffers briefly after stream end so
					// late packet-level FEC recovery can repair the latest album snapshot.
					a.rxManager.ReleaseGIFExport(update.SessionID)
				}
			}
		}
	} else if update.PNGBase64 != "" && (update.ArchivePreview || update.Complete) && previewPNG != nil {
		if err := a.archiveRXImageData(update, previewPNG); err != nil {
			update.Error = err.Error()
		}
	}

	if update.Codec == "SSDV" && update.PacketDataBase64 != "" {
		if path, err := a.archiveRXSSDVPackets(update); err != nil {
			if update.Error == "" {
				update.Error = err.Error()
			}
		} else {
			update.PacketPath = path
		}
	}
	// Binary previews are fetched through GetLatestRXImageData; do not retain
	// large base64 PNGs in the bounded event history.
	update.PNGBase64 = ""
	update.PacketDataBase64 = ""
	a.emit("rx:image", update)
}

// archiveRXGIF exports the current decodable snapshot of a GIF stream. The
// same album record is updated at RX idle checkpoints and once more when the
// stream terminates, so interrupted transfers remain available as partial GIFs
// without re-encoding the entire animation after every received frame.
func (a *App) archiveRXGIF(update rxengine.Update) error {
	if a.rxManager == nil || strings.TrimSpace(update.SessionID) == "" {
		return errors.New("received GIF session is unavailable")
	}
	data, callsign, frameCount, complete, err := a.rxManager.ExportGIFSnapshot(update.SessionID)
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 16*1024*1024 {
		return errors.New("reconstructed GIF is empty or exceeds the 16 MiB album limit")
	}
	config, err := gif.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("validate reconstructed GIF: %w", err)
	}
	dir, err := a.requireDataDir()
	if err != nil {
		return err
	}
	a.albumMu.Lock()
	defer a.albumMu.Unlock()
	imageDir := filepath.Join(dir, "album", "images")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		return err
	}
	created := time.Now().UTC()
	name := fmt.Sprintf("RX_%s_%s_%03d.gif", created.Format("20060102T150405.000000000Z"), safeAlbumName(callsign), update.ImageID)
	path := filepath.Join(imageDir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	indexPath := filepath.Join(dir, "album.json")
	var index album.Index
	if err := storage.LoadJSON(indexPath, &index); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(path)
		return err
	}

	// Prefer updating an existing record for this RX stream. This keeps the
	// album to one GIF item even if an older build already archived previews.
	updated := false
	var oldPaths []string
	for i := range index.Records {
		record := &index.Records[i]
		if record.Type != "RX" || record.SessionID != update.SessionID || record.Profile != update.Profile || record.Callsign != callsign {
			continue
		}
		if !updated {
			if record.ImagePath != "" && record.ImagePath != path {
				oldPaths = append(oldPaths, record.ImagePath)
			}
			if record.PacketPath != "" {
				oldPaths = append(oldPaths, record.PacketPath)
			}
			record.ImagePath = path
			record.PacketPath = ""
			record.ImageWidth, record.ImageHeight = config.Width, config.Height
			record.ImageID = uint16(update.ImageID)
			record.CreatedAt = created
			record.PacketsReceived, record.PacketsExpected = update.PacketsReceived, update.PacketsExpected
			record.Quality = fmt.Sprintf("SSDV GIF · %d frames", frameCount)
			record.Status = "partial"
			if complete {
				record.Status = "complete"
			}
			updated = true
		} else {
			if record.ImagePath != "" {
				oldPaths = append(oldPaths, record.ImagePath)
			}
			if record.PacketPath != "" {
				oldPaths = append(oldPaths, record.PacketPath)
			}
			record.ID = "" // removed from the compacted slice below
		}
	}
	if len(index.Records) > 0 {
		compacted := index.Records[:0]
		for _, record := range index.Records {
			if record.ID != "" {
				compacted = append(compacted, record)
			}
		}
		index.Records = compacted
	}
	if !updated {
		index.Records = append(index.Records, album.Record{
			ID:   fmt.Sprintf("rx-gif-%s-%s", created.Format("20060102150405.000000000"), safeAlbumName(update.Profile)),
			Type: "RX", ImagePath: path, ImageWidth: config.Width, ImageHeight: config.Height,
			CreatedAt: created, Profile: update.Profile, Callsign: callsign, ImageID: uint16(update.ImageID),
			SessionID: update.SessionID, PacketsReceived: update.PacketsReceived, PacketsExpected: update.PacketsExpected,
			Quality: fmt.Sprintf("SSDV GIF · %d frames", frameCount), Status: "partial",
		})
		if complete {
			index.Records[len(index.Records)-1].Status = "complete"
		}
	}
	if err := storage.SaveJSON(indexPath, index); err != nil {
		_ = os.Remove(path)
		return err
	}
	// Remove only superseded media that remains inside the application album.
	root, rootErr := filepath.Abs(filepath.Join(dir, "album"))
	if rootErr == nil {
		for _, old := range oldPaths {
			if old == "" || old == path {
				continue
			}
			target, absErr := filepath.Abs(old)
			if absErr != nil {
				continue
			}
			rel, relErr := filepath.Rel(root, target)
			if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				_ = os.Remove(target)
			}
		}
	}
	// The completed animated file becomes the live RX preview. Progressive PNG
	// frame snapshots are useful while receiving, but leaving the last PNG in
	// this slot would make the main RX pane appear frozen after GIF completion.
	a.setLatestRXGIF(data)
	return nil
}

func decodeRXPreviewPNG(encoded string) ([]byte, error) {
	const maximumPNGBytes = 16 * 1024 * 1024
	if len(encoded) == 0 || len(encoded) > (maximumPNGBytes*4/3)+8 {
		return nil, errors.New("reconstructed PNG is empty or exceeds the 16 MiB preview limit")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode reconstructed PNG: %w", err)
	}
	if len(data) == 0 || len(data) > maximumPNGBytes {
		return nil, errors.New("reconstructed PNG is empty or exceeds the 16 MiB preview limit")
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("validate reconstructed PNG: %w", err)
	}
	return data, nil
}

func (a *App) setLatestRXPreview(data []byte) {
	if a == nil || len(data) == 0 || len(data) > 16*1024*1024 {
		return
	}
	copyOfData := append([]byte(nil), data...)
	a.rxPreviewMu.Lock()
	a.rxPreviewPNG = copyOfData
	// A newer progressive still-image preview supersedes the previous GIF.
	a.rxPreviewGIF = nil
	a.rxPreviewMu.Unlock()
}

func (a *App) setLatestRXGIF(data []byte) {
	if a == nil || len(data) == 0 || len(data) > 16*1024*1024 {
		return
	}
	if _, err := gif.DecodeConfig(bytes.NewReader(data)); err != nil {
		return
	}
	copyOfData := append([]byte(nil), data...)
	a.rxPreviewMu.Lock()
	a.rxPreviewGIF = copyOfData
	// A completed animation supersedes the last progressive PNG snapshot.
	a.rxPreviewPNG = nil
	a.rxPreviewMu.Unlock()
}

func (a *App) archiveRXImage(update rxengine.Update) error {
	data, err := decodeRXPreviewPNG(update.PNGBase64)
	if err != nil {
		return err
	}
	return a.archiveRXImageData(update, data)
}

func (a *App) archiveRXImageData(update rxengine.Update, data []byte) error {
	if len(data) == 0 || len(data) > 16*1024*1024 {
		return errors.New("reconstructed PNG is empty or exceeds the 16 MiB album limit")
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("validate reconstructed PNG: %w", err)
	}
	dir, err := a.requireDataDir()
	if err != nil {
		return err
	}
	a.albumMu.Lock()
	defer a.albumMu.Unlock()
	imageDir := filepath.Join(dir, "album", "images")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		return err
	}
	created := time.Now().UTC()
	name := fmt.Sprintf("RX_%s_%s_%03d.png", created.Format("20060102T150405.000000000Z"), safeAlbumName(update.Callsign), update.ImageID)
	path := filepath.Join(imageDir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	indexPath := filepath.Join(dir, "album.json")
	var index album.Index
	if err := storage.LoadJSON(indexPath, &index); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(path)
		return err
	}
	status := "receiving"
	if update.Complete {
		status = "complete"
	}
	var previousPath string
	updated := false
	for i := len(index.Records) - 1; i >= 0; i-- {
		record := &index.Records[i]
		if record.Type == "RX" && record.Profile == update.Profile && record.Callsign == update.Callsign &&
			record.ImageID == uint16(update.ImageID) && record.SessionID == update.SessionID {
			previousPath = record.ImagePath
			record.ImagePath = path
			record.ImageWidth, record.ImageHeight = update.ImageWidth, update.ImageHeight
			record.PacketsReceived, record.PacketsExpected = update.PacketsReceived, update.PacketsExpected
			record.Status = status
			if update.Complete {
				record.CreatedAt = created
			}
			if update.Codec == "SSDV" {
				record.Quality = fmt.Sprintf("JPEG Q%d", update.Quality)
			} else {
				record.Quality = "PCSI preview"
			}
			updated = true
			break
		}
	}
	if !updated {
		quality := "PCSI preview"
		if update.Codec == "SSDV" {
			quality = fmt.Sprintf("JPEG Q%d", update.Quality)
		}
		index.Records = append(index.Records, album.Record{
			ID:   fmt.Sprintf("rx-%s-%d-%s", created.Format("20060102150405.000000000"), update.ImageID, safeAlbumName(update.Profile)),
			Type: "RX", ImagePath: path, ImageWidth: update.ImageWidth, ImageHeight: update.ImageHeight,
			CreatedAt: created, Profile: update.Profile, Callsign: update.Callsign, ImageID: uint16(update.ImageID), SessionID: update.SessionID,
			PacketsReceived: update.PacketsReceived, PacketsExpected: update.PacketsExpected, Quality: quality, Status: status,
		})
	}
	duplicatePaths := collapseRXSessionRecords(&index, update)
	if err := storage.SaveJSON(indexPath, index); err != nil {
		_ = os.Remove(path)
		return err
	}
	removeUnreferencedAlbumFiles(index, dir, duplicatePaths)
	if previousPath != "" && previousPath != path {
		root, rootErr := filepath.Abs(filepath.Join(dir, "album", "images"))
		target, targetErr := filepath.Abs(previousPath)
		if rootErr == nil && targetErr == nil {
			rel, relErr := filepath.Rel(root, target)
			if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				_ = os.Remove(target)
			}
		}
	}
	return nil
}

func safeAlbumName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "UNKNOWN"
	}
	var out strings.Builder
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			out.WriteRune(r)
		} else {
			out.WriteByte('_')
		}
	}
	return out.String()
}

func (a *App) archiveRXSSDVPackets(update rxengine.Update) (string, error) {
	data, err := base64.StdEncoding.DecodeString(update.PacketDataBase64)
	if err != nil {
		return "", fmt.Errorf("decode SSDV packet archive: %w", err)
	}
	if len(data) == 0 || len(data)%256 != 0 {
		return "", errors.New("SSDV packet archive has an invalid length")
	}
	dir, err := a.requireDataDir()
	if err != nil {
		return "", err
	}
	a.albumMu.Lock()
	defer a.albumMu.Unlock()
	imageDir := filepath.Join(dir, "album", "packets")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		return "", err
	}
	created := time.Now().UTC()
	name := fmt.Sprintf("RX_%s_%s_%03d.ssdv", created.Format("20060102T150405.000000000Z"), safeAlbumName(update.Callsign), update.ImageID)
	path := filepath.Join(imageDir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	var index album.Index
	if err := storage.LoadJSON(filepath.Join(dir, "album.json"), &index); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(path)
		return "", err
	}
	// Attach/update the raw packet archive on the same RX record as its image
	// preview. Partial archives retain packet IDs in each 256-byte packet, so
	// gaps remain detectable and can be recovered from a later capture.
	attached := false
	var previousPath string
	for i := len(index.Records) - 1; i >= 0; i-- {
		record := &index.Records[i]
		if record.Type != "RX" || record.Profile != update.Profile || record.Callsign != update.Callsign ||
			record.ImageID != uint16(update.ImageID) || record.SessionID != update.SessionID {
			continue
		}
		previousPath = record.PacketPath
		record.PacketPath = path
		record.ImageWidth, record.ImageHeight = update.ImageWidth, update.ImageHeight
		record.PacketsReceived, record.PacketsExpected = update.PacketsReceived, update.PacketsExpected
		record.Quality = fmt.Sprintf("JPEG Q%d", update.Quality)
		if update.Complete {
			record.Status = "complete"
			record.CreatedAt = created
		} else if record.Status != "complete" {
			record.Status = "receiving"
		}
		attached = true
		break
	}
	if !attached {
		status := "receiving"
		if update.Complete {
			status = "packet-complete-image-unavailable"
		}
		index.Records = append(index.Records, album.Record{
			ID:   fmt.Sprintf("rx-ssdv-%s-%d-%s", created.Format("20060102150405.000000000"), update.ImageID, safeAlbumName(update.Profile)),
			Type: "RX", PacketPath: path, ImageWidth: update.ImageWidth, ImageHeight: update.ImageHeight,
			CreatedAt: created, Profile: update.Profile, Callsign: update.Callsign, ImageID: uint16(update.ImageID), SessionID: update.SessionID,
			PacketsReceived: update.PacketsReceived, PacketsExpected: update.PacketsExpected,
			Quality: fmt.Sprintf("JPEG Q%d", update.Quality), Status: status,
		})
	}
	duplicatePaths := collapseRXSessionRecords(&index, update)
	if err := storage.SaveJSON(filepath.Join(dir, "album.json"), index); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	removeUnreferencedAlbumFiles(index, dir, duplicatePaths)
	if previousPath != "" && previousPath != path {
		root, _ := filepath.Abs(filepath.Join(dir, "album", "packets"))
		previous, _ := filepath.Abs(previousPath)
		rel, relErr := filepath.Rel(root, previous)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			_ = os.Remove(previous)
		}
	}
	return path, nil
}

func (a *App) GetSerialPorts() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.ports...)
}
func (a *App) RefreshSerialPorts() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ports, err := serialptt.ListPorts(ctx)
	if err != nil {
		a.setPTTError(err)
		return nil, err
	}
	a.mu.Lock()
	a.ports = append([]string(nil), ports...)
	a.mu.Unlock()
	return ports, nil
}
func (a *App) StartPTTTest() error {
	a.txGate.Lock()
	defer a.txGate.Unlock()
	a.mu.Lock()
	mode := a.settings.PTT.Mode
	runner := a.pttTest
	ctx := a.ctx
	duplex := a.audioDuplex
	a.mu.Unlock()
	if a.runtimeChanging || a.txReserved || (duplex != nil && duplex.State().TXActive) {
		return errors.New("cannot test PTT while TX is transmitting")
	}
	if mode == ptt.ModeVOX {
		return errors.New("VOX mode does not use software PTT")
	}
	if runner == nil {
		return errors.New("PTT controller is not initialized")
	}
	if err := runner.Start(ctx); err != nil {
		a.setPTTError(err)
		return err
	}
	return nil
}
func (a *App) StopPTTTest() error {
	a.mu.Lock()
	runner := a.pttTest
	a.mu.Unlock()
	if runner == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := runner.Stop(ctx); err != nil {
		a.setPTTError(err)
		return err
	}
	return nil
}
func (a *App) GetPTTTestState() ptt.TestState {
	a.mu.Lock()
	runner := a.pttTest
	a.mu.Unlock()
	if runner == nil {
		return ptt.TestState{}
	}
	return runner.State()
}
func (a *App) GetPTTError() string       { a.mu.Lock(); defer a.mu.Unlock(); return a.pttError }
func (a *App) GetLocales() []i18n.Locale { return i18n.Supported() }
func (a *App) GetLocaleDictionary(locale string) map[string]string {
	return i18n.Dictionary(i18n.Normalize(locale))
}

// migrateLegacyTemplateBackground moves the old per-template background field
// into the shared application setting and removes it from every template.
func (a *App) migrateLegacyTemplateBackground() error {
	dir, err := a.requireDataDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "templates.json")
	var items []templatepkg.Template
	if err := storage.LoadJSON(path, &items); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	settings := a.GetSettings()
	settingsChanged := false
	templatesChanged := false
	for i := range items {
		if settings.TemplateBackgroundAssetID == "" && strings.TrimSpace(items[i].BackgroundAssetID) != "" {
			settings.TemplateBackgroundAssetID = strings.TrimSpace(items[i].BackgroundAssetID)
			settingsChanged = true
		}
		if items[i].BackgroundAssetID != "" {
			items[i].BackgroundAssetID = ""
			templatesChanged = true
		}
	}
	if settingsChanged {
		if err := a.SaveSettings(settings); err != nil {
			return err
		}
	}
	if templatesChanged {
		return storage.SaveJSON(path, items)
	}
	return nil
}

func (a *App) GetTemplates() ([]templatepkg.Template, error) {
	dir, err := a.requireDataDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "templates.json")
	var items []templatepkg.Template
	if err := storage.LoadJSON(path, &items); err == nil {
		return items, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	items = templatepkg.DefaultTemplates()
	if err := storage.SaveJSON(path, items); err != nil {
		return nil, err
	}
	return items, nil
}
func (a *App) SaveTemplates(items []templatepkg.Template) error {
	// Backgrounds are shared TX-workbench settings, never part of an individual preset.
	for i := range items {
		items[i].BackgroundAssetID = ""
	}
	if err := templatepkg.Validate(items); err != nil {
		return err
	}
	dir, err := a.requireDataDir()
	if err != nil {
		return err
	}
	return storage.SaveJSON(filepath.Join(dir, "templates.json"), items)
}
func (a *App) GetAlbum() ([]album.Record, error) {
	a.albumMu.Lock()
	defer a.albumMu.Unlock()
	dir, err := a.requireDataDir()
	if err != nil {
		return nil, err
	}
	var idx album.Index
	err = storage.LoadJSON(filepath.Join(dir, "album.json"), &idx)
	if errors.Is(err, os.ErrNotExist) {
		return []album.Record{}, nil
	}
	album.SortNewestFirst(idx.Records)
	return idx.Records, err
}
func (a *App) GetGallery() ([]gallery.Asset, error) {
	dir, err := a.requireDataDir()
	if err != nil {
		return nil, err
	}
	var idx gallery.Index
	err = storage.LoadJSON(filepath.Join(dir, "gallery", "index.json"), &idx)
	if errors.Is(err, os.ErrNotExist) {
		return []gallery.Asset{}, nil
	}
	return idx.Assets, err
}
func (a *App) ChooseGalleryImage() (gallery.Asset, error) {
	return gallery.Asset{}, errors.New("native UI must select a file and call ImportGalleryImage(path)")
}

func (a *App) ImportGalleryImage(path string) (gallery.Asset, error) {
	file, err := os.Open(path)
	if err != nil {
		return gallery.Asset{}, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return gallery.Asset{}, err
	}
	if !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > 32*1024*1024 {
		return gallery.Asset{}, errors.New("image file must be a regular file between 1 byte and 32 MiB")
	}
	config, format, err := image.DecodeConfig(file)
	if err != nil {
		return gallery.Asset{}, fmt.Errorf("unsupported or invalid image: %w", err)
	}
	if format != "png" && format != "jpeg" && format != "gif" {
		return gallery.Asset{}, fmt.Errorf("unsupported image format %q; use PNG, JPEG or GIF", format)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 32*1024*1024 {
		return gallery.Asset{}, errors.New("image dimensions exceed the 8192 px / 32 megapixel import limit")
	}
	dir, err := a.requireDataDir()
	if err != nil {
		return gallery.Asset{}, err
	}
	store := gallery.Store{Root: filepath.Join(dir, "gallery", "files")}
	asset, err := store.Import(path)
	if err != nil {
		return gallery.Asset{}, err
	}
	asset.Width, asset.Height = config.Width, config.Height
	items, _ := a.GetGallery()
	items = append(items, asset)
	if err := storage.SaveJSON(filepath.Join(dir, "gallery", "index.json"), gallery.Index{Assets: items}); err != nil {
		_ = store.Delete(asset)
		return gallery.Asset{}, err
	}
	return asset, nil
}

// GetGalleryImageData returns a local image as a data URL for the template editor.
func (a *App) GetGalleryImageData(id string) (string, error) {
	items, err := a.GetGallery()
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		stat, err := os.Stat(item.Path)
		if err != nil {
			return "", err
		}
		if !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > 16*1024*1024 {
			return "", errors.New("gallery image must be a regular file no larger than 16 MiB")
		}
		data, err := os.ReadFile(item.Path)
		if err != nil {
			return "", err
		}
		mime := http.DetectContentType(data)
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	return "", errors.New("gallery image not found")
}

// GetLatestRXGIFData returns only the completed animated RX preview.
// Keeping this separate from the generic latest-image RPC prevents a queued
// still-image refresh from winning a race with the final GIF event.
func (a *App) GetLatestRXGIFData() (string, error) {
	a.rxPreviewMu.RLock()
	animated := append([]byte(nil), a.rxPreviewGIF...)
	a.rxPreviewMu.RUnlock()
	if len(animated) == 0 {
		return "", nil
	}
	if _, err := gif.DecodeConfig(bytes.NewReader(animated)); err != nil {
		return "", fmt.Errorf("validate latest RX GIF preview: %w", err)
	}
	return "data:image/gif;base64," + base64.StdEncoding.EncodeToString(animated), nil
}

// GetLatestRXImageData returns the newest archived RX image for template layers.
func (a *App) GetLatestRXImageData() (string, error) {
	a.rxPreviewMu.RLock()
	animated := append([]byte(nil), a.rxPreviewGIF...)
	preview := append([]byte(nil), a.rxPreviewPNG...)
	a.rxPreviewMu.RUnlock()
	if len(animated) > 0 {
		return "data:image/gif;base64," + base64.StdEncoding.EncodeToString(animated), nil
	}
	if len(preview) > 0 {
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(preview), nil
	}
	items, err := a.GetAlbum()
	if err != nil {
		return "", err
	}
	// Resolve the most recent RX image at any supported output resolution.
	// Do not filter by Quality: Last RX must support low, medium and high.
	for _, item := range album.NewestRXImages(items) {
		stat, err := os.Stat(item.ImagePath)
		if err != nil || !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > 16*1024*1024 {
			continue
		}
		data, err := os.ReadFile(item.ImagePath)
		if err != nil {
			continue
		}
		return "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	return "", nil
}

func (a *App) DeleteGalleryImage(id string) error {
	dir, err := a.requireDataDir()
	if err != nil {
		return err
	}
	items, err := a.GetGallery()
	if err != nil {
		return err
	}
	store := gallery.Store{Root: filepath.Join(dir, "gallery", "files")}
	out := make([]gallery.Asset, 0, len(items))
	var deleted *gallery.Asset
	for _, item := range items {
		if item.ID == id {
			copy := item
			deleted = &copy
			continue
		}
		out = append(out, item)
	}
	if deleted == nil {
		return errors.New("gallery asset not found")
	}
	// Remove stale references from templates before removing the underlying
	// asset so the editor never opens with broken background/image layers.
	templates, err := a.GetTemplates()
	if err != nil {
		return err
	}
	for i := range templates {
		if templates[i].BackgroundAssetID == id {
			templates[i].BackgroundAssetID = ""
		}
		kept := templates[i].Components[:0]
		for _, component := range templates[i].Components {
			if component.Type == "image" && component.AssetID == id {
				continue
			}
			kept = append(kept, component)
		}
		templates[i].Components = kept
	}
	// Clear the shared background reference as well as component references.
	settings := a.GetSettings()
	if settings.TemplateBackgroundAssetID == id {
		settings.TemplateBackgroundAssetID = ""
		if err := a.SaveSettings(settings); err != nil {
			return err
		}
	}
	// Commit indexes first. If either write fails, keep the image file intact.
	if err := a.SaveTemplates(templates); err != nil {
		return err
	}
	if err := storage.SaveJSON(filepath.Join(dir, "gallery", "index.json"), gallery.Index{Assets: out}); err != nil {
		return err
	}
	return store.Delete(*deleted)
}

// GetAlbumImageData returns an archived RX/TX image as a data URL. Only files
// inside this application's album directory are exposed to the native UI.
func (a *App) GetAlbumImageData(id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", errors.New("album record ID is empty")
	}
	dir, err := a.requireDataDir()
	if err != nil {
		return "", err
	}
	items, err := a.GetAlbum()
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(filepath.Join(dir, "album"))
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		if item.ImagePath == "" {
			return "", errors.New("album record has no reconstructed image")
		}
		target, err := filepath.Abs(item.ImagePath)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return "", errors.New("album image path escapes the album directory")
		}
		info, err := os.Stat(target)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 16*1024*1024 {
			return "", errors.New("album image is invalid or exceeds 16 MiB")
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return "", err
		}
		return "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	return "", errors.New("album record not found")
}

func (a *App) DeleteAlbumRecord(id string) error {
	dir, err := a.requireDataDir()
	if err != nil {
		return err
	}
	a.albumMu.Lock()
	defer a.albumMu.Unlock()
	path := filepath.Join(dir, "album.json")
	var idx album.Index
	if err := storage.LoadJSON(path, &idx); err != nil {
		return err
	}
	out := make([]album.Record, 0, len(idx.Records))
	var removed *album.Record
	for _, record := range idx.Records {
		if record.ID == id {
			copy := record
			removed = &copy
			continue
		}
		out = append(out, record)
	}
	if removed == nil {
		return errors.New("album record not found")
	}
	if err := storage.SaveJSON(path, album.Index{Records: out}); err != nil {
		return err
	}
	root, err := filepath.Abs(filepath.Join(dir, "album"))
	if err != nil {
		return err
	}
	for _, candidate := range []string{removed.ImagePath, removed.PacketPath} {
		if candidate == "" {
			continue
		}
		target, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			continue
		}
		_ = os.Remove(target)
	}
	return nil
}
func (a *App) requireDataDir() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dataDir == "" {
		return "", errors.New("application data directory is not initialized")
	}
	return a.dataDir, nil
}
