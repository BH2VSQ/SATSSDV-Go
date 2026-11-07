package transport

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"time"

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
	streamCCSDSFrameBits = 32 + ccsds.PayloadLength*8
	streamMaxImageStates = 64
)

type packetDedupState struct {
	seen         map[uint16]struct{}
	packetZero   []byte
	eoiPacketID  uint16
	hasEOI       bool
	complete     bool
	gifStreamEnd bool
	lastUpdate   time.Time
}

// StreamDecoder keeps modem, resampler and frame-search state across audio
// callback blocks. Feed returns only newly accepted image packets. A nil block
// marks an input discontinuity and resets modem acquisition while retaining
// packet de-duplication for the current image session.
type packetFECGroup struct {
	imageKey string
	parity   [2]*fec.PacketParityPayload
	last     time.Time
}

type StreamDecoder struct {
	profile             domain.Profile
	inputRate           int
	resampler           *dsp.StreamResampler
	fsk                 *dsp.FSKStreamDemodulator
	gmsk2400            *ccsds.ARCSSD2400Demodulator
	mary                *mary.StreamDemodulator
	maryBurstGeneration uint64
	maryFECRearmPending bool
	ax25                *ax25.StreamDecoder
	ccsdsBits           []bool
	// Packet de-duplication is scoped to an image session, not the lifetime
	// of the audio stream. A new packet 0 starts a fresh session when an
	// image ID is reused by a transmitter or after its 8-bit ID wraps.
	imageStates map[string]*packetDedupState

	maryFECMode         int8 // -1 undecided, 0 legacy hard-decision, 1 soft-FEC burst
	maryHeaderSoft      []float32
	maryFECHeader       fec.BurstHeader
	maryFECDecoder      *fec.ViterbiDecoder
	maryFECSoft         []float32
	maryFECBlocksDone   int64
	maryFECBitsDone     int64
	maryFECDecodedBits  int64
	maryFECDecodedBytes uint32
	maryFECByte         byte
	maryFECByteBits     uint8
	maryFECCRC          uint32
	maryFECFinished     bool
	maryFECError        error

	packetFECRecent        map[string]map[uint16][]byte
	packetFECRecentOrder   map[string][]uint16
	packetFECRecentUpdated map[string]time.Time
	packetFECGroups        map[string]*packetFECGroup
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
		imageStates:            make(map[string]*packetDedupState),
		maryFECMode:            -1,
		packetFECRecent:        make(map[string]map[uint16][]byte),
		packetFECRecentOrder:   make(map[string][]uint16),
		packetFECRecentUpdated: make(map[string]time.Time),
		packetFECGroups:        make(map[string]*packetFECGroup),
	}
	if profile.LinkLayer == "CCSDS-MARY" {
		decoder.mary, err = mary.NewStreamDemodulator(mary.Config{SampleRate: profile.SampleRate, SymbolRate: profile.SymbolRate, BitsPerSymbol: profile.BitsPerSymbol, CarrierHz: profile.CarrierHz})
		if err != nil {
			return nil, err
		}
		decoder.maryBurstGeneration = decoder.mary.BurstGeneration()
	} else if profile.LinkLayer == "CCSDS" && profile.BitRate == 2400 {
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
	d.maryFECRearmPending = false
	if d.fsk != nil {
		d.fsk.Reset()
	}
	if d.gmsk2400 != nil {
		d.gmsk2400.Reset()
	}
	if d.mary != nil {
		d.mary.Reset()
		d.maryBurstGeneration = d.mary.BurstGeneration()
	}
	if d.ax25 != nil {
		d.ax25.Reset()
	}
	d.ccsdsBits = nil
	d.resetMARYFEC()
	d.packetFECRecent = make(map[string]map[uint16][]byte)
	d.packetFECRecentOrder = make(map[string][]uint16)
	d.packetFECRecentUpdated = make(map[string]time.Time)
	d.packetFECGroups = make(map[string]*packetFECGroup)
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
	if d.maryFECRearmPending {
		d.resetMARYFEC()
		d.ccsdsBits = nil
		d.maryFECRearmPending = false
	}
	native := d.resampler.Feed(samples)
	if len(native) == 0 {
		return nil, nil
	}
	var bits []bool
	var err error
	if d.mary != nil {
		soft := d.mary.FeedSoft(native)
		packets := d.consumeMARYSoft(soft)
		if generation := d.mary.BurstGeneration(); generation != d.maryBurstGeneration {
			d.maryBurstGeneration = generation
			d.resetMARYFEC()
			d.ccsdsBits = nil
		}
		if d.maryFECFinished {
			// The burst descriptor gives an exact coded length. Re-arm immediately
			// after consuming it; the next call may contain an inter-burst gap or
			// the next training word. Keep image de-duplication and FEC recovery
			// caches intact across this physical-burst boundary.
			d.mary.Rearm()
			d.maryBurstGeneration = d.mary.BurstGeneration()
			d.maryFECRearmPending = true
		}
		if d.maryFECError != nil {
			return packets, d.maryFECError
		}
		return packets, nil
	} else if d.gmsk2400 != nil {
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
				packet.HasAX25SSID = true
				packet.AX25SourceSSID = frame.Source.SSID
				packet.AX25Destination = frame.Destination.Callsign
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
		if fec.IsPacketParityPayload(payload) {
			packets = append(packets, d.handlePacketParity(payload)...)
			start += streamCCSDSFrameBits
			continue
		}
		packet, parseErr := parseImagePacket(d.profile, payload)
		if parseErr == nil {
			d.rememberPacketForFEC(packet)
			if d.markNew(packet) {
				packets = append(packets, packet)
			}
			packets = append(packets, d.tryPendingPacketFEC(packet.Callsign, packet.ImageID)...)
		}
		start += streamCCSDSFrameBits
	}
	if start > 0 {
		d.ccsdsBits = append([]bool(nil), d.ccsdsBits[start:]...)
	}
	return packets
}

func (d *StreamDecoder) resetMARYFEC() {
	d.maryFECMode = -1
	d.maryHeaderSoft = nil
	d.maryFECHeader = fec.BurstHeader{}
	d.maryFECDecoder = nil
	d.maryFECSoft = nil
	d.maryFECBlocksDone = 0
	d.maryFECBitsDone = 0
	d.maryFECDecodedBits = 0
	d.maryFECDecodedBytes = 0
	d.maryFECByte = 0
	d.maryFECByteBits = 0
	d.maryFECCRC = 0
	d.maryFECFinished = false
	d.maryFECError = nil
}

func (d *StreamDecoder) consumeMARYSoft(llrs []float32) []ReceivedPacket {
	if len(llrs) == 0 || d.maryFECFinished {
		return nil
	}
	if d.maryFECMode < 0 {
		d.maryHeaderSoft = append(d.maryHeaderSoft, llrs...)
		const headerBits = fec.BurstHeaderWireBits
		if len(d.maryHeaderSoft) < headerBits {
			return nil
		}
		headerBytes, combineErr := fec.CombineRepeatedBurstHeader(d.maryHeaderSoft[:headerBits])
		var header fec.BurstHeader
		var err error
		if combineErr == nil {
			header, err = fec.ParseBurstHeader(headerBytes)
		} else {
			err = combineErr
		}
		if err == nil {
			d.maryFECMode = 1
			d.maryFECHeader = header
			d.maryFECDecoder = fec.NewViterbiDecoder()
			remaining := append([]float32(nil), d.maryHeaderSoft[headerBits:]...)
			d.maryHeaderSoft = nil
			return d.consumeMARYFECSoft(remaining)
		}
		// A recognizable SGV2 marker with a failed CRC is a damaged new-format
		// header, not a legacy burst. Do not silently reinterpret coded payload
		// bits as old hard-decision CCSDS data.
		if combineErr == nil && len(headerBytes) >= 4 && string(headerBytes[:4]) == "SDF2" {
			d.maryFECError = err
			d.maryFECFinished = true
			d.maryHeaderSoft = nil
			return nil
		}
		// The first bytes of a legacy M-ary burst are the CCSDS randomizer
		// preamble. If no recognized descriptor is present, preserve the old
		// hard-decision path byte-for-byte.
		d.maryFECMode = 0
		legacy := softToBits(d.maryHeaderSoft)
		d.maryHeaderSoft = nil
		return d.consumeCCSDSBits(legacy)
	}
	if d.maryFECMode == 0 {
		return d.consumeCCSDSBits(softToBits(llrs))
	}
	return d.consumeMARYFECSoft(llrs)
}

func (d *StreamDecoder) consumeMARYFECSoft(llrs []float32) []ReceivedPacket {
	if len(llrs) > 0 {
		d.maryFECSoft = append(d.maryFECSoft, llrs...)
	}
	codedBits := d.maryFECHeader.CodedBits()
	var packets []ReceivedPacket
	for len(d.maryFECSoft) >= fec.InterleaveBlockBits && d.maryFECBitsDone < codedBits {
		block, err := fec.DeinterleaveSoftBlock(d.maryFECSoft[:fec.InterleaveBlockBits])
		if err != nil {
			d.maryFECError = err
			d.maryFECFinished = true
			return packets
		}
		d.maryFECSoft = d.maryFECSoft[fec.InterleaveBlockBits:]
		remaining := codedBits - d.maryFECBitsDone
		use := int64(len(block))
		if use > remaining {
			use = remaining
		}
		decoded, decodeErr := d.maryFECDecoder.Feed(block[:int(use)])
		if decodeErr != nil {
			d.maryFECError = decodeErr
			d.maryFECFinished = true
			return packets
		}
		packets = append(packets, d.consumeMARYFECDecodedBits(decoded)...)
		d.maryFECBitsDone += use
		d.maryFECBlocksDone++
	}
	if d.maryFECBitsDone >= codedBits {
		tail, err := d.maryFECDecoder.Flush()
		if err != nil {
			d.maryFECError = err
			d.maryFECFinished = true
			return packets
		}
		packets = append(packets, d.consumeMARYFECDecodedBits(tail)...)
		d.maryFECFinished = true
		if d.maryFECDecodedBits != int64(d.maryFECHeader.RawBytes)*8 || d.maryFECDecodedBytes != d.maryFECHeader.RawBytes {
			d.maryFECError = fmt.Errorf("soft-FEC decoded %d bytes, expected %d", d.maryFECDecodedBytes, d.maryFECHeader.RawBytes)
		} else if d.maryFECCRC != d.maryFECHeader.CRC32 {
			d.maryFECError = fmt.Errorf("soft-FEC burst CRC mismatch: got %08x want %08x", d.maryFECCRC, d.maryFECHeader.CRC32)
		}
	}
	return packets
}

func (d *StreamDecoder) consumeMARYFECDecodedBits(bits []bool) []ReceivedPacket {
	remaining := int64(d.maryFECHeader.RawBytes)*8 - d.maryFECDecodedBits
	if remaining <= 0 {
		return nil
	}
	if int64(len(bits)) > remaining {
		bits = bits[:int(remaining)]
	}
	for _, bit := range bits {
		d.maryFECByte <<= 1
		if bit {
			d.maryFECByte |= 1
		}
		d.maryFECByteBits++
		d.maryFECDecodedBits++
		if d.maryFECByteBits == 8 {
			d.maryFECCRC = crc32.Update(d.maryFECCRC, crc32.IEEETable, []byte{d.maryFECByte})
			d.maryFECDecodedBytes++
			d.maryFECByte = 0
			d.maryFECByteBits = 0
		}
	}
	return d.consumeCCSDSBits(bits)
}

func softToBits(llrs []float32) []bool {
	bits := make([]bool, len(llrs))
	for i, value := range llrs {
		bits[i] = value < 0
	}
	return bits
}

func (d *StreamDecoder) rememberPacketForFEC(packet ReceivedPacket) {
	now := time.Now()
	d.prunePacketFEC(now)
	if len(packet.Raw) != ssdv.PacketSize || packet.Raw[1] != ssdv.TypeFEC || packet.PacketID >= 0xfffe {
		return
	}
	key := fmt.Sprintf("%s|%s|%03d", packet.Profile, packet.Callsign, packet.ImageID)
	if d.packetFECRecent == nil {
		d.packetFECRecent = make(map[string]map[uint16][]byte)
	}
	if d.packetFECRecentOrder == nil {
		d.packetFECRecentOrder = make(map[string][]uint16)
	}
	if d.packetFECRecentUpdated == nil {
		d.packetFECRecentUpdated = make(map[string]time.Time)
	}
	if d.packetFECRecent[key] == nil {
		d.packetFECRecent[key] = make(map[uint16][]byte)
	}
	if _, exists := d.packetFECRecent[key][packet.PacketID]; !exists {
		d.packetFECRecentOrder[key] = append(d.packetFECRecentOrder[key], packet.PacketID)
	}
	d.packetFECRecent[key][packet.PacketID] = append([]byte(nil), packet.Raw...)
	d.packetFECRecentUpdated[key] = now
	for len(d.packetFECRecentOrder[key]) > 512 {
		oldest := d.packetFECRecentOrder[key][0]
		d.packetFECRecentOrder[key] = d.packetFECRecentOrder[key][1:]
		delete(d.packetFECRecent[key], oldest)
	}
}

func (d *StreamDecoder) prunePacketFEC(now time.Time) {
	for key, updated := range d.packetFECRecentUpdated {
		if now.Sub(updated) > 2*time.Minute {
			delete(d.packetFECRecent, key)
			delete(d.packetFECRecentOrder, key)
			delete(d.packetFECRecentUpdated, key)
		}
	}
	for key, group := range d.packetFECGroups {
		if now.Sub(group.last) > 30*time.Second {
			delete(d.packetFECGroups, key)
		}
	}
}

func (d *StreamDecoder) handlePacketParity(payload []byte) []ReceivedPacket {
	d.prunePacketFEC(time.Now())
	if d.packetFECGroups == nil {
		d.packetFECGroups = make(map[string]*packetFECGroup)
	}
	parity, err := fec.ParsePacketParityPayload(payload)
	if err != nil {
		return nil
	}
	callsign := ssdv.DecodeBase40(parity.Callsign)
	if callsign == "" {
		return nil
	}
	imageKey := fmt.Sprintf("%s|%s|%03d", d.profile.ID, callsign, parity.ImageID)
	groupKey := fmt.Sprintf("%s|%d", imageKey, parity.IDs[0])
	group := d.packetFECGroups[groupKey]
	if group == nil {
		if len(d.packetFECGroups) >= 128 {
			oldestKey := ""
			var oldest time.Time
			for key, candidate := range d.packetFECGroups {
				if oldestKey == "" || candidate.last.Before(oldest) {
					oldestKey, oldest = key, candidate.last
				}
			}
			if oldestKey != "" {
				delete(d.packetFECGroups, oldestKey)
			}
		}
		group = &packetFECGroup{imageKey: imageKey}
		d.packetFECGroups[groupKey] = group
	}
	copyParity := parity
	group.parity[parity.Shard] = &copyParity
	group.last = time.Now()
	return d.tryRecoverPacketFEC(groupKey, group)
}

func (d *StreamDecoder) tryPendingPacketFEC(callsign string, imageID uint8) []ReceivedPacket {
	imageKey := fmt.Sprintf("%s|%s|%03d", d.profile.ID, callsign, imageID)
	var out []ReceivedPacket
	for key, group := range d.packetFECGroups {
		if group.imageKey == imageKey {
			out = append(out, d.tryRecoverPacketFEC(key, group)...)
		}
	}
	return out
}

func (d *StreamDecoder) tryRecoverPacketFEC(key string, group *packetFECGroup) []ReceivedPacket {
	var parities []fec.PacketParityPayload
	for _, parity := range group.parity {
		if parity != nil {
			parities = append(parities, *parity)
		}
	}
	if len(parities) == 0 {
		return nil
	}
	recent := d.packetFECRecent[group.imageKey]
	recovered, err := fec.RecoverPacketParity(parities, recent)
	if err != nil {
		return nil
	}
	delete(d.packetFECGroups, key)
	var out []ReceivedPacket
	for _, raw := range recovered {
		packet, parseErr := parseImagePacket(d.profile, raw)
		if parseErr != nil {
			continue
		}
		packet.FECRecovered = true
		d.rememberPacketForFEC(packet)
		if d.markNew(packet) {
			out = append(out, packet)
		}
	}
	return out
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
		item.GIFMode, item.GIFKeyframe, item.GIFControl, item.GIFDelayCS = parsed.GIFMode, parsed.GIFKeyframe, parsed.GIFControl, parsed.GIFDelayCS
		item.GIFFrameStart, item.GIFFrameEnd, item.GIFDataPackets = parsed.GIFFrameStart, parsed.GIFFrameEnd, parsed.GIFDataPackets
		item.GIFStart, item.GIFEnd = parsed.GIFStart, parsed.GIFEnd
		if parsed.GIFMode {
			item.Codec = "SSDV-GIF"
		}
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
	if d.imageStates == nil {
		d.imageStates = make(map[string]*packetDedupState)
	}
	key := fmt.Sprintf("%s|%s|%03d", packet.Profile, packet.Callsign, packet.ImageID)
	state := d.imageStates[key]
	if state != nil && packet.GIFMode && packet.GIFStart && state.gifStreamEnd {
		// A repeated GIF burst may reuse the same 8-bit image IDs and packet
		// IDs. Reset only after the preceding stream-end marker, so the three
		// physical copies of this burst's start marker remain de-duplicated.
		state = nil
	}
	if state != nil && packet.GIFMode && state.complete && !packet.GIFControl {
		// GIF frame IDs are 8-bit. A completed frame ID can be reused after
		// wraparound; the next packet starts a fresh deduplication state.
		state = nil
	}
	if state != nil && packet.PacketID == 0 {
		hasPacketZero := len(state.packetZero) > 0
		samePacketZero := hasPacketZero && bytes.Equal(state.packetZero, packet.Raw)
		idleFor := time.Since(state.lastUpdate)
		// Packet 0 may arrive last in an out-of-order burst. A high packet ID
		// alone is not evidence of a new image. Prefer a changed packet-0
		// fingerprint, an already-complete image, or a long inter-image pause.
		newSession := (hasPacketZero && !samePacketZero && len(state.seen) >= 1) ||
			(hasPacketZero && state.complete) ||
			(idleFor > 5*time.Second && (hasPacketZero || len(state.seen) >= 10))
		if newSession {
			state = nil
		}
	}
	if state == nil {
		state = &packetDedupState{seen: make(map[uint16]struct{})}
		d.imageStates[key] = state
	}
	if _, exists := state.seen[packet.PacketID]; exists {
		state.lastUpdate = time.Now()
		return false
	}
	state.seen[packet.PacketID] = struct{}{}
	if packet.PacketID == 0 {
		state.packetZero = append(state.packetZero[:0], packet.Raw...)
	}
	if packet.GIFControl {
		state.complete = true
		if packet.GIFEnd {
			state.gifStreamEnd = true
		}
	} else if packet.EOI && !packet.GIFMode {
		state.eoiPacketID = packet.PacketID
		state.hasEOI = true
	}
	if state.hasEOI && !packet.GIFMode {
		state.complete = true
		for id := uint16(0); id <= state.eoiPacketID; id++ {
			if _, exists := state.seen[id]; !exists {
				state.complete = false
				break
			}
		}
	}
	state.lastUpdate = time.Now()

	// Bound retained image sessions. A normal image has at most a few hundred
	// packet IDs; evict the least recently touched session when the map grows.
	if len(d.imageStates) > streamMaxImageStates {
		oldestKey := ""
		var oldest time.Time
		for candidateKey, candidate := range d.imageStates {
			if candidateKey == key {
				continue
			}
			if oldestKey == "" || candidate.lastUpdate.Before(oldest) {
				oldestKey, oldest = candidateKey, candidate.lastUpdate
			}
		}
		if oldestKey != "" {
			delete(d.imageStates, oldestKey)
		}
	}
	return true
}
