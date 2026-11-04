package transport

import (
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ccsds"
)

func TestDecodeConstellationWindowUsesProfileModem(t *testing.T) {
	bits := make([]bool, 256)
	for i := range bits {
		bits[i] = i%2 == 0 || (i/7)%2 == 0
	}
	fskWave, err := ax25.ModulateAFSK(bits, 19200, 1200, 1200, 2200)
	if err != nil {
		t.Fatal(err)
	}
	gmskWave, err := ccsds.Modulate([]byte{0x55, 0xaa, 0x96, 0x69, 0xc3, 0x3c, 0x5a, 0xa5, 0x0f, 0xf0, 0x87, 0x78}, 2400, 1200, 2400, 19200)
	if err != nil {
		t.Fatal(err)
	}

	for _, profile := range []domain.TransportMode{domain.SSDVAX25, domain.SSDVCCSDS1200, domain.PCSICCSDS1200} {
		update, err := DecodeConstellationWindow(profile, fskWave, 19200)
		if err != nil {
			t.Fatalf("%s: %v", profile, err)
		}
		if update.Kind != "2FSK-SPACE" || len(update.Points) == 0 {
			t.Fatalf("%s returned kind=%q points=%d", profile, update.Kind, len(update.Points))
		}
	}
	for _, profile := range []domain.TransportMode{domain.SSDVCCSDS2400, domain.PCSICCSDS2400} {
		update, err := DecodeConstellationWindow(profile, gmskWave, 19200)
		if err != nil {
			t.Fatalf("%s: %v", profile, err)
		}
		if update.Kind != "GMSK-SOFT" || len(update.Points) == 0 {
			t.Fatalf("%s returned kind=%q points=%d", profile, update.Kind, len(update.Points))
		}
	}
}
