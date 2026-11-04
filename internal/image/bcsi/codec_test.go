package bcsi

import (
	"context"
	"image"
	"image/color"
	"math"
	"testing"
)

func fixtureImage(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// A smooth field plus sharp geometry and a deterministic texture
			// exercises both low- and high-frequency wavelet coefficients.
			r := uint8((x*3 + y*2) & 255)
			g := uint8((x/3 + y*5) & 255)
			b := uint8((x*7 ^ y*11) & 255)
			if x%31 < 3 || y%29 < 2 {
				r, g, b = 245, 32, 170
			}
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}

func TestHaar2DRoundTrip(t *testing.T) {
	input := [blockPixels]float64{}
	for i := range input {
		input[i] = math.Sin(float64(i)*0.31)*100 + float64(i%7)
	}
	want := input
	haar2D(input[:], false)
	haar2D(input[:], true)
	for i := range input {
		if math.Abs(input[i]-want[i]) > 1e-9 {
			t.Fatalf("Haar round-trip mismatch at %d: got %.12f want %.12f", i, input[i], want[i])
		}
	}
}

func TestLowSequencyHadamardMeasurementAdjoint(t *testing.T) {
	var spatial [blockPixels]float64
	for i := range spatial {
		spatial[i] = float64((i*17)%101) - 50
	}
	const blockIndex, channel, count = 37, 1, 23
	const seed = 0x1234abcd
	measurements := measure(spatial, blockIndex, channel, count, seed)
	rows := sensingRows(blockIndex, channel, seed)
	var residual [blockPixels]float64
	var expected float64
	for i := 0; i < count; i++ {
		v := math.Cos(float64(i) * 0.7)
		residual[rows[i]] = v
		expected += measurements[i] * v
	}
	fwht2D(residual[:])
	perm, signs := sensingPermutation(blockIndex, channel, seed)
	var adjoint [blockPixels]float64
	for i := 0; i < blockPixels; i++ {
		adjoint[perm[i]] = residual[i] * signs[i] / BlockSize
	}
	var actual float64
	for i := range spatial {
		actual += spatial[i] * adjoint[i]
	}
	if math.Abs(actual-expected) > 1e-8 {
		t.Fatalf("measurement operator/adjoint mismatch: got %.12f want %.12f", actual, expected)
	}
}

func TestBlockPacketRoundTripAndCRC(t *testing.T) {
	img := fixtureImage(32, 24)
	cfg := Config{SamplingRate: 0.375, Adaptive: true, QuantStep: 3, Seed: 0x12345678}
	packets, err := Encode(img, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(packets), 12; got != want {
		t.Fatalf("packet count=%d want=%d", got, want)
	}
	block, err := parsePacket(packets[0])
	if err != nil {
		t.Fatal(err)
	}
	if block.info.Width != 32 || block.info.Height != 24 || block.info.BlockIndex != 0 || block.info.Seed != cfg.Seed {
		t.Fatalf("packet header mismatch: %+v", block.info)
	}
	corrupt := append([]byte(nil), packets[0]...)
	corrupt[len(corrupt)/2] ^= 0x80
	if _, err := parsePacket(corrupt); err == nil {
		t.Fatal("expected CRC failure for corrupted packet")
	}
}

func TestProgressiveOutOfOrderAndPacketLoss(t *testing.T) {
	img := fixtureImage(64, 48)
	packets, err := Encode(img, Config{SamplingRate: 0.5, Adaptive: true, QuantStep: 2, Seed: 99})
	if err != nil {
		t.Fatal(err)
	}
	decoder := NewDecoder()
	// Deliberately add only every third block in reverse order. Blocks have no
	// inter-packet prediction dependency, so this is a valid early preview.
	for i := len(packets) - 1; i >= 0; i -= 3 {
		if err := decoder.AddPacket(packets[i]); err != nil {
			t.Fatal(err)
		}
	}
	preview, stats, err := decoder.Reconstruct(context.Background(), 3, 8)
	if err != nil {
		t.Fatal(err)
	}
	if preview == nil || preview.Bounds().Dx() != 64 || preview.Bounds().Dy() != 48 {
		t.Fatalf("invalid progressive preview: %v", preview)
	}
	if stats.BlocksReceived != 16 || stats.BlocksExpected != 48 || stats.BlocksReconstructed != 16 || stats.BlocksConcealed != 32 {
		t.Fatalf("unexpected partial stats: %+v", stats)
	}
	// Fill the missing block records; previously reconstructed blocks are cached.
	for i := range packets {
		if i%3 == 2 {
			continue
		}
		if err := decoder.AddPacket(packets[i]); err != nil {
			t.Fatal(err)
		}
	}
	final, finalStats, err := decoder.Reconstruct(context.Background(), 4, 12)
	if err != nil {
		t.Fatal(err)
	}
	if final == nil || finalStats.BlocksReconstructed != 48 || finalStats.BlocksConcealed != 0 || finalStats.Coverage != 1 {
		t.Fatalf("incomplete final reconstruction: %+v", finalStats)
	}
}

func TestDecoderRejectsMixedImageAndBadConfig(t *testing.T) {
	first, err := Encode(fixtureImage(16, 16), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encode(fixtureImage(24, 16), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	decoder := NewDecoder()
	if err := decoder.AddPacket(first[0]); err != nil {
		t.Fatal(err)
	}
	if err := decoder.AddPacket(second[0]); err == nil {
		t.Fatal("expected mixed-image packet rejection")
	}
	if _, err := Encode(fixtureImage(16, 16), Config{SamplingRate: 0.9}); err == nil {
		t.Fatal("expected sampling rate validation error")
	}
}

func TestDecoderHonorsCancellation(t *testing.T) {
	packets, err := Encode(fixtureImage(64, 48), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	decoder := NewDecoder()
	for _, packet := range packets {
		if err := decoder.AddPacket(packet); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := decoder.Reconstruct(ctx, 2, 20); err == nil {
		t.Fatal("expected canceled reconstruction")
	}
}

func TestRepeatedPacketIsIgnored(t *testing.T) {
	packets, err := Encode(fixtureImage(16, 16), DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	decoder := NewDecoder()
	if err := decoder.AddPacket(packets[0]); err != nil {
		t.Fatal(err)
	}
	before := decoder.Stats()
	if err := decoder.AddPacket(packets[0]); err != nil {
		t.Fatal(err)
	}
	after := decoder.Stats()
	if before.BlocksReceived != after.BlocksReceived || before.EncodedBytes != after.EncodedBytes {
		t.Fatalf("duplicate packet changed decoder state: before=%+v after=%+v", before, after)
	}
}

func TestFullMeasurementsRecover(t *testing.T) {
	var input [blockPixels]float64
	for i := range input {
		input[i] = float64((i*17)%201) - 100
	}
	measurements := measure(input, 17, 0, 64, 123)
	got, err := recoverChannelSeed(context.Background(), measurements[:], 17, 0, 1, 10, 123)
	if err != nil {
		t.Fatal(err)
	}
	var mse float64
	for i := range input {
		d := got[i] - input[i]
		mse += d * d
	}
	mse /= blockPixels
	if mse > 100 {
		t.Fatalf("full measurement reconstruction too inaccurate: mse %.4f", mse)
	}
}

func TestConstantColorBlockRecovery(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 220, G: 40, B: 90, A: 255})
		}
	}
	packets, err := Encode(img, Config{SamplingRate: 0.375, Adaptive: true, QuantStep: 1, Seed: 123})
	if err != nil {
		t.Fatal(err)
	}
	decoder := NewDecoder()
	for _, packet := range packets {
		if err := decoder.AddPacket(packet); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := decoder.Reconstruct(context.Background(), 2, 24)
	if err != nil {
		t.Fatal(err)
	}
	var total float64
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			a := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			b := color.NRGBAModel.Convert(got.At(x, y)).(color.NRGBA)
			dr, dg, db := float64(a.R)-float64(b.R), float64(a.G)-float64(b.G), float64(a.B)-float64(b.B)
			total += dr*dr + dg*dg + db*db
		}
	}
	mse := total / float64(32*32*3)
	if mse > 25 {
		t.Fatalf("constant block reconstruction too inaccurate: MSE %.3f", mse)
	}
}

func TestPooledFlateRoundTrip(t *testing.T) {
	for i := 0; i < 10; i++ {
		src := make([]byte, 100+i)
		for j := range src {
			src[j] = byte((j*i + j/3) & 255)
		}
		z := deflate(src)
		got, err := inflate(z, 1024)
		if err != nil {
			t.Fatalf("iteration %d compressed=%x: %v", i, z, err)
		}
		if string(got) != string(src) {
			t.Fatalf("iteration %d mismatch", i)
		}
	}
}
