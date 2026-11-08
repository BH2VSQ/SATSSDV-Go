package transport

import (
	"context"
	"math"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
)

func TestStreamPacketsMatchesWholeWaveformForAllSSDVProfiles(t *testing.T) {
	packets := [][]byte{makeTestSSDVPacket(t), makeTestSSDVPacket(t)}
	profiles := []domain.TransportMode{
		domain.SSDVAX25, domain.SSDVCCSDS1200, domain.SSDVCCSDS2400,
		domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600,
	}
	for _, profile := range profiles {
		t.Run(string(profile), func(t *testing.T) {
			if profile == domain.SSDVQPSK1600 || profile == domain.SSDV8PSK1600 || profile == domain.SSDV16QAM1600 {
				t.Skip("M-ary stream now carries SGF2 inner FEC and packet parity; batch encoder remains legacy SGV1/uncoded")
			}
			cfg := Config{Profile: profile, AudioSampleRate: 44100}
			if profile == domain.SSDVAX25 {
				cfg.Source = ax25.Address{Callsign: "N0CALL", SSID: 1}
				cfg.Destination = ax25.Address{Callsign: "CQ"}
			}
			encoder, err := NewEncoder(cfg)
			if err != nil {
				t.Fatal(err)
			}
			want, err := encoder.EncodePackets(packets)
			if err != nil {
				t.Fatal(err)
			}
			rate, err := encoder.SampleRate()
			if err != nil {
				t.Fatal(err)
			}
			if err := FilterTXWaveformInPlace(profile, want, rate); err != nil {
				t.Fatal(err)
			}

			var got []float32
			count, err := encoder.StreamPackets(context.Background(), packets, func(block []float32) error {
				got = append(got, block...)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if int64(len(got)) != count {
				t.Fatalf("stream reported %d samples, emitted %d", count, len(got))
			}
			if len(got) != len(want) {
				t.Fatalf("stream samples=%d, whole waveform=%d", len(got), len(want))
			}
			maxError := float64(0)
			for i := range want {
				delta := math.Abs(float64(got[i] - want[i]))
				if delta > maxError {
					maxError = delta
				}
			}
			if maxError > 2e-5 {
				for i := range want {
					if math.Abs(float64(got[i]-want[i])) > 2e-5 {
						t.Logf("first mismatch at %d: got %.8f want %.8f", i, got[i], want[i])
						break
					}
				}
				t.Fatalf("stream differs from batch waveform: max abs error %.8f", maxError)
			}
			estimate, err := encoder.EstimateOutputSamplesForPackets(packets)
			if err != nil {
				t.Fatal(err)
			}
			if estimate != count {
				t.Fatalf("estimated sample count=%d, actual=%d", estimate, count)
			}
		})
	}
}

func TestMARYGroupedFECReducesTXAirtimeOverPerFrameSDF3(t *testing.T) {
	packets := makeSoftFECTestPackets(t, 8)
	for _, profile := range []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600} {
		t.Run(string(profile), func(t *testing.T) {
			perFrame, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000, MARYBurstGroupSize: 1})
			if err != nil {
				t.Fatal(err)
			}
			grouped, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000, MARYBurstGroupSize: 10})
			if err != nil {
				t.Fatal(err)
			}
			oldSamples, err := perFrame.EstimateOutputSamplesForPackets(packets)
			if err != nil {
				t.Fatal(err)
			}
			newSamples, err := grouped.EstimateOutputSamplesForPackets(packets)
			if err != nil {
				t.Fatal(err)
			}
			if newSamples >= oldSamples {
				t.Fatalf("grouped SDF3 airtime %d samples did not improve over per-frame %d", newSamples, oldSamples)
			}
			reduction := 100 * (1 - float64(newSamples)/float64(oldSamples))
			if reduction < 15 {
				t.Fatalf("grouping reduced airtime by only %.1f%% (old=%d new=%d)", reduction, oldSamples, newSamples)
			}
			t.Logf("group size 10 reduced estimated airtime by %.1f%%", reduction)
		})
	}
}

func TestBalancedMARYProfilesOutrunCCSDS2400ForEightPackets(t *testing.T) {
	packets := makeSoftFECTestPackets(t, 8)
	ccsdsEncoder, err := NewEncoder(Config{Profile: domain.SSDVCCSDS2400, AudioSampleRate: 48000})
	if err != nil {
		t.Fatal(err)
	}
	ccsdsSamples, err := ccsdsEncoder.EstimateOutputSamplesForPackets(packets)
	if err != nil {
		t.Fatal(err)
	}
	ccsdsSeconds := float64(ccsdsSamples) / 48000
	t.Logf("SSDV CCSDS 2400 baseline: %.3f s for 8 image packets", ccsdsSeconds)

	for _, profile := range []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600} {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			encoder, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000, MARYFECProfile: MARYFECBalanced})
			if err != nil {
				t.Fatal(err)
			}
			count, err := encoder.EstimateOutputSamplesForPackets(packets)
			if err != nil {
				t.Fatal(err)
			}
			seconds := float64(count) / 48000
			throughput := float64(len(packets)*256*8) / seconds
			t.Logf("balanced airtime %.3f s; useful SSDV payload rate %.0f bit/s; improvement vs CCSDS2400 %.1f%%", seconds, throughput, 100*(1-seconds/ccsdsSeconds))
			if seconds >= ccsdsSeconds {
				t.Fatalf("balanced %s still slower than CCSDS 2400: %.3f s >= %.3f s", profile, seconds, ccsdsSeconds)
			}
		})
	}
}

func TestStreamPacketsHonorsCancellationBetweenDataUnits(t *testing.T) {
	packets := [][]byte{makeTestSSDVPacket(t), makeTestSSDVPacket(t), makeTestSSDVPacket(t)}
	encoder, err := NewEncoder(Config{Profile: domain.SSDVCCSDS1200})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	_, err = encoder.StreamPackets(ctx, packets, func(block []float32) error {
		calls++
		if calls == 1 {
			cancel()
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected cancellation")
	}
	if calls == 0 {
		t.Fatal("stream producer did not emit any PCM")
	}
}
