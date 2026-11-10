package transport

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
	"math/rand"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
	"github.com/BH2VSQ/SATSSDV-Go/internal/fec"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ccsds"
)

func makeSoftFECTestPackets(t *testing.T, count int) [][]byte {
	t.Helper()
	call, err := ssdv.EncodeBase40("N0CALL")
	if err != nil {
		t.Fatal(err)
	}
	packets := make([][]byte, count)
	for i := 0; i < count; i++ {
		packet := makeTestSSDVPacket(t)
		binary.BigEndian.PutUint32(packet[2:6], call)
		packet[6] = 0x31
		binary.BigEndian.PutUint16(packet[7:9], uint16(100+i))
		binary.BigEndian.PutUint32(packet[220:224], crc32.ChecksumIEEE(packet[1:220]))
		parity, parityErr := fec.RS8Parity(packet[1:224])
		if parityErr != nil {
			t.Fatal(parityErr)
		}
		copy(packet[224:], parity)
		if _, parseErr := ssdv.ParsePacket(packet); parseErr != nil {
			t.Fatalf("packet %d invalid: %v", i, parseErr)
		}
		packets[i] = packet
	}
	return packets
}

func TestSoftFECMARYStreamRoundTripAllConstellations(t *testing.T) {
	packets := makeSoftFECTestPackets(t, 8)
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	for _, profile := range profiles {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			for _, audioRate := range []int{44100, 48000} {
				audioRate := audioRate
				t.Run(fmt.Sprintf("audio-%d", audioRate), func(t *testing.T) {
					enc, err := NewEncoder(Config{Profile: profile, AudioSampleRate: audioRate})
					if err != nil {
						t.Fatal(err)
					}
					var waveform []float32
					count, err := enc.StreamPackets(context.Background(), packets, func(block []float32) error {
						waveform = append(waveform, block...)
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					if count != int64(len(waveform)) {
						t.Fatalf("stream reports %d, got %d samples", count, len(waveform))
					}
					estimate, err := enc.EstimateOutputSamplesForPackets(packets)
					if err != nil {
						t.Fatal(err)
					}
					if estimate != count {
						t.Fatalf("estimated %d samples, actual %d", estimate, count)
					}
					wireCount, err := enc.EstimateWirePacketCount(packets)
					if err != nil {
						t.Fatal(err)
					}
					wantWireCount := map[domain.TransportMode]int{
						domain.SSDVQPSK1600:  8,
						domain.SSDV8PSK1600:  9,
						domain.SSDV16QAM1600: 9,
					}[profile]
					if wireCount != wantWireCount {
						t.Fatalf("physical packet count=%d, want %d for balanced FEC", wireCount, wantWireCount)
					}

					dec, err := NewStreamDecoder(profile, audioRate)
					if err != nil {
						t.Fatal(err)
					}
					// A real audio capture is continuous and includes post-burst noise.
					// Feed a short tail so the RX resampler can emit its final delayed
					// interpolation sample(s) before checking the declared FEC length.
					waveform = append(waveform, make([]float32, audioRate/10)...)
					var got []ReceivedPacket
					for offset := 0; offset < len(waveform); offset += 1920 {
						end := min(len(waveform), offset+1920)
						decoded, feedErr := dec.Feed(waveform[offset:end])
						if feedErr != nil {
							t.Fatalf("RX feed: %v", feedErr)
						}
						got = append(got, decoded...)
					}
					wireGroups := uint64((wireCount + 9) / 10)
					if dec.marySDF3PacketsOK < wireGroups || dec.marySDF3PacketsBad != 0 {
						t.Fatalf("SDF3 receiver state mode=%d groups=%d/%d resync=%d error=%q diag=%+v", dec.maryFECMode, dec.marySDF3PacketsOK, wireGroups, dec.marySDF3Resyncs, dec.marySDF3LastError, dec.Diagnostics())
					}
					if len(got) != len(packets) {
						t.Fatalf("decoded %d/%d image packets", len(got), len(packets))
					}
					seenIDs := make(map[uint16]bool, len(got))
					for _, packet := range got {
						if packet.ImageID != 0x31 {
							t.Fatalf("image ID mismatch: %+v", packet)
						}
						seenIDs[packet.PacketID] = true
					}
					for id := uint16(100); id < 100+uint16(len(packets)); id++ {
						if !seenIDs[id] {
							t.Fatalf("decoded packets missing packet ID %d (seen=%v)", id, seenIDs)
						}
					}
				})
			}
		})
	}
}

func TestSoftFECMARYStreamRoundTripWithAWGN(t *testing.T) {
	packets := makeSoftFECTestPackets(t, 8)
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	for _, profile := range profiles {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			enc, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000})
			if err != nil {
				t.Fatal(err)
			}
			var waveform []float32
			_, err = enc.StreamPackets(context.Background(), packets, func(block []float32) error {
				waveform = append(waveform, block...)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			var power float64
			for _, value := range waveform {
				power += float64(value * value)
			}
			rms := math.Sqrt(power / float64(len(waveform)))
			const snrDB = 18.0
			noiseRMS := rms / math.Pow(10, snrDB/20)
			rng := rand.New(rand.NewSource(0x5eed + int64(len(profile))))
			noisy := make([]float32, 2048, 2048+len(waveform))
			for _, value := range waveform {
				noisy = append(noisy, value+float32(rng.NormFloat64()*noiseRMS))
			}
			dec, err := NewStreamDecoder(profile, 48000)
			if err != nil {
				t.Fatal(err)
			}
			var got []ReceivedPacket
			for offset := 0; offset < len(noisy); offset += 1920 {
				decoded, feedErr := dec.Feed(noisy[offset:min(len(noisy), offset+1920)])
				if feedErr != nil {
					t.Fatalf("RX feed: %v", feedErr)
				}
				got = append(got, decoded...)
			}
			if dec.maryFECError != nil {
				t.Fatalf("soft-FEC decoder: %v", dec.maryFECError)
			}
			if len(got) != len(packets) {
				t.Fatalf("at %.1f dB SNR decoded %d/%d packets: diag=%+v", snrDB, len(got), len(packets), dec.Diagnostics())
			}
		})
	}
}

// TestSoftFECMARYWithAudioCFOAndAWGN exercises the current SDF3 production
// transmitter and streaming receiver together. This deliberately does not use
// EncodePackets, which is retained for legacy-format compatibility testing.
func TestSoftFECMARYWithAudioCFOAndAWGN(t *testing.T) {
	packets := makeSoftFECTestPackets(t, 8)
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	for _, profile := range profiles {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			encoder, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000})
			if err != nil {
				t.Fatal(err)
			}
			var waveform []float32
			_, err = encoder.StreamPackets(context.Background(), packets, func(block []float32) error {
				waveform = append(waveform, block...)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			var power float64
			for _, sample := range waveform {
				power += float64(sample * sample)
			}
			waveRMS := math.Sqrt(power / float64(len(waveform)))
			const snrDB = 18.0
			noiseRMS := waveRMS / math.Pow(10, snrDB/20)

			for _, offsetHz := range []float64{-200, 0, 200} {
				t.Run(fmt.Sprintf("snr-%.0fdB-cfo-%+0.fHz", snrDB, offsetHz), func(t *testing.T) {
					shifted, shiftErr := dsp.ShiftFrequency(waveform, 48000, offsetHz)
					if shiftErr != nil {
						t.Fatal(shiftErr)
					}
					rng := rand.New(rand.NewSource(0x5eed + int64(len(profile))))
					input := make([]float32, 2048, 2048+len(shifted)+4800)
					for _, sample := range shifted {
						input = append(input, sample+float32(rng.NormFloat64()*noiseRMS))
					}
					// Keep the receiver running after PTT drops so the resampler and
					// symbol pipeline can drain the last complete SDF3 unit.
					input = append(input, make([]float32, 4800)...)
					decoder, decodeErr := NewStreamDecoder(profile, 48000)
					if decodeErr != nil {
						t.Fatal(decodeErr)
					}
					var received []ReceivedPacket
					for offset := 0; offset < len(input); offset += 1920 {
						end := min(len(input), offset+1920)
						batch, feedErr := decoder.Feed(input[offset:end])
						if feedErr != nil {
							t.Fatalf("RX feed: %v", feedErr)
						}
						received = append(received, batch...)
					}
					if len(received) != len(packets) {
						t.Fatalf("18 dB SNR / %+0.f Hz CFO decoded %d/%d packets: diag=%+v", offsetHz, len(received), len(packets), decoder.Diagnostics())
					}
					if decoder.marySDF3PacketsBad != 0 || decoder.marySDF3LastError != "" {
						t.Fatalf("SDF3 physical-unit errors: bad=%d last=%q", decoder.marySDF3PacketsBad, decoder.marySDF3LastError)
					}
				})
			}

			// A changing residual offset models the sign-changing portion of a
			// satellite Doppler curve rather than only a fixed tuning error.
			for _, ramp := range [][2]float64{{-150, 150}, {150, -150}} {
				t.Run(fmt.Sprintf("cfo-ramp-%+.0f-to-%+.0fHz", ramp[0], ramp[1]), func(t *testing.T) {
					ramped, rampErr := dsp.ShiftFrequencyRamp(waveform, 48000, ramp[0], ramp[1])
					if rampErr != nil {
						t.Fatal(rampErr)
					}
					input := append(make([]float32, 2048), ramped...)
					input = append(input, make([]float32, 4800)...)
					decoder, decodeErr := NewStreamDecoder(profile, 48000)
					if decodeErr != nil {
						t.Fatal(decodeErr)
					}
					var received []ReceivedPacket
					for offset := 0; offset < len(input); offset += 1920 {
						batch, feedErr := decoder.Feed(input[offset:min(len(input), offset+1920)])
						if feedErr != nil {
							t.Fatalf("RX feed: %v", feedErr)
						}
						received = append(received, batch...)
					}
					if len(received) != len(packets) {
						t.Fatalf("linear CFO drift %+0.f to %+0.f Hz decoded %d/%d packets: diag=%+v", ramp[0], ramp[1], len(received), len(packets), decoder.Diagnostics())
					}
				})
			}
		})
	}
}

// TestSoftFECMARYAcquiresAtLowPCMLevels ensures the preamble detector and
// packetized-burst guard do not impose an absolute audio RMS floor that makes
// low-gain satellite recordings impossible to acquire.
func TestSoftFECMARYAcquiresAtLowPCMLevels(t *testing.T) {
	packets := makeSoftFECTestPackets(t, 8)
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	for _, profile := range profiles {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			encoder, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000})
			if err != nil {
				t.Fatal(err)
			}
			var waveform []float32
			_, err = encoder.StreamPackets(context.Background(), packets, func(block []float32) error {
				waveform = append(waveform, block...)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, gain := range []float32{0.01, 0.003, 0.001} {
				t.Run(fmt.Sprintf("gain-%.3f", gain), func(t *testing.T) {
					input := make([]float32, 2048, 2048+len(waveform)+4800)
					for _, sample := range waveform {
						input = append(input, sample*gain)
					}
					input = append(input, make([]float32, 4800)...)
					decoder, decodeErr := NewStreamDecoder(profile, 48000)
					if decodeErr != nil {
						t.Fatal(decodeErr)
					}
					var received []ReceivedPacket
					for offset := 0; offset < len(input); offset += 1920 {
						batch, feedErr := decoder.Feed(input[offset:min(len(input), offset+1920)])
						if feedErr != nil {
							t.Fatalf("RX feed: %v", feedErr)
						}
						received = append(received, batch...)
					}
					if len(received) != len(packets) {
						t.Fatalf("gain %.3f decoded %d/%d packets: diag=%+v", gain, len(received), len(packets), decoder.Diagnostics())
					}
				})
			}
		})
	}
}

// TestGMSKAFCWaitsForCoherentSignal verifies that idle audio noise cannot
// permanently seed the CCSDS 2400-baud AFC. After 500 ms of noise the AFC must
// remain unlocked, then acquire a shifted GMSK burst and decode its packet.
func TestGMSKAFCWaitsForCoherentSignal(t *testing.T) {
	packet := makeTestSSDVPacket(t)
	for _, offsetHz := range []float64{-300, 300} {
		t.Run(fmt.Sprintf("cfo-%+.0fHz", offsetHz), func(t *testing.T) {
			encoder, err := NewEncoder(Config{Profile: domain.SSDVCCSDS2400, AudioSampleRate: 19200})
			if err != nil {
				t.Fatal(err)
			}
			waveform, err := encoder.EncodePacket(packet)
			if err != nil {
				t.Fatal(err)
			}
			shifted, err := dsp.ShiftFrequency(waveform, 19200, offsetHz)
			if err != nil {
				t.Fatal(err)
			}
			rng := rand.New(rand.NewSource(int64(20261108 + int(offsetHz))))
			input := make([]float32, 19200/2, 19200/2+len(shifted)+19200/10)
			for i := range input {
				input[i] = float32(rng.NormFloat64() * 0.01)
			}
			decoder, err := NewStreamDecoder(domain.SSDVCCSDS2400, 19200)
			if err != nil {
				t.Fatal(err)
			}
			for offset := 0; offset < len(input); offset += 960 {
				if _, err := decoder.Feed(input[offset:min(len(input), offset+960)]); err != nil {
					t.Fatal(err)
				}
			}
			if got := decoder.gmsk2400.CarrierOffsetHz(); got != 0 {
				t.Fatalf("AFC locked to idle noise at %.1f Hz before a signal arrived", got)
			}
			input = append(input[:0], shifted...)
			input = append(input, make([]float32, 19200/10)...)
			var received []ReceivedPacket
			for offset := 0; offset < len(input); offset += 960 {
				batch, feedErr := decoder.Feed(input[offset:min(len(input), offset+960)])
				if feedErr != nil {
					t.Fatal(feedErr)
				}
				received = append(received, batch...)
			}
			if len(received) != 1 || received[0].PacketID != 12 {
				t.Fatalf("GMSK failed after idle noise at %+g Hz: packets=%d AFC=%.1f diagnostics=%+v", offsetHz, len(received), decoder.gmsk2400.CarrierOffsetHz(), decoder.Diagnostics())
			}
			if got := decoder.gmsk2400.CarrierOffsetHz(); math.Abs(got-offsetHz) > 80 {
				t.Fatalf("GMSK AFC estimate %.1f Hz, want %+g Hz ±80 Hz", got, offsetHz)
			}
		})
	}
}

// TestSDF3MARYRecoversWeakPacketAfterStrongPacket exercises a live RX stream
// whose signal level drops sharply between two independently trained SDF3
// bursts. The gap also ends with a callback that contains both signal and
// silence, which must not discard the final symbol(s) of the first burst.
func TestSDF3MARYRecoversWeakPacketAfterStrongPacket(t *testing.T) {
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	packets := makeSoftFECTestPackets(t, 2)
	for _, profile := range profiles {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			encodeOne := func(packet []byte) []float32 {
				t.Helper()
				encoder, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000})
				if err != nil {
					t.Fatal(err)
				}
				var waveform []float32
				if _, err := encoder.StreamPackets(context.Background(), [][]byte{packet}, func(block []float32) error {
					waveform = append(waveform, block...)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				return waveform
			}
			strong := encodeOne(packets[0])
			weak := encodeOne(packets[1])
			input := make([]float32, 2413, 2413+len(strong)+9600+len(weak)+4800)
			input = append(input, strong...)
			input = append(input, make([]float32, 9600)...) // 200 ms inter-burst gap.
			for _, sample := range weak {
				input = append(input, sample*0.003)
			}
			input = append(input, make([]float32, 4800)...)

			decoder, err := NewStreamDecoder(profile, 48000)
			if err != nil {
				t.Fatal(err)
			}
			var received []ReceivedPacket
			for offset := 0; offset < len(input); offset += 960 {
				batch, feedErr := decoder.Feed(input[offset:min(len(input), offset+960)])
				if feedErr != nil {
					t.Fatal(feedErr)
				}
				received = append(received, batch...)
			}
			if len(received) != 2 {
				t.Fatalf("strong-to-weak stream decoded %d/2 packets: diagnostics=%+v SDF3=%d/%d last=%q", len(received), decoder.Diagnostics(), decoder.marySDF3PacketsOK, decoder.marySDF3PacketsBad, decoder.marySDF3LastError)
			}
			if received[0].PacketID != 100 || received[1].PacketID != 101 {
				t.Fatalf("strong-to-weak packet IDs out of order: %d, %d", received[0].PacketID, received[1].PacketID)
			}
		})
	}
}

func TestSDF3MARYReacquiresAfterDroppedPacketAndUsesPacketFEC(t *testing.T) {
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	cases := []struct {
		name       string
		dropUnits  []int
		wantRepair int
	}{
		{name: "one-loss-first-parity-only", dropUnits: []int{1, 9}, wantRepair: 1},
		{name: "one-loss-second-parity-only", dropUnits: []int{1, 8}, wantRepair: 1},
		{name: "two-losses-both-parities", dropUnits: []int{1, 2}, wantRepair: 2},
	}
	for _, profile := range profiles {
		profile := profile
		for _, tc := range cases {
			t.Run(string(profile)+"/"+tc.name, func(t *testing.T) {
				packets := makeSoftFECTestPackets(t, 8)
				enc, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000, MARYBurstGroupSize: 1, MARYFECProfile: MARYFECRobust})
				if err != nil {
					t.Fatal(err)
				}
				var waveform []float32
				if _, err := enc.StreamPackets(context.Background(), packets, func(block []float32) error {
					waveform = append(waveform, block...)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				wirePackets, err := buildMARYWirePackets(packets)
				if err != nil {
					t.Fatal(err)
				}
				interleavedBytes, err := fec.ExpectedInterleavedBytes(4 + ccsds.PayloadLength)
				if err != nil {
					t.Fatal(err)
				}
				profileInfo, _ := domain.ParseProfile(string(profile))
				bits := int64(fec.BurstHeaderWireBits + interleavedBytes*8)
				symbols := int64(maryTrainingSymbols) + (bits+int64(profileInfo.BitsPerSymbol)-1)/int64(profileInfo.BitsPerSymbol)
				unitSamples := (symbols * int64(profileInfo.SampleRate/profileInfo.SymbolRate))
				guardSamples := int64(profileInfo.SampleRate / 5)
				for _, unitIndex := range tc.dropUnits {
					if unitIndex < 0 || unitIndex >= len(wirePackets) {
						t.Fatalf("invalid dropped unit index %d", unitIndex)
					}
					start := int64(unitIndex) * (unitSamples + guardSamples)
					end := start + unitSamples
					if end > int64(len(waveform)) {
						t.Fatalf("drop range %d..%d exceeds waveform %d", start, end, len(waveform))
					}
					for i := int(start); i < int(end); i++ {
						waveform[i] = 0
					}
				}

				dec, err := NewStreamDecoder(profile, 48000)
				if err != nil {
					t.Fatal(err)
				}
				var got []ReceivedPacket
				for offset := 0; offset < len(waveform); offset += 1920 {
					part, feedErr := dec.Feed(waveform[offset:min(offset+1920, len(waveform))])
					if feedErr != nil {
						t.Fatalf("RX feed: %v", feedErr)
					}
					got = append(got, part...)
				}
				if len(got) != len(packets) {
					t.Fatalf("received %d/%d SSDV packets after losses %v: diag=%+v", len(got), len(packets), tc.dropUnits, dec.Diagnostics())
				}
				recovered := 0
				seen := make(map[uint16]bool)
				for _, packet := range got {
					seen[packet.PacketID] = true
					if packet.FECRecovered {
						recovered++
					}
				}
				if recovered != tc.wantRepair {
					t.Fatalf("recovered %d packets, want %d; diag=%+v", recovered, tc.wantRepair, dec.Diagnostics())
				}
				for _, original := range packets {
					id := binary.BigEndian.Uint16(original[7:9])
					if !seen[id] {
						t.Fatalf("packet %d missing after recovery", id)
					}
				}
				if dec.Diagnostics().MARYReacquireCount < 8 {
					t.Fatalf("receiver did not repeatedly reacquire packet preambles: %+v", dec.Diagnostics())
				}
			})
		}
	}
}

func TestPacketErasureRecoveryThroughCCSDSFrameParser(t *testing.T) {
	packets := makeSoftFECTestPackets(t, 8)
	parity, err := fec.BuildPacketParityPayloads(packets)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := NewStreamDecoder(domain.SSDVQPSK1600, 48000)
	if err != nil {
		t.Fatal(err)
	}
	var bits []bool
	for index, packet := range packets {
		if index == 2 || index == 6 {
			continue
		}
		unit, buildErr := ccsds.BuildDataUnit(packet)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		bits = append(bits, bytesAsBits(unit)...)
	}
	for _, shard := range parity {
		unit, buildErr := ccsds.BuildDataUnit(shard)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		bits = append(bits, bytesAsBits(unit)...)
	}
	got := decoder.consumeCCSDSBits(bits)
	if len(got) != 8 {
		t.Fatalf("recovered %d packets, want 8", len(got))
	}
	recovered := 0
	for _, packet := range got {
		if packet.FECRecovered {
			recovered++
		}
	}
	if recovered != 2 {
		t.Fatalf("FEC recovered %d packets, want 2", recovered)
	}
}

func bytesAsBits(data []byte) []bool {
	out := make([]bool, 0, len(data)*8)
	for _, value := range data {
		for shift := 7; shift >= 0; shift-- {
			out = append(out, value&(1<<uint(shift)) != 0)
		}
	}
	return out
}

func TestSoftFECMARYDecoderReacquiresAfterSilence(t *testing.T) {
	first := makeSoftFECTestPackets(t, 8)
	second := makeSoftFECTestPackets(t, 8)
	for i := range second {
		second[i][6] = 0x32
		binary.BigEndian.PutUint16(second[i][7:9], uint16(200+i))
		binary.BigEndian.PutUint32(second[i][220:224], crc32.ChecksumIEEE(second[i][1:220]))
		parity, err := fec.RS8Parity(second[i][1:224])
		if err != nil {
			t.Fatal(err)
		}
		copy(second[i][224:], parity)
	}
	profile := domain.SSDVQPSK1600
	enc, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000, MARYFECProfile: MARYFECRobust})
	if err != nil {
		t.Fatal(err)
	}
	var firstWave, secondWave []float32
	if _, err = enc.StreamPackets(context.Background(), first, func(block []float32) error { firstWave = append(firstWave, block...); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err = enc.StreamPackets(context.Background(), second, func(block []float32) error { secondWave = append(secondWave, block...); return nil }); err != nil {
		t.Fatal(err)
	}
	dec, err := NewStreamDecoder(profile, 48000)
	if err != nil {
		t.Fatal(err)
	}
	var got []ReceivedPacket
	feed := func(samples []float32) {
		t.Helper()
		for offset := 0; offset < len(samples); offset += 1920 {
			part, feedErr := dec.Feed(samples[offset:min(len(samples), offset+1920)])
			if feedErr != nil {
				t.Fatalf("RX feed: %v", feedErr)
			}
			got = append(got, part...)
		}
	}
	feed(firstWave)
	if dec.marySDF3PacketsOK != 1 {
		t.Fatalf("first SDF3 burst decoded %d/2 groups: %+v", dec.marySDF3PacketsOK, dec.Diagnostics())
	}
	noise := make([]float32, 7200)
	noiseSource := rand.New(rand.NewSource(0xface))
	for i := range noise {
		noise[i] = float32(noiseSource.NormFloat64() * 0.008)
	}
	feed(noise) // 150 ms receiver noise floor re-arms acquisition.
	if dec.maryFECMode != -1 {
		t.Fatalf("decoder was not re-armed after silence: mode=%d", dec.maryFECMode)
	}
	feed(secondWave)
	if dec.marySDF3PacketsOK != 2 || dec.marySDF3LastError != "" {
		t.Fatalf("second burst state: groups=%d error=%q diag=%+v", dec.marySDF3PacketsOK, dec.marySDF3LastError, dec.Diagnostics())
	}
	if len(got) != 16 {
		t.Fatalf("received %d packets across two bursts, want 16", len(got))
	}
	seenFirst, seenSecond := 0, 0
	for _, packet := range got {
		if packet.ImageID == 0x31 {
			seenFirst++
		}
		if packet.ImageID == 0x32 {
			seenSecond++
		}
	}
	if seenFirst != 8 || seenSecond != 8 {
		t.Fatalf("burst packets first=%d second=%d", seenFirst, seenSecond)
	}
}

// TestSDF3MARYReacquiresAcrossNoisyGroupGuards verifies that channel noise in
// the 200 ms inter-group guard does not keep the previous carrier lock alive.
// The group boundary is re-armed from the exact SDF3 body length, independent
// of the relative RF/audio noise level in the inserted guard interval.
func TestSDF3MARYReacquiresAcrossNoisyGroupGuards(t *testing.T) {
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	chunkSizes := []int{480, 997, 1920, 4096}
	for _, profile := range profiles {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			packets := makeSoftFECTestPackets(t, 16)
			encoder, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000, MARYBurstGroupSize: 2})
			if err != nil {
				t.Fatal(err)
			}
			var waveform []float32
			if _, err := encoder.StreamPackets(context.Background(), packets, func(block []float32) error {
				waveform = append(waveform, block...)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			var activePower float64
			activeSamples := 0
			for _, sample := range waveform {
				if sample != 0 {
					activePower += float64(sample * sample)
					activeSamples++
				}
			}
			if activeSamples == 0 {
				t.Fatal("encoder produced no active samples")
			}
			activeRMS := math.Sqrt(activePower / float64(activeSamples))
			const snrDB = 6.0
			noiseRMS := activeRMS / math.Pow(10, snrDB/20)
			rng := rand.New(rand.NewSource(0x5344463300 + int64(len(profile))))
			input := make([]float32, 2048, 2048+len(waveform)+9600)
			for _, sample := range waveform {
				input = append(input, sample+float32(rng.NormFloat64()*noiseRMS))
			}
			for i := 0; i < 9600; i++ {
				input = append(input, float32(rng.NormFloat64()*noiseRMS))
			}
			for _, chunkSize := range chunkSizes {
				t.Run(fmt.Sprintf("chunk-%d", chunkSize), func(t *testing.T) {
					decoder, err := NewStreamDecoder(profile, 48000)
					if err != nil {
						t.Fatal(err)
					}
					var received []ReceivedPacket
					for offset := 0; offset < len(input); offset += chunkSize {
						batch, feedErr := decoder.Feed(input[offset:min(len(input), offset+chunkSize)])
						if feedErr != nil {
							t.Fatal(feedErr)
						}
						received = append(received, batch...)
					}
					if len(received) != len(packets) {
						t.Fatalf("at %.0f dB SNR recovered %d/%d packets across noisy group guards; diag=%+v SDF3=%d/%d last=%q", snrDB, len(received), len(packets), decoder.Diagnostics(), decoder.marySDF3PacketsOK, decoder.marySDF3PacketsBad, decoder.marySDF3LastError)
					}
					wireCount, countErr := encoder.EstimateWirePacketCount(packets)
					if countErr != nil {
						t.Fatal(countErr)
					}
					wantGroups := (wireCount + 1) / 2
					if decoder.marySDF3PacketsOK != uint64(wantGroups) {
						t.Fatalf("decoded %d SDF3 groups, want %d two-unit groups for %d wire units: %+v", decoder.marySDF3PacketsOK, wantGroups, wireCount, decoder.Diagnostics())
					}
					if decoder.Diagnostics().MARYReacquireCount < uint64(wantGroups) {
						t.Fatalf("receiver did not reacquire the second group's training word: %+v", decoder.Diagnostics())
					}
				})
			}
		})
	}
}

func TestMARYSmallGroupsReduceProgressivePacketLatency(t *testing.T) {
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	for _, profile := range profiles {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			packets := makeSoftFECTestPackets(t, 10)
			latencySamples := make(map[int]int64, 2)
			for _, groupSize := range []int{1, 10} {
				encoder, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000, MARYBurstGroupSize: groupSize})
				if err != nil {
					t.Fatal(err)
				}
				var waveform []float32
				if _, err := encoder.StreamPackets(context.Background(), packets, func(block []float32) error {
					waveform = append(waveform, block...)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				decoder, err := NewStreamDecoder(profile, 48000)
				if err != nil {
					t.Fatal(err)
				}
				input := make([]float32, 2413, 2413+len(waveform)+4800)
				input = append(input, waveform...)
				input = append(input, make([]float32, 4800)...)
				var consumed int64
				for offset := 0; offset < len(input); offset += 1920 {
					end := min(len(input), offset+1920)
					decoded, feedErr := decoder.Feed(input[offset:end])
					if feedErr != nil {
						t.Fatal(feedErr)
					}
					consumed += int64(end - offset)
					if len(decoded) > 0 {
						latencySamples[groupSize] = consumed
						break
					}
				}
				if latencySamples[groupSize] == 0 {
					t.Fatalf("group size %d did not progressively decode the first packet", groupSize)
				}
			}
			lowLatency := float64(latencySamples[1]) / 48000
			batched := float64(latencySamples[10]) / 48000
			t.Logf("first valid packet latency: group=1 %.3fs; group=10 %.3fs", lowLatency, batched)
			if lowLatency >= batched*0.30 {
				t.Fatalf("single-unit groups did not materially improve first-packet latency: group=1 %.3fs group=10 %.3fs", lowLatency, batched)
			}
		})
	}
}

// TestMARYGIFAudioLoopbackDeliversOnePacketPerFeed verifies the real streamed
// TX -> PCM -> RX path with the same short callback sizes used by audio drivers.
// GIF M-ary transmission uses one CCSDS data unit per SDF3/FEC boundary, so a
// successfully received image packet must be returned immediately rather than
// being held until another image packet arrives.
func TestMARYGIFAudioLoopbackDeliversOnePacketPerFeed(t *testing.T) {
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	for _, profile := range profiles {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			// Eight source packets exercise the outer parity-shard cadence as well
			// as repeated independent SDF3 groups during a longer clock-mismatched
			// soundcard-style transfer.
			packets := makeSoftFECTestPackets(t, 8)
			encoder, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000, MARYBurstGroupSize: 1})
			if err != nil {
				t.Fatal(err)
			}
			var waveform []float32
			if _, err := encoder.StreamPackets(context.Background(), packets, func(block []float32) error {
				waveform = append(waveform, block...)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			waveform = append(waveform, make([]float32, 4800)...)

			wireCount, err := encoder.EstimateWirePacketCount(packets)
			if err != nil {
				t.Fatal(err)
			}
			for _, callbackSamples := range []int{480, 997} {
				t.Run(fmt.Sprintf("callback-%d", callbackSamples), func(t *testing.T) {
					decoder, err := NewStreamDecoder(profile, 48000)
					if err != nil {
						t.Fatal(err)
					}
					seen := make([]uint16, 0, len(packets))
					packetCallbacks := 0
					for offset := 0; offset < len(waveform); offset += callbackSamples {
						end := min(len(waveform), offset+callbackSamples)
						decoded, feedErr := decoder.Feed(waveform[offset:end])
						if feedErr != nil {
							t.Fatalf("PCM loopback Feed: %v", feedErr)
						}
						if len(decoded) > 1 {
							t.Fatalf("one audio callback delivered %d image packets; expected per-packet delivery, diag=%+v", len(decoded), decoder.Diagnostics())
						}
						if len(decoded) == 1 {
							packetCallbacks++
							seen = append(seen, decoded[0].PacketID)
						}
					}
					if len(seen) != len(packets) {
						t.Fatalf("decoded %d/%d source image packets; seen=%v diagnostics=%+v", len(seen), len(packets), seen, decoder.Diagnostics())
					}
					for i, raw := range packets {
						want := binary.BigEndian.Uint16(raw[7:9])
						if seen[i] != want {
							t.Fatalf("packet order[%d]=%d, want %d; seen=%v", i, seen[i], want, seen)
						}
					}
					if packetCallbacks != len(packets) {
						t.Fatalf("packet callbacks=%d, want %d", packetCallbacks, len(packets))
					}
					diag := decoder.Diagnostics()
					if diag.FECChunksBad != 0 || diag.FECChunksOK < uint64(wireCount) {
						t.Fatalf("SDF3/FEC errors during clean PCM loopback: expected at least %d good groups, diagnostics=%+v", wireCount, diag)
					}
				})
			}
		})
	}
}

func resampleForClockOffset(input []float32, ppm float64) []float32 {
	if len(input) < 2 || ppm == 0 {
		return append([]float32(nil), input...)
	}
	step := 1 + ppm*1e-6
	out := make([]float32, 0, int(float64(len(input))/step))
	for position := 0.0; position < float64(len(input)-1); position += step {
		left := int(position)
		fraction := float32(position - float64(left))
		out = append(out, input[left]*(1-fraction)+input[left+1]*fraction)
	}
	return out
}

// TestMARYGIFAudioLoopbackToleratesSoundcardClockOffset exercises the streamed
// modem path with independent playback/capture clocks, realistic callback
// blocks and deterministic audio noise. This is a PCM clock-domain simulation,
// not a measurement of a physical soundcard or RF receiver.
func TestMARYGIFAudioLoopbackToleratesSoundcardClockOffset(t *testing.T) {
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	for _, profile := range profiles {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			// Eight source packets exercise the outer parity-shard cadence as well as
			// repeated independent SDF3 groups during a longer clock-mismatched transfer.
			packets := makeSoftFECTestPackets(t, 8)
			encoder, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000, MARYBurstGroupSize: 1})
			if err != nil {
				t.Fatal(err)
			}
			var waveform []float32
			if _, err := encoder.StreamPackets(context.Background(), packets, func(block []float32) error {
				waveform = append(waveform, block...)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			waveform = append(waveform, make([]float32, 4800)...)
			for _, ppm := range []float64{-100, 100} {
				t.Run(fmt.Sprintf("clock-%+.0fppm", ppm), func(t *testing.T) {
					input := resampleForClockOffset(waveform, ppm)
					var power float64
					active := 0
					for _, sample := range input {
						if math.Abs(float64(sample)) > 1e-6 {
							power += float64(sample * sample)
							active++
						}
					}
					if active == 0 {
						t.Fatal("modulator produced no active samples")
					}
					const snrDB = 24.0
					noiseRMS := math.Sqrt(power/float64(active)) / math.Pow(10, snrDB/20)
					rng := rand.New(rand.NewSource(int64(0x53434c4b) + int64(len(profile))*17 + int64(ppm)))
					for i := range input {
						input[i] += float32(rng.NormFloat64() * noiseRMS)
					}
					decoder, err := NewStreamDecoder(profile, 48000)
					if err != nil {
						t.Fatal(err)
					}
					seen := make([]uint16, 0, len(packets))
					for offset := 0; offset < len(input); offset += 480 {
						decoded, feedErr := decoder.Feed(input[offset:min(offset+480, len(input))])
						if feedErr != nil {
							t.Fatalf("PCM loopback Feed: %v", feedErr)
						}
						if len(decoded) > 1 {
							t.Fatalf("callback emitted %d image packets at once; diagnostics=%+v", len(decoded), decoder.Diagnostics())
						}
						for _, packet := range decoded {
							seen = append(seen, packet.PacketID)
						}
					}
					if len(seen) != len(packets) {
						t.Fatalf("clock %+g ppm at %.1f dB decoded %d/%d packets; seen=%v diagnostics=%+v", ppm, snrDB, len(seen), len(packets), seen, decoder.Diagnostics())
					}
					for i, raw := range packets {
						want := binary.BigEndian.Uint16(raw[7:9])
						if seen[i] != want {
							t.Fatalf("packet order[%d]=%d, want %d", i, seen[i], want)
						}
					}
				})
			}
		})
	}
}

// TestMARYPacketizedHeaderSearchUsesIncrementalOffsets guards the SDF3 parser
// against regressing to a full accumulated-body scan on every Feed callback.

func TestSDF3HeaderScannerResumesAcrossAudioCallbacks(t *testing.T) {
	raw := bytes.Repeat([]byte{0x5a}, 260)
	headerBytes, err := fec.NewPacketBurstHeaderWithProfile(raw, fec.PacketFECProfileRate34).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := fec.RepeatBurstHeader(headerBytes)
	if err != nil {
		t.Fatal(err)
	}
	llrs := make([]float32, 0, len(wire)*8)
	for _, value := range wire {
		for bit := 7; bit >= 0; bit-- {
			if value&(1<<uint(bit)) != 0 {
				llrs = append(llrs, -4)
			} else {
				llrs = append(llrs, 4)
			}
		}
	}
	// Keep the preamble-tail LLR strong enough that a one-bit-early
	// candidate cannot borrow the first header bit and pass the CRC.
	prefix := make([]float32, 900)
	for i := range prefix {
		prefix[i] = -32
	}
	buffer := append([]float32(nil), prefix...)
	cursor := 0
	// Deliberately split the repeated 432-bit descriptor over multiple short
	// callback blocks, mirroring 10-20 ms audio callback delivery.
	for _, cut := range []int{73, 211, 389, len(llrs)} {
		previous := 0
		if cut != 73 {
			for _, prior := range []int{73, 211, 389} {
				if prior < cut && prior > previous {
					previous = prior
				}
			}
		}
		buffer = append(buffer, llrs[previous:cut]...)
		offset, got, ok := findSDF3PacketHeaderFrom(buffer, cursor)
		if cut != len(llrs) {
			if ok {
				t.Fatalf("incomplete SDF3 descriptor accepted at bit %d", cut)
			}
			cursor = len(buffer) - fec.BurstHeaderWireBits + 1
			if cursor < 0 {
				cursor = 0
			}
			continue
		}
		if !ok {
			t.Fatal("complete SDF3 descriptor was not found after the final callback")
		}
		if offset != len(prefix) {
			t.Fatalf("SDF3 header offset=%d, want %d", offset, len(prefix))
		}
		if got.RawBytes != uint32(len(raw)) || got.CodeProfile != fec.PacketFECProfileRate34 {
			t.Fatalf("decoded wrong SDF3 header: %+v", got)
		}
	}
}

func TestMARYPacketizedHeaderSearchUsesIncrementalOffsets(t *testing.T) {
	llrs := make([]float32, 12000)
	for i := range llrs {
		llrs[i] = 1
	}
	for offset := 0; offset < len(llrs)-fec.BurstHeaderWireBits; offset += 37 {
		if _, _, ok := findSDF3PacketHeaderFrom(llrs, offset); ok {
			t.Fatal("random positive LLRs unexpectedly decoded as an SDF3 header")
		}
	}
	// The caller's cursor is advanced over all earlier candidate offsets and
	// keeps only a tail large enough for a cross-callback descriptor.
	cursor := len(llrs) - fec.BurstHeaderWireBits + 1
	if cursor <= 0 || cursor+fec.BurstHeaderWireBits-1 < len(llrs) {
		t.Fatalf("invalid incremental search cursor %d for %d buffered LLRs", cursor, len(llrs))
	}
	if _, _, ok := findSDF3PacketHeaderFrom(llrs, cursor); ok {
		t.Fatal("tail-only scan unexpectedly decoded an SDF3 header")
	}
}

func TestSDF3MARYStreamPublishesEVMSNRDuringReceive(t *testing.T) {
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	for _, profile := range profiles {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			packets := makeSoftFECTestPackets(t, 8)
			encoder, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000})
			if err != nil {
				t.Fatal(err)
			}
			var waveform []float32
			if _, err := encoder.StreamPackets(context.Background(), packets, func(block []float32) error {
				waveform = append(waveform, block...)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			var power float64
			active := 0
			for _, sample := range waveform {
				if sample != 0 {
					power += float64(sample * sample)
					active++
				}
			}
			if active == 0 {
				t.Fatal("encoder produced no active samples")
			}
			const injectedSNRDB = 12.0
			noiseRMS := math.Sqrt(power/float64(active)) / math.Pow(10, injectedSNRDB/20)
			rng := rand.New(rand.NewSource(0x45564d00 + int64(len(profile))))
			input := make([]float32, 2413, 2413+len(waveform)+4800)
			for _, sample := range waveform {
				input = append(input, sample+float32(rng.NormFloat64()*noiseRMS))
			}
			for i := 0; i < 4800; i++ {
				input = append(input, float32(rng.NormFloat64()*noiseRMS))
			}
			decoder, err := NewStreamDecoder(profile, 48000)
			if err != nil {
				t.Fatal(err)
			}
			observed := false
			var measured float64
			for offset := 0; offset < len(input); offset += 960 {
				if _, err := decoder.Feed(input[offset:min(len(input), offset+960)]); err != nil {
					t.Fatal(err)
				}
				diagnostics := decoder.Diagnostics()
				if diagnostics.MARYLocked && diagnostics.MARYSNRValid {
					observed = true
					measured = diagnostics.MARYSNRDB
					if math.IsNaN(measured) || math.IsInf(measured, 0) || measured < 0 || measured > 60 {
						t.Fatalf("invalid live EVM SNR %.2f dB: %+v", measured, diagnostics)
					}
				}
			}
			if !observed {
				t.Fatalf("SDF3 RX never published a synchronized EVM SNR: %+v", decoder.Diagnostics())
			}
			t.Logf("injected full-band audio SNR %.1f dB; observed post-demod EVM SNR %.1f dB", injectedSNRDB, measured)
		})
	}
}

func TestMARYFECZeroValueDefaultsToBalancedProfile(t *testing.T) {
	for _, profile := range []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600} {
		encoder, err := NewEncoder(Config{Profile: profile})
		if err != nil {
			t.Fatal(err)
		}
		code, parity := encoder.maryFECSettings(encoderProfileForTest(t, profile))
		wantCode, wantParity := fec.PacketFECProfileRate23, 1
		switch profile {
		case domain.SSDVQPSK1600:
			wantCode, wantParity = fec.PacketFECProfileRate56, 0
		case domain.SSDV8PSK1600:
			wantCode, wantParity = fec.PacketFECProfileRate34, 1
		}
		if code != wantCode || parity != wantParity {
			t.Fatalf("zero-value %s FEC=%d parity=%d, want balanced profile %d + %d shards", profile, code, parity, wantCode, wantParity)
		}
	}
}

func TestMARYFECExplicitRobustPolicy(t *testing.T) {
	for _, profile := range []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600} {
		encoder, err := NewEncoder(Config{Profile: profile, MARYFECProfile: MARYFECRobust})
		if err != nil {
			t.Fatal(err)
		}
		code, parity := encoder.maryFECSettings(encoderProfileForTest(t, profile))
		if code != fec.PacketFECProfileRate12 || parity != 2 {
			t.Fatalf("explicit robust %s FEC=%d parity=%d, want rate-1/2 + 2 shards", profile, code, parity)
		}
	}
}

func encoderProfileForTest(t *testing.T, profile domain.TransportMode) domain.Profile {
	t.Helper()
	parsed, err := domain.ParseProfile(string(profile))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestSDF3HeaderProbeRecoversAfterBadInitialDescriptor(t *testing.T) {
	decoder, err := NewStreamDecoder(domain.SSDVQPSK1600, 48000)
	if err != nil {
		t.Fatal(err)
	}
	// The first descriptor is intentionally undecodable, as can happen when
	// acquisition begins late or a short fade covers its repeated header.
	badHeader := make([]float32, fec.BurstHeaderWireBits)
	for i := range badHeader {
		badHeader[i] = 8 // all-zero hard decisions, not a valid SDF3 header
	}
	decoder.consumeMARYSoft(badHeader)
	if decoder.maryFECMode != 3 {
		t.Fatalf("after damaged descriptor mode=%d, want legacy-probing", decoder.maryFECMode)
	}

	raw := make([]byte, 260)
	header := fec.NewPacketBurstHeaderWithProfile(raw, fec.PacketFECProfileRate56)
	encoded, err := header.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := fec.RepeatBurstHeader(encoded)
	if err != nil {
		t.Fatal(err)
	}
	llrs := make([]float32, 0, len(wire)*8)
	for _, value := range wire {
		for bit := 7; bit >= 0; bit-- {
			if value&(1<<uint(bit)) != 0 {
				llrs = append(llrs, -16)
			} else {
				llrs = append(llrs, 16)
			}
		}
	}
	// Split the repeated header across arbitrary audio callback boundaries.
	for offset := 0; offset < len(llrs); offset += 17 {
		end := min(offset+17, len(llrs))
		decoder.consumeMARYSoft(llrs[offset:end])
	}
	if decoder.maryFECMode != 2 || decoder.maryPacketHeader == nil {
		t.Fatalf("valid later SDF3 descriptor did not recover packetized mode: %+v", decoder.Diagnostics())
	}
	if decoder.maryPacketHeader.RawBytes != uint32(len(raw)) || decoder.maryPacketHeader.CodeProfile != fec.PacketFECProfileRate56 {
		t.Fatalf("recovered wrong SDF3 descriptor: %+v", *decoder.maryPacketHeader)
	}
	if decoder.marySDF3HeaderRecoveries != 1 {
		t.Fatalf("header recovery count=%d, want 1", decoder.marySDF3HeaderRecoveries)
	}
}
