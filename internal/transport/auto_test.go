package transport

import (
	"fmt"
	"testing"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
)

func TestAutoStreamDecoderIdentifiesEverySupportedProfile(t *testing.T) {
	ssdvPackets, err := ssdv.EncodeImage(roundTripFixtureImage(), ssdv.EncodeConfig{Callsign: "N0CALL", ImageID: 0x42, Quality: 4, FEC: true})
	if err != nil {
		t.Fatal(err)
	}
	pcsiImage, err := pcsi.NewEncoder(roundTripFixtureImage(), pcsi.Config{Callsign: "N0CALL", ImageID: 0x43})
	if err != nil {
		t.Fatal(err)
	}
	pcsiPacket, err := pcsiImage.EncodePacket(0)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		profile domain.TransportMode
		packets [][]byte
	}{
		{domain.SSDVAX25, ssdvPackets[:1]},
		{domain.SSDVCCSDS1200, ssdvPackets[:1]},
		{domain.SSDVCCSDS2400, ssdvPackets[:1]},
		{domain.SSDVQPSK1600, ssdvPackets[:1]},
		{domain.SSDV8PSK1600, ssdvPackets[:1]},
		{domain.SSDV16QAM1600, ssdvPackets[:1]},
		{domain.PCSICCSDS1200, [][]byte{pcsiPacket}},
		{domain.PCSICCSDS2400, [][]byte{pcsiPacket}},
	}
	for _, tc := range cases {
		t.Run(string(tc.profile), func(t *testing.T) {
			cfg := Config{Profile: tc.profile, AudioSampleRate: 48000}
			if tc.profile == domain.SSDVAX25 {
				cfg.Source = ax25.Address{Callsign: "N0CALL", SSID: 1}
				cfg.Destination = ax25.Address{Callsign: "CQ"}
			}
			encoder, err := NewEncoder(cfg)
			if err != nil {
				t.Fatal(err)
			}
			wave, err := encoder.EncodePackets(tc.packets)
			if err != nil {
				t.Fatal(err)
			}
			// Include a short receiver-start interval before the actual burst.
			input := make([]float32, 2413)
			input = append(input, wave...)
			auto, err := NewAutoStreamDecoder(48000)
			if err != nil {
				t.Fatal(err)
			}
			var got []ReceivedPacket
			for i := 0; i < len(input); i += 960 {
				end := min(len(input), i+960)
				part, feedErr := auto.Feed(input[i:end])
				if feedErr != nil {
					t.Fatal(feedErr)
				}
				got = append(got, part...)
			}
			if len(got) == 0 {
				t.Fatalf("automatic receiver did not identify profile %s", tc.profile)
			}
			for _, packet := range got {
				if packet.Profile != string(tc.profile) {
					t.Fatalf("auto identified %s, expected %s (%s)", packet.Profile, tc.profile, fmt.Sprintf("%d packets", len(got)))
				}
			}
		})
	}
}

func BenchmarkAutoStreamDecoderIdleAudio(b *testing.B) {
	decoder, err := NewAutoStreamDecoder(48000)
	if err != nil {
		b.Fatal(err)
	}
	block := make([]float32, 480)
	b.ReportAllocs()
	b.SetBytes(int64(len(block) * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := decoder.Feed(block); err != nil {
			b.Fatal(err)
		}
	}
}

func TestAutoReceiverDigitalSilenceGateIsBelowUsableAudio(t *testing.T) {
	if !belowAutoNoiseFloor([]float32{0, 1e-7, -1e-6}) {
		t.Fatal("digital silence should be gated while RX is unlocked")
	}
	if belowAutoNoiseFloor([]float32{0, 1.01e-6, 0}) {
		t.Fatal("audio above the -120 dBFS floor must be passed to acquisition")
	}
}

func TestAutoDiagnosticsRequireRecentlyValidatedPacket(t *testing.T) {
	decoder, err := NewAutoStreamDecoder(48000)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoder.Diagnostics(); got.ValidPacketRecent {
		t.Fatalf("new auto decoder claims a recent packet: %+v", got)
	}
	// Front-end carrier/symbol lock flags are diagnostic only. They must not
	// be conflated with a successfully parsed image packet.
	decoder.lastPacketAt = time.Now().Add(-4 * time.Second)
	if got := decoder.Diagnostics(); got.ValidPacketRecent {
		t.Fatalf("stale packet remained valid for SNR gating: %+v", got)
	}
	decoder.lastPacketAt = time.Now()
	if got := decoder.Diagnostics(); !got.ValidPacketRecent {
		t.Fatalf("recent validated packet was not exposed: %+v", got)
	}
}
