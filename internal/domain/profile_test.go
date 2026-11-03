package domain

import "testing"

func TestAllFiveProfilesAreDistinctAndComplete(t *testing.T) {
	got := Profiles()
	if len(got) != 5 {
		t.Fatalf("profiles=%d, want 5", len(got))
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
