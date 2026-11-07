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
