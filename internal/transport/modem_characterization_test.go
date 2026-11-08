package transport

import (
    "fmt"
    "image"
    "image/color"
    "math"
    "math/rand"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/BH2VSQ/SATSSDV-Go/internal/domain"
    "github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
    "github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
    "github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
)

var characterizationModes = []domain.TransportMode{
    domain.SSDVAX25, domain.SSDVCCSDS1200, domain.SSDVCCSDS2400,
    domain.PCSICCSDS1200, domain.PCSICCSDS2400,
    domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600,
}
var characterizationSNRs = []float64{30, 24, 18, 12, 9, 6, 3, 0}
var characterizationCFOs = []int{-600, -500, -400, -300, -200, -100, 0, 100, 200, 300, 400, 500, 600}

// TestModeNoiseAndOffsetCharacterization is opt-in because it performs
// hundreds of end-to-end decodes. CI runs it in a dedicated report job.
func TestModeNoiseAndOffsetCharacterization(t *testing.T) {
    if os.Getenv("SATSSDV_CHARACTERIZATION") != "1" {
        t.Skip("set SATSSDV_CHARACTERIZATION=1 to run the full sweep")
    }
    lines := []string{
        "# SATSSDV-Go modem noise and frequency-offset characterization", "",
        "Generated: " + time.Now().UTC().Format(time.RFC3339), "",
        "Synthetic audio-PCM/baseband test only; this is not calibrated RF SNR, Eb/N0, receiver sensitivity or an over-the-air result.", "",
        "AWGN SNR is defined as the generated waveform RMS divided by the injected white-Gaussian-noise RMS over the PCM buffer. Ten deterministic seeds are tested per SNR point; success means exactly one valid packet with the expected packet ID.", "",
        "Constant audio-IF frequency shift is tested from -600 to +600 Hz in 100 Hz steps. Only the contiguous pass interval around zero is counted; isolated successes beyond a failing point are excluded.", "",
        "## Method", "",
        "- SNR points: 30, 24, 18, 12, 9, 6, 3 and 0 dB; ten trials per point.",
        "- CFO points: -600 to +600 Hz in 100 Hz steps.",
        "- Production encoder, streaming decoder, packet parser and integrity checks are used.",
        "- Legacy AX.25/CCSDS modes use their 19.2 kHz native rate; experimental M-ary modes use 48 kHz.", "",
    }

    for modeIndex, mode := range characterizationModes {
        profile, err := domain.ParseProfile(string(mode))
        if err != nil { t.Fatalf("parse %s: %v", mode, err) }
        packet, wantID := characterizationPacket(t, profile)
        sampleRate := profile.SampleRate
        cfg := Config{Profile: mode, AudioSampleRate: sampleRate}
        if profile.LinkLayer == "AX.25 UI / HDLC" {
            cfg.Source = ax25.Address{Callsign: "N0CALL"}
            cfg.Destination = ax25.Address{Callsign: "CQ"}
        }
        encoder, err := NewEncoder(cfg)
        if err != nil { t.Fatalf("%s encoder: %v", mode, err) }
        waveform, err := encoder.EncodePackets([][]byte{packet})
        if err != nil { t.Fatalf("%s encode: %v", mode, err) }
        if profile.LinkLayer == "CCSDS-MARY" {
            if err := dsp.FilterModulatedAudioInPlace(waveform, sampleRate); err != nil { t.Fatalf("%s filter: %v", mode, err) }
        }

        lines = append(lines, "## "+string(mode), "",
            fmt.Sprintf("- Modulation: `%s`; codec: `%s`; link layer: `%s`.", profile.Modulation, profile.ImageCodec, profile.LinkLayer),
            fmt.Sprintf("- Audio rate: %d Hz; bit rate: %d bit/s; symbol rate: %d baud; bits/symbol: %d.", sampleRate, profile.BitRate, profile.SymbolRate, profile.BitsPerSymbol))
        if profile.MarkFrequency != 0 || profile.SpaceFrequency != 0 {
            lines = append(lines, fmt.Sprintf("- FSK tones: mark %d Hz, space %d Hz.", profile.MarkFrequency, profile.SpaceFrequency))
        }
        if profile.CarrierHz != 0 { lines = append(lines, fmt.Sprintf("- M-ary carrier: %d Hz; acquisition word: 96 symbols.", profile.CarrierHz)) }

        lines = append(lines, "", "### AWGN SNR sweep", "", "| Injected SNR | Valid packets | Success |", "|---:|---:|---:|")
        passCounts := make([]int, len(characterizationSNRs))
        for levelIndex, snr := range characterizationSNRs {
            for trial := 0; trial < 10; trial++ {
                seed := int64(20261108 + modeIndex*100000 + levelIndex*100 + trial)
                noisy := addCharacterizationNoise(waveform, snr, seed)
                ok, decodeErr := decodeCharacterizationPacket(mode, sampleRate, noisy, wantID)
                if ok { passCounts[levelIndex]++ } else if decodeErr != "" {
                    t.Logf("%s %.0f dB trial %d: %s", mode, snr, trial, decodeErr)
                }
            }
            lines = append(lines, fmt.Sprintf("| %.0f dB | %d/10 | %.0f%% |", snr, passCounts[levelIndex], float64(passCounts[levelIndex])*10))
            if snr == 30 && passCounts[levelIndex] == 0 { t.Errorf("%s failed every 30 dB baseline trial", mode) }
        }
        threshold := "not reached in tested range"
        higherPointsPass := true
        // SNR grid is descending: once a high point fails, a lower point cannot
        // be reported as a threshold even if its random trials happen to pass.
        for i := 0; i < len(characterizationSNRs); i++ {
            if passCounts[i] < 9 { higherPointsPass = false }
            if higherPointsPass { threshold = fmt.Sprintf("%.0f dB (coarse grid)", characterizationSNRs[i]) }
        }
        lines = append(lines, "", "Lowest tested SNR with at least 9/10 successes at this and every higher point: **"+threshold+"**.", "")

        lines = append(lines, "### Constant audio-IF frequency offset", "", "| Offset | Result |", "|---:|:---:|")
        results := make(map[int]bool, len(characterizationCFOs))
        for _, offset := range characterizationCFOs {
            shifted, shiftErr := dsp.ShiftFrequency(waveform, sampleRate, float64(offset))
            if shiftErr != nil { t.Fatalf("%s shift %+d Hz: %v", mode, offset, shiftErr) }
            ok, decodeErr := decodeCharacterizationPacket(mode, sampleRate, shifted, wantID)
            results[offset] = ok
            label := "FAIL"
            if ok { label = "PASS" } else if decodeErr != "" { t.Logf("%s CFO %+d Hz: %s", mode, offset, decodeErr) }
            lines = append(lines, fmt.Sprintf("| %+d Hz | %s |", offset, label))
            if offset == 0 && !ok { t.Errorf("%s failed zero-offset baseline", mode) }
        }
        negative := contiguousCharacterizationCFO(results, -1)
        positive := contiguousCharacterizationCFO(results, 1)
        symmetric := negative
        if positive < symmetric { symmetric = positive }
        lines = append(lines, "", fmt.Sprintf("Contiguous interval: **-%d / +%d Hz**; symmetric verified interval: **±%d Hz** (100 Hz step).", negative, positive, symmetric), "")
    }

    lines = append(lines,
        "## Limitations", "",
        "The SNR threshold is a coarse tested-grid estimate for one packet fixture and ten deterministic noise seeds per point, not a statistically certified packet-error-rate curve. Refine transitions using multiple payloads, 1 dB steps, at least 100 seeds, realistic gain/filter/clipping variation and recorded receiver audio.", "",
        "The CFO test translates the audio waveform; it does not exercise RF front-end selectivity, oscillator phase noise, fading, multipath, AGC or a complete satellite Doppler trajectory.", "")
    report := "\n" + characterizationJoin(lines)
    t.Log(report)
    if path := os.Getenv("MODEM_REPORT_PATH"); path != "" {
        if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { t.Fatalf("mkdir report: %v", err) }
        if err := os.WriteFile(path, []byte(report), 0o644); err != nil { t.Fatalf("write report: %v", err) }
    }
}

func characterizationPacket(t *testing.T, profile domain.Profile) ([]byte, uint16) {
    t.Helper()
    if profile.ImageCodec != "PCSI-MMT" { return makeTestSSDVPacket(t), 12 }
    img := image.NewRGBA(image.Rect(0, 0, 32, 32))
    for y := 0; y < 32; y++ { for x := 0; x < 32; x++ {
        img.SetRGBA(x, y, color.RGBA{R:uint8(x*7), G:uint8(y*7), B:uint8(x+y), A:255})
    } }
    enc, err := pcsi.NewEncoder(img, pcsi.Config{Callsign:"N0CALL", ImageID:3, BitDepth:12, ChromaCompression:20})
    if err != nil { t.Fatalf("PCSI fixture encoder: %v", err) }
    packet, err := enc.EncodePacket(0)
    if err != nil { t.Fatalf("PCSI fixture packet: %v", err) }
    return packet, 0
}

func addCharacterizationNoise(signal []float32, snrDB float64, seed int64) []float32 {
    var power float64
    for _, v := range signal { power += float64(v)*float64(v) }
    rms := math.Sqrt(power / float64(max(1, len(signal))))
    noiseRMS := rms / math.Pow(10, snrDB/20)
    rng := rand.New(rand.NewSource(seed))
    out := make([]float32, len(signal))
    for i, v := range signal { out[i] = v + float32(rng.NormFloat64()*noiseRMS) }
    return out
}

func decodeCharacterizationPacket(mode domain.TransportMode, sampleRate int, signal []float32, wantID uint16) (bool, string) {
    decoder, err := NewStreamDecoder(mode, sampleRate)
    if err != nil { return false, err.Error() }
    profile, _ := domain.ParseProfile(string(mode))
    input := signal
    if profile.LinkLayer == "CCSDS-MARY" {
        input = make([]float32, 2413+len(signal))
        copy(input[2413:], signal)
    }
    var received []ReceivedPacket
    for offset := 0; offset < len(input); offset += 960 {
        part, feedErr := decoder.Feed(input[offset:min(len(input), offset+960)])
        if feedErr != nil { return false, feedErr.Error() }
        received = append(received, part...)
    }
    return len(received) == 1 && received[0].PacketID == wantID, ""
}

func contiguousCharacterizationCFO(results map[int]bool, direction int) int {
    limit := 0
    for offset := 100; offset <= 600; offset += 100 {
        if !results[direction*offset] { break }
        limit = offset
    }
    return limit
}

func characterizationJoin(lines []string) string {
    out := ""
    for _, line := range lines { out += line + "\n" }
    return out
}
