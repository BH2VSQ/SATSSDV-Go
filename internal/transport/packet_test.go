package transport

import (
	"image"
	"image/color"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
)

func TestPCSIProfilesProduceCCSDSAudio(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 255})
		}
	}
	enc, err := pcsi.NewEncoder(img, pcsi.Config{Callsign: "N0CALL", ImageID: 7})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := enc.EncodePacket(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []domain.TransportMode{domain.PCSICCSDS1200, domain.PCSICCSDS2400} {
		t.Run(string(mode), func(t *testing.T) {
			tr, err := NewEncoder(Config{Profile: mode})
			if err != nil {
				t.Fatal(err)
			}
			wave, err := tr.EncodePacket(packet)
			if err != nil {
				t.Fatal(err)
			}
			profile, _ := domain.ParseProfile(string(mode))
			want := (150 + 4 + 256 + 150) * 8 * (profile.SampleRate / profile.BitRate)
			if len(wave) != want {
				t.Fatalf("wave length %d, want %d", len(wave), want)
			}
		})
	}
}

func TestAX25ProfileRequiresValidSSDVPacketAndAddresses(t *testing.T) {
	if _, err := NewEncoder(Config{Profile: domain.SSDVAX25}); err == nil {
		t.Fatal("expected missing AX.25 addresses to fail")
	}
	_, err := NewEncoder(Config{Profile: domain.SSDVAX25, Source: ax25.Address{Callsign: "N0CALL"}, Destination: ax25.Address{Callsign: "CQ"}})
	if err != nil {
		t.Fatal(err)
	}
}
