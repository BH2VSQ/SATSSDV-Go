package gifmode

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
)

func TestGIFPacketsRoundTripThroughExistingCCSDSLink(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			frame.SetRGBA(x, y, color.RGBA{
				R: uint8((x / 4) & 0xff), G: uint8((y / 3) & 0xff), B: 70, A: 255,
			})
		}
	}
	second := image.NewRGBA(frame.Bounds())
	draw.Draw(second, second.Bounds(), frame, frame.Bounds().Min, draw.Src)
	draw.Draw(second, image.Rect(96, 96, 112, 112), image.NewUniform(color.RGBA{R: 240, G: 20, B: 35, A: 255}), image.Point{}, draw.Src)
	packets, _, err := Encode([]Frame{{Image: frame, DelayCS: 12}, {Image: second, DelayCS: 23}}, "N0CALL", 23, 4)
	if err != nil {
		t.Fatal(err)
	}
	profiles := []domain.TransportMode{
		domain.SSDVCCSDS1200, domain.SSDVAX25,
		domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600,
	}
	for _, profile := range profiles {
		t.Run(string(profile), func(t *testing.T) {
			config := transport.Config{Profile: profile}
			if profile == domain.SSDVAX25 {
				config.Source = ax25.Address{Callsign: "N0CALL", SSID: 1}
				config.Destination = ax25.Address{Callsign: "CQ"}
			}
			encoder, err := transport.NewEncoder(config)
			if err != nil {
				t.Fatal(err)
			}
			waveform, err := encoder.EncodePackets(packets)
			if err != nil {
				t.Fatal(err)
			}
			profileInfo, profileErr := domain.ParseProfile(string(profile))
			if profileErr != nil {
				t.Fatal(profileErr)
			}
			if err := transport.FilterTXWaveformInPlace(profile, waveform, profileInfo.SampleRate); err != nil {
				t.Fatalf("prepare production TX waveform: %v", err)
			}
			decoder, err := transport.NewStreamDecoder(profile, profileInfo.SampleRate)
			if err != nil {
				t.Fatal(err)
			}
			var received []transport.ReceivedPacket
			for offset := 0; offset < len(waveform); offset += 1920 {
				end := min(offset+1920, len(waveform))
				batch, feedErr := decoder.Feed(waveform[offset:end])
				if feedErr != nil {
					t.Fatalf("decode GIF waveform: %v", feedErr)
				}
				received = append(received, batch...)
			}
			// Repeated frame delimiters are de-duplicated by the RX stream
			// decoder. Count unique frame-local packet IDs rather than the
			// physical packet copies sent over the air.
			unique := make(map[string]struct{})
			for _, raw := range packets {
				parsed, parseErr := ssdv.ParsePacket(raw)
				if parseErr != nil {
					t.Fatal(parseErr)
				}
				key := fmt.Sprintf("%d/%d", parsed.ImageID, parsed.PacketID)
				unique[key] = struct{}{}
			}
			if len(received) != len(unique) {
				t.Fatalf("received %d/%d unique packets (physical TX packets=%d)", len(received), len(unique), len(packets))
			}
			controls := make(map[uint8]map[bool]transport.ReceivedPacket)
			dataCounts := make(map[uint8]int)
			for _, packet := range received {
				if packet.GIFControl {
					if controls[packet.ImageID] == nil {
						controls[packet.ImageID] = make(map[bool]transport.ReceivedPacket)
					}
					controls[packet.ImageID][packet.GIFFrameStart] = packet
				} else {
					dataCounts[packet.ImageID]++
				}
			}
			firstOpen, firstOpenOK := controls[23][true]
			firstClose, firstCloseOK := controls[23][false]
			lastOpen, lastOpenOK := controls[24][true]
			lastClose, lastCloseOK := controls[24][false]
			if !firstOpenOK || !firstCloseOK || !lastOpenOK || !lastCloseOK || !firstOpen.GIFStart || firstOpen.GIFEnd || firstClose.GIFStart || firstClose.GIFEnd || lastOpen.GIFStart || lastOpen.GIFEnd || lastClose.GIFStart || !lastClose.GIFEnd {
				t.Fatalf("incorrect stream/frame markers after modem roundtrip: first-open=%+v first-close=%+v last-open=%+v last-close=%+v", firstOpen, firstClose, lastOpen, lastClose)
			}
			if firstOpen.GIFDelayCS != 12 || lastClose.GIFDelayCS != 23 {
				t.Fatalf("frame delays were not preserved: first=%d last=%d", firstOpen.GIFDelayCS, lastClose.GIFDelayCS)
			}
			if dataCounts[24] == 0 || dataCounts[24] >= dataCounts[23] {
				t.Fatalf("delta frame should use fewer data packets: key=%d delta=%d", dataCounts[23], dataCounts[24])
			}
			for i, packet := range received {
				if packet.Codec != "SSDV-GIF" || !packet.GIFMode {
					t.Fatalf("packet %d was not identified as SSDV-GIF: %+v", i, packet)
				}
			}
		})
	}
}
