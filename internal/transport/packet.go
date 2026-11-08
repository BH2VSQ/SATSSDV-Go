// Package transport connects image packets to the selected radio link layer.
// Image codecs produce 256-byte packets; this package frames and modulates
// batches so CCSDS preamble/trailer sequences are emitted once per burst.
package transport

import (
	"errors"
	"fmt"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
	"github.com/BH2VSQ/SATSSDV-Go/internal/fec"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ccsds"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/mary"
)

const (
	DefaultCCSDSPreambleBytes = 150
	DefaultCCSDSTrailerBytes  = 150
)

const (
	// MARYFECBalanced uses punctured K=7 rate-3/4 soft-decision FEC and
	// modulation-specific packet erasure parity to favour throughput.
	MARYFECBalanced = iota
	// MARYFECRobust retains K=7 rate-1/2 FEC and two packet parity shards.
	MARYFECRobust
)

type Config struct {
	Profile            domain.TransportMode
	Source             ax25.Address
	Destination        ax25.Address
	Digipeaters        []ax25.Address
	PreambleSize       int
	TrailerSize        int
	AudioSampleRate    int // zero keeps the profile's native modem sample rate
	MARYBurstGroupSize int // 1..15 CCSDS units per independently coded SDF3 group; zero defaults to 10.
	MARYFECProfile     int // MARYFECBalanced (default) or MARYFECRobust.
}

type Encoder struct{ cfg Config }

func NewEncoder(cfg Config) (*Encoder, error) {
	profile, err := domain.ParseProfile(string(cfg.Profile))
	if err != nil {
		return nil, err
	}
	if profile.LinkLayer == "AX.25 UI / HDLC" {
		if cfg.Source.Callsign == "" || cfg.Destination.Callsign == "" {
			return nil, errors.New("AX.25 source and destination callsigns are required")
		}
	}
	if cfg.PreambleSize == 0 {
		cfg.PreambleSize = DefaultCCSDSPreambleBytes
	}
	if cfg.TrailerSize == 0 {
		cfg.TrailerSize = DefaultCCSDSTrailerBytes
	}
	if cfg.MARYBurstGroupSize == 0 {
		cfg.MARYBurstGroupSize = 10
	}
	if cfg.MARYBurstGroupSize < 1 || cfg.MARYBurstGroupSize > 15 {
		return nil, errors.New("M-ary SDF3 group size must be between 1 and 15 CCSDS units")
	}
	if cfg.MARYFECProfile != MARYFECBalanced && cfg.MARYFECProfile != MARYFECRobust {
		return nil, errors.New("M-ary FEC profile must be balanced or robust")
	}
	if cfg.PreambleSize < 0 || cfg.PreambleSize > 4096 || cfg.TrailerSize < 0 || cfg.TrailerSize > 4096 {
		return nil, errors.New("CCSDS preamble/trailer size must be between 0 and 4096 bytes")
	}
	return &Encoder{cfg: cfg}, nil
}

// maryFECSettings maps the selected FEC policy and modulation to an on-air
// code profile and packet-erasure parity count. Balanced mode keeps the
// soft-decision inner code but trades some coding margin for throughput;
// robust mode preserves the previous rate-1/2 plus 8+2 parity configuration.
func (e *Encoder) maryFECSettings(profile domain.Profile) (uint8, int) {
	if e != nil && e.cfg.MARYFECProfile == MARYFECRobust {
		return fec.PacketFECProfileRate12, 2
	}
	switch profile.BitsPerSymbol {
	case 2:
		return fec.PacketFECProfileRate34, 0 // QPSK: 3/4 inner code; SSDV RS8 remains enabled.
	case 3:
		return fec.PacketFECProfileRate23, 1 // 8PSK: 2/3 inner code plus one erasure shard.
	default:
		return fec.PacketFECProfileRate12, 2 // 16QAM: retain strongest inner and packet FEC.
	}
}

// EncodePacket encodes one packet as a complete transmission burst.
func (e *Encoder) EncodePacket(packet []byte) ([]float32, error) {
	return e.EncodePackets([][]byte{packet})
}

// EncodePackets encodes an image packet batch as one continuous audio waveform.
// CCSDS guard bytes are placed once around the entire batch, matching the
// source ARCSSD engine, where preamble/trailer belong to a PTT burst rather
// than being repeated around each 256-byte frame.
func (e *Encoder) EncodePackets(packets [][]byte) ([]float32, error) {
	if e == nil {
		return nil, errors.New("transport encoder is nil")
	}
	if len(packets) == 0 {
		return nil, errors.New("transport packet batch is empty")
	}
	profile, err := domain.ParseProfile(string(e.cfg.Profile))
	if err != nil {
		return nil, err
	}
	for i, packet := range packets {
		if len(packet) != 256 {
			return nil, fmt.Errorf("image transport packet %d must be 256 bytes, got %d", i, len(packet))
		}
		if profile.ImageCodec == "SSDV" {
			if _, err := ssdv.ParsePacket(packet); err != nil {
				return nil, fmt.Errorf("invalid SSDV packet %d: %w", i, err)
			}
		} else if profile.ImageCodec == "PCSI-MMT" {
			if _, _, err := pcsi.CorrectPacket(packet); err != nil {
				return nil, fmt.Errorf("invalid PCSI packet %d: %w", i, err)
			}
		} else {
			return nil, domain.ErrCodecNotImplemented
		}
	}

	var waveform []float32
	if profile.LinkLayer == "AX.25 UI / HDLC" {
		frames := make([][]byte, 0, len(packets))
		for i, packet := range packets {
			frame, err := ax25.BuildUIFrame(e.cfg.Source, e.cfg.Destination, e.cfg.Digipeaters, packet)
			if err != nil {
				return nil, fmt.Errorf("build AX.25 frame %d: %w", i, err)
			}
			frames = append(frames, frame)
		}
		bits := ax25.HDLCBurstBits(frames, ax25.DefaultBurstPreambleFlags, ax25.DefaultBurstTrailerFlags)
		nrzi := ax25.NRZI(bits, true)
		waveform, err = ax25.ModulateAFSK(nrzi, profile.SampleRate, profile.BitRate, profile.MarkFrequency, profile.SpaceFrequency)
		if err != nil {
			return nil, err
		}
	} else if profile.LinkLayer == "CCSDS-MARY" {
		guarded, err := ccsds.BuildGuardedDataUnits(packets, e.cfg.PreambleSize, e.cfg.TrailerSize)
		if err != nil {
			return nil, err
		}
		waveform, err = mary.ModulateBytes(guarded, mary.Config{SampleRate: profile.SampleRate, SymbolRate: profile.SymbolRate, BitsPerSymbol: profile.BitsPerSymbol, CarrierHz: profile.CarrierHz})
		if err != nil {
			return nil, err
		}
	} else if profile.LinkLayer == "CCSDS" {
		guarded, err := ccsds.BuildGuardedDataUnits(packets, e.cfg.PreambleSize, e.cfg.TrailerSize)
		if err != nil {
			return nil, err
		}
		waveform, err = ccsds.Modulate(guarded, profile.BitRate, profile.MarkFrequency, profile.SpaceFrequency, profile.SampleRate)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("unsupported link layer %q", profile.LinkLayer)
	}

	if e.cfg.AudioSampleRate > 0 && e.cfg.AudioSampleRate != profile.SampleRate {
		return dsp.ResampleLinear(waveform, profile.SampleRate, e.cfg.AudioSampleRate)
	}
	return waveform, nil
}

func (e *Encoder) SampleRate() (int, error) {
	if e == nil {
		return 0, errors.New("transport encoder is nil")
	}
	p, err := domain.ParseProfile(string(e.cfg.Profile))
	if err != nil {
		return 0, err
	}
	if e.cfg.AudioSampleRate > 0 {
		return e.cfg.AudioSampleRate, nil
	}
	return p.SampleRate, nil
}
