package transport

import (
	"fmt"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/pcsi"
	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ccsds"
)

const (
	streamCCSDSFrameBits = 32 + ccsds.PayloadLength*8
	streamMaxSeenPackets = 16384
)

// StreamDecoder keeps modem, resampler and frame-search state across audio
// callback blocks. Feed returns only newly accepted image packets. A nil block
// marks an input discontinuity and resets modem acquisition while retaining
// packet de-duplication for the current image session.
type StreamDecoder struct {
	profile   domain.Profile
	inputRate int
	resampler *dsp.StreamResampler
	fsk       *dsp.FSKStreamDemodulator
	gmsk2400  *ccsds.ARCSSD2400Demodulator
	ax25      *ax25.StreamDecoder
	ccsdsBits []bool
	seen      map[string]struct{}
	seenOrder []string
}

func NewStreamDecoder(profileID domain.TransportMode, inputSampleRate int) (*StreamDecoder, error) {
	profile, err := domain.ParseProfile(string(profileID))
	if err != nil {
		return nil, err
	}
	if inputSampleRate < 8000 || inputSampleRate > 192000 {
		return nil, fmt.Errorf("input sample rate %d is outside 8000..192000 Hz", inputSampleRate)
	}
	resampler, err := dsp.NewStreamResampler(inputSampleRate, profile.SampleRate)
	if err != nil {
		return nil, err
	}
	decoder := &StreamDecoder{
		profile: profile, inputRate: inputSampleRate, resampler: resampler,
		seen: make(map[string]struct{}),
	}
	if profile.LinkLayer == "CCSDS" && profile.BitRate == 2400 {
		decoder.gmsk2400 = &ccsds.ARCSSD2400Demodulator{}
	} else {
		decoder.fsk, err = dsp.NewFSKStreamDemodulator(profile.SampleRate, profile.BitRate, profile.MarkFrequency, profile.SpaceFrequency)
		if err != nil {
			return nil, err
		}
	}
	if profile.LinkLayer == "AX.25 UI / HDLC" {
		decoder.ax25 = &ax25.StreamDecoder{}
	}
	return decoder, nil
}

// Reset discards partial modem and framing state. Already emitted packet IDs
// stay in the de-duplication set, preventing a capture discontinuity from
// causing old packets still in the audio queue to be delivered again.
func (d *StreamDecoder) Reset() {
	if d == nil {
		return
	}
	d.resampler.Reset()
	if d.fsk != nil {
		d.fsk.Reset()
	}
	if d.gmsk2400 != nil {
		d.gmsk2400.Reset()
	}
	if d.ax25 != nil {
		d.ax25.Reset()
	}
	d.ccsdsBits = nil
}

func (d *StreamDecoder) Feed(samples []float32) ([]ReceivedPacket, error) {
	if d == nil {
		return nil, fmt.Errorf("stream decoder is nil")
	}
	if samples == nil {
		d.Reset()
		return nil, nil
	}
	if len(samples) == 0 {
		return nil, nil
	}
	native := d.resampler.Feed(samples)
	if len(native) == 0 {
		return nil, nil
	}
	var bits []bool
	var err error
	if d.gmsk2400 != nil {
		bits, err = d.gmsk2400.Feed(native, d.profile.SampleRate)
	} else {
		levels := d.fsk.Feed(native)
		if d.ax25 != nil {
			frames := d.ax25.Feed(levels)
			packets := make([]ReceivedPacket, 0, len(frames))
			for _, frame := range frames {
				packet, parseErr := parseImagePacket(d.profile, frame.Information)
				if parseErr != nil {
					continue
				}
				if d.markNew(packet) {
					packets = append(packets, packet)
				}
			}
			return packets, nil
		}
		bits = levels
	}
	if err != nil {
		return nil, err
	}
	return d.consumeCCSDSBits(bits), nil
}

func (d *StreamDecoder) consumeCCSDSBits(bits []bool) []ReceivedPacket {
	if len(bits) == 0 {
		return nil
	}
	d.ccsdsBits = append(d.ccsdsBits, bits...)
	var packets []ReceivedPacket
	start := 0
	for start+streamCCSDSFrameBits <= len(d.ccsdsBits) {
		if !ccsds.MatchASMBits(d.ccsdsBits[start : start+32]) {
			start++
			continue
		}
		frame := make([]byte, 4+ccsds.PayloadLength)
		for i := 0; i < streamCCSDSFrameBits; i++ {
			if d.ccsdsBits[start+i] {
				frame[i/8] |= 1 << uint(7-i%8)
			}
		}
		frame[0], frame[1], frame[2], frame[3] = 0x1a, 0xcf, 0xfc, 0x1d
		payload, err := ccsds.ParseDataUnit(frame)
		if err != nil {
			start++
			continue
		}
		packet, err := parseImagePacket(d.profile, payload)
		if err == nil && d.markNew(packet) {
			packets = append(packets, packet)
		}
		start += streamCCSDSFrameBits
	}
	// Keep only the incomplete suffix that has not yet been searched. This
	// also preserves a partial ASM at the end of the current audio block.
	if start > 0 {
		d.ccsdsBits = append([]bool(nil), d.ccsdsBits[start:]...)
	}
	return packets
}

func parseImagePacket(profile domain.Profile, raw []byte) (ReceivedPacket, error) {
	item := ReceivedPacket{Profile: string(profile.ID), Codec: profile.ImageCodec}
	if profile.ImageCodec == "SSDV" {
		parsed, err := ssdv.ParsePacket(raw)
		if err != nil {
			return ReceivedPacket{}, err
		}
		item.Callsign, item.ImageID, item.PacketID = parsed.Callsign, parsed.ImageID, parsed.PacketID
		item.ImageWidth, item.ImageHeight, item.Quality = parsed.Width, parsed.Height, parsed.Quality
		item.EOI, item.Subsampling = parsed.EOI, parsed.Subsampling
		item.MCUOffset, item.MCUIndex, item.FEC = parsed.MCUOffset, parsed.MCUIndex, parsed.FEC
		item.Raw = append([]byte(nil), parsed.Raw[:]...)
		return item, nil
	}
	corrected, _, err := pcsi.CorrectPacket(raw)
	if err != nil {
		return ReceivedPacket{}, err
	}
	parsed, err := pcsi.ParsePacket(corrected)
	if err != nil {
		return ReceivedPacket{}, err
	}
	item.Callsign, item.ImageID, item.PacketID = parsed.Callsign, parsed.ImageID, parsed.PacketNumber
	item.Raw = corrected
	return item, nil
}

func (d *StreamDecoder) markNew(packet ReceivedPacket) bool {
	key := fmt.Sprintf("%s|%s|%03d|%05d", packet.Profile, packet.Callsign, packet.ImageID, packet.PacketID)
	if _, exists := d.seen[key]; exists {
		return false
	}
	d.seen[key] = struct{}{}
	d.seenOrder = append(d.seenOrder, key)
	if len(d.seenOrder) > streamMaxSeenPackets {
		delete(d.seen, d.seenOrder[0])
		copy(d.seenOrder, d.seenOrder[1:])
		d.seenOrder = d.seenOrder[:len(d.seenOrder)-1]
	}
	return true
}
