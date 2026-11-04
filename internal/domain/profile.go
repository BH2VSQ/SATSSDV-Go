package domain

import (
	"errors"
	"fmt"
	"strings"
)

type TransportMode string

const (
	SSDVAX25      TransportMode = "ssdv-ax25"
	SSDVCCSDS1200 TransportMode = "ssdv-ccsds-1200"
	SSDVCCSDS2400 TransportMode = "ssdv-ccsds-2400"
	SSDVQPSK1600  TransportMode = "ssdv-qpsk-1600"
	SSDV8PSK1600  TransportMode = "ssdv-8psk-1600"
	SSDV16QAM1600 TransportMode = "ssdv-16qam-1600"
	PCSICCSDS1200 TransportMode = "pcsi-ccsds-1200"
	PCSICCSDS2400 TransportMode = "pcsi-ccsds-2400"
)

const AutoReceiveProfile = "auto"

type Modulation string

const (
	ModulationAFSK1200 Modulation = "afsk-1200"
	ModulationAFSK2400 Modulation = "afsk-2400"
	ModulationQPSK     Modulation = "qpsk"
	Modulation8PSK     Modulation = "8psk"
	Modulation16QAM    Modulation = "16qam"
)

type Profile struct {
	ID             TransportMode `json:"id"`
	Name           string        `json:"name"`
	ImageCodec     string        `json:"imageCodec"`
	LinkLayer      string        `json:"linkLayer"`
	BitRate        int           `json:"bitRate"`
	SampleRate     int           `json:"sampleRate"`
	Modulation     Modulation    `json:"modulation"`
	MarkFrequency  int           `json:"markFrequency"`
	SpaceFrequency int           `json:"spaceFrequency"`
	SymbolRate     int           `json:"symbolRate,omitempty"`
	BitsPerSymbol  int           `json:"bitsPerSymbol,omitempty"`
	CarrierHz      int           `json:"carrierHz,omitempty"`
}

var profiles = []Profile{
	{ID: SSDVAX25, Name: "SSDV AX.25", ImageCodec: "SSDV", LinkLayer: "AX.25 UI / HDLC", BitRate: 1200, SampleRate: 19200, Modulation: ModulationAFSK1200, MarkFrequency: 1200, SpaceFrequency: 2200},
	{ID: SSDVCCSDS1200, Name: "SSDV CCSDS 1200", ImageCodec: "SSDV", LinkLayer: "CCSDS", BitRate: 1200, SampleRate: 19200, Modulation: ModulationAFSK1200, MarkFrequency: 1200, SpaceFrequency: 2200},
	{ID: SSDVCCSDS2400, Name: "SSDV CCSDS 2400", ImageCodec: "SSDV", LinkLayer: "CCSDS", BitRate: 2400, SampleRate: 19200, Modulation: ModulationAFSK2400, MarkFrequency: 1200, SpaceFrequency: 2400},
	{ID: PCSICCSDS1200, Name: "PCSI CCSDS 1200", ImageCodec: "PCSI-MMT", LinkLayer: "CCSDS", BitRate: 1200, SampleRate: 19200, Modulation: ModulationAFSK1200, MarkFrequency: 1200, SpaceFrequency: 2200},
	{ID: PCSICCSDS2400, Name: "PCSI CCSDS 2400", ImageCodec: "PCSI-MMT", LinkLayer: "CCSDS", BitRate: 2400, SampleRate: 19200, Modulation: ModulationAFSK2400, MarkFrequency: 1200, SpaceFrequency: 2400},
	{ID: SSDVQPSK1600, Name: "SSDV QPSK 1600", ImageCodec: "SSDV", LinkLayer: "CCSDS-MARY", BitRate: 3200, SampleRate: 48000, Modulation: ModulationQPSK, SymbolRate: 1600, BitsPerSymbol: 2, CarrierHz: 1600},
	{ID: SSDV8PSK1600, Name: "SSDV 8PSK 1600", ImageCodec: "SSDV", LinkLayer: "CCSDS-MARY", BitRate: 4800, SampleRate: 48000, Modulation: Modulation8PSK, SymbolRate: 1600, BitsPerSymbol: 3, CarrierHz: 1600},
	{ID: SSDV16QAM1600, Name: "SSDV 16QAM 1600", ImageCodec: "SSDV", LinkLayer: "CCSDS-MARY", BitRate: 6400, SampleRate: 48000, Modulation: Modulation16QAM, SymbolRate: 1600, BitsPerSymbol: 4, CarrierHz: 1600},
}

func Profiles() []Profile {
	out := make([]Profile, len(profiles))
	copy(out, profiles)
	return out
}

func ParseProfile(id string) (Profile, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range profiles {
		if string(p.ID) == id {
			return p, nil
		}
	}
	return Profile{}, fmt.Errorf("unsupported transport profile %q", id)
}

func ValidateIndependentModes(rx, tx string) error {
	if rx != AutoReceiveProfile {
		if _, err := ParseProfile(rx); err != nil {
			return fmt.Errorf("RX profile: %w", err)
		}
	}
	if _, err := ParseProfile(tx); err != nil {
		return fmt.Errorf("TX profile: %w", err)
	}
	return nil
}

var ErrCodecNotImplemented = errors.New("codec implementation is not available yet; this profile must not be used for on-air transmission")
