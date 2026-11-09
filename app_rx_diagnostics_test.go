package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/BH2VSQ/SATSSDV-Go/internal/transport"
)

type panicRXTestDecoder struct{}

func (panicRXTestDecoder) Feed([]float32) ([]transport.ReceivedPacket, error) {
	panic("simulated malformed FEC recovery state")
}

func (panicRXTestDecoder) Reset() {}

func TestSafeFeedRXDecoderContainsPanicAndReturnsStack(t *testing.T) {
	packets, err, panicMessage, panicStack := safeFeedRXDecoder(panicRXTestDecoder{}, []float32{0.1, -0.1})
	if err == nil || !strings.Contains(err.Error(), "RX decoder panic") {
		t.Fatalf("panic was not converted to an RX decoder error: %v", err)
	}
	if panicMessage != "simulated malformed FEC recovery state" {
		t.Fatalf("unexpected panic message %q", panicMessage)
	}
	if len(panicStack) == 0 || !strings.Contains(panicStack, "safeFeedRXDecoder") {
		t.Fatalf("panic stack was not captured: %q", panicStack)
	}
	if len(packets) != 0 {
		t.Fatalf("panic returned partial packets: %d", len(packets))
	}
}

type errorWithPacketsRXTestDecoder struct{}

func (errorWithPacketsRXTestDecoder) Feed([]float32) ([]transport.ReceivedPacket, error) {
	return []transport.ReceivedPacket{{Codec: "SSDV", PacketID: 7}}, errors.New("soft-FEC warning after valid packet")
}

func TestSafeFeedRXDecoderPreservesPacketsAlongsideFeedError(t *testing.T) {
	packets, err, panicMessage, _ := safeFeedRXDecoder(errorWithPacketsRXTestDecoder{}, []float32{0.2})
	if err == nil || panicMessage != "" {
		t.Fatalf("expected an ordinary feed error, got err=%v panic=%q", err, panicMessage)
	}
	if len(packets) != 1 || packets[0].PacketID != 7 {
		t.Fatalf("valid packets were discarded with a feed warning: %+v", packets)
	}
}

type fixedMARYSNRDiagnostics struct{ diagnostics transport.DecoderDiagnostics }

func (d fixedMARYSNRDiagnostics) Diagnostics() transport.DecoderDiagnostics { return d.diagnostics }

type fixedAutoMARYSNRDiagnostics struct {
	diagnostics transport.AutoDecoderDiagnostics
}

func (d fixedAutoMARYSNRDiagnostics) Diagnostics() transport.AutoDecoderDiagnostics {
	return d.diagnostics
}

func TestRXDecoderMARYSNRRequiresLiveSynchronizedEstimate(t *testing.T) {
	valid := transport.DecoderDiagnostics{MARYLocked: true, MARYSNRValid: true, MARYSNRDB: 17.25}
	if got, ok := rxDecoderMARYSNR(fixedMARYSNRDiagnostics{diagnostics: valid}); !ok || got != 17.25 {
		t.Fatalf("valid synchronized M-ary SNR was not forwarded: got %.2f valid=%v", got, ok)
	}
	for _, invalid := range []transport.DecoderDiagnostics{
		{MARYLocked: false, MARYSNRValid: true, MARYSNRDB: 17.25},
		{MARYLocked: true, MARYSNRValid: false, MARYSNRDB: 17.25},
	} {
		if got, ok := rxDecoderMARYSNR(fixedMARYSNRDiagnostics{diagnostics: invalid}); ok || got != 0 {
			t.Fatalf("invalid/unsynchronized M-ary estimate leaked to UI: got %.2f valid=%v", got, ok)
		}
	}
}

func TestRXDecoderMARYSNRReadsAutoCandidates(t *testing.T) {
	auto := transport.AutoDecoderDiagnostics{MARYCandidates: []transport.DecoderDiagnostics{
		{Profile: "ssdv-qpsk-1600", MARYLocked: false, MARYSNRValid: true, MARYSNRDB: 99},
		{Profile: "ssdv-8psk-1600", MARYLocked: true, MARYSNRValid: true, MARYSNRDB: 14.5},
	}}
	if got, ok := rxDecoderMARYSNR(fixedAutoMARYSNRDiagnostics{diagnostics: auto}); !ok || got != 14.5 {
		t.Fatalf("Auto RX did not forward a live M-ary estimate: got %.2f valid=%v", got, ok)
	}
}
