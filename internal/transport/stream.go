package transport

import (
	"bytes"
	"errors"
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

var errSDF3GroupCRC = errors.New("SDF3 group CRC32 mismatch")

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

	maryFECMode              int8 // -1 undecided, 0 legacy hard-decision, 1 SDF2 soft-FEC, 2 SDF3 packetized, 3 legacy with SDF3 probing
	maryHeaderSoft           []float32
	maryLegacyProbeSoft      []float32
	maryFECHeader            fec.BurstHeader
	maryFECDecoder           *fec.ViterbiDecoder
	maryFECSoft              []float32
	maryFECBlocksDone        int64
	maryFECBitsDone          int64
	maryFECDecodedBits       int64
	maryFECDecodedBytes      uint32
	maryFECByte              byte
	maryFECByteBits          uint8
	maryFECCRC               uint32
	maryFECFinished          bool
	maryFECError             error
	maryPacketHeader         *fec.PacketBurstHeader
	maryPacketSoft           []float32
	maryPacketBodyBits       int
	maryPacketNextHeaderScan int
	marySDF3PacketsOK        uint64
	marySDF3PacketsBad       uint64
	marySDF3Resyncs          uint64
	marySDF3HeaderFailures   uint64
	marySDF3HeaderRecoveries uint64
	marySDF3LastError        string
	inputSamples             uint64
	decodedPackets           uint64
	lastValidPacketAt        time.Time

	packetFECRecent        map[string]map[uint16][]byte
	packetFECRecentOrder   map[string][]uint16
	packetFECRecentUpdated map[string]time.Time
	packetFECGroups        map[string]*packetFECGroup
}

type DecoderDiagnostics struct {
	Profile              string  `json:"profile"`
	InputSamples         uint64  `json:"inputSamples"`
	DecodedPackets       uint64  `json:"decodedPackets"`
	ValidPacketRecent    bool    `json:"validPacketRecent"`
	LastPacketAgeMS      int64   `json:"lastPacketAgeMs"`
	MARYLocked           bool    `json:"maryLocked"`
	FSKLocked            bool    `json:"fskLocked"`
	GMSKLocked           bool    `json:"gmskLocked"`
	MARYBufferedSamples  int     `json:"maryBufferedSamples"`
	MARYSignalRMS        float64 `json:"marySignalRms"`
	MARYSilenceSamples   int     `json:"marySilenceSamples"`
	MARYPacketizedMode   bool    `json:"maryPacketizedMode"`
	MARYReacquireCount   uint64  `json:"maryReacquireCount"`
	MARYEVMRMS           float64 `json:"maryEvmRms"`
	MARYSNRDB            float64 `json:"marySnrDb"`
	MARYSamplesPerSymbol float64 `json:"marySamplesPerSymbol"`
	MARYClockOffsetPPM   float64 `json:"maryClockOffsetPpm"`
	MARYEVMSamples       uint64  `json:"maryEvmSamples"`
	MARYSNRValid         bool    `json:"marySnrValid"`
	FECMode              string  `json:"fecMode"`
	FECBufferedBits      int     `json:"fecBufferedBits"`
	FECExpectedBits      int     `json:"fecExpectedBits"`
	FECChunksOK          uint64  `json:"fecChunksOk"`
	FECChunksBad         uint64  `json:"fecChunksBad"`
	FECResyncs           uint64  `json:"fecResyncs"`
	SDF3HeaderFailures   uint64  `json:"sdf3HeaderFailures"`
	SDF3HeaderRecoveries uint64  `json:"sdf3HeaderRecoveries"`
	LastFECError         string  `json:"lastFecError,omitempty"`
}

func (d *StreamDecoder) Diagnostics() DecoderDiagnostics {
	if d == nil {
		return DecoderDiagnostics{}
	}
	status := DecoderDiagnostics{Profile: string(d.profile.ID), InputSamples: d.inputSamples, DecodedPackets: d.decodedPackets,
		FECChunksOK: d.marySDF3PacketsOK, FECChunksBad: d.marySDF3PacketsBad, FECResyncs: d.marySDF3Resyncs,
		SDF3HeaderFailures: d.marySDF3HeaderFailures, SDF3HeaderRecoveries: d.marySDF3HeaderRecoveries,
		LastFECError: d.marySDF3LastError}
	if !d.lastValidPacketAt.IsZero() {
		status.LastPacketAgeMS = time.Since(d.lastValidPacketAt).Milliseconds()
		status.ValidPacketRecent = status.LastPacketAgeMS >= 0 && status.LastPacketAgeMS <= int64(3*time.Second/time.Millisecond)
	}
	if d.fsk != nil {
		status.FSKLocked = d.fsk.Locked()
	}
	if d.gmsk2400 != nil {
		status.GMSKLocked = d.gmsk2400.CarrierLocked()
	}
	if d.mary != nil {
		modem := d.mary.Diagnostics()
		status.MARYLocked = modem.Locked
		status.MARYBufferedSamples = modem.BufferedSamples
		status.MARYSignalRMS = modem.SignalRMS
		status.MARYSilenceSamples = modem.SilenceSamples
		status.MARYPacketizedMode = modem.PacketizedMode
		status.MARYReacquireCount = modem.ReacquireCount
		status.MARYEVMRMS = modem.EVMRMS
		status.MARYSNRDB = modem.SNRDB
		status.MARYSamplesPerSymbol = modem.SamplesPerSymbol
		status.MARYClockOffsetPPM = modem.ClockOffsetPPM
		status.MARYEVMSamples = modem.SNRSamples
		status.MARYSNRValid = modem.SNRValid
	}
	switch d.maryFECMode {
	case -1:
		status.FECMode = "acquiring"
	case 0:
		status.FECMode = "legacy-hard"
	case 3:
		status.FECMode = "legacy-probing-sdf3"
		status.FECBufferedBits = len(d.maryLegacyProbeSoft)
		status.FECExpectedBits = fec.BurstHeaderWireBits
	case 1:
		status.FECMode = "sdf2-image-burst"
		status.FECBufferedBits = len(d.maryFECSoft) + len(d.maryHeaderSoft)
		status.FECExpectedBits = int(max(int64(0), d.maryFECHeader.CodedBits()-d.maryFECBitsDone))
	case 2:
		status.FECMode = "sdf3-packetized"
		status.FECBufferedBits = len(d.maryPacketSoft)
		status.FECExpectedBits = d.maryPacketBodyBits
	default:
		status.FECMode = "unknown"
	}
	return status
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
	d.lastValidPacketAt = time.Time{}
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
		d.inputSamples += uint64(len(samples))
		soft := d.mary.FeedSoft(native)
		packets := d.consumeMARYSoft(soft)
		d.decodedPackets += uint64(len(packets))
		d.noteValidPackets(packets)
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
			d.noteValidPackets(packets)
			return packets, nil
		}
		bits = levels
	}
	if err != nil {
		return nil, err
	}
	packets := d.consumeCCSDSBits(bits)
	d.noteValidPackets(packets)
	return packets, nil
}

func (d *StreamDecoder) noteValidPackets(packets []ReceivedPacket) {
	if d != nil && len(packets) > 0 {
		d.lastValidPacketAt = time.Now()
	}
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
	if d.mary != nil {
		d.mary.SetPacketizedMode(false)
	}
	d.maryFECMode = -1
	d.maryHeaderSoft = nil
	d.maryLegacyProbeSoft = nil
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
	d.maryPacketHeader = nil
	d.maryPacketSoft = nil
	d.maryPacketBodyBits = 0
	d.maryPacketNextHeaderScan = 0
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

		// Search the complete acquisition window instead of assuming that the
		// demodulator's first returned payload bit is exactly bit zero of the
		// repeated SDF3 descriptor. One or two late/early symbols during carrier
		// acquisition must not permanently demote a new-format stream to legacy.
		if offset, packetHeader, ok := findSDF3PacketHeaderFrom(d.maryHeaderSoft, 0); ok {
			if offset > 0 {
				d.marySDF3Resyncs++
			}
			d.maryFECMode = 2
			d.mary.SetPacketizedMode(true)
			d.maryPacketHeader = &packetHeader
			d.maryPacketBodyBits = int(packetHeader.InterleavedBits())
			remaining := append([]float32(nil), d.maryHeaderSoft[offset+headerBits:]...)
			d.maryHeaderSoft = nil
			return d.consumeMARYPacketizedSoft(remaining)
		}

		// Keep SDF2 compatibility for older image-burst recordings. A damaged
		// descriptor is a recoverable acquisition miss, except for a recognizable
		// SDF2 marker whose own header CRC proves corruption.
		headerBytes, combineErr := fec.CombineRepeatedBurstHeader(d.maryHeaderSoft[:headerBits])
		if combineErr == nil && len(headerBytes) >= 4 && string(headerBytes[:4]) == "SDF2" {
			header, err := fec.ParseBurstHeader(headerBytes)
			if err != nil {
				d.maryFECError = err
				d.maryFECFinished = true
				d.maryHeaderSoft = nil
				return nil
			}
			d.maryFECMode = 1
			d.maryFECHeader = header
			d.maryFECDecoder = fec.NewViterbiDecoder()
			remaining := append([]float32(nil), d.maryHeaderSoft[headerBits:]...)
			d.maryHeaderSoft = nil
			return d.consumeMARYFECSoft(remaining)
		}

		// Do not permanently switch to hard-decision legacy parsing after one
		// failed SDF3 header. Keep a rolling soft-bit window and continue probing
		// every later packet boundary; the next good descriptor can restore SDF3
		// decoding even when continuous RF noise prevents silence-based re-lock.
		d.marySDF3HeaderFailures++
		d.maryFECMode = 3
		d.maryLegacyProbeSoft = append(d.maryLegacyProbeSoft[:0], d.maryHeaderSoft...)
		d.maryHeaderSoft = nil
		return d.consumeMARYLegacyProbe(nil)
	}
	switch d.maryFECMode {
	case 0:
		return d.consumeCCSDSBits(softToBits(llrs))
	case 1:
		return d.consumeMARYFECSoft(llrs)
	case 2:
		return d.consumeMARYPacketizedSoft(llrs)
	case 3:
		return d.consumeMARYLegacyProbe(llrs)
	default:
		return nil
	}
}

// consumeMARYLegacyProbe preserves legacy bitstream reception while retaining
// enough soft bits to find a later valid SDF3 descriptor. Only bits that have
// moved out of the overlap window are committed to the legacy CCSDS scanner.
// A descriptor is accepted only after repetition combining and its header CRC.
func (d *StreamDecoder) consumeMARYLegacyProbe(llrs []float32) []ReceivedPacket {
	combined := make([]float32, 0, len(d.maryLegacyProbeSoft)+len(llrs))
	combined = append(combined, d.maryLegacyProbeSoft...)
	combined = append(combined, llrs...)
	if len(combined) == 0 {
		return nil
	}

	if offset, header, ok := findSDF3PacketHeaderFrom(combined, 0); ok {
		var packets []ReceivedPacket
		if offset > 0 {
			packets = append(packets, d.consumeCCSDSBits(softToBits(combined[:offset]))...)
			d.marySDF3Resyncs++
		}
		d.maryLegacyProbeSoft = nil
		d.maryFECMode = 2
		d.marySDF3HeaderRecoveries++
		d.mary.SetPacketizedMode(true)
		d.maryPacketHeader = &header
		d.maryPacketBodyBits = int(header.InterleavedBits())
		d.maryPacketNextHeaderScan = 0
		remaining := combined[offset+fec.BurstHeaderWireBits:]
		packets = append(packets, d.consumeMARYPacketizedSoft(remaining)...)
		return packets
	}

	keep := fec.BurstHeaderWireBits - 1
	stableEnd := len(combined) - keep
	if stableEnd <= 0 {
		d.maryLegacyProbeSoft = combined
		return nil
	}
	packets := d.consumeCCSDSBits(softToBits(combined[:stableEnd]))
	d.maryLegacyProbeSoft = append(d.maryLegacyProbeSoft[:0], combined[stableEnd:]...)
	return packets
}

// headerBytesToLLRs is intentionally a no-op copy helper used to make ownership
// explicit when a malformed first SDF3 descriptor is retained for resync.
func headerBytesToLLRs(values []float32) []float32 { return values }

func (d *StreamDecoder) consumeMARYPacketizedSoft(llrs []float32) []ReceivedPacket {
	if len(llrs) > 0 {
		d.maryPacketSoft = append(d.maryPacketSoft, llrs...)
	}
	var packets []ReceivedPacket
	for {
		if d.maryPacketHeader == nil {
			offset, header, ok := findSDF3PacketHeader(d.maryPacketSoft)
			if !ok {
				keep := fec.BurstHeaderWireBits - 1
				if len(d.maryPacketSoft) > keep {
					d.maryPacketSoft = append(d.maryPacketSoft[:0], d.maryPacketSoft[len(d.maryPacketSoft)-keep:]...)
				}
				return packets
			}
			if offset > 0 {
				d.marySDF3Resyncs++
			}
			d.maryPacketSoft = d.maryPacketSoft[offset+fec.BurstHeaderWireBits:]
			d.maryPacketHeader = &header
			d.maryPacketBodyBits = int(header.InterleavedBits())
		}

		// A valid next SDF3 descriptor before the advertised body length means
		// the current radio/audio chunk was truncated. Scan only newly completed
		// candidate offsets: re-scanning the accumulated FEC body on every audio
		// callback makes the live decoder's work grow quadratically with duration.
		if offset, next, ok := findSDF3PacketHeaderFrom(d.maryPacketSoft, d.maryPacketNextHeaderScan); ok && offset < d.maryPacketBodyBits {
			d.marySDF3PacketsBad++
			d.marySDF3LastError = "previous SDF3 group truncated before next header"
			d.maryPacketSoft = d.maryPacketSoft[offset+fec.BurstHeaderWireBits:]
			d.maryPacketHeader = &next
			d.maryPacketBodyBits = int(next.InterleavedBits())
			d.maryPacketNextHeaderScan = 0
			d.marySDF3Resyncs++
			continue
		}
		// Keep the final headerBits-1 positions eligible because a descriptor can
		// straddle two arbitrary audio callback blocks. All earlier offsets have
		// already been checked exactly once for this current body.
		d.maryPacketNextHeaderScan = len(d.maryPacketSoft) - fec.BurstHeaderWireBits + 1
		if d.maryPacketNextHeaderScan < 0 {
			d.maryPacketNextHeaderScan = 0
		}
		if len(d.maryPacketSoft) < d.maryPacketBodyBits {
			return packets
		}

		header := *d.maryPacketHeader
		body := d.maryPacketSoft[:d.maryPacketBodyBits]
		d.maryPacketSoft = d.maryPacketSoft[d.maryPacketBodyBits:]
		d.maryPacketHeader = nil
		d.maryPacketBodyBits = 0
		d.maryPacketNextHeaderScan = 0
		raw, err := decodeSDF3PacketBody(header, body)
		if err != nil {
			d.marySDF3PacketsBad++
			d.marySDF3LastError = err.Error()
			// A group CRC failure does not mean every CCSDS frame in the group
			// is bad. Keep scanning the decoded bytes: the CCSDS ASM, image packet
			// CRC and SSDV RS8 checks validate each physical frame independently.
			// This salvages unaffected frames after a short fade or burst error.
			if errors.Is(err, errSDF3GroupCRC) && len(raw) > 0 {
				packets = append(packets, d.consumeCCSDSBits(softBytesToBits(raw))...)
			}
		} else {
			d.marySDF3PacketsOK++
			d.marySDF3LastError = ""
			packets = append(packets, d.consumeCCSDSBits(softBytesToBits(raw))...)
		}

		// SDF3 advertises the exact end of each independently protected group.
		// Do not depend on an audio-level silence threshold to release the old
		// carrier lock: a real receiver has continuous RF noise during the guard,
		// and at low channel SNR that noise may be comparable to the old threshold.
		// If this callback already contains a complete next group header, keep
		// scanning it; otherwise force a clean symbol/carrier re-acquisition now.
		if _, _, nextReady := findSDF3PacketHeader(d.maryPacketSoft); !nextReady {
			d.maryPacketSoft = nil
			d.mary.Rearm()
			return packets
		}
	}
}

func findSDF3PacketHeader(llrs []float32) (int, fec.PacketBurstHeader, bool) {
	return findSDF3PacketHeaderFrom(llrs, 0)
}

// findSDF3PacketHeaderFrom resumes the descriptor search at start. This keeps
// packetized receive work linear as soft bits arrive in short audio callback
// blocks instead of checking every old body offset repeatedly.
func findSDF3PacketHeaderFrom(llrs []float32, start int) (int, fec.PacketBurstHeader, bool) {
	const headerBits = fec.BurstHeaderWireBits
	if start < 0 {
		start = 0
	}
	for offset := start; offset+headerBits <= len(llrs); offset++ {
		if !hasSDF3MagicAt(llrs, offset) {
			continue
		}
		combined, err := fec.CombineRepeatedBurstHeader(llrs[offset : offset+headerBits])
		if err != nil {
			continue
		}
		header, err := fec.ParsePacketBurstHeader(combined)
		if err == nil {
			return offset, header, true
		}
	}
	return -1, fec.PacketBurstHeader{}, false
}

func hasSDF3MagicAt(llrs []float32, offset int) bool {
	if offset < 0 || offset+96 > len(llrs) {
		return false
	}
	var magic [4]byte
	bitPos := 0
	for byteIndex := 0; byteIndex < len(magic); byteIndex++ {
		for bit := 0; bit < 8; bit++ {
			combined := llrs[offset+bitPos] + llrs[offset+bitPos+1] + llrs[offset+bitPos+2]
			if combined < 0 {
				magic[byteIndex] |= 1 << uint(7-bit)
			}
			bitPos += fec.BurstHeaderRepeat
		}
	}
	return magic == [4]byte{'S', 'D', 'F', '3'}
}

func decodeSDF3PacketBody(header fec.PacketBurstHeader, body []float32) ([]byte, error) {
	if len(body) != int(header.InterleavedBits()) {
		return nil, fmt.Errorf("SDF3 group has %d interleaved bits, expected %d", len(body), header.InterleavedBits())
	}
	codedRemaining := header.CodedBits()
	codedSoft := make([]float32, 0, codedRemaining)
	for offset := 0; offset < len(body); offset += fec.InterleaveBlockBits {
		deinterleaved, err := fec.DeinterleaveSoftBlock(body[offset : offset+fec.InterleaveBlockBits])
		if err != nil {
			return nil, err
		}
		use := int64(len(deinterleaved))
		if use > codedRemaining {
			use = codedRemaining
		}
		if use > 0 {
			codedSoft = append(codedSoft, deinterleaved[:int(use)]...)
			codedRemaining -= use
		}
	}
	if codedRemaining != 0 {
		return nil, fmt.Errorf("SDF3 group is missing %d coded bits", codedRemaining)
	}

	inputBits := int64(header.RawBytes)*8 + fec.ConvMemory
	var err error
	switch header.CodeProfile {
	case fec.PacketFECProfileRate34:
		codedSoft, err = fec.UnpunctureConvRate3of4Soft(codedSoft, inputBits)
	case fec.PacketFECProfileRate23:
		codedSoft, err = fec.UnpunctureConvRate2of3Soft(codedSoft, inputBits)
	case fec.PacketFECProfileRate56:
		codedSoft, err = fec.UnpunctureConvRate5of6Soft(codedSoft, inputBits)
	}
	if err != nil {
		return nil, err
	}
	decoder := fec.NewViterbiDecoder()
	decoded, err := decoder.Feed(codedSoft)
	if err != nil {
		return nil, err
	}
	tail, err := decoder.Flush()
	if err != nil {
		return nil, err
	}
	decoded = append(decoded, tail...)
	expectedBits := int(header.RawBytes) * 8
	if len(decoded) < expectedBits {
		return nil, fmt.Errorf("SDF3 decoded %d bits, expected %d", len(decoded), expectedBits)
	}
	decoded = decoded[:expectedBits]
	raw := softBitsToBytes(decoded)
	if crc32.ChecksumIEEE(raw) != header.CRC32 {
		return raw, fmt.Errorf("%w: got %08x want %08x", errSDF3GroupCRC, crc32.ChecksumIEEE(raw), header.CRC32)
	}
	return raw, nil
}

func softBytesToBits(data []byte) []bool {
	out := make([]bool, 0, len(data)*8)
	for _, value := range data {
		for bit := 7; bit >= 0; bit-- {
			out = append(out, value&(1<<uint(bit)) != 0)
		}
	}
	return out
}

func softBitsToBytes(bits []bool) []byte {
	out := make([]byte, len(bits)/8)
	for i, value := range bits[:len(out)*8] {
		if value {
			out[i/8] |= 1 << uint(7-i%8)
		}
	}
	return out
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
