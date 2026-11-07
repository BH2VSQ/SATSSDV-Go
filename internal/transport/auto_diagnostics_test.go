package transport

import "testing"

func TestAutoStreamDecoderDiagnosticsListsMARYCandidates(t *testing.T) {
	decoder, err := NewAutoStreamDecoder(48000)
	if err != nil {
		t.Fatal(err)
	}
	status := decoder.Diagnostics()
	if status.LockedProfile != "" {
		t.Fatalf("new decoder unexpectedly locked profile %q", status.LockedProfile)
	}
	if len(status.MARYCandidates) != 3 {
		t.Fatalf("diagnostics reported %d M-ary candidates, want 3", len(status.MARYCandidates))
	}
	for _, candidate := range status.MARYCandidates {
		if candidate.Profile == "" || candidate.MARYLocked {
			t.Fatalf("unexpected initial candidate state: %+v", candidate)
		}
	}
}
