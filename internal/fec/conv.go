package fec

import (
	"errors"
	"math"
)

const (
	ConvConstraint       = 7
	ConvMemory           = ConvConstraint - 1
	ConvStates           = 1 << ConvMemory
	ConvPolyA            = 0x79 // 171 octal
	ConvPolyB            = 0x5B // 133 octal
	InterleaveBlockBytes = 64
	InterleaveBlockBits  = InterleaveBlockBytes * 8
)

// ConvStreamEncoder implements a rate-1/2, K=7 convolutional code. Input and
// output bits are MSB-first. Close appends six zero tail bits to terminate the
// trellis at state zero. Each input byte maps to exactly two output bytes.
type ConvStreamEncoder struct {
	state  uint8
	closed bool
}

func NewConvStreamEncoder() *ConvStreamEncoder { return &ConvStreamEncoder{} }

func (e *ConvStreamEncoder) WriteBytes(data []byte) ([]byte, error) {
	if e == nil || e.closed {
		return nil, errors.New("convolutional encoder is closed")
	}
	out := make([]byte, len(data)*2)
	bitPos := 0
	for _, value := range data {
		for shift := 7; shift >= 0; shift-- {
			bit := (value >> uint(shift)) & 1
			reg := (uint16(e.state)<<1 | uint16(bit)) & 0x7f
			a := parity7(uint8(reg) & ConvPolyA)
			b := parity7(uint8(reg) & ConvPolyB)
			putPackedBit(out, bitPos, a != 0)
			bitPos++
			putPackedBit(out, bitPos, b != 0)
			bitPos++
			e.state = uint8(reg) & 0x3f
		}
	}
	return out, nil
}

func (e *ConvStreamEncoder) Close() ([]byte, error) {
	if e == nil || e.closed {
		return nil, nil
	}
	e.closed = true
	out := make([]byte, 2) // twelve coded tail bits, four zero pad bits
	bitPos := 0
	for i := 0; i < ConvMemory; i++ {
		reg := (uint16(e.state) << 1) & 0x7f
		putPackedBit(out, bitPos, parity7(uint8(reg)&ConvPolyA) != 0)
		bitPos++
		putPackedBit(out, bitPos, parity7(uint8(reg)&ConvPolyB) != 0)
		bitPos++
		e.state = uint8(reg) & 0x3f
	}
	return out, nil
}

// BlockInterleaver transposes each 8x8 byte block. It retains at most 63 bytes
// between calls; Close zero-pads the final partial block. The receiver knows
// the unpadded coded-bit count from the FEC burst header.
type BlockInterleaver struct {
	pending [InterleaveBlockBytes]byte
	used    int
	closed  bool
}

func NewBlockInterleaver() *BlockInterleaver { return &BlockInterleaver{} }

func (i *BlockInterleaver) Write(data []byte) ([]byte, error) {
	if i == nil || i.closed {
		return nil, errors.New("interleaver is closed")
	}
	out := make([]byte, 0, (len(data)/InterleaveBlockBytes)*InterleaveBlockBytes)
	for _, value := range data {
		i.pending[i.used] = value
		i.used++
		if i.used == InterleaveBlockBytes {
			out = appendInterleavedBlock(out, i.pending[:])
			i.used = 0
		}
	}
	return out, nil
}

func (i *BlockInterleaver) Close() ([]byte, error) {
	if i == nil || i.closed {
		return nil, nil
	}
	i.closed = true
	if i.used == 0 {
		return nil, nil
	}
	for n := i.used; n < InterleaveBlockBytes; n++ {
		i.pending[n] = 0
	}
	return appendInterleavedBlock(nil, i.pending[:]), nil
}

func appendInterleavedBlock(dst, src []byte) []byte {
	if len(src) != InterleaveBlockBytes {
		return dst
	}
	start := len(dst)
	dst = append(dst, make([]byte, InterleaveBlockBytes)...)
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			dst[start+col*8+row] = src[row*8+col]
		}
	}
	return dst
}

func DeinterleaveSoftBlock(src []float32) ([]float32, error) {
	if len(src) != InterleaveBlockBits {
		return nil, errors.New("soft interleaver block must contain 512 LLR values")
	}
	out := make([]float32, InterleaveBlockBits)
	for row := 0; row < 8; row++ {
		for col := 0; col < 8; col++ {
			inByte := col*8 + row
			outByte := row*8 + col
			copy(out[outByte*8:(outByte+1)*8], src[inByte*8:(inByte+1)*8])
		}
	}
	return out, nil
}

// ViterbiDecoder consumes max-log LLRs where positive values favor bit zero
// and negative values favor bit one. It uses a bounded traceback window, so
// the receiver does not need to retain an entire burst of soft samples.
type ViterbiDecoder struct {
	metrics     [ConvStates]float64
	survivors   [48][ConvStates]uint8
	steps       int
	emitted     int
	initialized bool
	closed      bool
}

func NewViterbiDecoder() *ViterbiDecoder {
	d := &ViterbiDecoder{}
	for i := range d.metrics {
		d.metrics[i] = math.Inf(1)
	}
	d.metrics[0] = 0
	d.initialized = true
	return d
}

func (d *ViterbiDecoder) Feed(llrs []float32) ([]bool, error) {
	if d == nil || d.closed {
		return nil, errors.New("Viterbi decoder is closed")
	}
	if len(llrs)%2 != 0 {
		return nil, errors.New("rate-1/2 Viterbi input must contain an even number of LLR values")
	}
	out := make([]bool, 0, len(llrs)/2)
	for p := 0; p < len(llrs); p += 2 {
		var next [ConvStates]float64
		var decisions [ConvStates]uint8
		for s := range next {
			next[s] = math.Inf(1)
		}
		for nextState := 0; nextState < ConvStates; nextState++ {
			input := uint8(nextState & 1)
			prev0 := nextState >> 1
			prev1 := prev0 | (1 << (ConvMemory - 1))
			a0, b0 := transitionOutput(prev0, input)
			a1, b1 := transitionOutput(prev1, input)
			cost0 := branchCost(llrs[p], llrs[p+1], a0, b0)
			cost1 := branchCost(llrs[p], llrs[p+1], a1, b1)
			m0, m1 := d.metrics[prev0]+cost0, d.metrics[prev1]+cost1
			if m1 < m0 {
				next[nextState], decisions[nextState] = m1, 1
			} else {
				next[nextState], decisions[nextState] = m0, 0
			}
		}
		minimum := math.Inf(1)
		for _, value := range next {
			if value < minimum {
				minimum = value
			}
		}
		for s := range next {
			d.metrics[s] = next[s] - minimum
			d.survivors[d.steps%len(d.survivors)][s] = decisions[s]
		}
		d.steps++
		if d.steps >= len(d.survivors) {
			out = append(out, d.traceOldestBest())
			d.emitted++
		}
	}
	return out, nil
}

// Flush traces the final survivor path from state zero, which is guaranteed by
// the six zero tail bits emitted by ConvStreamEncoder.Close.
func (d *ViterbiDecoder) Flush() ([]bool, error) {
	if d == nil || d.closed {
		return nil, nil
	}
	d.closed = true
	if d.steps == 0 {
		return nil, errors.New("Viterbi decoder received no coded bits")
	}
	remaining := d.steps - d.emitted
	if remaining > len(d.survivors) {
		return nil, errors.New("Viterbi traceback window overrun")
	}
	reversed := make([]bool, 0, remaining)
	state := 0
	for step := d.steps - 1; step >= d.emitted; step-- {
		reversed = append(reversed, state&1 != 0)
		predecessorMSB := d.survivors[step%len(d.survivors)][state]
		state = (state >> 1) | (int(predecessorMSB) << (ConvMemory - 1))
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed, nil
}

func (d *ViterbiDecoder) traceOldestBest() bool {
	bestState := 0
	for s := 1; s < ConvStates; s++ {
		if d.metrics[s] < d.metrics[bestState] {
			bestState = s
		}
	}
	state := bestState
	var oldest bool
	for back := 0; back < len(d.survivors); back++ {
		bit := state&1 != 0
		if back == len(d.survivors)-1 {
			oldest = bit
		}
		step := d.steps - 1 - back
		predecessorMSB := d.survivors[step%len(d.survivors)][state]
		state = (state >> 1) | (int(predecessorMSB) << (ConvMemory - 1))
	}
	return oldest
}

func transitionOutput(state int, input uint8) (uint8, uint8) {
	reg := uint8(((state << 1) | int(input)) & 0x7f)
	return parity7(reg & ConvPolyA), parity7(reg & ConvPolyB)
}

func branchCost(llr0, llr1 float32, out0, out1 uint8) float64 {
	cost := -float64(llr0)
	if out0 != 0 {
		cost = float64(llr0)
	}
	cost2 := -float64(llr1)
	if out1 != 0 {
		cost2 = float64(llr1)
	}
	return cost + cost2
}

func parity7(v uint8) uint8 {
	v ^= v >> 4
	v ^= v >> 2
	v ^= v >> 1
	return v & 1
}

func putPackedBit(dst []byte, bitPos int, value bool) {
	if value {
		dst[bitPos/8] |= 1 << uint(7-bitPos%8)
	}
}

// EncodeErasureParity builds two systematic parity shards for up to eight
// equal-length data shards. It can recover any two missing data shards.
func EncodeErasureParity(data [][]byte) ([]byte, []byte, error) {
	if len(data) < 1 || len(data) > 8 {
		return nil, nil, errors.New("erasure group must contain 1..8 data shards")
	}
	shardLen := len(data[0])
	if shardLen == 0 {
		return nil, nil, errors.New("erasure shard is empty")
	}
	p0, p1 := make([]byte, shardLen), make([]byte, shardLen)
	for index, shard := range data {
		if len(shard) != shardLen {
			return nil, nil, errors.New("erasure shards have inconsistent lengths")
		}
		coefficient := alphaTo[index]
		for pos, value := range shard {
			p0[pos] ^= value
			p1[pos] ^= gfMul(coefficient, value)
		}
	}
	return p0, p1, nil
}

// RecoverErasureShards restores up to two nil entries in data from the two
// parity shards. Existing data shards are never modified.
func RecoverErasureShards(data [][]byte, parity0, parity1 []byte) (int, error) {
	if len(data) < 1 || len(data) > 8 {
		return 0, errors.New("erasure group must contain 1..8 data shards")
	}
	if len(parity0) == 0 && len(parity1) == 0 {
		return 0, errors.New("at least one erasure parity shard is required")
	}
	shardLen := len(parity0)
	if shardLen == 0 {
		shardLen = len(parity1)
	}
	if (len(parity0) != 0 && len(parity0) != shardLen) || (len(parity1) != 0 && len(parity1) != shardLen) {
		return 0, errors.New("erasure parity shards have invalid lengths")
	}
	missing := make([]int, 0, 2)
	for index, shard := range data {
		if shard == nil {
			missing = append(missing, index)
			continue
		}
		if len(shard) != shardLen {
			return 0, errors.New("erasure data shard has invalid length")
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}
	if len(missing) > 2 {
		return 0, errors.New("two parity shards cannot recover more than two losses")
	}
	residual0 := append([]byte(nil), parity0...)
	residual1 := append([]byte(nil), parity1...)
	for index, shard := range data {
		if shard == nil {
			continue
		}
		coefficient := alphaTo[index]
		for pos, value := range shard {
			residual0[pos] ^= value
			residual1[pos] ^= gfMul(coefficient, value)
		}
	}
	if len(missing) == 1 {
		index := missing[0]
		if len(parity0) != 0 {
			data[index] = residual0
		} else {
			coefficient := alphaTo[index]
			recovered := make([]byte, shardLen)
			for pos, value := range residual1 {
				recovered[pos] = gfDiv(value, coefficient)
			}
			data[index] = recovered
		}
		return 1, nil
	}
	if len(parity0) == 0 || len(parity1) == 0 {
		return 0, errors.New("two missing shards require both parity shards")
	}
	first, second := missing[0], missing[1]
	a, b := alphaTo[first], alphaTo[second]
	denominator := a ^ b
	if denominator == 0 {
		return 0, errors.New("erasure recovery coefficients are singular")
	}
	x, y := make([]byte, shardLen), make([]byte, shardLen)
	for pos := 0; pos < shardLen; pos++ {
		// residual0 = x ^ y; residual1 = a*x ^ b*y.
		// Therefore x = (residual1 ^ b*residual0) / (a ^ b).
		x[pos] = gfDiv(residual1[pos]^gfMul(b, residual0[pos]), denominator)
		y[pos] = residual0[pos] ^ x[pos]
	}
	data[first], data[second] = x, y
	return 2, nil
}
