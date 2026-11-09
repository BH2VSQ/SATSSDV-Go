package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"strings"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/audio"
	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/gifmode"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
	"github.com/BH2VSQ/SATSSDV-Go/internal/txqueue"
	"runtime/debug"
)

type gifTransmissionRequest struct {
	rendered        RenderedTXImage
	basePNG         []byte
	profileID       string
	quality         string
	callsign        string
	ax25SSID        int
	audioRate       int
	ax25To          string
	repeat          bool
	intervalSeconds int
}

type gifTransmissionSession struct {
	app              *App
	request          gifTransmissionRequest
	profile          domain.Profile
	maryFECProfile   string
	gifData          []byte
	base             *image.RGBA
	overlay          *image.RGBA
	prepared         bool
	prepareErr       error
	archiveGIF       []byte
	archiveWidth     int
	archiveHeight    int
	stats            gifmode.Stats
	waveformRate     int
	packets          [][]byte
	encoder          *transport.Encoder
	estimatedSamples int64
	wireFrameCount   int
	duplex           *audio.Duplex
	currentCallsign  string
	baseImageID      uint8
}

func (a *App) startTransmissionGIF(request gifTransmissionRequest) error {
	a.mu.Lock()
	fecProfile := a.settings.MARYFECProfile
	a.mu.Unlock()
	session, err := newGIFTransmissionSession(a, request, fecProfile)
	if err != nil {
		return err
	}
	job := txqueue.Job{
		ID: request.rendered.ID, ImageName: request.rendered.Name,
		Profile: request.profileID, Quality: request.quality,
		AX25SSID: request.ax25SSID, AX25To: request.ax25To,
	}
	return a.startTransmissionWithSender(job, request.repeat, request.intervalSeconds, session.send)
}

func newGIFTransmissionSession(app *App, request gifTransmissionRequest, fecProfile string) (*gifTransmissionSession, error) {
	gifData, err := base64.StdEncoding.DecodeString(request.rendered.GIFBase64)
	if err != nil || len(gifData) == 0 || len(gifData) > 8*1024*1024 {
		return nil, errors.New("template GIF must be valid and no larger than 8 MiB")
	}
	baseImage, err := png.Decode(bytes.NewReader(request.basePNG))
	if err != nil {
		return nil, fmt.Errorf("decode template base PNG: %w", err)
	}
	base := image.NewRGBA(image.Rect(0, 0, baseImage.Bounds().Dx(), baseImage.Bounds().Dy()))
	draw.Draw(base, base.Bounds(), baseImage, baseImage.Bounds().Min, draw.Src)
	overlay, err := decodeGIFOverlay(request.rendered.OverlayPNGBase64, base.Bounds())
	if err != nil {
		return nil, err
	}
	profile, err := domain.ParseProfile(request.profileID)
	if err != nil {
		return nil, err
	}
	if profile.ImageCodec != "SSDV" {
		return nil, errors.New("GIF template components require an SSDV TX profile")
	}
	if profile.LinkLayer == "CCSDS-MARY" && request.audioRate < 44100 {
		return nil, errors.New("QPSK/8PSK/16QAM 1600-symbol TX requires an audio sample rate of at least 44100 Hz")
	}
	if request.intervalSeconds < 0 || request.intervalSeconds > 86400 {
		return nil, errors.New("TX interval must be between 0 and 86400 seconds")
	}
	if strings.TrimSpace(request.ax25To) == "" {
		request.ax25To = "CQ"
	}
	if request.quality == "" {
		request.quality = "medium"
	}
	return &gifTransmissionSession{
		app: app, request: request, profile: profile, maryFECProfile: fecProfile,
		gifData: gifData, base: base, overlay: overlay,
	}, nil
}

func decodeGIFOverlay(encoded string, bounds image.Rectangle) (*image.RGBA, error) {
	if encoded == "" {
		return image.NewRGBA(bounds), nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > 8*1024*1024 {
		return nil, errors.New("template GIF overlay PNG is invalid or exceeds 8 MiB")
	}
	source, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode template overlay PNG: %w", err)
	}
	if source.Bounds().Dx() != bounds.Dx() || source.Bounds().Dy() != bounds.Dy() {
		return nil, errors.New("template GIF overlay dimensions do not match the base canvas")
	}
	overlay := image.NewRGBA(bounds)
	draw.Draw(overlay, overlay.Bounds(), source, source.Bounds().Min, draw.Src)
	return overlay, nil
}

func (s *gifTransmissionSession) send(ctx context.Context, _ txqueue.Job) error {
	if err := s.prepare(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	startedAt := time.Time{}
	estimatedDuration := durationForSamples(s.estimatedSamples, s.waveformRate)
	producer := func(streamCtx context.Context, emit func([]float32) error) error {
		streamedSamples, streamErr := s.encoder.StreamPackets(streamCtx, s.packets, func(block []float32) error {
			if emitErr := emit(block); emitErr != nil {
				return emitErr
			}
			s.app.emitTXStreamSpectrum(block, s.waveformRate)
			return nil
		})
		if streamErr == nil && streamedSamples != s.estimatedSamples {
			return fmt.Errorf("GIF stream encoder produced %d samples, estimated %d", streamedSamples, s.estimatedSamples)
		}
		return streamErr
	}
	if err := s.duplex.TransmitStreamWithStart(ctx, s.estimatedSamples, producer, func() {
		startedAt = time.Now().UTC()
		s.app.emitTXMetrics(startedAt, estimatedDuration, s.wireFrameCount, 0)
	}, func(progress float64) {
		s.app.emitTXProgress(nil, s.waveformRate, progress)
		s.app.emitTXMetrics(startedAt, estimatedDuration, s.wireFrameCount, progress)
	}); err != nil {
		return err
	}
	if err := s.app.archiveTXGIFData(s.archiveGIF, s.archiveWidth, s.archiveHeight, s.request.profileID, s.currentCallsign, s.baseImageID, s.stats, startedAt); err != nil {
		return fmt.Errorf("archive transmitted GIF: %w", err)
	}
	s.app.emit("tx:gif-sent", map[string]any{
		"fileName": s.request.rendered.Name, "frames": s.stats.Frames,
		"packetsSent": s.stats.TotalPackets, "wirePacketsSent": s.wireFrameCount,
		"startedAt": startedAt,
	})
	return nil
}

func (s *gifTransmissionSession) prepare(ctx context.Context) error {
	if s.prepared {
		return s.prepareErr
	}
	defer func() { s.prepared = true }()
	frames, err := gifmode.DecodeTemplate(s.gifData, s.request.quality, s.base, s.overlay, s.request.rendered.GIFRect)
	if err != nil {
		s.prepareErr = err
		return err
	}
	if err := ctx.Err(); err != nil {
		s.prepareErr = err
		return err
	}
	s.archiveWidth = frames[0].Image.Bounds().Dx()
	s.archiveHeight = frames[0].Image.Bounds().Dy()
	s.archiveGIF, err = gifmode.EncodeGIF(frames)
	if err != nil {
		s.prepareErr = fmt.Errorf("prepare TX GIF album preview: %w", err)
		return s.prepareErr
	}
	if len(s.archiveGIF) == 0 || len(s.archiveGIF) > 16*1024*1024 {
		s.prepareErr = errors.New("transmitted GIF preview is empty or exceeds the 16 MiB album limit")
		return s.prepareErr
	}
	s.app.mu.Lock()
	s.currentCallsign = strings.TrimSpace(s.app.settings.Callsign)
	if s.currentCallsign == "" {
		s.currentCallsign = s.request.callsign
	}
	s.app.txImageID++
	s.baseImageID = s.app.txImageID
	if len(frames) > 1 {
		s.app.txImageID += uint8(len(frames) - 1)
	}
	s.duplex = s.app.audioDuplex
	s.app.mu.Unlock()
	if s.duplex == nil {
		s.prepareErr = audio.ErrRuntimeUnavailable
		return s.prepareErr
	}
	s.packets, s.stats, err = gifmode.EncodeContextV2(ctx, frames, s.currentCallsign, s.baseImageID, ssdv.DefaultJPEGQuality)
	if err != nil {
		s.prepareErr = err
		return err
	}
	frames = nil
	s.gifData, s.base, s.overlay = nil, nil, nil
	transportConfig := transport.Config{
		Profile: domain.TransportMode(s.request.profileID), AudioSampleRate: s.request.audioRate,
		MARYFECProfile:     maryFECProfileValue(s.maryFECProfile),
		MARYBurstGroupSize: gifMARYBurstGroupSize(s.profile),
	}
	if s.profile.LinkLayer == "AX.25 UI / HDLC" {
		transportConfig.Source = ax25.Address{Callsign: s.currentCallsign, SSID: uint8(s.request.ax25SSID)}
		transportConfig.Destination = ax25.Address{Callsign: s.request.ax25To}
	}
	s.encoder, err = transport.NewEncoder(transportConfig)
	if err != nil {
		s.prepareErr = err
		return err
	}
	s.estimatedSamples, err = s.encoder.EstimateOutputSamplesForPackets(s.packets)
	if err != nil {
		s.prepareErr = fmt.Errorf("estimate GIF template stream: %w", err)
		return s.prepareErr
	}
	s.wireFrameCount, err = s.encoder.EstimateWirePacketCount(s.packets)
	if err != nil {
		s.prepareErr = fmt.Errorf("estimate GIF physical data frames: %w", err)
		return s.prepareErr
	}
	debug.FreeOSMemory()
	s.waveformRate, err = s.encoder.SampleRate()
	if err != nil {
		s.prepareErr = fmt.Errorf("prepare GIF template stream: %w", err)
		return s.prepareErr
	}
	s.app.emit("tx:gif-stats", map[string]any{
		"frames": s.stats.Frames, "keyframes": s.stats.Keyframes,
		"dataPackets": s.stats.DataPackets, "totalPackets": s.stats.TotalPackets,
		"wirePackets": s.wireFrameCount,
		"changedMCUs": s.stats.ChangedMCUs, "totalMCUs": s.stats.TotalMCUs,
		"motionVectors": s.stats.MotionVectors, "predictedMCUs": s.stats.PredictedMCUs,
		"estimatedSeconds": int(durationForSamples(s.estimatedSamples, s.waveformRate).Seconds()),
	})
	return nil
}
