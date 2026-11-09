package transport

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"math/cmplx"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
)

type satelliteChannelScenario struct {
	ID                  string
	Reference           string
	Model               string
	FrequencySpreadHz   float64
	DifferentialDelayMS float64
	ScintillationS4     float64
	PhaseSigmaRad       float64
	ScintillationRateHz float64
	FixedLossDB         float64
	EchoRelativeDB      float64
	ShadowExceedancePct float64
	OpticalShadowingPct float64
	ElevationDeg        float64
	CarrierGHz          float64
}

var satelliteChannelScenarios = []satelliteChannelScenario{
	{ID: "AWGN", Reference: "QEX baseline", Model: "additive white Gaussian noise"},
	{ID: "QEX_mid_latitude_quiet", Reference: "QEX Table 6 / ITU-R F.1487 concept", Model: "two independent equal-mean Rayleigh paths", FrequencySpreadHz: 0.1, DifferentialDelayMS: 0.5},
	{ID: "QEX_mid_latitude_moderate", Reference: "QEX Table 6 / ITU-R F.1487 concept", Model: "two independent equal-mean Rayleigh paths", FrequencySpreadHz: 0.5, DifferentialDelayMS: 1.0},
	{ID: "QEX_mid_latitude_disturbed", Reference: "QEX Table 6 / ITU-R F.1487 concept", Model: "two independent equal-mean Rayleigh paths", FrequencySpreadHz: 1.0, DifferentialDelayMS: 2.0},
	{ID: "QEX_high_latitude_moderate", Reference: "QEX Table 6 / ITU-R F.1487 concept", Model: "two independent equal-mean Rayleigh paths", FrequencySpreadHz: 10.0, DifferentialDelayMS: 3.0},
	{ID: "ITU_P531_weak_S4_0p2", Reference: "ITU-R P.531-16 §5.1, §5.3-5.4", Model: "Nakagami-m intensity marginal fitted to S4, Gaussian phase proxy, slow fade process", ScintillationS4: 0.2, PhaseSigmaRad: 0.2, ScintillationRateHz: 0.1, CarrierGHz: 0.435},
	{ID: "ITU_P531_moderate_S4_0p45", Reference: "ITU-R P.531-16 §5.1, §5.3-5.4", Model: "Nakagami-m intensity marginal fitted to S4, Gaussian phase proxy, slow fade process", ScintillationS4: 0.45, PhaseSigmaRad: 0.45, ScintillationRateHz: 0.5, CarrierGHz: 0.435},
	{ID: "ITU_P531_strong_S4_0p8", Reference: "ITU-R P.531-16 §5.1, §5.3-5.4", Model: "Nakagami-m intensity marginal fitted to S4, Gaussian phase proxy, slow fade process", ScintillationS4: 0.8, PhaseSigmaRad: 0.8, ScintillationRateHz: 1.0, CarrierGHz: 0.435},
	{ID: "ITU_P681_LMSS_tree_shadow_p10", Reference: "ITU-R P.681-11 §4.1.1 equations (1)-(4)", Model: "1.6 GHz roadside tree-shadow fade quantile at 45 degree elevation, 65 percent optical tree shadow and 10 percent distance exceedance, plus a test-only -9 dB echo", DifferentialDelayMS: 3.0, EchoRelativeDB: -9.0, ShadowExceedancePct: 10, OpticalShadowingPct: 65, ElevationDeg: 45, CarrierGHz: 1.6},
	{ID: "ITU_P618_earth_space_3dB_margin", Reference: "ITU-R P.618-14 (margin context)", Model: "fixed 3 dB Earth-space fade-margin stress point, not a site-specific P.618 prediction", FixedLossDB: 3.0, CarrierGHz: 2.4},
}

func satelliteChannelSNR2500Points(scenario satelliteChannelScenario) []float64 {
	switch {
	case scenario.ShadowExceedancePct > 0:
		// The P.681 roadside-tree p10 fade at 45 degrees is around 24 dB at
		// 1.6 GHz. This axis is pre-shadow channel SNR, so it needs a higher
		// span to show the post-shadow decoder transition.
		return []float64{16, 20, 24, 28, 32, 36, 40}
	case scenario.FixedLossDB > 0:
		return []float64{-4, 0, 4, 8, 12, 16, 20}
	case scenario.FrequencySpreadHz >= 10:
		return []float64{-4, 0, 4, 8, 12, 16, 20}
	default:
		return []float64{-4, 0, 4, 8, 12, 16}
	}
}

var satelliteChannelModes = []domain.TransportMode{
	domain.SSDVAX25, domain.SSDVCCSDS1200, domain.SSDVCCSDS2400,
	domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600,
	domain.PCSICCSDS1200, domain.PCSICCSDS2400,
}

// TestSatelliteChannelSensitivityCharacterization uses the production stream
// encoder and receiver. An empty SATSSDV_CHANNEL_PROFILE runs all eight profiles
// in one invocation; SATSSDV_CHANNEL_TRIALS controls deterministic trials.
func TestSatelliteChannelSensitivityCharacterization(t *testing.T) {
	if os.Getenv("SATSSDV_CHANNEL_TESTS") != "1" {
		t.Skip("set SATSSDV_CHANNEL_TESTS=1 to run the QEX/ITU channel sweep")
	}
	trials := readChannelTrialCount(t)
	profileModes, err := selectedChannelModes(os.Getenv("SATSSDV_CHANNEL_PROFILE"))
	if err != nil {
		t.Fatal(err)
	}
	output := os.Getenv("SATSSDV_CHANNEL_CSV")
	if output == "" {
		output = filepath.Join("reports", "satellite-channel-results.csv")
	}
	writer, closeFile := openChannelCSV(t, output)
	defer closeFile(t, writer)
	for _, mode := range profileModes {
		profile, parseErr := domain.ParseProfile(string(mode))
		if parseErr != nil {
			t.Fatalf("parse profile %s: %v", mode, parseErr)
		}
		packet, wantID := characterizationPacket(t, profile)
		encoder := newChannelTestEncoder(t, mode, profile)
		waveform := encodeChannelFixture(t, encoder, packet, mode)
		for scenarioIndex, scenario := range scenariosForProfile(mode) {
			runChannelScenario(t, writer, mode, profile, waveform, wantID, scenario, scenarioIndex, trials)
		}
	}
	t.Logf("wrote QEX/ITU channel sensitivity CSV to %s at %s", output, time.Now().UTC().Format(time.RFC3339))
}

func readChannelTrialCount(t *testing.T) int {
	t.Helper()
	trials := 6
	if raw := os.Getenv("SATSSDV_CHANNEL_TRIALS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 2 || parsed > 500 {
			t.Fatalf("SATSSDV_CHANNEL_TRIALS must be in 2..500, got %q", raw)
		}
		trials = parsed
	}
	return trials
}

func TestSelectedChannelModesDefaultsToAllProfiles(t *testing.T) {
	modes, err := selectedChannelModes("")
	if err != nil {
		t.Fatalf("select default channel profiles: %v", err)
	}
	if len(modes) != len(satelliteChannelModes) {
		t.Fatalf("default channel sweep selected %d profiles, want %d", len(modes), len(satelliteChannelModes))
	}
	for index, want := range satelliteChannelModes {
		if modes[index] != want {
			t.Fatalf("default profile %d = %q, want %q", index, modes[index], want)
		}
	}
}

func selectedChannelModes(filter string) ([]domain.TransportMode, error) {
	if filter == "" {
		return satelliteChannelModes, nil
	}
	profile, err := domain.ParseProfile(filter)
	if err != nil {
		return nil, err
	}
	return []domain.TransportMode{profile.ID}, nil
}

func openChannelCSV(t *testing.T, path string) (*csv.Writer, func(*testing.T, *csv.Writer)) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create channel report directory: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create channel results CSV: %v", err)
	}
	writer := csv.NewWriter(file)
	header := []string{
		"profile", "modulation", "bit_rate", "sample_rate", "scenario", "reference", "channel_model",
		"frequency_spread_hz", "differential_delay_ms", "scintillation_s4", "phase_sigma_rad", "scintillation_rate_hz",
		"fixed_loss_db", "shadow_exceedance_pct", "optical_shadowing_pct", "elevation_deg", "carrier_ghz",
		"measured_s4", "measured_phase_sigma_rad", "shadow_fade_db",
		"snr_2500_db", "trials", "successes", "probability", "per", "seed_base",
	}
	if err := writer.Write(header); err != nil {
		_ = file.Close()
		t.Fatalf("write channel CSV header: %v", err)
	}
	closeFile := func(test *testing.T, output *csv.Writer) {
		test.Helper()
		output.Flush()
		if err := output.Error(); err != nil {
			test.Errorf("flush channel results CSV: %v", err)
		}
		if err := file.Close(); err != nil {
			test.Errorf("close channel results CSV: %v", err)
		}
	}
	return writer, closeFile
}

func newChannelTestEncoder(t *testing.T, mode domain.TransportMode, profile domain.Profile) *Encoder {
	t.Helper()
	config := Config{Profile: mode, AudioSampleRate: profile.SampleRate, MARYBurstGroupSize: 2}
	if profile.LinkLayer == "AX.25 UI / HDLC" {
		config.Source = ax25.Address{Callsign: "N0CALL"}
		config.Destination = ax25.Address{Callsign: "CQ"}
	}
	encoder, err := NewEncoder(config)
	if err != nil {
		t.Fatalf("%s encoder: %v", mode, err)
	}
	return encoder
}

func encodeChannelFixture(t *testing.T, encoder *Encoder, packet []byte, mode domain.TransportMode) []float32 {
	t.Helper()
	waveform := make([]float32, 0, 2*48000)
	if _, err := encoder.StreamPackets(context.Background(), [][]byte{packet}, func(block []float32) error {
		waveform = append(waveform, block...)
		return nil
	}); err != nil {
		t.Fatalf("%s stream encode: %v", mode, err)
	}
	if len(waveform) == 0 {
		t.Fatalf("%s encoder produced no waveform", mode)
	}
	return waveform
}

func scenariosForProfile(mode domain.TransportMode) []satelliteChannelScenario {
	if mode == domain.PCSICCSDS1200 || mode == domain.PCSICCSDS2400 {
		return satelliteChannelScenarios[:1]
	}
	return satelliteChannelScenarios
}

func runChannelScenario(t *testing.T, writer *csv.Writer, mode domain.TransportMode, profile domain.Profile, waveform []float32, wantID uint16, scenario satelliteChannelScenario, scenarioIndex, trials int) {
	t.Helper()
	snrPoints := satelliteChannelSNR2500Points(scenario)
	passCounts := make([]int, len(snrPoints))
	var measuredS4, measuredPhaseSigma float64
	seedBase := int64(20261109 + int(modeIndex(mode))*1_000_000 + scenarioIndex*10_000)
	for trial := 0; trial < trials; trial++ {
		channelSeed := seedBase + int64(trial)
		channeled, metrics := applySatelliteTestChannel(waveform, profile, scenario, channelSeed)
		measuredS4 += metrics.measuredS4
		measuredPhaseSigma += metrics.phaseSigmaRad
		for snrIndex, snr2500 := range snrPoints {
			noiseSeed := channelSeed + int64(snrIndex*1000) + 500_000
			received := add2500HzEquivalentNoise(channeled, waveform, profile, snr2500, noiseSeed)
			ok, decodeErr := decodeCharacterizationPacket(mode, profile.SampleRate, received, wantID)
			if ok {
				passCounts[snrIndex]++
			} else if decodeErr != "" {
				t.Logf("%s/%s SNR2500=%.1f dB trial=%d: %s", mode, scenario.ID, snr2500, trial, decodeErr)
			}
		}
	}
	writeScenarioRows(t, writer, mode, profile, scenario, snrPoints, passCounts, trials, seedBase, measuredS4/float64(trials), measuredPhaseSigma/float64(trials))
	t.Logf("completed %s / %s (%d trials per SNR point)", mode, scenario.ID, trials)
}

func modeIndex(mode domain.TransportMode) int {
	for index, candidate := range satelliteChannelModes {
		if candidate == mode {
			return index
		}
	}
	return 0
}

func writeScenarioRows(t *testing.T, writer *csv.Writer, mode domain.TransportMode, profile domain.Profile, scenario satelliteChannelScenario, snrPoints []float64, passCounts []int, trials int, seedBase int64, measuredS4, measuredPhaseSigma float64) {
	t.Helper()
	shadowFade := 0.0
	if scenario.ShadowExceedancePct > 0 {
		shadowFade = p681TreeShadowFadeDB(scenario.ShadowExceedancePct, scenario.OpticalShadowingPct, scenario.ElevationDeg, scenario.CarrierGHz)
	}
	for index, snr := range snrPoints {
		probability := float64(passCounts[index]) / float64(trials)
		row := []string{
			string(mode), string(profile.Modulation), strconv.Itoa(profile.BitRate), strconv.Itoa(profile.SampleRate),
			scenario.ID, scenario.Reference, scenario.Model,
			fmt.Sprintf("%.3f", scenario.FrequencySpreadHz), fmt.Sprintf("%.3f", scenario.DifferentialDelayMS),
			fmt.Sprintf("%.3f", scenario.ScintillationS4), fmt.Sprintf("%.3f", scenario.PhaseSigmaRad), fmt.Sprintf("%.3f", scenario.ScintillationRateHz),
			fmt.Sprintf("%.3f", scenario.FixedLossDB), fmt.Sprintf("%.3f", scenario.ShadowExceedancePct),
			fmt.Sprintf("%.3f", scenario.OpticalShadowingPct), fmt.Sprintf("%.3f", scenario.ElevationDeg), fmt.Sprintf("%.3f", scenario.CarrierGHz),
			fmt.Sprintf("%.4f", measuredS4), fmt.Sprintf("%.4f", measuredPhaseSigma), fmt.Sprintf("%.3f", shadowFade),
			fmt.Sprintf("%.1f", snr), strconv.Itoa(trials), strconv.Itoa(passCounts[index]),
			fmt.Sprintf("%.6f", probability), fmt.Sprintf("%.6f", 1-probability), strconv.FormatInt(seedBase, 10),
		}
		if err := writer.Write(row); err != nil {
			t.Fatalf("write channel result row: %v", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatalf("flush channel result rows: %v", err)
	}
}

func TestP681TreeShadowModelUsesProbabilityFractionAndApplicabilityBounds(t *testing.T) {
	got := p681TreeShadowFadeDB(10, 65, 45, 1.6)
	if math.Abs(got-24.46) > 0.15 {
		t.Fatalf("P.681 p10 tree-shadow fade = %.3f dB, want about 24.46 dB", got)
	}
	for _, scenario := range []struct {
		name                              string
		p, coverage, elevation, frequency float64
	}{
		{name: "optical coverage below model range", p: 10, coverage: 54.9, elevation: 45, frequency: 1.6},
		{name: "optical coverage above model range", p: 10, coverage: 75.1, elevation: 45, frequency: 1.6},
		{name: "below LMSS frequency range", p: 10, coverage: 65, elevation: 45, frequency: 0.7},
		{name: "above elevation range", p: 10, coverage: 65, elevation: 60.1, frequency: 1.6},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if value := p681TreeShadowFadeDB(scenario.p, scenario.coverage, scenario.elevation, scenario.frequency); value != 0 {
				t.Fatalf("out-of-scope P.681 scenario returned %.3f dB", value)
			}
		})
	}
	// P.681 clamps the coefficient elevation to 20 degrees below that value,
	// while the overall empirical LMSS profile remains valid from 7 degrees.
	low := p681TreeShadowFadeDB(10, 65, 7, 1.6)
	clamped := p681TreeShadowFadeDB(10, 65, 20, 1.6)
	if math.Abs(low-clamped) > 1e-9 {
		t.Fatalf("7-degree elevation should use 20-degree coefficients: %.3f vs %.3f", low, clamped)
	}
}

func TestNakagamiShapeMappingAndS4Measurement(t *testing.T) {
	if got := nakagamiMFromS4(0.8); got < 1 || got > 10 {
		t.Fatalf("strong-scintillation Nakagami m out of expected range: %.4f", got)
	}
	amplitude := nakagamiAmplitudeProcess(48000, 48000, 0.5, nakagamiMFromS4(0.45), 20261109)
	measured := measureS4(amplitude)
	if math.Abs(measured-0.45) > 0.18 {
		t.Fatalf("generated Nakagami intensity S4=%.3f, target 0.45 ±0.18", measured)
	}
}

type iqSample struct {
	i float64
	q float64
}

type appliedChannelMetrics struct {
	measuredS4    float64
	phaseSigmaRad float64
}

func applySatelliteTestChannel(input []float32, profile domain.Profile, scenario satelliteChannelScenario, seed int64) ([]float32, appliedChannelMetrics) {
	if len(input) == 0 || scenario.ID == "AWGN" {
		return append([]float32(nil), input...), appliedChannelMetrics{}
	}
	centerHz := modemAudioCarrier(profile)
	baseband := downconvertForChannel(input, profile.SampleRate, centerHz)
	metrics := appliedChannelMetrics{}
	switch {
	case scenario.ScintillationS4 > 0:
		metrics = applyScintillation(baseband, profile.SampleRate, scenario, seed)
	case scenario.ShadowExceedancePct > 0:
		applyP681ShadowAndEcho(baseband, profile.SampleRate, scenario, seed)
	case scenario.FrequencySpreadHz > 0:
		applyQEXTwoPath(baseband, profile.SampleRate, scenario, seed)
	}
	if scenario.FixedLossDB > 0 {
		gain := math.Pow(10, -scenario.FixedLossDB/20)
		for index := range baseband {
			baseband[index].i *= gain
			baseband[index].q *= gain
		}
	}
	return upconvertAfterChannel(baseband, profile.SampleRate, centerHz), metrics
}

func modemAudioCarrier(profile domain.Profile) float64 {
	if profile.LinkLayer == "CCSDS-MARY" {
		return float64(profile.CarrierHz)
	}
	if profile.BitRate == 2400 {
		return 1800
	}
	return float64(profile.MarkFrequency+profile.SpaceFrequency) / 2
}

func downconvertForChannel(input []float32, sampleRate int, carrierHz float64) []iqSample {
	iValues := make([]float64, len(input))
	qValues := make([]float64, len(input))
	for index, sample := range input {
		phase := 2 * math.Pi * carrierHz * float64(index) / float64(sampleRate)
		iValues[index] = 2 * float64(sample) * math.Cos(phase)
		qValues[index] = -2 * float64(sample) * math.Sin(phase)
	}
	cutoff := math.Min(2200, float64(sampleRate)/2-150)
	iValues = biquadFilter(biquadFilter(iValues, sampleRate, cutoff, true), sampleRate, cutoff, true)
	qValues = biquadFilter(biquadFilter(qValues, sampleRate, cutoff, true), sampleRate, cutoff, true)
	out := make([]iqSample, len(input))
	for index := range out {
		out[index] = iqSample{i: iValues[index], q: qValues[index]}
	}
	return out
}

func upconvertAfterChannel(input []iqSample, sampleRate int, carrierHz float64) []float32 {
	out := make([]float32, len(input))
	for index, sample := range input {
		phase := 2 * math.Pi * carrierHz * float64(index) / float64(sampleRate)
		out[index] = float32(sample.i*math.Cos(phase) - sample.q*math.Sin(phase))
	}
	return out
}

func applyQEXTwoPath(input []iqSample, sampleRate int, scenario satelliteChannelScenario, seed int64) {
	first := complexRayleighProcess(len(input), sampleRate, scenario.FrequencySpreadHz, seed+1)
	second := complexRayleighProcess(len(input), sampleRate, scenario.FrequencySpreadHz, seed+2)
	delay := int(math.Round(scenario.DifferentialDelayMS * float64(sampleRate) / 1000))
	const equalPathGain = 0.7071067811865476
	rng := rand.New(rand.NewSource(seed + 3))
	phaseRotation := cmplx.Rect(1, rng.Float64()*2*math.Pi)
	source := append([]iqSample(nil), input...)
	for index := range input {
		firstPath := complex(source[index].i, source[index].q) * first[index]
		var secondPath complex128
		if index >= delay {
			previous := source[index-delay]
			secondPath = complex(previous.i, previous.q) * second[index] * phaseRotation
		}
		value := (firstPath + secondPath) * equalPathGain
		input[index] = iqSample{i: real(value), q: imag(value)}
	}
	// QEX Table 6 defines equal-mean paths. Normalize the realization to unit
	// average power so the SNR axis remains the mean input-channel SNR.
	normalizeIQPower(input)
}

func applyScintillation(input []iqSample, sampleRate int, scenario satelliteChannelScenario, seed int64) appliedChannelMetrics {
	m := nakagamiMFromS4(scenario.ScintillationS4)
	amplitude := nakagamiAmplitudeProcess(len(input), sampleRate, scenario.ScintillationRateHz, m, seed+11)
	phase := gaussianFadeProcess(len(input), sampleRate, max(scenario.ScintillationRateHz, 0.1), seed+12)
	for index, sample := range input {
		phaseRotation := cmplx.Rect(1, scenario.PhaseSigmaRad*phase[index])
		value := complex(sample.i, sample.q) * complex(amplitude[index], 0) * phaseRotation
		input[index] = iqSample{i: real(value), q: imag(value)}
	}
	metrics := appliedChannelMetrics{
		measuredS4:    measureS4(amplitude),
		phaseSigmaRad: scenario.PhaseSigmaRad * standardDeviation(phase),
	}
	// Maintain unit mean channel power; measured S4 is recorded before this
	// normalization and is invariant to a constant gain.
	normalizeIQPower(input)
	return metrics
}

func applyP681ShadowAndEcho(input []iqSample, sampleRate int, scenario satelliteChannelScenario, seed int64) {
	echo := complexRayleighProcess(len(input), sampleRate, 0.3, seed+21)
	delay := int(math.Round(scenario.DifferentialDelayMS * float64(sampleRate) / 1000))
	echoGain := math.Pow(10, scenario.EchoRelativeDB/20)
	const losGain = 0.8
	source := append([]iqSample(nil), input...)
	for index, sample := range source {
		value := complex(sample.i, sample.q) * complex(losGain, 0)
		if index >= delay {
			previous := source[index-delay]
			value += complex(previous.i, previous.q) * echo[index] * complex(echoGain, 0)
		}
		input[index] = iqSample{i: real(value), q: imag(value)}
	}
	fadeDB := p681TreeShadowFadeDB(scenario.ShadowExceedancePct, scenario.OpticalShadowingPct, scenario.ElevationDeg, scenario.CarrierGHz)
	gain := math.Pow(10, -fadeDB/20)
	for index := range input {
		input[index].i *= gain
		input[index].q *= gain
	}
}

func p681TreeShadowFadeDB(exceedancePercent, opticalShadowingPct, elevationDeg, frequencyGHz float64) float64 {
	// P.681-11 roadside tree-shadow model: p is a probability fraction in
	// the logarithm, while the CI scenario stores it as a percentage. The
	// empirical model is for LMSS paths with 55..75% optical tree shadowing,
	// 0.8..20 GHz and 7..60 degree elevation. Below 20 degrees, the
	// Recommendation uses the 20-degree coefficient set.
	if exceedancePercent < 1 || exceedancePercent > 80 || opticalShadowingPct < 55 || opticalShadowingPct > 75 || elevationDeg < 7 || elevationDeg > 60 || frequencyGHz < 0.8 || frequencyGHz > 20 {
		return 0
	}
	theta := math.Max(20, elevationDeg)
	m := 3.44 + 0.0975*theta - 0.002*theta*theta
	n := -0.443*theta + 34.76
	p := exceedancePercent / 100
	fade15GHz := -m*math.Log(p) + n
	frequencyFactor := math.Exp(1.5 * (1/math.Sqrt(1.5) - 1/math.Sqrt(frequencyGHz)))
	return fade15GHz * frequencyFactor
}

func nakagamiMFromS4(s4 float64) float64 {
	if s4 <= 0 {
		return 1e6
	}
	if s4 >= 1 {
		return 1
	}
	return math.Exp(5.69*math.Exp(-3.055*s4) + 0.292*math.Exp(0.344*s4))
}

func nakagamiAmplitudeProcess(sampleCount, sampleRate int, fadeRateHz, shape float64, seed int64) []float64 {
	if sampleCount <= 0 {
		return nil
	}
	updateRate := max(4, int(math.Ceil(max(fadeRateHz, 0.1)*16)))
	count := int(math.Ceil(float64(sampleCount)*float64(updateRate)/float64(sampleRate))) + 2
	rng := rand.New(rand.NewSource(seed))
	coarse := make([]float64, count)
	for index := range coarse {
		intensity := gammaSample(rng, shape, 1/shape)
		coarse[index] = math.Sqrt(math.Max(0, intensity))
	}
	out := make([]float64, sampleCount)
	for sample := range out {
		position := float64(sample) * float64(updateRate) / float64(sampleRate)
		index := int(position)
		fraction := position - float64(index)
		out[sample] = coarse[index]*(1-fraction) + coarse[index+1]*fraction
	}
	return out
}

func gammaSample(rng *rand.Rand, shape, scale float64) float64 {
	if shape < 1 {
		return gammaSample(rng, shape+1, scale) * math.Pow(rng.Float64(), 1/shape)
	}
	d := shape - 1.0/3.0
	c := 1 / math.Sqrt(9*d)
	for {
		x := rng.NormFloat64()
		v := 1 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := rng.Float64()
		if u < 1-0.0331*x*x*x*x || math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
			return d * v * scale
		}
	}
}

func measureS4(amplitude []float64) float64 {
	if len(amplitude) == 0 {
		return 0
	}
	var mean, meanSquare float64
	for _, value := range amplitude {
		intensity := value * value
		mean += intensity
		meanSquare += intensity * intensity
	}
	mean /= float64(len(amplitude))
	meanSquare /= float64(len(amplitude))
	if mean <= 0 {
		return 0
	}
	return math.Sqrt(math.Max(0, meanSquare-mean*mean)) / mean
}

func standardDeviation(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var mean, square float64
	for _, value := range values {
		mean += value
		square += value * value
	}
	mean /= float64(len(values))
	variance := square/float64(len(values)) - mean*mean
	if variance < 0 {
		variance = 0
	}
	return math.Sqrt(variance)
}

func complexRayleighProcess(sampleCount, sampleRate int, spreadHz float64, seed int64) []complex128 {
	if sampleCount <= 0 {
		return nil
	}
	if spreadHz <= 0 {
		out := make([]complex128, sampleCount)
		for index := range out {
			out[index] = 1
		}
		return out
	}
	rng := rand.New(rand.NewSource(seed))
	rate := fadeUpdateRate(spreadHz)
	count := int(math.Ceil(float64(sampleCount)*float64(rate)/float64(sampleRate))) + 2
	const components = 24
	frequencies := make([]float64, components)
	phases := make([]float64, components)
	for index := range frequencies {
		frequencies[index] = (rng.Float64() - 0.5) * spreadHz
		phases[index] = rng.Float64() * 2 * math.Pi
	}
	coarse := make([]complex128, count)
	scale := 1 / math.Sqrt(float64(components))
	for index := range coarse {
		timeSec := float64(index) / float64(rate)
		var value complex128
		for component := 0; component < components; component++ {
			phase := 2*math.Pi*frequencies[component]*timeSec + phases[component]
			value += cmplx.Rect(scale, phase)
		}
		coarse[index] = value
	}
	return interpolateComplexProcess(coarse, sampleCount, sampleRate, rate)
}

func gaussianFadeProcess(sampleCount, sampleRate int, spreadHz float64, seed int64) []float64 {
	if sampleCount <= 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(seed))
	rate := fadeUpdateRate(spreadHz)
	count := int(math.Ceil(float64(sampleCount)*float64(rate)/float64(sampleRate))) + 2
	const components = 24
	frequencies := make([]float64, components)
	phases := make([]float64, components)
	for index := range frequencies {
		frequencies[index] = (rng.Float64() - 0.5) * max(spreadHz, 0.1)
		phases[index] = rng.Float64() * 2 * math.Pi
	}
	coarse := make([]float64, count)
	for index := range coarse {
		timeSec := float64(index) / float64(rate)
		for component := 0; component < components; component++ {
			coarse[index] += math.Cos(2*math.Pi*frequencies[component]*timeSec + phases[component])
		}
		coarse[index] /= math.Sqrt(components / 2)
	}
	return interpolateRealProcess(coarse, sampleCount, sampleRate, rate)
}

func fadeUpdateRate(spreadHz float64) int {
	rate := int(math.Ceil(spreadHz * 24))
	return min(512, max(32, rate))
}

func interpolateComplexProcess(coarse []complex128, sampleCount, sampleRate, updateRate int) []complex128 {
	out := make([]complex128, sampleCount)
	for sample := range out {
		out[sample] = complexInterpolate(coarse, float64(sample)*float64(updateRate)/float64(sampleRate))
	}
	return out
}

func complexInterpolate(values []complex128, position float64) complex128 {
	index := int(position)
	fraction := position - float64(index)
	if index+1 >= len(values) {
		return values[len(values)-1]
	}
	return values[index]*(1-complex(fraction, 0)) + values[index+1]*complex(fraction, 0)
}

func interpolateRealProcess(coarse []float64, sampleCount, sampleRate, updateRate int) []float64 {
	out := make([]float64, sampleCount)
	for sample := range out {
		position := float64(sample) * float64(updateRate) / float64(sampleRate)
		index := int(position)
		fraction := position - float64(index)
		if index+1 >= len(coarse) {
			index = len(coarse) - 2
			fraction = 1
		}
		out[sample] = coarse[index]*(1-fraction) + coarse[index+1]*fraction
	}
	return out
}

func normalizeIQPower(samples []iqSample) {
	var power float64
	for _, sample := range samples {
		power += sample.i*sample.i + sample.q*sample.q
	}
	if power <= 0 {
		return
	}
	gain := 1 / math.Sqrt(power/float64(len(samples)))
	for index := range samples {
		samples[index].i *= gain
		samples[index].q *= gain
	}
}

func add2500HzEquivalentNoise(channel, transmitted []float32, profile domain.Profile, snrDB float64, seed int64) []float32 {
	if len(channel) == 0 || len(channel) != len(transmitted) {
		return append([]float32(nil), channel...)
	}
	low, high := modem2500Band(profile)
	rng := rand.New(rand.NewSource(seed))
	noise := make([]float32, len(channel))
	for index := range noise {
		noise[index] = float32(rng.NormFloat64())
	}
	noise = filterNoiseBand(noise, profile.SampleRate, low, high)
	var signalPower, noisePower float64
	for index := range noise {
		signalPower += float64(transmitted[index]) * float64(transmitted[index])
		noisePower += float64(noise[index]) * float64(noise[index])
	}
	if signalPower <= 0 || noisePower <= 0 {
		return append([]float32(nil), channel...)
	}
	scale := math.Sqrt(signalPower/noisePower) / math.Pow(10, snrDB/20)
	out := make([]float32, len(channel))
	for index := range out {
		out[index] = channel[index] + float32(float64(noise[index])*scale)
	}
	return out
}

func modem2500Band(profile domain.Profile) (float64, float64) {
	centre := modemAudioCarrier(profile)
	low, high := centre-1250, centre+1250
	if low < 100 {
		low = 100
	}
	if high > float64(profile.SampleRate)/2-100 {
		high = float64(profile.SampleRate)/2 - 100
	}
	return low, high
}

func filterNoiseBand(input []float32, sampleRate int, lowHz, highHz float64) []float32 {
	filtered := make([]float64, len(input))
	for index, sample := range input {
		filtered[index] = float64(sample)
	}
	filtered = biquadFilter(filtered, sampleRate, lowHz, false)
	filtered = biquadFilter(filtered, sampleRate, highHz, true)
	out := make([]float32, len(filtered))
	for index, sample := range filtered {
		out[index] = float32(sample)
	}
	return out
}

func biquadFilter(input []float64, sampleRate int, cutoff float64, lowPass bool) []float64 {
	w0 := 2 * math.Pi * cutoff / float64(sampleRate)
	cosine, sine := math.Cos(w0), math.Sin(w0)
	alpha := sine / math.Sqrt2
	b0, b1, b2 := 0.0, 0.0, 0.0
	if lowPass {
		b0, b1, b2 = (1-cosine)/2, 1-cosine, (1-cosine)/2
	} else {
		b0, b1, b2 = (1+cosine)/2, -(1 + cosine), (1+cosine)/2
	}
	a0, a1, a2 := 1+alpha, -2*cosine, 1-alpha
	b0, b1, b2, a1, a2 = b0/a0, b1/a0, b2/a0, a1/a0, a2/a0
	out := make([]float64, len(input))
	var x1, x2, y1, y2 float64
	for index, value := range input {
		y := b0*value + b1*x1 + b2*x2 - a1*y1 - a2*y2
		out[index] = y
		x2, x1, y2, y1 = x1, value, y1, y
	}
	return out
}
