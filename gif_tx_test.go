package main

import (
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
)

func TestGIFMARYGroupSizePrioritizesProgressiveLatency(t *testing.T) {
	for _, mode := range []domain.TransportMode{
		domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600,
	} {
		profile, err := domain.ParseProfile(string(mode))
		if err != nil {
			t.Fatal(err)
		}
		if got := gifMARYBurstGroupSize(profile); got != 2 {
			t.Fatalf("GIF M-ary mode %s group size = %d, want 2", mode, got)
		}
	}
	for _, mode := range []domain.TransportMode{domain.SSDVAX25, domain.SSDVCCSDS1200, domain.SSDVCCSDS2400} {
		profile, err := domain.ParseProfile(string(mode))
		if err != nil {
			t.Fatal(err)
		}
		if got := gifMARYBurstGroupSize(profile); got != 0 {
			t.Fatalf("GIF legacy mode %s unexpectedly overrides transport group size: %d", mode, got)
		}
	}
}
