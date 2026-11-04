package transport

import (
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
)

func TestSSDVExperimentalMaryProfilesRoundTrip(t *testing.T) {
	packets, err := ssdv.EncodeImage(roundTripFixtureImage(), ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 0x5a, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	profiles := []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600}
	for _, profile := range profiles {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			enc, err := NewEncoder(Config{Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			wave, err := enc.EncodePackets(packets)
			if err != nil {
				t.Fatal(err)
			}
			standalone, err := DecodeAudio(profile, wave)
			if err != nil {
				t.Fatalf("standalone: %v", err)
			}
			if len(standalone) != len(packets) {
				t.Fatalf("standalone received %d/%d packets", len(standalone), len(packets))
			}
			for _, chunk := range []int{len(wave), 960, 1920, 4096} {
				dec, err := NewStreamDecoder(profile, 48000)
				if err != nil {
					t.Fatal(err)
				}
				var got []ReceivedPacket
				for i := 0; i < len(wave); i += chunk {
					end := i + chunk
					if end > len(wave) {
						end = len(wave)
					}
					part, e := dec.Feed(wave[i:end])
					if e != nil {
						t.Fatal(e)
					}
					got = append(got, part...)
				}
				if len(got) != len(packets) {
					t.Fatalf("chunk %d received %d/%d packets", chunk, len(got), len(packets))
				}
				for i, p := range got {
					if p.PacketID != uint16(i) || p.ImageID != 0x5a {
						t.Fatalf("chunk %d packet %d metadata mismatch: %+v", chunk, i, p)
					}
				}
			}
		})
	}
}
