//go:build wails

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/album"
	"github.com/BH2VSQ/SATSSDV-Go/internal/audio"
	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
	"github.com/BH2VSQ/SATSSDV-Go/internal/gallery"
	"github.com/BH2VSQ/SATSSDV-Go/internal/i18n"
	"github.com/BH2VSQ/SATSSDV-Go/internal/radio/ptt"
	"github.com/BH2VSQ/SATSSDV-Go/internal/radio/serialptt"
	rxengine "github.com/BH2VSQ/SATSSDV-Go/internal/rx"
	"github.com/BH2VSQ/SATSSDV-Go/internal/storage"
	templatepkg "github.com/BH2VSQ/SATSSDV-Go/internal/template"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
	"github.com/BH2VSQ/SATSSDV-Go/internal/tximage"
	"github.com/BH2VSQ/SATSSDV-Go/internal/txqueue"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	mu               sync.Mutex
	txGate           sync.Mutex
	txReserved       bool
	runtimeChanging  bool
	ctx              context.Context
	dataDir          string
	settings         Settings
	ports            []string
	keyer            ptt.Controller
	pttTest          *ptt.TestController
	pttError         string
	audioRuntime     audio.Runtime
	audioDuplex      *audio.Duplex
	audioError       string
	rxSpectrumMu     sync.Mutex
	rxSpectrumWindow []float32
	rxQueue          chan []float32
	rxQueueMu        sync.Mutex
	rxGapPending     bool
	rxCancel         context.CancelFunc
	rxDone           chan struct{}
	rxManager        *rxengine.Manager
	albumMu          sync.Mutex
	txCancel         context.CancelFunc
	txDone           chan struct{}
	txImageID        uint8
}

func NewApp() *App {
	a := &App{settings: DefaultSettings()}
	a.rxManager = rxengine.NewManager(a.handleRXImageUpdate)
	return a
}
func (a *App) startup(ctx context.Context) {
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
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, "ptt:error", err.Error())
	}
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
		a.mu.Lock()
		ctx := a.ctx
		a.mu.Unlock()
		if ctx != nil {
			runtime.EventsEmit(ctx, "ptt:test-state", s)
		}
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
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, "audio:error", err.Error())
	}
}

func (a *App) GetAudioState() AudioState {
	a.mu.Lock()
	d := a.audioDuplex
	rate := a.settings.AudioSampleRate
	audioErr := a.audioError
	ready := a.audioRuntime != nil
	a.mu.Unlock()
	state := AudioState{Connected: ready, SampleRate: rate, Error: audioErr}
	if d != nil {
		s := d.State()
		state.RXActive, state.TXActive = s.RXActive, s.TXActive
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
	a.mu.Lock()
	d, ctx := a.audioDuplex, a.ctx
	a.mu.Unlock()
	if d == nil {
		return audio.ErrRuntimeUnavailable
	}
	if err := d.StartRX(ctx, a.handleRXAudio); err != nil {
		a.setAudioError(err)
		return err
	}
	return nil
}
func (a *App) StopRX() error {
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
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	done := make(chan struct{})
	a.txReserved = true
	a.mu.Lock()
	a.txCancel, a.txDone = cancel, done
	appCtx := a.ctx
	a.mu.Unlock()
	a.txGate.Unlock()
	if appCtx != nil {
		runtime.EventsEmit(appCtx, "tx:tone-state", map[string]any{"active": true})
	}
	if appCtx != nil {
		runtime.EventsEmit(appCtx, "tx:state", map[string]any{"active": true})
	}
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
			currentCtx := a.ctx
			a.mu.Unlock()
			a.txGate.Unlock()
			if currentCtx != nil {
				runtime.EventsEmit(currentCtx, "tx:tone-state", map[string]any{"active": false})
				runtime.EventsEmit(currentCtx, "tx:state", map[string]any{"active": false})
			}
		}()
		const duration = 60.0
		samples := make([]float32, int(float64(rate)*duration))
		const frequency = 1500.0
		ramp := int(float64(rate) * 0.01)
		for i := range samples {
			gain := 0.30
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
			if currentCtx := a.getContext(); currentCtx != nil {
				runtime.EventsEmit(currentCtx, "tx:error", err.Error())
			}
		}
	}()
	return nil
}

type RenderedTXImage struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	PNGBase64 string `json:"pngBase64"`
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
	profile, quality, ax25SSID := a.settings.TXProfile, a.settings.Quality, a.settings.AX25SSID
	a.mu.Unlock()
	ax25To = strings.ToUpper(strings.TrimSpace(ax25To))
	if ax25To == "" {
		ax25To = "CQ"
	}
	if _, err := domain.ParseProfile(profile); err != nil {
		return err
	}
	job := txqueue.Job{ID: image.ID, ImageName: image.Name, ImageDataBase64: image.PNGBase64, Profile: profile, Quality: quality, AX25SSID: ax25SSID, AX25To: ax25To}
	return a.startTransmission(job, repeat, intervalSeconds)
}

func (a *App) startTransmission(job txqueue.Job, repeat bool, intervalSeconds int) error {
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
			appCtx := a.ctx
			a.mu.Unlock()
			a.txGate.Unlock()
			if appCtx != nil {
				runtime.EventsEmit(appCtx, "tx:state", map[string]any{"active": false})
			}
		}()
		if ctx.Err() == nil {
			if appCtx := a.getContext(); appCtx != nil {
				runtime.EventsEmit(appCtx, "tx:state", map[string]any{"active": true})
			}
		}
		err := txqueue.Run(ctx, []txqueue.Job{job}, txqueue.Config{Repeat: repeat, Interval: time.Duration(intervalSeconds) * time.Second}, func(sendCtx context.Context, job txqueue.Job) error {
			return a.sendImageJob(sendCtx, job)
		}, func(event txqueue.Event) {
			if appCtx := a.getContext(); appCtx != nil {
				runtime.EventsEmit(appCtx, "tx:status", event)
			}
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			if appCtx := a.getContext(); appCtx != nil {
				runtime.EventsEmit(appCtx, "tx:error", err.Error())
			}
		}
	}()
	return nil
}

func (a *App) getContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
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
	a.txImageID++
	imageID := a.txImageID
	a.mu.Unlock()
	if duplex == nil {
		return audio.ErrRuntimeUnavailable
	}
	result, err := tximage.EncodeImageWithAX25(source, job.Profile, job.Quality, callsign, imageID, rate, uint8(job.AX25SSID), job.AX25To)
	if err != nil {
		return err
	}
	if appCtx := a.getContext(); appCtx != nil {
		runtime.EventsEmit(appCtx, "tx:progress", 0.0)
	}
	transmissionStartedAt := time.Now().UTC()
	if err := duplex.Transmit(ctx, result.Waveform, func(progress float64) {
		a.emitTXProgress(result.Waveform, result.SampleRate, progress)
	}); err != nil {
		return err
	}
	return a.archiveTXImage(result.Image, job, callsign, imageID, result.PacketCount, transmissionStartedAt)
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

func (a *App) emitTXProgress(samples []float32, sampleRate int, progress float64) {
	if appCtx := a.getContext(); appCtx != nil {
		runtime.EventsEmit(appCtx, "tx:progress", progress)
	}
	if len(samples) < 1024 {
		return
	}
	end := int(progress * float64(len(samples)-1))
	start := end - 1024
	if start < 0 {
		start = 0
	}
	if start+1024 > len(samples) {
		start = len(samples) - 1024
	}
	bins, err := dsp.Spectrum(samples[start:start+1024], sampleRate)
	if err == nil {
		if appCtx := a.getContext(); appCtx != nil {
			runtime.EventsEmit(appCtx, "tx:spectrum", bins)
		}
	}

}

// TransmitPCM accepts mono float32 samples from the Go image/modem pipeline.
// Wails callers should normally invoke a backend image-send method rather than
// transferring large PCM arrays through JavaScript.
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
	pcm, err := dsp.ResampleLinear(samples, waveformSampleRate, targetRate)
	if err != nil {
		return err
	}
	return d.Transmit(ctx, pcm, func(progress float64) {
		a.emitTXProgress(pcm, targetRate, progress)
	})
}

func (a *App) handleRXAudio(samples []float32) {
	if samples == nil {
		a.rxSpectrumMu.Lock()
		a.rxSpectrumWindow = nil
		a.rxSpectrumMu.Unlock()
		a.enqueueRXSamples(nil)
		return
	}
	if len(samples) == 0 {
		return
	}
	a.mu.Lock()
	ctx, rate := a.ctx, a.settings.AudioSampleRate
	a.mu.Unlock()
	a.rxSpectrumMu.Lock()
	a.rxSpectrumWindow = append(a.rxSpectrumWindow, samples...)
	if len(a.rxSpectrumWindow) > 4096 {
		a.rxSpectrumWindow = append([]float32(nil), a.rxSpectrumWindow[len(a.rxSpectrumWindow)-4096:]...)
	}
	if len(a.rxSpectrumWindow) < 1024 {
		a.rxSpectrumMu.Unlock()
		a.enqueueRXSamples(samples)
		return
	}
	window := append([]float32(nil), a.rxSpectrumWindow[len(a.rxSpectrumWindow)-1024:]...)
	a.rxSpectrumMu.Unlock()
	bins, err := dsp.Spectrum(window, rate)
	if err == nil && ctx != nil {
		runtime.EventsEmit(ctx, "rx:spectrum", bins)
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
	if ctx != nil {
		runtime.EventsEmit(ctx, "rx:level", map[string]float64{"rmsDbfs": db, "peak": float64(peak)})
	}
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
	}
}

func (a *App) runRXDecodeWorker(ctx context.Context, queue <-chan []float32) {
	var decoder *transport.StreamDecoder
	activeProfile := ""
	activeRate := 0
	for {
		select {
		case <-ctx.Done():
			return
		case block, ok := <-queue:
			if !ok {
				return
			}
			a.mu.Lock()
			profileID, rate := a.settings.RXProfile, a.settings.AudioSampleRate
			a.mu.Unlock()
			if profileID != activeProfile || rate != activeRate {
				activeProfile, activeRate = profileID, rate
				var err error
				decoder, err = transport.NewStreamDecoder(domain.TransportMode(profileID), rate)
				if err != nil {
					a.setAudioError(fmt.Errorf("initialize %s streaming decoder: %w", profileID, err))
					decoder = nil
					continue
				}
			}
			if decoder == nil {
				continue
			}
			packets, err := decoder.Feed(block)
			if err != nil {
				// Short acquisition windows and incomplete frame tails are normal
				// during live RX. Only surface persistent decoder state elsewhere.
				continue
			}
			for _, packet := range packets {
				if a.rxManager != nil {
					a.rxManager.Submit(packet)
				}
			}
		}
	}
}

func (a *App) handleRXImageUpdate(update rxengine.Update) {
	// Archive every decodable preview, not only the final packet-complete image.
	// The same RX album record is updated as the scan grows and when it completes.
	if update.PNGBase64 != "" && (update.ArchivePreview || update.Complete) {
		if err := a.archiveRXImage(update); err != nil {
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
			update.PacketDataBase64 = ""
		}
	}
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		runtime.EventsEmit(ctx, "rx:image", update)
	}
}

func (a *App) archiveRXImage(update rxengine.Update) error {
	data, err := base64.StdEncoding.DecodeString(update.PNGBase64)
	if err != nil {
		return fmt.Errorf("decode reconstructed PNG: %w", err)
	}
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
	ctx := a.getContext()
	if ctx == nil {
		return gallery.Asset{}, errors.New("application window is not ready")
	}
	path, err := runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title:   "Import image into SATSSDV-Go gallery",
		Filters: []runtime.FileFilter{{DisplayName: "Supported images", Pattern: "*.png;*.jpg;*.jpeg;*.gif"}},
	})
	if err != nil {
		return gallery.Asset{}, err
	}
	if strings.TrimSpace(path) == "" {
		return gallery.Asset{}, nil
	}
	return a.ImportGalleryImage(path)
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

// GetLatestRXImageData returns the newest archived RX image for template layers.
func (a *App) GetLatestRXImageData() (string, error) {
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
// inside this application's album directory are exposed to the frontend.
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
