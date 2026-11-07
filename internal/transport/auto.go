package transport

import (
	"fmt"
	"time"

	"github.com/BH2VSQ/SATSSDV-Go/internal/domain"
	"github.com/BH2VSQ/SATSSDV-Go/internal/dsp"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ax25"
	"github.com/BH2VSQ/SATSSDV-Go/internal/modem/ccsds"
)

const autoReacquireAfter = 5 * time.Second

// AutoStreamDecoder detects the image transport profile from valid link-layer
// frames and validated SSDV/PCSI packets. It shares the 1200-bit/s FSK front
// end between AX.25 and CCSDS, and the 2400-bit/s GMSK front end between SSDV
// and PCSI. Only the three 1600-baud M-ary hypotheses require separate symbol
// slicers. Once a profile is identified, unrelated decoder branches are
// paused until the stream has been idle long enough to permit reacquisition.
type AutoStreamDecoder struct {
	inputRate int

	resample1200 *dsp.StreamResampler
	fsk1200      *dsp.FSKStreamDemodulator
	ax25         *ax25.StreamDecoder
	ccsds1200    []bool

	resample2400 *dsp.StreamResampler
	gmsk2400     *ccsds.ARCSSD2400Demodulator
	ccsds2400    []bool

	mary map[domain.TransportMode]*StreamDecoder

	dedup map[domain.TransportMode]*StreamDecoder

	lockedProfile domain.TransportMode
	lastPacketAt  time.Time
	lastError     string
}

type AutoDecoderDiagnostics struct {
	LockedProfile   string               `json:"lockedProfile"`
	LastPacketAgeMS int64                `json:"lastPacketAgeMs"`
	LastError       string               `json:"lastError,omitempty"`
	MARYCandidates  []DecoderDiagnostics `json:"maryCandidates,omitempty"`
}

func NewAutoStreamDecoder(inputSampleRate int) (*AutoStreamDecoder, error) {
	if inputSampleRate < 8000 || inputSampleRate > 192000 {
		return nil, fmt.Errorf("input sample rate %d is outside 8000..192000 Hz", inputSampleRate)
	}
	r1200, err := dsp.NewStreamResampler(inputSampleRate, 19200)
	if err != nil {
		return nil, err
	}
	r2400, err := dsp.NewStreamResampler(inputSampleRate, 19200)
	if err != nil {
		return nil, err
	}
	fsk, err := dsp.NewFSKStreamDemodulator(19200, 1200, 1200, 2200)
	if err != nil {
		return nil, err
	}
	d := &AutoStreamDecoder{
		inputRate: inputSampleRate, resample1200: r1200, fsk1200: fsk,
		ax25: &ax25.StreamDecoder{}, resample2400: r2400,
		gmsk2400: &ccsds.ARCSSD2400Demodulator{},
		mary:     make(map[domain.TransportMode]*StreamDecoder),
		dedup:    make(map[domain.TransportMode]*StreamDecoder),
	}
	if inputSampleRate >= 44100 {
		for _, mode := range []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600} {
			decoder, createErr := NewStreamDecoder(mode, inputSampleRate)
			if createErr != nil {
				return nil, createErr
			}
			d.mary[mode] = decoder
		}
	}
	return d, nil
}

func (d *AutoStreamDecoder) Reset() {
	if d == nil {
		return
	}
	d.resample1200.Reset()
	d.fsk1200.Reset()
	d.ax25.Reset()
	d.ccsds1200 = nil
	d.resample2400.Reset()
	d.gmsk2400.Reset()
	d.ccsds2400 = nil
	for _, decoder := range d.mary {
		decoder.Reset()
	}
	d.lockedProfile = ""
	d.lastPacketAt = time.Time{}
	d.lastError = ""
}

func (d *AutoStreamDecoder) Feed(samples []float32) ([]ReceivedPacket, error) {
	if d == nil {
		return nil, fmt.Errorf("automatic stream decoder is nil")
	}
	if samples == nil {
		d.Reset()
		return nil, nil
	}
	if len(samples) == 0 {
		return nil, nil
	}
	if d.lockedProfile == "" && belowAutoNoiseFloor(samples) {
		d.resetAcquisition()
		return nil, nil
	}
	if d.lockedProfile != "" && time.Since(d.lastPacketAt) > autoReacquireAfter {
		d.resetAcquisition()
		d.lockedProfile = ""
		d.lastPacketAt = time.Time{}
	}

	var packets []ReceivedPacket
	locked := d.lockedProfile
	use1200 := locked == "" || locked == domain.SSDVAX25 || locked == domain.SSDVCCSDS1200 || locked == domain.PCSICCSDS1200
	use2400 := locked == "" || locked == domain.SSDVCCSDS2400 || locked == domain.PCSICCSDS2400
	useMary := locked == "" || locked == domain.SSDVQPSK1600 || locked == domain.SSDV8PSK1600 || locked == domain.SSDV16QAM1600

	if use1200 {
		native := d.resample1200.Feed(samples)
		if len(native) > 0 {
			levels := d.fsk1200.Feed(native)
			if len(levels) > 0 {
				if locked == "" || locked == domain.SSDVAX25 {
					for _, frame := range d.ax25.Feed(levels) {
						packet, err := parseImagePacket(mustProfile(domain.SSDVAX25), frame.Information)
						if err != nil {
							continue
						}
						packet.HasAX25SSID = true
						packet.AX25SourceSSID = frame.Source.SSID
						packet.AX25Destination = frame.Destination.Callsign
						if d.accept(packet) {
							packets = append(packets, packet)
						}
					}
				}
				if locked == "" || locked == domain.SSDVCCSDS1200 || locked == domain.PCSICCSDS1200 {
					packets = append(packets, d.consumeAutoCCSDS(levels, &d.ccsds1200, domain.SSDVCCSDS1200, domain.PCSICCSDS1200)...)
				}
			}
		}
	}

	if use2400 {
		native := d.resample2400.Feed(samples)
		if len(native) > 0 {
			bits, err := d.gmsk2400.Feed(native, 19200)
			if err != nil {
				d.lastError = err.Error()
				return packets, err
			}
			if locked == "" || locked == domain.SSDVCCSDS2400 || locked == domain.PCSICCSDS2400 {
				packets = append(packets, d.consumeAutoCCSDS(bits, &d.ccsds2400, domain.SSDVCCSDS2400, domain.PCSICCSDS2400)...)
			}
		}
	}

	if useMary {
		for mode, decoder := range d.mary {
			if locked != "" && locked != mode {
				continue
			}
			decoded, err := decoder.Feed(samples)
			if err != nil {
				d.lastError = fmt.Sprintf("%s: %v", mode, err)
				continue
			}
			for _, packet := range decoded {
				if d.accept(packet) {
					packets = append(packets, packet)
				}
			}
		}
	}
	if len(packets) > 0 {
		d.lastError = ""
	}
	return packets, nil
}

// Diagnostics snapshots automatic-profile acquisition and all M-ary candidate
// states. It is called by the RX worker (the same goroutine as Feed).
func (d *AutoStreamDecoder) Diagnostics() AutoDecoderDiagnostics {
	if d == nil {
		return AutoDecoderDiagnostics{}
	}
	status := AutoDecoderDiagnostics{LockedProfile: string(d.lockedProfile), LastError: d.lastError}
	if !d.lastPacketAt.IsZero() {
		status.LastPacketAgeMS = time.Since(d.lastPacketAt).Milliseconds()
	}
	for _, mode := range []domain.TransportMode{domain.SSDVQPSK1600, domain.SSDV8PSK1600, domain.SSDV16QAM1600} {
		if decoder := d.mary[mode]; decoder != nil {
			status.MARYCandidates = append(status.MARYCandidates, decoder.Diagnostics())
		}
	}
	return status
}

func belowAutoNoiseFloor(samples []float32) bool {
	const peakFloor = 1e-6 // -120 dBFS full-scale amplitude
	for _, sample := range samples {
		if sample > peakFloor || sample < -peakFloor {
			return false
		}
	}
	return true
}

func (d *AutoStreamDecoder) consumeAutoCCSDS(bits []bool, buffer *[]bool, ssdvMode, pcsiMode domain.TransportMode) []ReceivedPacket {
	if len(bits) == 0 {
		return nil
	}
	*buffer = append(*buffer, bits...)
	var packets []ReceivedPacket
	start := 0
	for start+streamCCSDSFrameBits <= len(*buffer) {
		if !ccsds.MatchASMBits((*buffer)[start : start+32]) {
			start++
			continue
		}
		frame := make([]byte, 4+ccsds.PayloadLength)
		for i := 0; i < streamCCSDSFrameBits; i++ {
			if (*buffer)[start+i] {
				frame[i/8] |= 1 << uint(7-i%8)
			}
		}
		frame[0], frame[1], frame[2], frame[3] = 0x1a, 0xcf, 0xfc, 0x1d
		payload, err := ccsds.ParseDataUnit(frame)
		if err != nil {
			start++
			continue
		}
		for _, mode := range []domain.TransportMode{ssdvMode, pcsiMode} {
			profile, parseErr := domain.ParseProfile(string(mode))
			if parseErr != nil {
				continue
			}
			packet, parseErr := parseImagePacket(profile, payload)
			if parseErr == nil && d.accept(packet) {
				packets = append(packets, packet)
				break
			}
		}
		start += streamCCSDSFrameBits
	}
	if start > 0 {
		*buffer = append([]bool(nil), (*buffer)[start:]...)
	}
	return packets
}

func (d *AutoStreamDecoder) accept(packet ReceivedPacket) bool {
	mode := domain.TransportMode(packet.Profile)
	state := d.dedup[mode]
	if state == nil {
		state = &StreamDecoder{imageStates: make(map[string]*packetDedupState)}
		d.dedup[mode] = state
	}
	accepted := state.markNew(packet)
	// A duplicate is still proof that the currently locked waveform is active.
	d.lastPacketAt = time.Now()
	if !accepted {
		return false
	}
	d.lockedProfile = mode
	return true
}

func (d *AutoStreamDecoder) resetAcquisition() {
	d.resample1200.Reset()
	d.fsk1200.Reset()
	d.ax25.Reset()
	d.ccsds1200 = nil
	d.resample2400.Reset()
	d.gmsk2400.Reset()
	d.ccsds2400 = nil
	for _, decoder := range d.mary {
		decoder.Reset()
	}
}

func mustProfile(mode domain.TransportMode) domain.Profile {
	profile, _ := domain.ParseProfile(string(mode))
	return profile
}
