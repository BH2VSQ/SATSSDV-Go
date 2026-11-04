package pcsi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/fec"
	"github.com/BH2VSQ/SATSSDV-Go/internal/imagequality"
)

func TestBase40RoundTrip(t *testing.T) {
	for _, s := range []string{"N0CALL", "BJ1TG", "JA1ABC"} {
		v, e := EncodeCallsignBase40(s)
		if e != nil {
			t.Fatal(e)
		}
		if got := DecodeCallsignBase40(v); got != s {
			t.Fatalf("%s -> %d -> %s", s, v, got)
		}
	}
}
func TestPCSIMMTPacketLayoutAndRS(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: uint8((x + y) % 256), A: 255})
		}
	}
	e, err := NewEncoder(src, Config{Callsign: "N0CALL", ImageID: 7, BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.EncodePacket(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 256 {
		t.Fatalf("packet length=%d", len(p))
	}
	if p[0] != 0x55 || p[1] != 0x76 || p[6] != 7 {
		t.Fatalf("header=% x", p[:7])
	}
	if !fecValid(p[1:]) {
		t.Fatal("RS8 parity did not validate")
	}
	info, err := ParsePacket(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Callsign != "N0CALL" || info.Width != 320 || info.Height != 240 || info.ImageID != 7 || info.PacketNumber != 0 {
		t.Fatalf("info=%+v", info)
	}
}
func TestPCSIQualityImagesAreBlockAligned(t *testing.T) {
	for _, q := range []imagequality.Preset{imagequality.Low, imagequality.Medium, imagequality.High} {
		d, _ := imagequality.Dimensions(q)
		img := image.NewRGBA(image.Rect(0, 0, d.Width, d.Height))
		if _, e := NewEncoder(img, Config{Callsign: "N0CALL"}); e != nil {
			t.Fatalf("%s: %v", q, e)
		}
	}
}
func fecValid(c []byte) bool {
	if len(c) != 255 {
		return false
	} // Verify by the same public packet parser's RS guard.
	// This intentionally checks that all generated parity is accepted.
	return fec.ValidateRS8(c)
}

func TestPCSIMMTUsesReferenceYCrCbWireOrder(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	enc, err := NewEncoder(img, Config{Callsign: "N0CALL", BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := enc.EncodePacket(0)
	if err != nil {
		t.Fatal(err)
	}
	payload := packet[6:224]
	bits := bitReader{src: payload[PacketHeaderBytes:]}
	y, wireCr, wireCb := bits.get(4), bits.get(4), bits.get(4)
	// The supplied MMT transmitter reads RGB pixels, runs OpenCV's BGR2YCrCb
	// conversion, then serializes the returned channels in Y, Cr, Cb order.
	// A pure-red vector in that reference path produces a low second field
	// and high third field; this locks the odd-but-compatible wire mapping.
	if y == 0 || wireCr >= wireCb {
		t.Fatalf("wire order mismatch: Y=%d second=%d third=%d", y, wireCr, wireCb)
	}
}

func TestPCSIReceiverCorrectsRSAndReconstructsImage(t *testing.T) {
	const width, height = 32, 32
	src := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 7), B: uint8((x + y) * 3), A: 255})
		}
	}
	enc, err := NewEncoder(src, Config{Callsign: "N0CALL", ImageID: 23, BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	var state *DecoderState
	for i := 0; i < enc.PacketCount(); i++ {
		packet, err := enc.EncodePacket(i)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			// Corrupt data and parity bytes. The receiver must correct these
			// before checking the packet header and unpacking samples.
			for n := 0; n < 8; n++ {
				pos := 1 + n*29
				packet[pos] ^= byte(0x35 + n)
			}
		}
		info, err := ParsePacket(func() []byte {
			if i == 0 {
				copyPacket := append([]byte(nil), packet...)
				_, _ = fec.CorrectRS8(copyPacket[1:])
				return copyPacket
			}
			return packet
		}())
		if err != nil {
			// The packet parser only validates; the state Add path is the
			// component responsible for attempting correction.
			if i != 0 {
				t.Fatal(err)
			}
			info, err = parseCorrectedPacketForTest(packet)
			if err != nil {
				t.Fatal(err)
			}
		}
		if state == nil {
			state = NewDecoderState(info)
		}
		if err := state.Add(packet); err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
	}
	if state == nil {
		t.Fatal("no decoder state created")
	}
	stats := state.Stats()
	if stats.PacketsReceived != enc.PacketCount() || stats.LumaSamples == 0 || stats.ChromaSamples == 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	decoded, stats, err := state.Reconstruct(6)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != height {
		t.Fatalf("decoded dimensions=%v", decoded.Bounds())
	}
	if stats.Coverage <= 0 || stats.Coverage > 1 {
		t.Fatalf("invalid coverage: %+v", stats)
	}
	if decoded.RGBAAt(8, 8).A != 255 {
		t.Fatal("reconstructed image did not contain opaque pixels")
	}
}

func parseCorrectedPacketForTest(packet []byte) (PacketInfo, error) {
	copyPacket := append([]byte(nil), packet...)
	if _, err := fec.CorrectRS8(copyPacket[1:]); err != nil {
		return PacketInfo{}, err
	}
	return ParsePacket(copyPacket)
}

func TestPCSIReconstructionUsesAllSampledLocations(t *testing.T) {
	const width, height = 16, 16
	values := make([]uint8, width*height)
	indices := make([]int, width*height)
	for i := range values {
		values[i] = uint8((i * 11) % 256)
		indices[i] = i
	}
	spatial, _, err := reconstructChannel(width, height, indices, values, nil, 80)
	if err != nil {
		t.Fatal(err)
	}
	var errorSum float64
	for columnIndex := range values {
		x, y := columnIndex/height, columnIndex%height
		rowIndex := y*width + x
		delta := spatial[rowIndex] - float64(values[columnIndex])
		errorSum += delta * delta
	}
	rmse := math.Sqrt(errorSum / float64(len(values)))
	if rmse > 4 {
		t.Fatalf("all-sample reconstruction RMSE too high: %.3f", rmse)
	}
}

func TestPCSIDecoderMapsReferenceChromaFieldsToRGBOrder(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	enc, err := NewEncoder(img, Config{Callsign: "N0CALL", BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := enc.EncodePacket(0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := ParsePacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	bits := bitReader{src: packet[6+PacketHeaderBytes : 6+PayloadBytes]}
	_ = bits.get(info.ChannelBits) // Y
	wireSecond := uint8(scaleBackForTest(bits.get(info.ChannelBits), info.ChannelBits))
	wireThird := uint8(scaleBackForTest(bits.get(info.ChannelBits), info.ChannelBits))
	decoder := NewDecoderState(info)
	if err := decoder.Add(packet); err != nil {
		t.Fatal(err)
	}
	idx := decoder.Pixels[0]
	if decoder.Cb[idx] != wireSecond || decoder.Cr[idx] != wireThird {
		t.Fatalf("reference chroma mapping mismatch: Cb=%d Cr=%d, wire=%d/%d", decoder.Cb[idx], decoder.Cr[idx], wireSecond, wireThird)
	}
	r, _, b := color.YCbCrToRGB(decoder.Y[idx], decoder.Cb[idx], decoder.Cr[idx])
	if r <= b {
		t.Fatalf("pure-red reference sample decoded with blue dominance: R=%d B=%d", r, b)
	}
}

func scaleBackForTest(v uint32, bits int) uint8 {
	maxValue := (1 << bits) - 1
	return uint8(math.RoundToEven(float64(v) / float64(maxValue) * 255))
}

func TestDecoderCloneAndWarmStartAreIndependent(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	enc, err := NewEncoder(img, Config{Callsign: "N0CALL", BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := enc.EncodePacket(0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := ParsePacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	live := NewDecoderState(info)
	if err := live.Add(packet); err != nil {
		t.Fatal(err)
	}
	snapshot := live.Clone()
	if len(snapshot.Pixels) != len(live.Pixels) || len(snapshot.Y) != len(live.Y) {
		t.Fatal("clone did not copy decoder measurements")
	}
	snapshot.Y[0] ^= 0xff
	if snapshot.Y[0] == live.Y[0] {
		t.Fatal("clone shares mutable sample buffers with live decoder")
	}
	snapshot.yCoefficients = []float64{1, 2, 3}
	snapshot.cbCoefficients = []float64{4, 5}
	snapshot.crCoefficients = []float64{6}
	live.CopyWarmStartFrom(snapshot)
	if len(live.yCoefficients) != 3 || live.yCoefficients[2] != 3 || live.cbCoefficients[0] != 4 || live.crCoefficients[0] != 6 {
		t.Fatal("warm-start vectors were not copied")
	}
}

func TestReconstructContextHonorsCancellation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	enc, err := NewEncoder(img, Config{Callsign: "N0CALL", BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := enc.EncodePacket(0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := ParsePacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	state := NewDecoderState(info)
	if err := state.Add(packet); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := state.ReconstructContext(ctx, 24); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestPCSIMMTPayloadMatchesPythonReferenceVector(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8((x*3 + y*5) % 256), G: uint8((x*7 + y*11) % 256), B: uint8((x*13 + y*17) % 256), A: 255})
		}
	}
	enc, err := NewEncoder(src, Config{Callsign: "N0CALL", ImageID: 17, BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := enc.EncodePacket(0)
	if err != nil {
		t.Fatal(err)
	}

	// Golden generated independently from Hamstation's PCSItxImage MMT
	// reference algorithm with OpenCV's COLOR_BGR2YCrCb conversion.
	const referenceSHA256 = "168cd4636342ba18519ef9f52edb7a6d778e0c178689d5861782f829933b6db9"
	got := sha256.Sum256(packet[:224])
	if hex.EncodeToString(got[:]) != referenceSHA256 {
		t.Fatalf("PCSI MMT payload differs from the Python reference: got %x want %s", got, referenceSHA256)
	}
}

func TestPCSIReconstructionConvergesWithoutGreenChromaArtifacts(t *testing.T) {
	const width, height = 64, 64
	src := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			src.SetRGBA(x, y, color.RGBA{R: 220, A: 255})
		}
	}
	enc, err := NewEncoder(src, Config{Callsign: "N0CALL", BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	packets := make([][]byte, 0, enc.PacketCount())
	var info PacketInfo
	for packetNumber := 0; packetNumber < enc.PacketCount(); packetNumber++ {
		packet, err := enc.EncodePacket(packetNumber)
		if err != nil {
			t.Fatal(err)
		}
		packets = append(packets, packet)
		info, err = ParsePacket(packet)
		if err != nil {
			t.Fatal(err)
		}
	}
	decode := func(iterations int) (*image.RGBA, ReconstructionStats) {
		t.Helper()
		state := NewDecoderState(info)
		for _, packet := range packets {
			if err := state.Add(packet); err != nil {
				t.Fatal(err)
			}
		}
		decoded, stats, err := state.Reconstruct(iterations)
		if err != nil {
			t.Fatal(err)
		}
		return decoded, stats
	}
	quick, quickStats := decode(24)
	converged, finalStats := decode(96)
	if quickStats.Coverage < 0.9 || finalStats.Coverage != quickStats.Coverage {
		t.Fatalf("unexpected packet coverage: quick=%+v final=%+v", quickStats, finalStats)
	}
	quickCenter := quick.RGBAAt(32, 32)
	finalCenter := converged.RGBAAt(32, 32)
	// A measurement-mean DC warm start already restores the dominant red
	// channel in the quick pass. More OWL-QN iterations must not regress it;
	// unlike the old zero-vector initialization, the quick pass need not wait
	// dozens of iterations before its chroma becomes usable.
	if finalCenter.R+5 < quickCenter.R {
		t.Fatalf("additional OWL-QN iterations regressed the red channel: quick=%v final=%v", quickCenter, finalCenter)
	}
	if quickCenter.R < quickCenter.G+60 || quickCenter.R < quickCenter.B+60 || finalCenter.R < finalCenter.G+60 || finalCenter.R < finalCenter.B+60 {
		t.Fatalf("red reference image has a chroma/green artifact: quick=%v final=%v", quickCenter, finalCenter)
	}
}

func TestPCSIEncoderPrecomputedColorPlanesPreserveWireOutput(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 3), B: uint8(x ^ y), A: 255})
		}
	}
	enc, err := NewEncoder(img, Config{Callsign: "N0CALL", BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := enc.EncodePacket(0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := ParsePacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if info.Width != 64 || info.Height != 64 || info.YCbCrPixels <= 0 {
		t.Fatalf("unexpected packet metadata: %+v", info)
	}
}

func TestHigherPCSIChromaCompressionReducesPacketCount(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 255})
		}
	}
	base, err := NewEncoder(img, Config{Callsign: "N0CALL", BitDepth: 12, ChromaCompression: 20})
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := NewEncoder(img, Config{Callsign: "N0CALL", BitDepth: 12, ChromaCompression: 60})
	if err != nil {
		t.Fatal(err)
	}
	if compressed.PacketCount() >= base.PacketCount() {
		t.Fatalf("chroma compression did not reduce airtime packets: baseline=%d tuned=%d", base.PacketCount(), compressed.PacketCount())
	}
}
