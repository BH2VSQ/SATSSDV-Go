// Command bcsi-benchmark produces a reproducible codec comparison report.
// The JPEG/optimized-JPEG/Haar baselines are benchmark references, not new
// radio protocol profiles.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/image/bcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/imagequality"
)

func main() {
	output := flag.String("out", "docs/BCSI_BENCHMARK_REPORT.md", "Markdown report path")
	iterations := flag.Int("iterations", 14, "BCSI reconstruction iterations (1..80)")
	workers := flag.Int("workers", min(8, runtime.GOMAXPROCS(0)), "BCSI reconstruction worker count")
	flag.Parse()
	if *iterations < 1 || *iterations > 80 || *workers < 1 || *workers > 16 {
		fatal("iterations must be 1..80 and workers must be 1..16")
	}
	var report strings.Builder
	fmt.Fprintln(&report, "# BCSI / JPEG / Haar codec benchmark")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- Date: %s\n- Go: %s\n- OS/Arch: %s/%s\n- CPUs visible: %d\n- Reconstruction: %d iterations, %d workers\n- Fixture: deterministic synthetic RGB image (smooth gradients + edges + texture)\n", time.Now().Format("2006-01-02 15:04:05 MST"), runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), *iterations, *workers)
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "All codec sizes include each codec's own headers/CRC/compressed payload. BCSI size is the sum of independent block packets; no existing CCSDS/AX.25 framing, link FEC, interleaving or modem waveform overhead is included. The JPEG target-size row uses Go's standard JPEG encoder with quality selected to approach the BCSI byte target; it is not a separate libjpeg-turbo optimized-Huffman implementation. JPEG and Haar candidates may be smaller/larger than the target when the tested quality/quantizer range cannot hit it exactly. Airtime is ideal payload-only time at 1200/2400 bit/s, not a modem/on-air measurement.")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Same-target-size comparison")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Image | Base luma / effective all-channel measurements | BCSI bytes | PSNR | SSIM | Encode ms | Decode ms | Encode alloc MB | Decode alloc MB | JPEG Q75 bytes / PSNR | Target-size JPEG Q / bytes / PSNR | Haar step / bytes / PSNR | Ideal payload time 1200 / 2400 bit/s |")
	fmt.Fprintln(&report, "|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")

	sizes := []image.Point{{320, 240}, {448, 336}, {640, 480}}
	rates := []float64{0.25, 0.375, 0.50}
	for _, size := range sizes {
		img := fixture(size.X, size.Y)
		for _, rate := range rates {
			cfg := bcsi.Config{SamplingRate: rate, Adaptive: true, QuantStep: 2, Seed: 0x53415453}
			runtime.GC()
			var memBefore, memAfterEncode, memAfterDecode runtime.MemStats
			runtime.ReadMemStats(&memBefore)
			start := time.Now()
			packets, err := bcsi.Encode(img, cfg)
			if err != nil {
				fatal(err.Error())
			}
			encDuration := time.Since(start)
			runtime.ReadMemStats(&memAfterEncode)
			encAllocMB := float64(memAfterEncode.TotalAlloc-memBefore.TotalAlloc) / (1024 * 1024)
			bcsiBytes := 0
			for _, packet := range packets {
				bcsiBytes += len(packet)
			}
			decoder := bcsi.NewDecoder()
			for _, packet := range packets {
				if err := decoder.AddPacket(packet); err != nil {
					fatal(err.Error())
				}
			}
			start = time.Now()
			bcsiImage, stats, err := decoder.Reconstruct(context.Background(), *workers, *iterations)
			decDuration := time.Since(start)
			if err != nil {
				fatal(err.Error())
			}
			runtime.ReadMemStats(&memAfterDecode)
			decAllocMB := float64(memAfterDecode.TotalAlloc-memAfterEncode.TotalAlloc) / (1024 * 1024)
			bcsiPSNR := imagequality.PSNR(img, bcsiImage)
			bcsiSSIM := imagequality.SSIM(img, bcsiImage)

			jpeg75, jpeg75Image, _, _ := encodeJPEG(img, 75)
			jpegOptQuality, jpegOptBytes := nearestJPEGSize(img, bcsiBytes)
			_, jpegOptImage, _, _ := encodeJPEG(img, jpegOptQuality)

			haarQ, haarBytes := nearestHaarSize(img, bcsiBytes)
			haarStart := time.Now()
			haarStream, err := bcsi.EncodeHaarBaseline(img, haarQ)
			haarEnc := time.Since(haarStart)
			if err != nil {
				fatal(err.Error())
			}
			haarStart = time.Now()
			haarImage, err := bcsi.DecodeHaarBaseline(haarStream)
			haarDec := time.Since(haarStart)
			if err != nil {
				fatal(err.Error())
			}
			airtime1200 := float64(bcsiBytes*8) / 1200
			airtime2400 := float64(bcsiBytes*8) / 2400
			fmt.Fprintf(&report, "| %dx%d | %.1f%% / %.1f%% | %d | %.2f dB | %.4f | %.1f | %.1f | %.2f | %.2f | Q75: %d / %.2f dB | Q%d: %d / %.2f dB | step %d: %d / %.2f dB | %.1f s / %.1f s |\n",
				size.X, size.Y, rate*100, stats.SamplingRate*100, bcsiBytes, bcsiPSNR, bcsiSSIM, float64(encDuration.Microseconds())/1000, float64(decDuration.Microseconds())/1000, encAllocMB, decAllocMB,
				len(jpeg75), imagequality.PSNR(img, jpeg75Image), jpegOptQuality, jpegOptBytes, imagequality.PSNR(img, jpegOptImage), haarQ, haarBytes, imagequality.PSNR(img, haarImage), airtime1200, airtime2400)
			fmt.Fprintf(&report, "<!-- Haar encode/decode %d/%d us; BCSI measurements %d; BCSI received blocks %d/%d -->\n", haarEnc.Microseconds(), haarDec.Microseconds(), stats.Measurements, stats.BlocksReconstructed, stats.BlocksExpected)
		}
	}

	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## BCSI packet-loss behavior (320×240, 37.5% base luma rate)")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Random block-packet loss | Blocks received | Blocks independently reconstructed | Blocks concealed | Output coverage | PSNR | SSIM |")
	fmt.Fprintln(&report, "|---:|---:|---:|---:|---:|---:|---:|")
	lossImage := fixture(320, 240)
	lossPackets, err := bcsi.Encode(lossImage, bcsi.Config{SamplingRate: 0.375, Adaptive: true, QuantStep: 2, Seed: 0x53415453})
	if err != nil {
		fatal(err.Error())
	}
	for _, lossPercent := range []int{10, 25, 40} {
		dec := bcsi.NewDecoder()
		for i, packet := range lossPackets {
			if (i*37+11)%100 < lossPercent {
				continue
			}
			if err := dec.AddPacket(packet); err != nil {
				fatal(err.Error())
			}
		}
		preview, stats, err := dec.Reconstruct(context.Background(), *workers, max(4, *iterations/2))
		if err != nil {
			fatal(err.Error())
		}
		fmt.Fprintf(&report, "| %d%% | %d/%d | %d | %d | %.1f%% | %.2f dB | %.4f |\n", lossPercent, stats.BlocksReceived, stats.BlocksExpected, stats.BlocksReconstructed, stats.BlocksConcealed, stats.Coverage*100, imagequality.PSNR(lossImage, preview), imagequality.SSIM(lossImage, preview))
	}

	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Interpretation and limitations")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "- BCSI is a block-local experimental codec. Every block packet carries image geometry, block index, measurement counts, quantizer, deterministic matrix seed and CRC. A missing/corrupt block does not invalidate neighbouring blocks; the preview conceals missing blocks by nearest recovered block. Concealment improves canvas continuity but does not recover the original missing detail.")
	fmt.Fprintln(&report, "- The test input is deterministic synthetic RGB content (gradients, geometric edges and texture), not a photographic corpus. Results must not be generalized to all satellite imagery; repeat with representative user images before selecting production defaults.")
	fmt.Fprintln(&report, "- The Haar baseline is a deliberately simple block Haar + scalar quantization + DEFLATE reference, not JPEG 2000. The target-size JPEG/Haar values are the nearest candidate from the tested quality/step range; actual byte counts are shown because exact rate matching is not always possible.")
	fmt.Fprintln(&report, "- Allocation columns show cumulative Go heap bytes allocated during the respective BCSI encode/decode phase. They are allocation volume, not peak resident set size (RSS) and do not include native audio/GUI allocations.")
	fmt.Fprintln(&report, "- BCSI measurements and reconstruction use floating-point arithmetic. The current prototype prioritizes correctness and packet independence; it is not yet integrated into a radio profile/modem, so on-air compatibility and real RF packet loss are not claimed.")
	fmt.Fprintln(&report, "- Reported encode/decode time is one local run and is workload/CPU dependent. Run this command on Windows, Linux and macOS release builds before making cross-platform performance claims.")

	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fatal(err.Error())
	}
	if err := os.WriteFile(*output, []byte(report.String()), 0o644); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("wrote %s\n", *output)
}

func encodeJPEG(img image.Image, quality int) ([]byte, image.Image, time.Duration, error) {
	var buffer bytes.Buffer
	start := time.Now()
	err := jpeg.Encode(&buffer, img, &jpeg.Options{Quality: quality})
	elapsed := time.Since(start)
	if err != nil {
		return nil, nil, elapsed, err
	}
	decoded, err := jpeg.Decode(bytes.NewReader(buffer.Bytes()))
	return buffer.Bytes(), decoded, elapsed, err
}

func nearestJPEGSize(img image.Image, target int) (int, int) {
	bestQuality, bestSize, bestDiff := 75, 0, int(^uint(0)>>1)
	for q := 1; q <= 100; q++ {
		var buffer bytes.Buffer
		if err := jpeg.Encode(&buffer, img, &jpeg.Options{Quality: q}); err != nil {
			fatal(err.Error())
		}
		diff := abs(buffer.Len() - target)
		if diff < bestDiff {
			bestQuality, bestSize, bestDiff = q, buffer.Len(), diff
		}
	}
	return bestQuality, bestSize
}

func nearestHaarSize(img image.Image, target int) (int, int) {
	bestStep, bestSize, bestDiff := 4, 0, int(^uint(0)>>1)
	for step := 1; step <= 32; step++ {
		data, err := bcsi.EncodeHaarBaseline(img, step)
		if err != nil {
			fatal(err.Error())
		}
		diff := abs(len(data) - target)
		if diff < bestDiff {
			bestStep, bestSize, bestDiff = step, len(data), diff
		}
	}
	return bestStep, bestSize
}

func fixture(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r := uint8((x*3 + y*2) & 255)
			g := uint8((x/3 + y*5) & 255)
			b := uint8((x*2 + y*3 + (x*y)/97) & 255)
			if x%31 < 3 || y%29 < 2 {
				r, g, b = 245, 32, 170
			}
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "bcsi-benchmark:", message)
	os.Exit(1)
}
