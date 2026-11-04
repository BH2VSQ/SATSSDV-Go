package transport

import (
	"image"
	"image/color"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
)

// This test is an audio-IF offset sweep, not an RF hardware Doppler test.
// A constant shift is applied to the generated passband waveform before the
// production streaming receiver. 0 Hz and all offsets within ±500 Hz are
// required to pass; the wider sweep characterizes the acquisition envelope.
func TestDopplerAudioOffsetToleranceSweep(t *testing.T) {
	profiles := []domain.TransportMode{
		domain.SSDVAX25,
		domain.SSDVCCSDS1200,
		domain.SSDVCCSDS2400,
		domain.PCSICCSDS1200,
		domain.PCSICCSDS2400,
	}
	offsets := []int{-1500, -1400, -1300, -1200, -1100, -1000, -900, -800, -700, -600, -500, -400, -300, -200, -100, 0, 100, 200, 300, 400, 500, 600, 700, 800, 900, 1000, 1100, 1200, 1300, 1400, 1500}
	for _, profile := range profiles {
		t.Run(string(profile), func(t *testing.T) {
			payload := makeTestSSDVPacket(t)
			if profile == domain.PCSICCSDS1200 || profile == domain.PCSICCSDS2400 {
				img := image.NewRGBA(image.Rect(0, 0, 32, 32))
				for y := 0; y < 32; y++ {
					for x := 0; x < 32; x++ {
						img.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 7), B: uint8(x + y), A: 255})
					}
				}
				pcsiEncoder, encErr := pcsi.NewEncoder(img, pcsi.Config{Callsign: "N0CALL", ImageID: 3, BitDepth: 12, ChromaCompression: 20})
				if encErr != nil {
					t.Fatal(encErr)
				}
				payload, encErr = pcsiEncoder.EncodePacket(0)
				if encErr != nil {
					t.Fatal(encErr)
				}
			}
			cfg := Config{Profile: profile, AudioSampleRate: 19200}
			if profile == domain.SSDVAX25 {
				cfg.Source = ax25.Address{Callsign: "N0CALL"}
				cfg.Destination = ax25.Address{Callsign: "CQ"}
			}
			encoder, err := NewEncoder(cfg)
			if err != nil {
				t.Fatal(err)
			}
			wave, err := encoder.EncodePackets([][]byte{payload})
			if err != nil {
				t.Fatal(err)
			}
			results := make([]string, 0, len(offsets))
			wantPacketID := uint16(12)
			if profile == domain.PCSICCSDS1200 || profile == domain.PCSICCSDS2400 {
				wantPacketID = 0
			}
			for _, offset := range offsets {
				shifted, shiftErr := dsp.ShiftFrequency(wave, 19200, float64(offset))
				if shiftErr != nil {
					t.Fatalf("shift %+d Hz: %v", offset, shiftErr)
				}
				decoder, decoderErr := NewStreamDecoder(profile, 19200)
				if decoderErr != nil {
					t.Fatal(decoderErr)
				}
				var received []ReceivedPacket
				for start := 0; start < len(shifted); {
					end := start + 1024
					if end > len(shifted) {
						end = len(shifted)
					}
					batch, feedErr := decoder.Feed(shifted[start:end])
					if feedErr != nil {
						t.Fatalf("offset %+d Hz feed: %v", offset, feedErr)
					}
					received = append(received, batch...)
					start = end
				}
				pass := len(received) == 1 && received[0].PacketID == wantPacketID
				if offset == 0 && !pass {
					t.Fatalf("nominal waveform did not decode: got %d packets", len(received))
				}
				if offset >= -500 && offset <= 500 && !pass {
					t.Fatalf("expected audio-IF offset %+d Hz to decode within the verified ±500 Hz range; got %d packets", offset, len(received))
				}
				if pass {
					results = append(results, "+")
				} else {
					results = append(results, "-")
				}
			}
			t.Logf("offsets %v Hz; pass/fail %v", offsets, results)

			// A linear passband drift exercises carrier tracking in both directions
			// across one complete packet burst; this is synthetic audio-IF drift.
			for _, ramp := range [][2]float64{{100, -100}, {-100, 100}} {
				ramped, rampErr := dsp.ShiftFrequencyRamp(wave, 19200, ramp[0], ramp[1])
				if rampErr != nil {
					t.Fatal(rampErr)
				}
				rampDecoder, rampErr := NewStreamDecoder(profile, 19200)
				if rampErr != nil {
					t.Fatal(rampErr)
				}
				var rampReceived []ReceivedPacket
				for start := 0; start < len(ramped); {
					end := start + 1024
					if end > len(ramped) {
						end = len(ramped)
					}
					batch, feedErr := rampDecoder.Feed(ramped[start:end])
					if feedErr != nil {
						t.Fatalf("ramped offset feed: %v", feedErr)
					}
					rampReceived = append(rampReceived, batch...)
					start = end
				}
				if len(rampReceived) != 1 || rampReceived[0].PacketID != wantPacketID {
					t.Fatalf("linear %+g to %+g Hz drift did not decode: got %d packets", ramp[0], ramp[1], len(rampReceived))
				}
			}
		})
	}
}
