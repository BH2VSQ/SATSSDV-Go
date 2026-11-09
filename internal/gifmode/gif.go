// Package gifmode implements SATSSDV-Go's experimental incremental GIF mode.
// It uses SSDV JPEG MCU restart points but marks packets with the reserved
// SATSSDV-Go GIF extension flag. These packets are not interoperable with
// unmodified SSDV receivers.
package gifmode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"sort"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/imagequality"
)

const (
	MaxFrames              = 120
	MaxLogicalScreenPixels = 8 * 1024 * 1024
	MaxDecodedFramePixels  = 64 * 1024 * 1024
	KeyframeInterval       = 12
	KeyframeChangeRatio    = 0.60
	ControlPacketCopies    = 3
)

type Frame struct {
	Image   *image.RGBA
	DelayCS uint16
}

type Stats struct {
	Frames         int
	Keyframes      int
	DataPackets    int
	ControlPackets int
	TotalPackets   int
	ChangedMCUs    int
	TotalMCUs      int
	MotionVectors  int
	PredictedMCUs  int
}

// Decode composes GIF image descriptors, transparency and disposal operations
// into full logical-screen frames before resizing them to the selected SSDV
// image preset. Frame delays retain GIF's centisecond unit.
func Decode(data []byte, preset string) ([]Frame, error) {
	return decodeGIF(data, func(canvas *image.RGBA) (*image.RGBA, error) {
		return imagequality.ResizeCover(canvas, imagequality.Preset(preset))
	})
}

// Rect describes the destination of an animated GIF image component in the
// final SSDV template canvas. Coordinates are in final preset pixels.
type Rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// DecodeTemplate decodes and composites each GIF frame into a normal TX
// template. base contains everything below the GIF component; overlay contains
// everything above it. This preserves template layering while the animation
// itself is transmitted as SSDV MCU updates.
func DecodeTemplate(data []byte, preset string, base, overlay *image.RGBA, rect Rect) ([]Frame, error) {
	if base == nil || base.Bounds().Empty() {
		return nil, errors.New("GIF template base canvas is empty")
	}
	if overlay == nil {
		overlay = image.NewRGBA(base.Bounds())
	}
	if overlay.Bounds().Dx() != base.Bounds().Dx() || overlay.Bounds().Dy() != base.Bounds().Dy() {
		return nil, errors.New("GIF template overlay dimensions do not match base canvas")
	}
	target := image.Rect(rect.X, rect.Y, rect.X+rect.Width, rect.Y+rect.Height).Intersect(base.Bounds())
	if rect.Width <= 0 || rect.Height <= 0 || target.Empty() {
		return nil, errors.New("GIF template image component has invalid bounds")
	}
	return decodeGIF(data, func(frame *image.RGBA) (*image.RGBA, error) {
		result := cloneRGBA(base)
		layer := resizeRGBA(frame, rect.Width, rect.Height)
		draw.Draw(result, target, layer, image.Point{X: target.Min.X - rect.X, Y: target.Min.Y - rect.Y}, draw.Over)
		draw.Draw(result, result.Bounds(), overlay, overlay.Bounds().Min, draw.Over)
		return result, nil
	})
}

// decodeGIF composes GIF logical-screen frames (including transparency,
// offsets and disposal) and applies a caller-provided frame transform.
func decodeGIF(data []byte, transform func(*image.RGBA) (*image.RGBA, error)) ([]Frame, error) {
	if len(data) == 0 {
		return nil, errors.New("GIF data is empty")
	}
	config, err := gif.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("read GIF header: %w", err)
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > MaxLogicalScreenPixels {
		return nil, errors.New("GIF logical screen exceeds the 8192 px / 8 megapixel limit")
	}
	if err := validateGIFFrameBudget(data, config.Width, config.Height); err != nil {
		return nil, err
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode GIF: %w", err)
	}
	if len(decoded.Image) == 0 {
		return nil, errors.New("GIF contains no frames")
	}
	if len(decoded.Image) > MaxFrames {
		return nil, fmt.Errorf("GIF contains %d frames; the experimental mode limit is %d", len(decoded.Image), MaxFrames)
	}
	width, height := decoded.Config.Width, decoded.Config.Height
	if width <= 0 || height <= 0 {
		bounds := decoded.Image[0].Bounds()
		width, height = bounds.Max.X, bounds.Max.Y
	}
	if width < 1 || height < 1 || width > 8192 || height > 8192 || int64(width)*int64(height) > MaxLogicalScreenPixels {
		return nil, errors.New("GIF logical screen exceeds the 8192 px / 8 megapixel limit")
	}

	canvasBounds := image.Rect(0, 0, width, height)
	background := gifBackground(decoded)
	if gifHasTransparency(decoded) {
		// Transparent pixels are part of an animated overlay, not black or the
		// GIF global-screen background. Keeping the logical canvas transparent
		// lets DecodeTemplate composite those pixels over the template layers.
		background = color.RGBA{A: 0}
	}
	canvas := image.NewRGBA(canvasBounds)
	draw.Draw(canvas, canvasBounds, image.NewUniform(background), image.Point{}, draw.Src)
	frames := make([]Frame, 0, len(decoded.Image))
	var previousBounds image.Rectangle
	var previousDisposal byte
	var restorePrevious *image.RGBA
	for i, sourceFrame := range decoded.Image {
		if i > 0 {
			switch previousDisposal {
			case gif.DisposalBackground:
				bounds := previousBounds.Intersect(canvasBounds)
				if !bounds.Empty() {
					draw.Draw(canvas, bounds, image.NewUniform(background), image.Point{}, draw.Src)
				}
			case gif.DisposalPrevious:
				if restorePrevious != nil {
					canvas = cloneRGBA(restorePrevious)
				}
			}
		}
		frameBounds := sourceFrame.Bounds().Intersect(canvasBounds)
		if frameBounds.Empty() {
			return nil, fmt.Errorf("GIF frame %d has no pixels inside the logical screen", i)
		}
		if i < len(decoded.Disposal) && decoded.Disposal[i] == gif.DisposalPrevious {
			restorePrevious = cloneRGBA(canvas)
		} else {
			restorePrevious = nil
		}
		draw.Draw(canvas, frameBounds, sourceFrame, frameBounds.Min, draw.Over)
		transformed, transformErr := transform(cloneRGBA(canvas))
		if transformErr != nil {
			return nil, fmt.Errorf("prepare GIF frame %d: %w", i, transformErr)
		}
		delay := uint16(10)
		if i < len(decoded.Delay) && decoded.Delay[i] > 0 {
			delay = uint16(min(decoded.Delay[i], 65535))
		}
		frames = append(frames, Frame{Image: transformed, DelayCS: delay})
		previousBounds = frameBounds
		previousDisposal = gif.DisposalNone
		if i < len(decoded.Disposal) {
			previousDisposal = decoded.Disposal[i]
		}
	}
	return frames, nil
}

// resizeRGBA uses bilinear interpolation and preserves alpha so transparent
// GIF pixels can reveal the template layers beneath the animated component.
func resizeRGBA(src *image.RGBA, width, height int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	if src == nil || width <= 0 || height <= 0 || src.Bounds().Empty() {
		return dst
	}
	bounds := src.Bounds()
	for y := 0; y < height; y++ {
		sy := (float64(y)+0.5)*float64(bounds.Dy())/float64(height) - 0.5
		y0 := int(sy)
		if sy < 0 {
			y0 = 0
		}
		y1 := min(y0+1, bounds.Max.Y-1)
		fy := sy - float64(y0)
		if fy < 0 {
			fy = 0
		}
		for x := 0; x < width; x++ {
			sx := (float64(x)+0.5)*float64(bounds.Dx())/float64(width) - 0.5
			x0 := int(sx)
			if sx < 0 {
				x0 = 0
			}
			x1 := min(x0+1, bounds.Max.X-1)
			fx := sx - float64(x0)
			if fx < 0 {
				fx = 0
			}
			p00 := src.RGBAAt(x0, y0)
			p10 := src.RGBAAt(x1, y0)
			p01 := src.RGBAAt(x0, y1)
			p11 := src.RGBAAt(x1, y1)
			mix := func(a, b, c, d uint8) uint8 {
				top := float64(a)*(1-fx) + float64(b)*fx
				bottom := float64(c)*(1-fx) + float64(d)*fx
				v := top*(1-fy) + bottom*fy
				return uint8(max(0, min(255, int(v+0.5))))
			}
			dst.SetRGBA(x, y, color.RGBA{R: mix(p00.R, p10.R, p01.R, p11.R), G: mix(p00.G, p10.G, p01.G, p11.G), B: mix(p00.B, p10.B, p01.B, p11.B), A: mix(p00.A, p10.A, p01.A, p11.A)})
		}
	}
	return dst
}

// validateGIFFrameBudget scans block descriptors before DecodeAll allocates
// paletted frame images. The byte-size upload limit alone does not prevent a
// tiny, highly-compressible GIF from expanding into gigabytes of pixel data.
func validateGIFFrameBudget(data []byte, logicalWidth, logicalHeight int) error {
	if len(data) < 13 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
		return errors.New("invalid GIF header")
	}
	i := 13
	packed := data[10]
	if packed&0x80 != 0 {
		size := 3 * (1 << ((packed & 0x07) + 1))
		if i+size > len(data) {
			return errors.New("truncated GIF global color table")
		}
		i += size
	}
	frames := 0
	var decodedPixels int64
	for i < len(data) {
		switch data[i] {
		case 0x3b: // Trailer
			if frames == 0 {
				return errors.New("GIF contains no image frames")
			}
			return nil
		case 0x21: // Extension: skip label and data sub-blocks.
			i += 2
			var err error
			i, err = skipGIFSubBlocks(data, i)
			if err != nil {
				return err
			}
		case 0x2c: // Image descriptor.
			if i+10 > len(data) {
				return errors.New("truncated GIF image descriptor")
			}
			w := int(data[i+5]) | int(data[i+6])<<8
			h := int(data[i+7]) | int(data[i+8])<<8
			framePacked := data[i+9]
			if w < 1 || h < 1 || w > logicalWidth || h > logicalHeight {
				return errors.New("GIF frame dimensions exceed the logical screen")
			}
			frames++
			if frames > MaxFrames {
				return fmt.Errorf("GIF contains more than %d frames", MaxFrames)
			}
			decodedPixels += int64(w) * int64(h)
			if decodedPixels > MaxDecodedFramePixels {
				return errors.New("GIF decoded frame area exceeds the 64 megapixel safety budget")
			}
			i += 10
			if framePacked&0x80 != 0 {
				size := 3 * (1 << ((framePacked & 0x07) + 1))
				if i+size > len(data) {
					return errors.New("truncated GIF local color table")
				}
				i += size
			}
			if i >= len(data) {
				return errors.New("truncated GIF LZW data")
			}
			i++ // LZW minimum code size.
			var err error
			i, err = skipGIFSubBlocks(data, i)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected GIF block marker 0x%02x", data[i])
		}
	}
	return errors.New("GIF is missing its trailer")
}

func skipGIFSubBlocks(data []byte, offset int) (int, error) {
	for {
		if offset >= len(data) {
			return 0, errors.New("truncated GIF data sub-block")
		}
		n := int(data[offset])
		offset++
		if n == 0 {
			return offset, nil
		}
		if offset+n > len(data) {
			return 0, errors.New("truncated GIF data sub-block payload")
		}
		offset += n
	}
}

func gifHasTransparency(decoded *gif.GIF) bool {
	if decoded == nil {
		return false
	}
	for _, frame := range decoded.Image {
		if frame == nil {
			continue
		}
		for _, entry := range frame.Palette {
			_, _, _, alpha := entry.RGBA()
			if alpha == 0 {
				return true
			}
		}
	}
	return false
}

// EncodeGIF serializes the fully composed frames used by TX into a playable
// album GIF. It stores the template-composited view rather than only the
// original animated asset, so the TX history represents what was transmitted.
func EncodeGIF(frames []Frame) ([]byte, error) {
	if len(frames) == 0 || len(frames) > MaxFrames {
		return nil, fmt.Errorf("GIF archive must contain 1..%d frames", MaxFrames)
	}
	first := frames[0].Image
	if first == nil || first.Bounds().Empty() {
		return nil, errors.New("GIF archive contains an empty first frame")
	}
	bounds := image.Rect(0, 0, first.Bounds().Dx(), first.Bounds().Dy())
	animation := &gif.GIF{Image: make([]*image.Paletted, len(frames)), Delay: make([]int, len(frames)), Disposal: make([]byte, len(frames)), LoopCount: 0}
	for i, frame := range frames {
		if frame.Image == nil || frame.Image.Bounds().Dx() != bounds.Dx() || frame.Image.Bounds().Dy() != bounds.Dy() {
			return nil, fmt.Errorf("GIF archive frame %d has inconsistent dimensions", i+1)
		}
		paletted := image.NewPaletted(bounds, palette.Plan9)
		draw.FloydSteinberg.Draw(paletted, bounds, frame.Image, frame.Image.Bounds().Min)
		animation.Image[i] = paletted
		delay := int(frame.DelayCS)
		if delay <= 0 {
			delay = 10
		}
		animation.Delay[i] = delay
		animation.Disposal[i] = gif.DisposalNone
	}
	var output bytes.Buffer
	if err := gif.EncodeAll(&output, animation); err != nil {
		return nil, fmt.Errorf("encode TX GIF album preview: %w", err)
	}
	return output.Bytes(), nil
}

func gifBackground(decoded *gif.GIF) color.Color {
	if palette, ok := decoded.Config.ColorModel.(color.Palette); ok && int(decoded.BackgroundIndex) < len(palette) {
		return palette[decoded.BackgroundIndex]
	}
	return color.RGBA{A: 0}
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	if src == nil {
		return nil
	}
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

// Encode converts full logical-screen GIF frames into sparse SSDV MCU updates.
// A complete SSDV keyframe is emitted at the start and periodically; intervening
// frames carry only packet segments whose MCU ranges overlap changed pixels.
// A reserved PacketID=0xffff control packet closes each frame and carries its
// display delay, including frames that have no visual changes.
func Encode(frames []Frame, callsign string, baseImageID uint8, quality uint8) ([][]byte, Stats, error) {
	return EncodeContext(context.Background(), frames, callsign, baseImageID, quality)
}

// EncodeContext is Encode with cancellation checks between frame encodes. A
// GIF can contain many frames, so Stop TX should not have to wait for the
// entire animation to be packetized before it takes effect.
func EncodeContext(ctx context.Context, frames []Frame, callsign string, baseImageID uint8, quality uint8) ([][]byte, Stats, error) {
	return encodeContext(ctx, frames, callsign, baseImageID, quality, 1)
}

// EncodeContextV2 enables MCU-level translation prediction and writes the
// SGV2 extension into frame-open controls. The RX parser remains compatible
// with SGV1; the existing Encode/EncodeContext APIs continue to emit SGV1.
func EncodeContextV2(ctx context.Context, frames []Frame, callsign string, baseImageID uint8, quality uint8) ([][]byte, Stats, error) {
	return encodeContext(ctx, frames, callsign, baseImageID, quality, 2)
}

type gifFrameEncodeOptions struct {
	callsign        string
	baseImageID     uint8
	quality         uint8
	protocolVersion uint8
	frameIndex      int
	frameCount      int
	previous        *image.RGBA
}

type encodedGIFFrame struct {
	packets  [][]byte
	stats    Stats
	previous *image.RGBA
}

func encodeContext(ctx context.Context, frames []Frame, callsign string, baseImageID uint8, quality uint8, protocolVersion uint8) ([][]byte, Stats, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var stats Stats
	if len(frames) == 0 {
		return nil, stats, errors.New("GIF contains no prepared frames")
	}
	if len(frames) > MaxFrames {
		return nil, stats, fmt.Errorf("GIF frame count exceeds %d", MaxFrames)
	}
	if quality > 7 {
		return nil, stats, errors.New("SSDV quality must be 0..7")
	}

	allPackets := make([][]byte, 0, len(frames)*8)
	var previous *image.RGBA
	for frameIndex, frame := range frames {
		result, err := encodeGIFFrame(ctx, frame, gifFrameEncodeOptions{
			callsign: callsign, baseImageID: baseImageID, quality: quality,
			protocolVersion: protocolVersion, frameIndex: frameIndex,
			frameCount: len(frames), previous: previous,
		})
		if err != nil {
			return nil, stats, err
		}
		allPackets = append(allPackets, result.packets...)
		stats.Frames += result.stats.Frames
		stats.Keyframes += result.stats.Keyframes
		stats.DataPackets += result.stats.DataPackets
		stats.ControlPackets += result.stats.ControlPackets
		stats.ChangedMCUs += result.stats.ChangedMCUs
		stats.TotalMCUs += result.stats.TotalMCUs
		stats.MotionVectors += result.stats.MotionVectors
		stats.PredictedMCUs += result.stats.PredictedMCUs
		previous = result.previous
	}
	stats.TotalPackets = len(allPackets)
	return allPackets, stats, nil
}

func encodeGIFFrame(ctx context.Context, frame Frame, options gifFrameEncodeOptions) (encodedGIFFrame, error) {
	result := encodedGIFFrame{}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if frame.Image == nil {
		return result, fmt.Errorf("GIF frame %d is nil", options.frameIndex)
	}
	previous := options.previous
	if previous != nil && previous.Bounds() != frame.Image.Bounds() {
		return result, errors.New("GIF frames have inconsistent resized dimensions")
	}

	frameID := uint8(uint16(options.baseImageID) + uint16(options.frameIndex))
	comparisonBase, motionVectors, predictedMCUs := selectFramePrediction(previous, frame.Image, options.protocolVersion)
	changed, changedCount, totalMCUs := changedMCUs(comparisonBase, frame.Image)
	keyframe := shouldUseKeyframe(previous, options.frameIndex, changedCount, totalMCUs)
	if keyframe {
		motionVectors = nil
		predictedMCUs = 0
		changed, changedCount, totalMCUs = changedMCUs(previous, frame.Image)
	}

	fullPackets, err := ssdv.EncodeImage(frame.Image, ssdv.EncodeConfig{
		Callsign: options.callsign, ImageID: frameID, Quality: options.quality, FEC: true,
		GIFMode: true, Keyframe: keyframe,
	})
	if err != nil {
		return result, fmt.Errorf("encode GIF frame %d: %w", options.frameIndex, err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	selection := framePacketSelection{
		frame: frame, fullPackets: fullPackets, changed: changed,
		changedCount: changedCount, totalMCUs: totalMCUs, keyframe: keyframe,
		motionVectors: motionVectors, predictedMCUs: predictedMCUs, options: options,
	}
	selected, keyframe, motionVectors, predictedMCUs, err := selectFramePackets(selection)
	if err != nil {
		return result, err
	}

	openOptions := frameControlOptions(frame, options, frameID, uint16(len(selected)), true)
	openOptions.Keyframe = keyframe
	openOptions.MotionVectors = motionVectors
	openMarker, err := buildFrameOpenMarker(openOptions, options.protocolVersion)
	if err != nil {
		return result, fmt.Errorf("build GIF frame %d open marker: %w", options.frameIndex, err)
	}
	packets := appendControlCopies(nil, openMarker)
	packets = append(packets, selected...)

	closeOptions := frameControlOptions(frame, options, frameID, uint16(len(selected)), false)
	closeOptions.Keyframe = keyframe
	closeOptions.StreamEnd = options.frameIndex == options.frameCount-1
	closeMarker, err := buildFrameCloseMarker(closeOptions, options.protocolVersion)
	if err != nil {
		return result, fmt.Errorf("build GIF frame %d close marker: %w", options.frameIndex, err)
	}
	packets = appendControlCopies(packets, closeMarker)

	frameStats := Stats{
		Frames: 1, DataPackets: len(selected), ControlPackets: ControlPacketCopies * 2,
		ChangedMCUs: changedCount, TotalMCUs: totalMCUs,
		MotionVectors: len(motionVectors), PredictedMCUs: predictedMCUs,
	}
	if keyframe {
		frameStats.Keyframes = 1
	}
	result.packets = packets
	result.stats = frameStats
	result.previous = cloneRGBA(frame.Image)
	return result, nil
}

func selectFramePrediction(previous, current *image.RGBA, protocolVersion uint8) (*image.RGBA, []ssdv.GIFMotionVector, int) {
	if protocolVersion < 2 || previous == nil {
		return previous, nil, 0
	}
	return PredictMotion(previous, current)
}

func shouldUseKeyframe(previous *image.RGBA, frameIndex, changedCount, totalMCUs int) bool {
	return previous == nil || frameIndex%KeyframeInterval == 0 ||
		float64(changedCount)/float64(max(1, totalMCUs)) >= KeyframeChangeRatio
}

type framePacketSelection struct {
	frame         Frame
	fullPackets   [][]byte
	changed       []bool
	changedCount  int
	totalMCUs     int
	keyframe      bool
	motionVectors []ssdv.GIFMotionVector
	predictedMCUs int
	options       gifFrameEncodeOptions
}

func selectFramePackets(selection framePacketSelection) ([][]byte, bool, []ssdv.GIFMotionVector, int, error) {
	selected := selection.fullPackets
	if selection.keyframe {
		return selected, true, selection.motionVectors, selection.predictedMCUs, nil
	}
	var sparseAvailable bool
	var err error
	selected, sparseAvailable, err = selectChangedSegments(selection.fullPackets, selection.changed, selection.totalMCUs)
	if err != nil {
		return nil, false, nil, 0, fmt.Errorf("select GIF frame %d MCU updates: %w", selection.options.frameIndex, err)
	}
	if sparseAvailable && selection.changedCount > 0 {
		_, _, decodedMCUs, previewErr := ssdv.UpdatePacketsPreviewSparse(image.NewRGBA(selection.frame.Image.Bounds()), selected)
		if previewErr != nil || decodedMCUs == 0 {
			sparseAvailable = false
		}
	}
	if sparseAvailable {
		return selected, false, selection.motionVectors, selection.predictedMCUs, nil
	}

	selected, err = ssdv.EncodeImage(selection.frame.Image, ssdv.EncodeConfig{
		Callsign: selection.options.callsign,
		ImageID:  uint8(uint16(selection.options.baseImageID) + uint16(selection.options.frameIndex)),
		Quality:  selection.options.quality, FEC: true, GIFMode: true, Keyframe: true,
	})
	if err != nil {
		return nil, false, nil, 0, fmt.Errorf("re-encode GIF frame %d as a recovery keyframe: %w", selection.options.frameIndex, err)
	}
	return selected, true, nil, 0, nil
}

func frameControlOptions(frame Frame, options gifFrameEncodeOptions, frameID uint8, dataPackets uint16, open bool) ssdv.GIFFrameControlOptions {
	control := ssdv.GIFFrameControlOptions{
		Callsign: options.callsign, ImageID: frameID,
		Width: frame.Image.Bounds().Dx(), Height: frame.Image.Bounds().Dy(),
		Quality: options.quality, DelayCS: frame.DelayCS, DataPackets: dataPackets,
		Keyframe: false,
	}
	if open {
		control.FrameStart = true
		control.StreamStart = options.frameIndex == 0
	}
	return control
}

func buildFrameOpenMarker(options ssdv.GIFFrameControlOptions, protocolVersion uint8) ([]byte, error) {
	if protocolVersion >= 2 {
		return ssdv.NewGIFFrameControlPacketV2(options)
	}
	return ssdv.NewGIFFrameControlPacket(options)
}

func buildFrameCloseMarker(options ssdv.GIFFrameControlOptions, protocolVersion uint8) ([]byte, error) {
	options.FrameEnd = true
	if protocolVersion >= 2 {
		return ssdv.NewGIFFrameControlPacketV2(options)
	}
	return ssdv.NewGIFFrameControlPacket(options)
}

func appendControlCopies(packets [][]byte, marker []byte) [][]byte {
	for copyIndex := 0; copyIndex < ControlPacketCopies; copyIndex++ {
		packets = append(packets, append([]byte(nil), marker...))
	}
	return packets
}

func changedMCUs(previous, current *image.RGBA) ([]bool, int, int) {
	bounds := current.Bounds()
	widthMCUs, heightMCUs := bounds.Dx()/16, bounds.Dy()/16
	total := widthMCUs * heightMCUs
	changed := make([]bool, total)
	if previous == nil || previous.Bounds() != bounds {
		for i := range changed {
			changed[i] = true
		}
		return changed, total, total
	}
	count := 0
	for my := 0; my < heightMCUs; my++ {
		for mx := 0; mx < widthMCUs; mx++ {
			isChanged := false
			for y := my * 16; y < my*16+16 && !isChanged; y++ {
				oldRow := (y - previous.Rect.Min.Y) * previous.Stride
				newRow := (y - current.Rect.Min.Y) * current.Stride
				for x := mx * 16; x < mx*16+16; x++ {
					o := oldRow + (x-previous.Rect.Min.X)*4
					n := newRow + (x-current.Rect.Min.X)*4
					if !bytes.Equal(previous.Pix[o:o+4], current.Pix[n:n+4]) {
						isChanged = true
						break
					}
				}
			}
			if isChanged {
				changed[my*widthMCUs+mx] = true
				count++
			}
		}
	}
	return changed, count, total
}

// selectChangedSegments transmits a packet containing each changed MCU range
// plus the following packet as an end restart point. The SSDV packet IDs remain
// original frame-local IDs; omitted packet IDs therefore represent unchanged
// image regions, not missing fragments of a binary file.
func selectChangedSegments(fullPackets [][]byte, changed []bool, totalMCUs int) ([][]byte, bool, error) {
	if len(fullPackets) == 0 {
		return nil, false, errors.New("SSDV encoder returned no packets")
	}
	parsed := make([]ssdv.Packet, len(fullPackets))
	validBoundary := make([]bool, len(fullPackets))
	boundaryCount := 0
	for i, raw := range fullPackets {
		packet, err := ssdv.ParsePacket(raw)
		if err != nil {
			return nil, false, fmt.Errorf("parse packet %d: %w", i, err)
		}
		parsed[i] = packet
		validBoundary[i] = packet.MCUOffset != 0xff && packet.MCUIndex != 0xffff
		if validBoundary[i] {
			boundaryCount++
		}
	}
	// A legacy/continuous encoder cannot provide independently decodable
	// ranges. Report that sparse mode is unavailable so the caller promotes
	// the frame to a full keyframe; a full packet set must never be mislabeled
	// as a delta frame.
	if boundaryCount == 0 {
		out := make([][]byte, len(fullPackets))
		for i := range fullPackets {
			out[i] = append([]byte(nil), fullPackets[i]...)
		}
		return out, false, nil
	}
	changedAny := false
	for _, value := range changed {
		if value {
			changedAny = true
			break
		}
	}
	if !changedAny {
		return nil, true, nil
	}
	include := make([]bool, len(fullPackets))
	for i, packet := range parsed {
		if !validBoundary[i] {
			continue
		}
		start := int(packet.MCUIndex)
		if start < 0 || start >= totalMCUs {
			continue
		}
		nextIndex := -1
		for j := i + 1; j < len(parsed); j++ {
			if validBoundary[j] && int(parsed[j].MCUIndex) > start {
				nextIndex = j
				break
			}
		}
		end := totalMCUs
		if nextIndex >= 0 {
			end = int(parsed[nextIndex].MCUIndex)
		}
		if end <= start || end > totalMCUs {
			continue
		}
		changedInSegment := false
		for mcu := start; mcu < end && mcu < len(changed); mcu++ {
			if changed[mcu] {
				changedInSegment = true
				break
			}
		}
		if !changedInSegment {
			continue
		}
		// Include the segment's start packet and every payload byte up to its
		// next MCU restart point. If the final packet has no new MCU start, it
		// is an entropy tail and must travel with the preceding restart packet.
		segmentEnd := len(fullPackets) - 1
		if nextIndex >= 0 {
			segmentEnd = nextIndex
		}
		for j := i; j <= segmentEnd; j++ {
			include[j] = true
		}
	}
	indices := make([]int, 0)
	for i, keep := range include {
		if keep {
			indices = append(indices, i)
		}
	}
	sort.Ints(indices)
	out := make([][]byte, 0, len(indices))
	for _, index := range indices {
		out = append(out, append([]byte(nil), fullPackets[index]...))
	}
	return out, true, nil
}

// EstimateAirtime provides a conservative estimate used to protect the UI from
// constructing impractically large in-memory audio waveforms.
func EstimateAirtime(packetCount, bitRate int) time.Duration {
	if packetCount <= 0 || bitRate <= 0 {
		return 0
	}
	// CCSDS data unit (ASM + 256-byte packet) plus 32 bytes of per-packet
	// allowance for AX.25 framing/bit stuffing. This intentionally errs high
	// for CCSDS and is safer for the AX.25 path.
	bits := int64(packetCount)*int64(32+256*8+32*8) + int64(300*8)
	return time.Duration(bits * int64(time.Second) / int64(bitRate))
}
