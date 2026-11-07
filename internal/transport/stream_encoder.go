package transport

import (
	"context"
	"encoding/binary"
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

const streamPCMChunkSamples = 4096
const maryTrainingSymbols = 96

// StreamPackets validates and modulates a packet batch without constructing a
// full-duration waveform. emit is called synchronously with reusable-sized PCM
// blocks; the consumer must finish using each block before returning. The
// modulator phase, M-ary bit carry, pulse-shaping FIR and resampler state are
// maintained across all blocks.
func (e *Encoder) StreamPackets(ctx context.Context, packets [][]byte, emit func([]float32) error) (int64, error) {
	if e == nil {
		return 0, errors.New("transport encoder is nil")
	}
	if len(packets) == 0 {
		return 0, errors.New("transport packet batch is empty")
	}
	if emit == nil {
		return 0, errors.New("PCM stream consumer is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	profile, err := domain.ParseProfile(string(e.cfg.Profile))
	if err != nil {
		return 0, err
	}
	for i, packet := range packets {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if len(packet) != ssdv.PacketSize {
			return 0, fmt.Errorf("image transport packet %d must be %d bytes, got %d", i, ssdv.PacketSize, len(packet))
		}
		if profile.ImageCodec == "SSDV" {
			if _, err := ssdv.ParsePacket(packet); err != nil {
				return 0, fmt.Errorf("invalid SSDV packet %d: %w", i, err)
			}
		} else if profile.ImageCodec == "PCSI-MMT" {
			if _, _, err := pcsi.CorrectPacket(packet); err != nil {
				return 0, fmt.Errorf("invalid PCSI packet %d: %w", i, err)
			}
		} else {
			return 0, domain.ErrCodecNotImplemented
		}
	}

	sink, err := newPCMStreamSink(profile.SampleRate, e.cfg.AudioSampleRate, profile.LinkLayer != "CCSDS-MARY", emit)
	if err != nil {
		return 0, err
	}
	if profile.LinkLayer == "AX.25 UI / HDLC" {
		return e.streamAX25(ctx, profile, packets, sink)
	}
	if profile.LinkLayer == "CCSDS-MARY" {
		return e.streamMARY(ctx, profile, packets, sink)
	}
	if profile.LinkLayer == "CCSDS" {
		return e.streamCCSDSFSK(ctx, profile, packets, sink)
	}
	return 0, fmt.Errorf("unsupported link layer %q", profile.LinkLayer)
}

func (e *Encoder) streamCCSDSFSK(ctx context.Context, profile domain.Profile, packets [][]byte, sink *pcmStreamSink) (int64, error) {
	mod, err := ccsds.NewCCSDSAFSKStreamModulator(profile.SampleRate, profile.BitRate, profile.MarkFrequency, profile.SpaceFrequency)
	if err != nil {
		return 0, err
	}
	randomizer := ccsds.RandomizerSequence()
	if err := sink.write(mod.WriteBytes(repeatRandomizer(randomizer, e.cfg.PreambleSize))); err != nil {
		return 0, err
	}
	for i, packet := range packets {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		unit, buildErr := ccsds.BuildDataUnit(packet)
		if buildErr != nil {
			return 0, fmt.Errorf("build CCSDS data unit %d: %w", i, buildErr)
		}
		if err := sink.write(mod.WriteBytes(unit)); err != nil {
			return 0, err
		}
	}
	if err := sink.write(mod.WriteBytes(repeatRandomizer(randomizer, e.cfg.TrailerSize))); err != nil {
		return 0, err
	}
	return sink.close()
}

func (e *Encoder) streamMARY(ctx context.Context, profile domain.Profile, packets [][]byte, sink *pcmStreamSink) (int64, error) {
	mod, err := mary.NewStreamModulator(mary.Config{SampleRate: profile.SampleRate, SymbolRate: profile.SymbolRate, BitsPerSymbol: profile.BitsPerSymbol, CarrierHz: profile.CarrierHz})
	if err != nil {
		return 0, err
	}
	wirePackets, err := buildMARYWirePackets(packets)
	if err != nil {
		return 0, err
	}

	// SDF3 protects each CCSDS data unit independently. Every unit receives a
	// fresh convolutional encoder/interleaver, a repeated CRC-protected header,
	// and a new training word. This bounds the damage from a radio fade or lost
	// audio block to one unit and gives the receiver a fresh symbol-acquisition
	// point before the next one. The receiver retains SDF2 and legacy support.
	training, err := mod.Start()
	if err != nil {
		return 0, err
	}
	if err := sink.write(training); err != nil {
		return 0, err
	}

	for index, packet := range wirePackets {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if index > 0 {
			// The 200 ms guard leaves at least 100 ms of complete low-energy
			// callback blocks even when the audio API splits the stream into
			// arbitrary blocks up to 40 ms. The receiver needs 100 ms to release
			// carrier lock before the next independent training word.
			if err := sink.writeMARYSilence(profile.SampleRate / 5); err != nil {
				return 0, err
			}
			reacquire, trainingErr := mod.WriteTraining()
			if trainingErr != nil {
				return 0, trainingErr
			}
			if err := sink.write(reacquire); err != nil {
				return 0, err
			}
		}
		unit, buildErr := ccsds.BuildDataUnit(packet)
		if buildErr != nil {
			return 0, fmt.Errorf("build CCSDS data unit %d: %w", index, buildErr)
		}
		header, headerErr := fec.NewPacketBurstHeader(unit).MarshalBinary()
		if headerErr != nil {
			return 0, headerErr
		}
		wireHeader, headerErr := fec.RepeatBurstHeader(header)
		if headerErr != nil {
			return 0, headerErr
		}
		headerPCM, modErr := mod.WriteBytes(wireHeader)
		if modErr != nil {
			return 0, modErr
		}
		if err := sink.write(headerPCM); err != nil {
			return 0, err
		}

		conv := fec.NewConvStreamEncoder()
		interleaver := fec.NewBlockInterleaver()
		coded, encodeErr := conv.WriteBytes(unit)
		if encodeErr != nil {
			return 0, encodeErr
		}
		tail, encodeErr := conv.Close()
		if encodeErr != nil {
			return 0, encodeErr
		}
		coded = append(coded, tail...)
		interleaved, interleaveErr := interleaver.Write(coded)
		if interleaveErr != nil {
			return 0, interleaveErr
		}
		interleaveTail, interleaveErr := interleaver.Close()
		if interleaveErr != nil {
			return 0, interleaveErr
		}
		interleaved = append(interleaved, interleaveTail...)
		codedPCM, modErr := mod.WriteBytes(interleaved)
		if modErr != nil {
			return 0, modErr
		}
		if err := sink.write(codedPCM); err != nil {
			return 0, err
		}
	}

	modTail, err := mod.Close()
	if err != nil {
		return 0, err
	}
	if err := sink.write(modTail); err != nil {
		return 0, err
	}
	return sink.close()
}

// buildMARYWirePackets inserts two parity data units after each group of up to
// eight adjacent FEC-enabled SSDV image packets. Control and non-FEC packets are
// passed through unchanged; group metadata records packet IDs explicitly.
func buildMARYWirePackets(packets [][]byte) ([][]byte, error) {
	out := make([][]byte, 0, len(packets)+((len(packets)+7)/8)*2)
	group := make([][]byte, 0, 8)
	var groupCall uint32
	var groupImage uint8
	flush := func() error {
		if len(group) >= 2 {
			parity, err := fec.BuildPacketParityPayloads(group)
			if err != nil {
				return err
			}
			out = append(out, parity...)
		}
		group = group[:0]
		return nil
	}
	for _, raw := range packets {
		parsed, parseErr := ssdv.ParsePacket(raw)
		eligible := parseErr == nil && parsed.FEC && !parsed.GIFControl
		if !eligible {
			if err := flush(); err != nil {
				return nil, err
			}
			out = append(out, raw)
			continue
		}
		call := binary.BigEndian.Uint32(raw[2:6])
		imageID := raw[6]
		if len(group) > 0 && (call != groupCall || imageID != groupImage || len(group) == 8) {
			if err := flush(); err != nil {
				return nil, err
			}
		}
		if len(group) == 0 {
			groupCall, groupImage = call, imageID
		}
		group = append(group, raw)
		out = append(out, raw)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}

func (sink *pcmStreamSink) writeMARYSilence(samples int) error {
	if samples <= 0 {
		return nil
	}
	const chunk = 2048
	zero := make([]float32, min(chunk, samples))
	for samples > 0 {
		count := min(len(zero), samples)
		if err := sink.write(zero[:count]); err != nil {
			return err
		}
		samples -= count
	}
	return nil
}

func (sink *pcmStreamSink) writeMARYBytes(mod *mary.StreamModulator, data []byte) error {
	block, err := mod.WriteBytes(data)
	if err != nil {
		return err
	}
	return sink.write(block)
}

func (e *Encoder) streamAX25(ctx context.Context, profile domain.Profile, packets [][]byte, sink *pcmStreamSink) (int64, error) {
	mod, err := ccsds.NewAFSKStreamModulator(profile.SampleRate, profile.BitRate, profile.MarkFrequency, profile.SpaceFrequency)
	if err != nil {
		return 0, err
	}
	level := true
	writeHDLCBits := func(bits []bool) error {
		levels := make([]bool, len(bits))
		for i, bit := range bits {
			if !bit {
				level = !level
			}
			levels[i] = level
		}
		return sink.write(mod.WriteBits(levels))
	}
	flag := make([]bool, 8)
	for i := range flag {
		flag[i] = (ax25.Flag>>uint(i))&1 != 0
	}
	for i := 0; i < ax25.DefaultBurstPreambleFlags; i++ {
		if err := writeHDLCBits(flag); err != nil {
			return 0, err
		}
	}
	for i, packet := range packets {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		frame, buildErr := ax25.BuildUIFrame(e.cfg.Source, e.cfg.Destination, e.cfg.Digipeaters, packet)
		if buildErr != nil {
			return 0, fmt.Errorf("build AX.25 frame %d: %w", i, buildErr)
		}
		if err := writeHDLCBits(ax25.HDLCBits(frame, 1)); err != nil {
			return 0, err
		}
	}
	for i := 0; i < ax25.DefaultBurstTrailerFlags; i++ {
		if err := writeHDLCBits(flag); err != nil {
			return 0, err
		}
	}
	return sink.close()
}

func repeatRandomizer(sequence []byte, count int) []byte {
	if count <= 0 {
		return nil
	}
	out := make([]byte, count)
	for i := range out {
		out[i] = sequence[i%len(sequence)]
	}
	return out
}

func (e *Encoder) EstimateOutputSamples(packetCount int) (int64, error) {
	if e == nil || packetCount <= 0 {
		return 0, errors.New("positive packet count is required")
	}
	profile, err := domain.ParseProfile(string(e.cfg.Profile))
	if err != nil {
		return 0, err
	}
	var native int64
	switch profile.LinkLayer {
	case "CCSDS":
		byteCount := int64(e.cfg.PreambleSize + e.cfg.TrailerSize + packetCount*(4+ccsds.PayloadLength))
		native = byteCount * 8 * int64(profile.SampleRate/profile.BitRate)
	case "CCSDS-MARY":
		// Packet count-only estimate assumes two parity frames per group of
		// eight SSDV frames and the rate-1/2 inner code with block interleaving.
		wirePackets := packetCount + 2*((packetCount+7)/8)
		rawBytes := int64(e.cfg.PreambleSize + e.cfg.TrailerSize + wirePackets*(4+ccsds.PayloadLength))
		codeBits := 2 * (rawBytes*8 + fec.ConvMemory)
		codeBytes := (codeBits + 7) / 8
		interleavedBytes := ((codeBytes + fec.InterleaveBlockBytes - 1) / fec.InterleaveBlockBytes) * fec.InterleaveBlockBytes
		modBits := int64(fec.BurstHeaderWireBits) + interleavedBytes*8
		symbols := int64(maryTrainingSymbols) + (modBits+int64(profile.BitsPerSymbol)-1)/int64(profile.BitsPerSymbol)
		native = symbols * int64(profile.SampleRate/profile.SymbolRate)
	case "AX.25 UI / HDLC":
		bits := int64(ax25.DefaultBurstPreambleFlags+ax25.DefaultBurstTrailerFlags) * 8
		// The AX.25 estimate is deliberately conservative because bit stuffing
		// depends on payload contents. The exact count is calculated by callers
		// with EstimateOutputSamplesForPackets when the packet batch is available.
		bits += int64(packetCount * (4 + ssdv.PacketSize + 16) * 8)
		native = bits * int64(profile.SampleRate/profile.BitRate)
	default:
		return 0, fmt.Errorf("unsupported link layer %q", profile.LinkLayer)
	}
	targetRate := e.cfg.AudioSampleRate
	if targetRate <= 0 {
		targetRate = profile.SampleRate
	}
	return (native*int64(targetRate) + int64(profile.SampleRate)/2) / int64(profile.SampleRate), nil
}

// EstimateOutputSamplesForPackets computes an exact native sample estimate,
// including AX.25 bit stuffing and the single burst guard sequences.
func (e *Encoder) EstimateOutputSamplesForPackets(packets [][]byte) (int64, error) {
	if e == nil || len(packets) == 0 {
		return 0, errors.New("non-empty packet batch is required")
	}
	profile, err := domain.ParseProfile(string(e.cfg.Profile))
	if err != nil {
		return 0, err
	}
	if profile.LinkLayer == "CCSDS-MARY" {
		wirePackets, wireErr := buildMARYWirePackets(packets)
		if wireErr != nil {
			return 0, wireErr
		}
		interleavedBytes, sizeErr := fec.ExpectedInterleavedBytes(4 + ccsds.PayloadLength)
		if sizeErr != nil {
			return 0, sizeErr
		}
		modBits := int64(fec.BurstHeaderWireBits + interleavedBytes*8)
		symbolsPerUnit := (modBits + int64(profile.BitsPerSymbol) - 1) / int64(profile.BitsPerSymbol)
		samplesPerUnit := (symbolsPerUnit + maryTrainingSymbols) * int64(profile.SampleRate/profile.SymbolRate)
		native := int64(len(wirePackets)) * samplesPerUnit
		if len(wirePackets) > 1 {
			native += int64(len(wirePackets)-1) * int64(profile.SampleRate/5)
		}
		targetRate := e.cfg.AudioSampleRate
		if targetRate <= 0 {
			targetRate = profile.SampleRate
		}
		return (native*int64(targetRate) + int64(profile.SampleRate)/2) / int64(profile.SampleRate), nil
	}
	if profile.LinkLayer != "AX.25 UI / HDLC" {
		return e.EstimateOutputSamples(len(packets))
	}
	bits := int64(ax25.DefaultBurstPreambleFlags+ax25.DefaultBurstTrailerFlags) * 8
	for i, packet := range packets {
		frame, frameErr := ax25.BuildUIFrame(e.cfg.Source, e.cfg.Destination, e.cfg.Digipeaters, packet)
		if frameErr != nil {
			return 0, fmt.Errorf("build AX.25 frame %d for estimate: %w", i, frameErr)
		}
		bits += int64(len(ax25.HDLCBits(frame, 1)))
	}
	native := bits * int64(profile.SampleRate/profile.BitRate)
	targetRate := e.cfg.AudioSampleRate
	if targetRate <= 0 {
		targetRate = profile.SampleRate
	}
	return (native*int64(targetRate) + int64(profile.SampleRate)/2) / int64(profile.SampleRate), nil
}

type pcmStreamSink struct {
	stage       []float32
	used        int
	filter      *dsp.StreamModulatedAudioFilter
	resampler   *dsp.StreamResampler
	emit        func([]float32) error
	outputCount int64
	lastNative  float32
	haveNative  bool
}

func newPCMStreamSink(sourceRate, targetRate int, filterEnabled bool, emit func([]float32) error) (*pcmStreamSink, error) {
	if targetRate <= 0 {
		targetRate = sourceRate
	}
	sink := &pcmStreamSink{stage: make([]float32, streamPCMChunkSamples), emit: emit}
	if filterEnabled {
		filter, err := dsp.NewStreamModulatedAudioFilter(targetRate)
		if err != nil {
			return nil, err
		}
		sink.filter = filter
	}
	if targetRate != sourceRate {
		resampler, err := dsp.NewStreamResampler(sourceRate, targetRate)
		if err != nil {
			return nil, err
		}
		sink.resampler = resampler
	}
	return sink, nil
}

func (s *pcmStreamSink) write(samples []float32) error {
	for len(samples) > 0 {
		count := copy(s.stage[s.used:], samples)
		s.used += count
		samples = samples[count:]
		if s.used == len(s.stage) {
			if err := s.flush(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *pcmStreamSink) flush() error {
	if s.used == 0 {
		return nil
	}
	block := s.stage[:s.used]
	s.lastNative, s.haveNative = block[len(block)-1], true
	output := block
	if s.resampler != nil {
		output = s.resampler.Feed(block)
	}
	if s.filter != nil && len(output) > 0 {
		s.filter.ProcessInPlace(output)
	}
	if len(output) > 0 {
		if err := s.emit(output); err != nil {
			return err
		}
		s.outputCount += int64(len(output))
	}
	s.used = 0
	return nil
}

func (s *pcmStreamSink) close() (int64, error) {
	if err := s.flush(); err != nil {
		return s.outputCount, err
	}
	if s.resampler != nil && s.haveNative {
		output := s.resampler.Flush()
		if s.filter != nil && len(output) > 0 {
			s.filter.ProcessInPlace(output)
		}
		if len(output) > 0 {
			if err := s.emit(output); err != nil {
				return s.outputCount, err
			}
			s.outputCount += int64(len(output))
		}
	}
	return s.outputCount, nil
}

// EstimateWirePacketCount returns the number of fixed-size physical image/data
// units emitted by the selected encoder, including SGV2 packet-erasure parity
// units for M-ary profiles. It does not include the CCSDS preamble/trailer.
func (e *Encoder) EstimateWirePacketCount(packets [][]byte) (int, error) {
	if e == nil || len(packets) == 0 {
		return 0, errors.New("non-empty packet batch is required")
	}
	profile, err := domain.ParseProfile(string(e.cfg.Profile))
	if err != nil {
		return 0, err
	}
	if profile.LinkLayer == "CCSDS-MARY" {
		wire, buildErr := buildMARYWirePackets(packets)
		if buildErr != nil {
			return 0, buildErr
		}
		return len(wire), nil
	}
	return len(packets), nil
}
