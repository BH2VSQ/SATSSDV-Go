package transport

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
	"math/rand"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
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
					if wireCount != 10 {
						t.Fatalf("physical packet count=%d, want 8+2 parity", wireCount)
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
					finished := false
					for offset := 0; offset < len(waveform); offset += 1920 {
						end := min(len(waveform), offset+1920)
						decoded, feedErr := dec.Feed(waveform[offset:end])
						if feedErr != nil {
							t.Fatalf("RX feed: %v", feedErr)
						}
						got = append(got, decoded...)
						if dec.maryFECFinished {
							finished = true
							break // leave the completed burst state intact for assertions
						}
					}
					if dec.maryFECMode != 1 || !finished || dec.maryFECError != nil {
						t.Fatalf("soft-FEC receiver state mode=%d finished=%v bits=%d/%d decoded=%d bytes=%d/%d err=%v", dec.maryFECMode, dec.maryFECFinished, dec.maryFECBitsDone, dec.maryFECHeader.CodedBits(), dec.maryFECDecodedBits, dec.maryFECDecodedBytes, dec.maryFECHeader.RawBytes, dec.maryFECError)
					}
					if len(got) != len(packets) {
						t.Fatalf("decoded %d/%d image packets", len(got), len(packets))
					}
					for i, packet := range got {
						if packet.PacketID != uint16(100+i) || packet.ImageID != 0x31 {
							t.Fatalf("packet %d metadata mismatch: %+v", i, packet)
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
				t.Fatalf("at %.1f dB SNR decoded %d/%d packets", snrDB, len(got), len(packets))
			}
		})
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
	enc, err := NewEncoder(Config{Profile: profile, AudioSampleRate: 48000})
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
	if !dec.maryFECFinished {
		t.Fatal("first burst did not finish")
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
	if !dec.maryFECFinished || dec.maryFECError != nil {
		t.Fatalf("second burst state: finished=%v error=%v", dec.maryFECFinished, dec.maryFECError)
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
