package domain

import (
	"strings"
	"testing"
)

func TestAllProfilesAreDistinctAndComplete(t *testing.T) {
	got := Profiles()
	if len(got) != 8 {
		t.Fatalf("profiles=%d, want 8", len(got))
	}
	seen := map[TransportMode]bool{}
	for _, p := range got {
		if p.ID == "" || p.ImageCodec == "" || p.LinkLayer == "" || p.BitRate == 0 || p.SampleRate == 0 {
			t.Fatalf("incomplete profile: %+v", p)
		}
		if seen[p.ID] {
			t.Fatalf("duplicate profile %q", p.ID)
		}
		seen[p.ID] = true
		if p.LinkLayer == "CCSDS-MARY" {
			if strings.Contains(strings.ToLower(p.Name), "baud") || strings.Contains(p.Name, "2000") {
				t.Fatalf("M-ary display name must show only the supported numeric symbol rate: %+v", p)
			}
			if p.SampleRate != 48000 || p.SymbolRate != 1600 || p.BitsPerSymbol < 2 || p.BitsPerSymbol > 4 {
				t.Fatalf("invalid M-ary profile: %+v", p)
			}
			if p.BitRate != p.SymbolRate*p.BitsPerSymbol {
				t.Fatalf("profile %s bitrate does not match baud x bits/symbol", p.ID)
			}
		}
	}
}

func TestIndependentRXTXProfiles(t *testing.T) {
	if err := ValidateIndependentModes(string(SSDVAX25), string(PCSICCSDS2400)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateIndependentModes("unknown", string(PCSICCSDS2400)); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestAutoReceiveModeValidation(t *testing.T) {
	if err := ValidateIndependentModes(AutoReceiveProfile, string(SSDVAX25)); err != nil {
		t.Fatalf("auto RX should be accepted: %v", err)
	}
	if err := ValidateIndependentModes(string(SSDVAX25), AutoReceiveProfile); err == nil {
		t.Fatal("auto must not be accepted as TX mode")
	}
}
