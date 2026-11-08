package main

import (
	"math"
	"math/rand"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ccsds"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/mary"
)

func TestEstimateRXAudioSNRRejectsIdleNoiseForAllProfiles(t *testing.T) {
	profiles := []string{
		domain.AutoReceiveProfile,
		string(domain.SSDVAX25),
		string(domain.SSDVCCSDS1200),
		string(domain.SSDVCCSDS2400),
		string(domain.SSDVQPSK1600),
		string(domain.SSDV8PSK1600),
		string(domain.SSDV16QAM1600),
	}
	for seed := int64(0); seed < 30; seed++ {
		rng := rand.New(rand.NewSource(seed + 0x534e52))
		noise := make([]float32, 4096)
		for i := range noise {
			noise[i] = float32(rng.NormFloat64() * 0.01)
		}
		bins, err := dsp.Spectrum(noise, 48000)
		if err != nil {
			t.Fatal(err)
		}
		for _, profile := range profiles {
			got := estimateRXAudioSNR(bins, profile, 48000)
			if got.Valid {
				t.Fatalf("idle white noise seed=%d profile=%s was reported as SNR %.1f dB: %+v", seed, profile, got.SNRDB, got)
			}
		}
	}
}

func TestEstimateRXAudioSNRRejectsColoredIdleNoise(t *testing.T) {
	// First-order low-pass noise approximates the sloped spectrum produced by
	// receiver/audio filtering. An estimator using only a distant upper guard
	// can mistake the passband/guard difference for modem signal power.
	for seed := int64(0); seed < 20; seed++ {
		rng := rand.New(rand.NewSource(seed + 0x434f4c))
		noise := make([]float32, 4096)
		var state float64
		for i := range noise {
			state = 0.18*state + 0.82*rng.NormFloat64()*0.01
			noise[i] = float32(state)
		}
		bins, err := dsp.Spectrum(noise, 48000)
		if err != nil {
			t.Fatal(err)
		}
		for _, profile := range []string{domain.AutoReceiveProfile, string(domain.SSDVAX25), string(domain.SSDVCCSDS1200), string(domain.SSDVCCSDS2400), string(domain.SSDVQPSK1600), string(domain.SSDV8PSK1600), string(domain.SSDV16QAM1600)} {
			got := estimateRXAudioSNR(bins, profile, 48000)
			if got.Valid {
				t.Fatalf("colored idle noise seed=%d profile=%s was reported as SNR %.1f dB: %+v", seed, profile, got.SNRDB, got)
			}
		}
	}
}

func TestEstimateRXAudioSNRGainInvariantForModemLikeSpectrum(t *testing.T) {
	// The stateless estimator is used only as a signal-presence hint during
	// tracker warm-up; its absolute value is not the UI's reported SNR.
	const sampleRate = 48000
	payload := make([]byte, 2048)
	rng := rand.New(rand.NewSource(0x4741494e))
	for i := range payload {
		payload[i] = byte(rng.Intn(256))
	}
	wave, err := mary.ModulateBytes(payload, mary.Config{
		SampleRate: sampleRate, SymbolRate: 1600, BitsPerSymbol: 4, CarrierHz: 1600,
	})
	if err != nil {
		t.Fatal(err)
	}
	const windowSize = 4096
	start := 96 * (sampleRate / 1600)
	segment := append([]float32(nil), wave[start:start+windowSize]...)
	var power float64
	for _, v := range segment {
		power += float64(v) * float64(v)
	}
	signalRMS := math.Sqrt(power / float64(len(segment)))
	noiseRMS := signalRMS / math.Pow(10, 12.0/20)
	for i := range segment {
		segment[i] += float32(rng.NormFloat64() * noiseRMS)
	}
	bins, err := dsp.Spectrum(segment, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	base := estimateRXAudioSNR(bins, string(domain.SSDV16QAM1600), sampleRate)
	if !base.Valid {
		t.Fatalf("modem-shaped burst not recognized during warm-up: %+v", base)
	}
	for i := range segment {
		segment[i] *= 0.1
	}
	bins, err = dsp.Spectrum(segment, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	lowerGain := estimateRXAudioSNR(bins, string(domain.SSDV16QAM1600), sampleRate)
	if !lowerGain.Valid {
		t.Fatalf("gain-scaled modem-shaped burst lost its signal hint: %+v", lowerGain)
	}
}

func makeSNRNoise(samples int, rms float64, seed int64) []float32 {
	rng := rand.New(rand.NewSource(seed))
	out := make([]float32, samples)
	for i := range out {
		out[i] = float32(rng.NormFloat64() * rms)
	}
	return out
}

func feedSNRTracker(t *testing.T, tracker *dsp.BandSNRTracker, samples []float32, profile string, sampleRate int, signalHint bool) dsp.BandSNR {
	t.Helper()
	bins, err := dsp.Spectrum(samples, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	low, high := rxAudioSNRPassband(profile, sampleRate)
	return tracker.Estimate(bins, low, high, signalHint)
}

func trainWhiteSNRTracker(t *testing.T, tracker *dsp.BandSNRTracker, profile string, sampleRate int, noiseRMS float64, seed int64) {
	t.Helper()
	for i := 0; i < 24; i++ {
		got := feedSNRTracker(t, tracker, makeSNRNoise(4096, noiseRMS, seed+int64(i)), profile, sampleRate, false)
		if got.Valid {
			t.Fatalf("noise-learning frame %d was reported as signal: %+v", i, got)
		}
	}
}

func spectralBandPower(t *testing.T, samples []float32, profile string, sampleRate int) float64 {
	t.Helper()
	bins, err := dsp.Spectrum(samples, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	low, high := rxAudioSNRPassband(profile, sampleRate)
	var total float64
	for _, bin := range bins {
		if bin.FrequencyHz >= low && bin.FrequencyHz <= high {
			total += math.Pow(10, bin.MagnitudeDBFS/10)
		}
	}
	return total
}

func TestTrackedRXAudioSNRMeasuresKnownMARYSignals(t *testing.T) {
	const sampleRate = 48000
	for _, tc := range []struct {
		bits    int
		profile domain.TransportMode
	}{{2, domain.SSDVQPSK1600}, {3, domain.SSDV8PSK1600}, {4, domain.SSDV16QAM1600}} {
		t.Run(string(tc.profile), func(t *testing.T) {
			payload := make([]byte, 4096)
			rng := rand.New(rand.NewSource(int64(0x4d415259 + tc.bits)))
			for i := range payload {
				payload[i] = byte(rng.Intn(256))
			}
			wave, err := mary.ModulateBytes(payload, mary.Config{
				SampleRate: sampleRate, SymbolRate: 1600, BitsPerSymbol: tc.bits, CarrierHz: 1600,
			})
			if err != nil {
				t.Fatal(err)
			}
			start := 96 * (sampleRate / 1600)
			segment := append([]float32(nil), wave[start:start+4096]...)
			var power float64
			for _, v := range segment {
				power += float64(v) * float64(v)
			}
			signalRMS := math.Sqrt(power / float64(len(segment)))
			const injectedWidebandSNRDB = 12.0
			noiseRMS := signalRMS / math.Pow(10, injectedWidebandSNRDB/20)
			tracker := &dsp.BandSNRTracker{}
			trainWhiteSNRTracker(t, tracker, string(tc.profile), sampleRate, noiseRMS, int64(0x1000+tc.bits*100))
			noise := makeSNRNoise(len(segment), noiseRMS, int64(0x9000+tc.bits*100))
			expectedSNR := 10 * math.Log10(spectralBandPower(t, segment, string(tc.profile), sampleRate)/spectralBandPower(t, noise, string(tc.profile), sampleRate))
			for i := range segment {
				segment[i] += noise[i]
			}
			hint := estimateRXAudioSNR(mustSpectrumForSNRTest(t, segment, sampleRate), string(tc.profile), sampleRate).Valid
			if !hint {
				t.Fatalf("signal presence hint failed for %s", tc.profile)
			}
			got := feedSNRTracker(t, tracker, segment, string(tc.profile), sampleRate, hint)
			if !got.Valid {
				t.Fatalf("tracked 12 dB MARY signal was not recognized: %+v", got)
			}
			if got.SNRDB < 3 || got.OccupiedBandwidth90Hz < 150 {
				t.Fatalf("invalid tracked MARY estimate: %+v", got)
			}
			if math.Abs(got.SNRDB-expectedSNR) > 2.5 {
				t.Fatalf("tracked MARY SNR %.2f dB differs from directly measured in-channel SNR %.2f dB: %+v", got.SNRDB, expectedSNR, got)
			}
		})
	}
}

func TestTrackedRXAudioSNRMeasuresKnownAFSK1200Signal(t *testing.T) {
	const sampleRate = 19200
	payload := make([]byte, 1024)
	rng := rand.New(rand.NewSource(0x4146534b12))
	for i := range payload {
		payload[i] = byte(rng.Intn(256))
	}
	wave, err := ccsds.Modulate(payload, 1200, 1200, 2200, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	segment := append([]float32(nil), wave[4096:8192]...)
	var power float64
	for _, v := range segment {
		power += float64(v) * float64(v)
	}
	signalRMS := math.Sqrt(power / float64(len(segment)))
	noiseRMS := signalRMS / math.Pow(10, 12.0/20)
	tracker := &dsp.BandSNRTracker{}
	profile := string(domain.SSDVCCSDS1200)
	trainWhiteSNRTracker(t, tracker, profile, sampleRate, noiseRMS, 0x1200)
	noise := makeSNRNoise(len(segment), noiseRMS, 0x1201)
	expectedSNR := 10 * math.Log10(spectralBandPower(t, segment, profile, sampleRate)/spectralBandPower(t, noise, profile, sampleRate))
	for i := range segment {
		segment[i] += noise[i]
	}
	hint := estimateRXAudioSNR(mustSpectrumForSNRTest(t, segment, sampleRate), profile, sampleRate).Valid
	if !hint {
		t.Fatalf("AFSK signal presence hint failed")
	}
	got := feedSNRTracker(t, tracker, segment, profile, sampleRate, hint)
	if !got.Valid {
		t.Fatalf("tracked AFSK signal was not recognized: %+v", got)
	}
	if math.Abs(got.SNRDB-expectedSNR) > 2.5 {
		t.Fatalf("tracked AFSK SNR %.2f dB differs from directly measured in-channel SNR %.2f dB: %+v", got.SNRDB, expectedSNR, got)
	}
}

func TestTrackedRXAudioSNRMeasuresKnownGMSK2400Signal(t *testing.T) {
	const sampleRate = 19200
	payload := make([]byte, 256)
	rng := rand.New(rand.NewSource(0x474d534b))
	for i := range payload {
		payload[i] = byte(rng.Intn(256))
	}
	unit, err := ccsds.BuildDataUnit(payload)
	if err != nil {
		t.Fatal(err)
	}
	wave, err := ccsds.Modulate(unit, 2400, 1200, 2400, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	segment := append([]float32(nil), wave[4096:8192]...)
	var power float64
	for _, v := range segment {
		power += float64(v) * float64(v)
	}
	signalRMS := math.Sqrt(power / float64(len(segment)))
	noiseRMS := signalRMS / math.Pow(10, 12.0/20)
	tracker := &dsp.BandSNRTracker{}
	profile := string(domain.SSDVCCSDS2400)
	trainWhiteSNRTracker(t, tracker, profile, sampleRate, noiseRMS, 0x2400)
	noise := makeSNRNoise(len(segment), noiseRMS, 0x2401)
	expectedSNR := 10 * math.Log10(spectralBandPower(t, segment, profile, sampleRate)/spectralBandPower(t, noise, profile, sampleRate))
	for i := range segment {
		segment[i] += noise[i]
	}
	hint := estimateRXAudioSNR(mustSpectrumForSNRTest(t, segment, sampleRate), profile, sampleRate).Valid
	if !hint {
		t.Fatalf("GMSK signal presence hint failed")
	}
	got := feedSNRTracker(t, tracker, segment, profile, sampleRate, hint)
	if !got.Valid {
		t.Fatalf("tracked GMSK signal was not recognized: %+v", got)
	}
	if math.Abs(got.SNRDB-expectedSNR) > 2.5 {
		t.Fatalf("tracked GMSK SNR %.2f dB differs from directly measured in-channel SNR %.2f dB: %+v", got.SNRDB, expectedSNR, got)
	}
}

func TestBandSNRTrackerDoesNotReportIdleNoiseOrSingleTone(t *testing.T) {
	const sampleRate = 48000
	profile := string(domain.SSDVQPSK1600)
	tracker := &dsp.BandSNRTracker{}
	for i := 0; i < 30; i++ {
		noise := makeSNRNoise(4096, 0.008, int64(0xb000+i))
		got := feedSNRTracker(t, tracker, noise, profile, sampleRate, false)
		if got.Valid {
			t.Fatalf("idle frame %d reported a false SNR: %+v", i, got)
		}
	}
	for i := 0; i < 8; i++ {
		rng := rand.New(rand.NewSource(int64(0x51a0 + i)))
		samples := make([]float32, 4096)
		for n := range samples {
			timeSec := float64(n) / sampleRate
			samples[n] = float32(0.04*math.Sin(2*math.Pi*1600*timeSec) + 0.008*rng.NormFloat64())
		}
		got := feedSNRTracker(t, tracker, samples, profile, sampleRate, false)
		if got.Valid {
			t.Fatalf("single-tone interference frame %d reported a modem SNR: %+v", i, got)
		}
	}
}

func TestBandSNRTrackerRejectsNoiseEvenWithFalsePositiveHint(t *testing.T) {
	const sampleRate = 48000
	profile := string(domain.SSDVQPSK1600)
	tracker := &dsp.BandSNRTracker{}
	trainWhiteSNRTracker(t, tracker, profile, sampleRate, 0.008, 0xc000)
	// Deliberately force the external signal-presence hint true. The tracker
	// must still reject stochastic broadband noise unless its own in-channel
	// excess-power and occupied-bandwidth tests are satisfied.
	for seed := int64(0); seed < 1000; seed++ {
		noise := makeSNRNoise(4096, 0.008, 0xd000+seed)
		got := feedSNRTracker(t, tracker, noise, profile, sampleRate, true)
		if got.Valid {
			t.Fatalf("noise seed=%d was reported as modem SNR with forced hint: %+v", seed, got)
		}
	}
}

func TestTrackedRXAudioSNRIsInvariantToLinearGain(t *testing.T) {
	const sampleRate = 48000
	profile := string(domain.SSDVQPSK1600)
	payload := make([]byte, 2048)
	rng := rand.New(rand.NewSource(0x534e5247))
	for i := range payload {
		payload[i] = byte(rng.Intn(256))
	}
	wave, err := mary.ModulateBytes(payload, mary.Config{SampleRate: sampleRate, SymbolRate: 1600, BitsPerSymbol: 2, CarrierHz: 1600})
	if err != nil {
		t.Fatal(err)
	}
	start := 96 * (sampleRate / 1600)
	signal := append([]float32(nil), wave[start:start+4096]...)
	var signalPower float64
	for _, value := range signal {
		signalPower += float64(value) * float64(value)
	}
	signalRMS := math.Sqrt(signalPower / float64(len(signal)))
	noiseRMS := signalRMS / math.Pow(10, 12.0/20)
	mixedNoise := makeSNRNoise(4096, noiseRMS, 0x534e5232)
	expectedSNR := 10 * math.Log10(spectralBandPower(t, signal, profile, sampleRate)/spectralBandPower(t, mixedNoise, profile, sampleRate))
	for _, gain := range []float32{0.05, 0.2, 0.8} {
		tracker := &dsp.BandSNRTracker{}
		for frame := 0; frame < 24; frame++ {
			noise := makeSNRNoise(4096, noiseRMS, int64(0x5400+frame))
			for i := range noise {
				noise[i] *= gain
			}
			feedSNRTracker(t, tracker, noise, profile, sampleRate, false)
		}
		mixed := make([]float32, len(signal))
		for i := range mixed {
			mixed[i] = (signal[i] + mixedNoise[i]) * gain
		}
		bins, err := dsp.Spectrum(mixed, sampleRate)
		if err != nil {
			t.Fatal(err)
		}
		hint := estimateRXAudioSNR(bins, profile, sampleRate).Valid
		if !hint {
			t.Fatalf("modem signal hint failed at gain %.2f", gain)
		}
		got := feedSNRTracker(t, tracker, mixed, profile, sampleRate, hint)
		if !got.Valid || math.Abs(got.SNRDB-expectedSNR) > 2.5 {
			t.Fatalf("gain %.2f changed tracked SNR unexpectedly: got=%.2f dB want=%.2f dB result=%+v", gain, got.SNRDB, expectedSNR, got)
		}
	}
}

func TestBandSNRTrackerDoesNotLearnAnAlreadyDetectedBurstAsNoise(t *testing.T) {
	const sampleRate = 48000
	profile := string(domain.SSDVQPSK1600)
	payload := make([]byte, 2048)
	rng := rand.New(rand.NewSource(0x51a1))
	for i := range payload {
		payload[i] = byte(rng.Intn(256))
	}
	wave, err := mary.ModulateBytes(payload, mary.Config{SampleRate: sampleRate, SymbolRate: 1600, BitsPerSymbol: 2, CarrierHz: 1600})
	if err != nil {
		t.Fatal(err)
	}
	segment := append([]float32(nil), wave[96*(sampleRate/1600):96*(sampleRate/1600)+4096]...)
	tracker := &dsp.BandSNRTracker{}
	// A strong recognizable signal is present immediately after RX start.
	// The hint must prevent these frames from initializing the idle floor.
	bins, err := dsp.Spectrum(segment, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	low, high := rxAudioSNRPassband(profile, sampleRate)
	for i := 0; i < 3; i++ {
		got := tracker.Estimate(bins, low, high, true)
		if got.Valid || tracker.Ready() {
			t.Fatalf("active-burst warmup contaminated noise profile at frame %d: ready=%t result=%+v", i, tracker.Ready(), got)
		}
	}
	// Once the burst ends, real idle audio can populate the baseline.
	for i := 0; i < 20; i++ {
		got := feedSNRTracker(t, tracker, makeSNRNoise(4096, 0.008, int64(0xa000+i)), profile, sampleRate, false)
		if got.Valid {
			t.Fatalf("idle baseline warmup produced a valid SNR: %+v", got)
		}
	}
	if !tracker.Ready() {
		t.Fatal("noise profile did not warm up after idle audio")
	}
}

func TestEstimateRXAudioSNRRejectsContinuousSingleToneInterference(t *testing.T) {
	const sampleRate = 48000
	for _, toneHz := range []float64{1200, 1600, 1800, 2200} {
		rng := rand.New(rand.NewSource(int64(toneHz)))
		samples := make([]float32, 4096)
		for i := range samples {
			t := float64(i) / sampleRate
			samples[i] = float32(0.03*math.Sin(2*math.Pi*toneHz*t) + 0.001*rng.NormFloat64())
		}
		bins, err := dsp.Spectrum(samples, sampleRate)
		if err != nil {
			t.Fatal(err)
		}
		for _, profile := range []string{domain.AutoReceiveProfile, string(domain.SSDVAX25), string(domain.SSDVCCSDS1200), string(domain.SSDVCCSDS2400), string(domain.SSDVQPSK1600), string(domain.SSDV8PSK1600), string(domain.SSDV16QAM1600)} {
			got := estimateRXAudioSNR(bins, profile, sampleRate)
			if got.Valid {
				t.Errorf("single %0.f Hz interferer was treated as a valid modem signal for %s: %+v", toneHz, profile, got)
			}
		}
	}
}

func TestEstimateRXAudioSNRRecognizesAFSKTonePair(t *testing.T) {
	const sampleRate = 48000
	const n = 4096
	rng := rand.New(rand.NewSource(0x4146534b))
	samples := make([]float32, n)
	for i := range samples {
		t := float64(i) / sampleRate
		samples[i] = float32(0.035*math.Sin(2*math.Pi*1200*t) + 0.035*math.Sin(2*math.Pi*2200*t) + 0.001*rng.NormFloat64())
	}
	bins, err := dsp.Spectrum(samples, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	got := estimateRXAudioSNR(bins, string(domain.SSDVCCSDS1200), sampleRate)
	if !got.Valid || got.EvidenceClusters < 2 {
		t.Fatalf("AFSK mark/space tone pair not recognized: %+v", got)
	}
}

func mustSpectrumForSNRTest(t *testing.T, samples []float32, sampleRate int) []dsp.SpectrumBin {
	t.Helper()
	bins, err := dsp.Spectrum(samples, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	return bins
}

func TestTrackedRXAudioSNRCalibrationSweep(t *testing.T) {
	const sampleRate = 48000
	profiles := []struct {
		bits    int
		profile domain.TransportMode
	}{
		{2, domain.SSDVQPSK1600}, {3, domain.SSDV8PSK1600}, {4, domain.SSDV16QAM1600},
	}
	for _, tc := range profiles {
		t.Run(string(tc.profile), func(t *testing.T) {
			payload := make([]byte, 4096)
			rng := rand.New(rand.NewSource(int64(0x534e5253 + tc.bits)))
			for i := range payload {
				payload[i] = byte(rng.Intn(256))
			}
			wave, err := mary.ModulateBytes(payload, mary.Config{SampleRate: sampleRate, SymbolRate: 1600, BitsPerSymbol: tc.bits, CarrierHz: 1600})
			if err != nil {
				t.Fatal(err)
			}
			start := 96 * (sampleRate / 1600)
			signal := append([]float32(nil), wave[start:start+4096]...)
			for _, target := range []float64{0, 3, 6, 9, 12, 18, 24} {
				seed := int64(0x530000 + int(target)*10 + tc.bits)
				noiseShape := makeSNRNoise(4096, 1, seed)
				signalPower := spectralBandPower(t, signal, string(tc.profile), sampleRate)
				noisePower := spectralBandPower(t, noiseShape, string(tc.profile), sampleRate)
				gain := float32(0.1)
				noiseScale := float64(gain) * math.Sqrt(signalPower/(noisePower*math.Pow(10, target/10)))
				noise := make([]float32, len(signal))
				mixed := make([]float32, len(signal))
				for i := range signal {
					noise[i] = float32(float64(noiseShape[i]) * noiseScale)
					mixed[i] = signal[i]*gain + noise[i]
				}
				expectedSNR := 10 * math.Log10(spectralBandPower(t, scaleSNRSignal(signal, gain), string(tc.profile), sampleRate)/spectralBandPower(t, noise, string(tc.profile), sampleRate))
				tracker := &dsp.BandSNRTracker{}
				for frame := 0; frame < 24; frame++ {
					base := makeSNRNoise(4096, 1, seed+int64(100+frame))
					for i := range base {
						base[i] = float32(float64(base[i]) * noiseScale)
					}
					feedSNRTracker(t, tracker, base, string(tc.profile), sampleRate, false)
				}
				bins, err := dsp.Spectrum(mixed, sampleRate)
				if err != nil {
					t.Fatal(err)
				}
				hint := estimateRXAudioSNR(bins, string(tc.profile), sampleRate).Valid
				got := feedSNRTracker(t, tracker, mixed, string(tc.profile), sampleRate, hint)
				if target == 0 && got.Valid {
					t.Fatalf("noise-dominated %0.0f dB sample unexpectedly displayed SNR (hint=%t): %+v", target, hint, got)
				}
				if target >= 6 {
					if !got.Valid {
						t.Fatalf("%0.0f dB signal not recognized: hint=%t result=%+v", target, hint, got)
					}
					if math.Abs(got.SNRDB-expectedSNR) > 2.5 {
						t.Fatalf("target %0.0f dB: got %.2f dB, measured %.2f dB: %+v", target, got.SNRDB, expectedSNR, got)
					}
				}
				t.Logf("target=%2.0f dB measured=%.2f hint=%t valid=%t estimate=%.2f", target, expectedSNR, hint, got.Valid, got.SNRDB)
			}
		})
	}
}

func scaleSNRSignal(input []float32, gain float32) []float32 {
	out := make([]float32, len(input))
	for i := range input {
		out[i] = input[i] * gain
	}
	return out
}

func makeSNRColoredNoise(samples int, rms float64, seed int64) []float32 {
	rng := rand.New(rand.NewSource(seed))
	out := make([]float32, samples)
	var state, power float64
	for i := range out {
		state = 0.18*state + 0.82*rng.NormFloat64()
		out[i] = float32(state)
		power += state * state
	}
	scale := rms / math.Sqrt(power/float64(max(1, samples)))
	for i := range out {
		out[i] = float32(float64(out[i]) * scale)
	}
	return out
}

func TestTrackedRXAudioSNRMeasuresKnownSignalWithColoredNoise(t *testing.T) {
	const sampleRate = 48000
	profile := string(domain.SSDVQPSK1600)
	payload := make([]byte, 4096)
	rng := rand.New(rand.NewSource(0x434f4c53))
	for i := range payload {
		payload[i] = byte(rng.Intn(256))
	}
	wave, err := mary.ModulateBytes(payload, mary.Config{SampleRate: sampleRate, SymbolRate: 1600, BitsPerSymbol: 2, CarrierHz: 1600})
	if err != nil {
		t.Fatal(err)
	}
	start := 96 * (sampleRate / 1600)
	signal := scaleSNRSignal(wave[start:start+4096], 0.1)
	noiseShape := makeSNRColoredNoise(4096, 1, 0x434f4c30)
	signalPower := spectralBandPower(t, signal, profile, sampleRate)
	noisePower := spectralBandPower(t, noiseShape, profile, sampleRate)
	noiseScale := math.Sqrt(signalPower / (noisePower * math.Pow(10, 12.0/10)))
	tracker := &dsp.BandSNRTracker{}
	for frame := 0; frame < 24; frame++ {
		base := makeSNRColoredNoise(4096, noiseScale, int64(0x434f4c40+frame))
		feedSNRTracker(t, tracker, base, profile, sampleRate, false)
	}
	noise := makeSNRColoredNoise(4096, noiseScale, 0x434f4c70)
	mixed := make([]float32, 4096)
	for i := range mixed {
		mixed[i] = signal[i] + noise[i]
	}
	expected := 10 * math.Log10(spectralBandPower(t, signal, profile, sampleRate)/spectralBandPower(t, noise, profile, sampleRate))
	bins, err := dsp.Spectrum(mixed, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	hint := estimateRXAudioSNR(bins, profile, sampleRate).Valid
	got := feedSNRTracker(t, tracker, mixed, profile, sampleRate, hint)
	if !hint || !got.Valid {
		t.Fatalf("colored-noise modem burst not recognized: hint=%t result=%+v", hint, got)
	}
	if math.Abs(got.SNRDB-expected) > 2.5 {
		t.Fatalf("colored-noise SNR mismatch: got %.2f dB want %.2f dB: %+v", got.SNRDB, expected, got)
	}
}

func TestBandSNRTrackerDoesNotReportNoiseGainStepAsSignal(t *testing.T) {
	const sampleRate = 48000
	profile := string(domain.SSDVQPSK1600)
	for _, forcedHint := range []bool{false, true} {
		tracker := &dsp.BandSNRTracker{}
		trainWhiteSNRTracker(t, tracker, profile, sampleRate, 0.001, 0x6a696e)
		// Simulate a receiver AGC/audio input gain jump while the channel remains
		// idle. The noise level rises by 26 dB across the whole audio spectrum.
		for frame := 0; frame < 40; frame++ {
			noise := makeSNRNoise(4096, 0.02, int64(0x6a7000+frame))
			got := feedSNRTracker(t, tracker, noise, profile, sampleRate, forcedHint)
			if got.Valid || got.SNRDB > 3.0 {
				t.Fatalf("noise gain step (forcedHint=%t) frame %d was reported as a modem signal: %+v", forcedHint, frame, got)
			}
		}
	}
}

func TestBandSNRTrackerRecoversAfterNoiseGainStep(t *testing.T) {
	const sampleRate = 48000
	profile := string(domain.SSDVQPSK1600)
	tracker := &dsp.BandSNRTracker{}
	trainWhiteSNRTracker(t, tracker, profile, sampleRate, 0.001, 0x6761696e)

	// First verify that a receiver/audio gain increase affecting only idle
	// noise is not misreported as an enormous SNR.
	for frame := 0; frame < 40; frame++ {
		noise := makeSNRNoise(4096, 0.02, int64(0x677300+frame))
		got := feedSNRTracker(t, tracker, noise, profile, sampleRate, false)
		if got.Valid {
			t.Fatalf("idle noise after gain step was reported as signal: %+v", got)
		}
	}

	// After the tracker has followed the new idle floor, a real modem signal
	// at the same input gain must produce the correct in-channel SNR again.
	payload := make([]byte, 4096)
	rng := rand.New(rand.NewSource(0x677349))
	for i := range payload {
		payload[i] = byte(rng.Intn(256))
	}
	wave, err := mary.ModulateBytes(payload, mary.Config{
		SampleRate: sampleRate, SymbolRate: 1600, BitsPerSymbol: 2, CarrierHz: 1600,
	})
	if err != nil {
		t.Fatal(err)
	}
	start := 96 * (sampleRate / 1600)
	baseSignal := append([]float32(nil), wave[start:start+4096]...)
	noise := makeSNRNoise(4096, 0.02, 0x67734a)
	baseSignalPower := spectralBandPower(t, baseSignal, profile, sampleRate)
	noisePower := spectralBandPower(t, noise, profile, sampleRate)
	const targetSNRDB = 12.0
	// The post-AGC signal and its noise must both use the new input scale.
	signalGain := math.Sqrt(noisePower * math.Pow(10, targetSNRDB/10) / baseSignalPower)
	signal := scaleSNRSignal(baseSignal, float32(signalGain))
	mixed := make([]float32, len(signal))
	for i := range signal {
		mixed[i] = signal[i] + noise[i]
	}
	expected := 10 * math.Log10(spectralBandPower(t, signal, profile, sampleRate)/noisePower)
	bins := mustSpectrumForSNRTest(t, mixed, sampleRate)
	hint := estimateRXAudioSNR(bins, profile, sampleRate).Valid
	got := feedSNRTracker(t, tracker, mixed, profile, sampleRate, hint)
	if !hint || !got.Valid {
		t.Fatalf("modem signal was not recognized after idle-noise gain adaptation: hint=%t result=%+v", hint, got)
	}
	if math.Abs(got.SNRDB-expected) > 2.5 {
		t.Fatalf("post-AGC SNR %.2f dB differs from measured in-channel SNR %.2f dB: %+v", got.SNRDB, expected, got)
	}
}
