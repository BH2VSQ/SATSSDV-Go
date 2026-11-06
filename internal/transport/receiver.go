package transport

import (
	"fmt"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ccsds"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/mary"
)

type ReceivedPacket struct {
	Profile         string `json:"profile"`
	Codec           string `json:"codec"`
	Callsign        string `json:"callsign"`
	ImageID         uint8  `json:"imageId"`
	PacketID        uint16 `json:"packetId"`
	ImageWidth      int    `json:"imageWidth,omitempty"`
	ImageHeight     int    `json:"imageHeight,omitempty"`
	Quality         uint8  `json:"quality,omitempty"`
	EOI             bool   `json:"eoi,omitempty"`
	Subsampling     uint8  `json:"subsampling,omitempty"`
	MCUOffset       uint8  `json:"mcuOffset,omitempty"`
	MCUIndex        uint16 `json:"mcuIndex,omitempty"`
	MCUCount        uint32 `json:"mcuCount,omitempty"`
	FEC             bool   `json:"fec"`
	GIFMode         bool   `json:"gifMode,omitempty"`
	GIFKeyframe     bool   `json:"gifKeyframe,omitempty"`
	GIFControl      bool   `json:"gifControl,omitempty"`
	GIFFrameStart   bool   `json:"gifFrameStart,omitempty"`
	GIFFrameEnd     bool   `json:"gifFrameEnd,omitempty"`
	GIFDataPackets  uint16 `json:"gifDataPackets,omitempty"`
	GIFDelayCS      uint16 `json:"gifDelayCs,omitempty"`
	GIFStart        bool   `json:"gifStart,omitempty"`
	GIFEnd          bool   `json:"gifEnd,omitempty"`
	HasAX25SSID     bool   `json:"hasAX25SSID,omitempty"`
	AX25SourceSSID  uint8  `json:"ax25SourceSSID,omitempty"`
	AX25Destination string `json:"ax25Destination,omitempty"`
	Raw             []byte `json:"raw"`
}

// DecodeAudio decodes a captured waveform in one profile. It supports block
// test/diagnostic captures; continuous audio requires stateful AFC and symbol
// timing recovery before on-air interoperability can be considered complete.
func DecodeAudio(profileID domain.TransportMode, samples []float32) ([]ReceivedPacket, error) {
	profile, err := domain.ParseProfile(string(profileID))
	if err != nil {
		return nil, err
	}
	return DecodeAudioAtRate(profileID, samples, profile.SampleRate)
}

func DecodeAudioAtRate(profileID domain.TransportMode, samples []float32, captureSampleRate int) ([]ReceivedPacket, error) {
	profile, err := domain.ParseProfile(string(profileID))
	if err != nil {
		return nil, err
	}
	if captureSampleRate != profile.SampleRate {
		samples, err = dsp.ResampleLinear(samples, captureSampleRate, profile.SampleRate)
		if err != nil {
			return nil, err
		}
	}
	var bits []bool
	if profile.LinkLayer == "CCSDS-MARY" {
		demod, createErr := mary.NewStreamDemodulator(mary.Config{SampleRate: profile.SampleRate, SymbolRate: profile.SymbolRate, BitsPerSymbol: profile.BitsPerSymbol, CarrierHz: profile.CarrierHz})
		if createErr != nil {
			return nil, createErr
		}
		bits = demod.Feed(samples)
	} else if profile.BitRate == 2400 && profile.SampleRate == 19200 && profile.LinkLayer == "CCSDS" {
		demod := &ccsds.ARCSSD2400Demodulator{}
		bits, err = demod.Feed(samples, profile.SampleRate)
	} else {
		bits, _, err = dsp.DemodulateFSK(samples, profile.SampleRate, profile.BitRate, profile.MarkFrequency, profile.SpaceFrequency)
	}
	if err != nil {
		return nil, err
	}
	var payloads [][]byte
	var payloadSSIDs []uint8
	var payloadDestinations []string
	if profile.LinkLayer == "AX.25 UI / HDLC" {
		for _, initial := range []bool{true, false} {
			nrzi := ax25.NRZIDecode(bits, initial)
			frames, decodeErr := ax25.DecodeHDLCFrames(nrzi)
			if decodeErr != nil {
				continue
			}
			for _, frame := range frames {
				payloads = append(payloads, frame.Information)
				payloadSSIDs = append(payloadSSIDs, frame.Source.SSID)
				payloadDestinations = append(payloadDestinations, frame.Destination.Callsign)
			}
			if len(payloads) > 0 {
				break
			}
		}
	} else {
		payloads, err = ccsds.FindDataUnitsInBits(bits)
		if err != nil {
			return nil, err
		}
	}
	packets := make([]ReceivedPacket, 0, len(payloads))
	for payloadIndex, payload := range payloads {
		var item ReceivedPacket
		item.Profile, item.Codec, item.Raw = string(profile.ID), profile.ImageCodec, append([]byte(nil), payload...)
		if profile.LinkLayer == "AX.25 UI / HDLC" && payloadIndex < len(payloadSSIDs) {
			item.HasAX25SSID = true
			item.AX25SourceSSID = payloadSSIDs[payloadIndex]
			if payloadIndex < len(payloadDestinations) {
				item.AX25Destination = payloadDestinations[payloadIndex]
			}
		}
		if profile.ImageCodec == "SSDV" {
			p, parseErr := ssdv.ParsePacket(payload)
			if parseErr != nil {
				continue
			}
			item.Callsign, item.ImageID, item.PacketID = p.Callsign, p.ImageID, p.PacketID
			item.ImageWidth, item.ImageHeight, item.Quality = p.Width, p.Height, p.Quality
			item.EOI, item.Subsampling, item.MCUOffset, item.MCUIndex, item.MCUCount, item.FEC = p.EOI, p.Subsampling, p.MCUOffset, p.MCUIndex, p.MCUCount, p.FEC
			item.GIFMode, item.GIFKeyframe, item.GIFControl, item.GIFDelayCS = p.GIFMode, p.GIFKeyframe, p.GIFControl, p.GIFDelayCS
			item.GIFFrameStart, item.GIFFrameEnd, item.GIFDataPackets = p.GIFFrameStart, p.GIFFrameEnd, p.GIFDataPackets
			item.GIFStart, item.GIFEnd = p.GIFStart, p.GIFEnd
			if p.GIFMode {
				item.Codec = "SSDV-GIF"
			}
			item.Raw = append([]byte(nil), p.Raw[:]...)
		} else {
			corrected, _, correctionErr := pcsi.CorrectPacket(payload)
			if correctionErr != nil {
				continue
			}
			p, parseErr := pcsi.ParsePacket(corrected)
			if parseErr != nil {
				continue
			}
			item.Callsign, item.ImageID, item.PacketID = p.Callsign, p.ImageID, p.PacketNumber
			item.Raw = corrected
		}
		packets = append(packets, item)
	}
	if len(packets) == 0 {
		return nil, fmt.Errorf("no valid %s image packets found", profile.ImageCodec)
	}
	return packets, nil
}
