// Package bcsi implements an independent block-compressive-sensing image codec.
// It deliberately does not alter SSDV or PCSI-MMT wire packets. Each 8x8 image
// block is encoded as a self-describing, CRC-protected packet so packet loss is
// spatially localized and received blocks can be reconstructed independently.
package bcsi

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"io"
	"math"
	"math/bits"
	"runtime"
	"sync"
)

const (
	Version      = 1
	BlockSize    = 8
	blockPixels  = BlockSize * BlockSize
	packetHeader = 22
	packetTail   = 4
	magic        = "BCSB"
	minRate      = 0.125
	maxRate      = 0.75
)

// Config controls the independent BCSI encoder. SamplingRate is the base
// luminance measurement fraction per 8x8 block. Chroma uses half as many
// measurements; Adaptive raises the luma budget for high-detail blocks and
// lowers it for smooth blocks. QuantStep is measured in normalized Hadamard
// coefficient units and is an integer in [1,32].
type Config struct {
	SamplingRate float64
	Adaptive     bool
	QuantStep    int
	Seed         uint32
}

func DefaultConfig() Config {
	return Config{SamplingRate: 0.375, Adaptive: true, QuantStep: 3, Seed: 0x53415453}
}

type PacketInfo struct {
	Width       int
	Height      int
	BlockIndex  int
	BlocksWide  int
	BlocksHigh  int
	LumaCount   int
	ChromaCount int
	QuantStep   int
	Compressed  bool
	Seed        uint32
}

type Stats struct {
	Width               int     `json:"width"`
	Height              int     `json:"height"`
	BlocksReceived      int     `json:"blocksReceived"`
	BlocksExpected      int     `json:"blocksExpected"`
	BlocksReconstructed int     `json:"blocksReconstructed"`
	BlocksConcealed     int     `json:"blocksConcealed"`
	EncodedBytes        int     `json:"encodedBytes"`
	Measurements        int     `json:"measurements"`
	SamplingRate        float64 `json:"samplingRate"`
	Coverage            float64 `json:"coverage"`
}

type encodedBlock struct {
	info    PacketInfo
	payload []byte
}

// Encode converts an image into one independently decodable packet per 8x8
// block. Rows of a seeded low-sequency 2-D partial Walsh-Hadamard matrix form the sensing
// operator; inverse Haar coefficients are used as the sparse reconstruction
// domain. Each channel is centered before measurement to reduce DC quantization.
func Encode(src image.Image, cfg Config) ([][]byte, error) {
	if src == nil {
		return nil, errors.New("BCSI source image is nil")
	}
	if cfg.SamplingRate == 0 {
		cfg.SamplingRate = DefaultConfig().SamplingRate
	}
	if cfg.SamplingRate < minRate || cfg.SamplingRate > maxRate {
		return nil, fmt.Errorf("BCSI sampling rate must be between %.3f and %.3f", minRate, maxRate)
	}
	if cfg.QuantStep == 0 {
		cfg.QuantStep = DefaultConfig().QuantStep
	}
	if cfg.QuantStep < 1 || cfg.QuantStep > 32 {
		return nil, errors.New("BCSI quantization step must be 1..32")
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || w%BlockSize != 0 || h%BlockSize != 0 || w > 65528 || h > 65528 || (w/BlockSize)*(h/BlockSize) > 65536 {
		return nil, errors.New("BCSI dimensions must be positive multiples of 8 and fit uint16")
	}
	cols, rows := w/BlockSize, h/BlockSize
	packets := make([][]byte, 0, cols*rows)
	planes := [3][blockPixels]float64{}
	for by := 0; by < rows; by++ {
		for bx := 0; bx < cols; bx++ {
			var detailSum, detailSq float64
			for y := 0; y < BlockSize; y++ {
				for x := 0; x < BlockSize; x++ {
					c := color.RGBAModel.Convert(src.At(b.Min.X+bx*BlockSize+x, b.Min.Y+by*BlockSize+y)).(color.RGBA)
					Y, Cb, Cr := color.RGBToYCbCr(c.R, c.G, c.B)
					i := y*BlockSize + x
					planes[0][i] = float64(Y) - 128
					planes[1][i] = float64(Cb) - 128
					planes[2][i] = float64(Cr) - 128
					detailSum += planes[0][i]
					detailSq += planes[0][i] * planes[0][i]
				}
			}
			base := int(math.Round(cfg.SamplingRate * blockPixels))
			lumaCount := base
			if cfg.Adaptive {
				mean := detailSum / blockPixels
				variance := math.Max(0, detailSq/blockPixels-mean*mean)
				contrast := math.Min(1, math.Sqrt(variance)/64)
				factor := 0.72 + 0.56*contrast
				lumaCount = int(math.Round(float64(base) * factor))
			}
			lumaCount = clampInt(lumaCount, 8, blockPixels)
			chromaCount := clampInt((lumaCount+1)/2, 4, blockPixels/2)
			packet, err := encodeBlock(planes, w, h, by*cols+bx, lumaCount, chromaCount, cfg)
			if err != nil {
				return nil, fmt.Errorf("encode BCSI block %d: %w", by*cols+bx, err)
			}
			packets = append(packets, packet)
		}
	}
	return packets, nil
}

func encodeBlock(planes [3][blockPixels]float64, width, height, index, lumaCount, chromaCount int, cfg Config) ([]byte, error) {
	var raw bytes.Buffer
	for channel := 0; channel < 3; channel++ {
		count := lumaCount
		if channel != 0 {
			count = chromaCount
		}
		measurements := measure(planes[channel], index, channel, count, cfg.Seed)
		if err := writeQuantized(&raw, measurements[:count], cfg.QuantStep); err != nil {
			return nil, err
		}
	}
	payload := raw.Bytes()
	var compressed []byte
	if len(payload) >= 64 {
		compressed = deflate(payload)
	}
	useCompressed := len(compressed) > 0 && len(compressed)+2 < len(payload)
	if useCompressed {
		payload = compressed
	}
	packet := make([]byte, packetHeader+len(payload)+packetTail)
	copy(packet[:4], magic)
	packet[4] = Version
	packet[5] = BlockSize
	binary.BigEndian.PutUint16(packet[6:8], uint16(width))
	binary.BigEndian.PutUint16(packet[8:10], uint16(height))
	binary.BigEndian.PutUint16(packet[10:12], uint16(index))
	packet[12] = byte(lumaCount)
	packet[13] = byte(chromaCount)
	packet[14] = byte(cfg.QuantStep)
	if useCompressed {
		packet[15] = 1
	}
	binary.BigEndian.PutUint16(packet[16:18], uint16(len(payload)))
	binary.BigEndian.PutUint32(packet[18:22], cfg.Seed)
	copy(packet[packetHeader:], payload)
	binary.BigEndian.PutUint32(packet[len(packet)-packetTail:], crc32.ChecksumIEEE(packet[:len(packet)-packetTail]))
	return packet, nil
}

type flateWorkspace struct {
	buffer bytes.Buffer
	writer *flate.Writer
}

var flateWorkspacePool = sync.Pool{New: func() any {
	workspace := &flateWorkspace{}
	writer, err := flate.NewWriter(&workspace.buffer, flate.BestSpeed)
	if err != nil {
		return nil
	}
	workspace.writer = writer
	return workspace
}}

func deflate(src []byte) []byte {
	workspaceValue := flateWorkspacePool.Get()
	if workspaceValue == nil {
		return nil
	}
	workspace, ok := workspaceValue.(*flateWorkspace)
	if !ok || workspace == nil || workspace.writer == nil {
		return nil
	}
	workspace.buffer.Reset()
	workspace.writer.Reset(&workspace.buffer)
	if _, err := workspace.writer.Write(src); err != nil {
		_ = workspace.writer.Close()
		flateWorkspacePool.Put(workspace)
		return nil
	}
	if err := workspace.writer.Close(); err != nil {
		flateWorkspacePool.Put(workspace)
		return nil
	}
	// The packet outlives the workspace; copy the compressed bytes before the
	// reusable buffer is returned to the pool.
	compressed := append([]byte(nil), workspace.buffer.Bytes()...)
	workspace.buffer.Reset()
	flateWorkspacePool.Put(workspace)
	return compressed
}

func writeQuantized(dst *bytes.Buffer, values []float64, step int) error {
	if len(values) > 255 {
		return errors.New("too many BCSI measurements")
	}
	dst.WriteByte(byte(len(values)))
	var previous int64
	var scratch [binary.MaxVarintLen64]byte
	for _, value := range values {
		q := int64(math.Round(value / float64(step)))
		delta := q - previous
		previous = q
		n := binary.PutVarint(scratch[:], delta)
		dst.Write(scratch[:n])
	}
	return nil
}

func measure(spatial [blockPixels]float64, blockIndex, channel, count int, seed uint32) [blockPixels]float64 {
	perm, signs := sensingPermutation(blockIndex, channel, seed)
	rows := sensingRows(blockIndex, channel, seed)
	var work [blockPixels]float64
	for i := 0; i < blockPixels; i++ {
		work[i] = spatial[perm[i]] * signs[i]
	}
	fwht2D(work[:])
	var out [blockPixels]float64
	for k := 0; k < count; k++ {
		out[k] = work[rows[k]] / BlockSize
	}
	return out
}

func sensingPermutation(_ int, _ int, _ uint32) ([blockPixels]int, [blockPixels]float64) {
	// The 2-D low-sequency operator preserves the spatial neighbourhood and
	// concentrates its first measurements on coarse horizontal/vertical
	// structure. Random pixel shuffling was tested and reduced quality on
	// natural raster images at low measurement counts.
	var perm [blockPixels]int
	var signs [blockPixels]float64
	for i := range perm {
		perm[i], signs[i] = i, 1
	}
	return perm, signs
}

// sensingRows follows increasing Walsh sequency, keeping DC and coarse image
// structure in the first measurements. Random column permutation varies the
// orthogonal matrix per block without erasing its low-frequency anchor.
func sensingRows(blockIndex, channel int, seed uint32) [blockPixels]int {
	var rows [blockPixels]int
	var order [BlockSize]int
	for i := 0; i < BlockSize; i++ {
		gray := i ^ (i >> 1)
		order[i] = int(bits.Reverse8(uint8(gray << 5)))
	}
	state := seed ^ uint32(blockIndex+1)*0x9e3779b9 ^ uint32(channel+1)*0x85ebca6b
	if state == 0 {
		state = 0x6d2b79f5
	}
	next := func() uint32 {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		return state
	}
	// Select 2-D Walsh coefficients in increasing total sequency. The DC
	// coefficient is always first, followed by coarse horizontal/vertical
	// structure. A seeded shuffle only changes the order within an equal-
	// sequency diagonal, preserving the coarse-to-fine sampling progression.
	index := 0
	for diagonal := 0; diagonal < BlockSize*2-1; diagonal++ {
		start := index
		for u := 0; u < BlockSize; u++ {
			v := diagonal - u
			if v >= 0 && v < BlockSize {
				rows[index] = order[v]*BlockSize + order[u]
				index++
			}
		}
		for i := index - 1; i > start; i-- {
			j := start + int(next()%uint32(i-start+1))
			rows[i], rows[j] = rows[j], rows[i]
		}
	}
	return rows
}

func fwht2D(values []float64) {
	var line [BlockSize]float64
	for y := 0; y < BlockSize; y++ {
		fwht(values[y*BlockSize : (y+1)*BlockSize])
	}
	for x := 0; x < BlockSize; x++ {
		for y := 0; y < BlockSize; y++ {
			line[y] = values[y*BlockSize+x]
		}
		fwht(line[:])
		for y := 0; y < BlockSize; y++ {
			values[y*BlockSize+x] = line[y]
		}
	}
}

func fwht(values []float64) {
	for width := 1; width < len(values); width <<= 1 {
		for start := 0; start < len(values); start += width << 1 {
			for i := 0; i < width; i++ {
				a, b := values[start+i], values[start+width+i]
				values[start+i], values[start+width+i] = a+b, a-b
			}
		}
	}
}

func parsePacket(packet []byte) (encodedBlock, error) {
	var out encodedBlock
	if len(packet) < packetHeader+packetTail || string(packet[:4]) != magic {
		return out, errors.New("invalid BCSI block packet magic or length")
	}
	if packet[4] != Version || packet[5] != BlockSize {
		return out, errors.New("unsupported BCSI packet version or block size")
	}
	payloadLen := int(binary.BigEndian.Uint16(packet[16:18]))
	if payloadLen != len(packet)-packetHeader-packetTail || payloadLen > 4096 {
		return out, errors.New("invalid BCSI payload length")
	}
	wantCRC := binary.BigEndian.Uint32(packet[len(packet)-packetTail:])
	if crc32.ChecksumIEEE(packet[:len(packet)-packetTail]) != wantCRC {
		return out, errors.New("BCSI block CRC mismatch")
	}
	w, h := int(binary.BigEndian.Uint16(packet[6:8])), int(binary.BigEndian.Uint16(packet[8:10]))
	idx := int(binary.BigEndian.Uint16(packet[10:12]))
	my, mc, q := int(packet[12]), int(packet[13]), int(packet[14])
	if w <= 0 || h <= 0 || w%BlockSize != 0 || h%BlockSize != 0 || w > 65528 || h > 65528 || (w/BlockSize)*(h/BlockSize) > 65536 || idx >= (w/BlockSize)*(h/BlockSize) {
		return out, errors.New("invalid BCSI image dimensions or block index")
	}
	if my < 4 || my > blockPixels || mc < 4 || mc > blockPixels/2 || q < 1 || q > 32 || packet[15]&^byte(1) != 0 {
		return out, errors.New("invalid BCSI measurement or quantization header")
	}
	seed := binary.BigEndian.Uint32(packet[18:22])
	payload := append([]byte(nil), packet[packetHeader:len(packet)-packetTail]...)
	if packet[15]&1 != 0 {
		var err error
		payload, err = inflate(payload, 512)
		if err != nil {
			return out, err
		}
	}
	counts := []int{my, mc, mc}
	offset := 0
	for _, count := range counts {
		if offset >= len(payload) || int(payload[offset]) != count {
			return out, errors.New("BCSI measurement count mismatch")
		}
		offset++
		var previous int64
		for i := 0; i < count; i++ {
			delta, n := binary.Varint(payload[offset:])
			if n <= 0 {
				return out, errors.New("invalid BCSI measurement varint")
			}
			offset += n
			previous += delta
			if previous < -4096 || previous > 4096 {
				return out, errors.New("BCSI quantized measurement outside supported range")
			}
		}
	}
	if offset != len(payload) {
		return out, errors.New("trailing bytes in BCSI measurement payload")
	}
	out.info = PacketInfo{Width: w, Height: h, BlockIndex: idx, BlocksWide: w / BlockSize, BlocksHigh: h / BlockSize,
		LumaCount: my, ChromaCount: mc, QuantStep: q, Compressed: packet[15]&1 != 0, Seed: seed}
	out.payload = payload
	return out, nil
}

func inflate(src []byte, limit int64) ([]byte, error) {
	r := flate.NewReader(bytes.NewReader(src))
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("inflate BCSI packet: %w", err)
	}
	if int64(len(out)) > limit {
		return nil, errors.New("BCSI inflated payload exceeds size limit")
	}
	return out, nil
}

// Decoder accepts packets in any order. Decode work is performed by a bounded
// worker pool; completed blocks are cached, so subsequent previews only process
// newly received packets.
type Decoder struct {
	mu       sync.Mutex
	width    int
	height   int
	packets  map[int]encodedBlock
	decoded  map[int][]byte
	bytes    int
	measures int
}

func NewDecoder() *Decoder {
	return &Decoder{packets: make(map[int]encodedBlock), decoded: make(map[int][]byte)}
}

func (d *Decoder) AddPacket(packet []byte) error {
	if d == nil {
		return errors.New("BCSI decoder is nil")
	}
	block, err := parsePacket(packet)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.width == 0 {
		d.width, d.height = block.info.Width, block.info.Height
	} else if d.width != block.info.Width || d.height != block.info.Height {
		return errors.New("BCSI packet belongs to a different image")
	}
	if _, exists := d.packets[block.info.BlockIndex]; exists {
		return nil
	}
	d.packets[block.info.BlockIndex] = block
	d.bytes += len(packet)
	d.measures += block.info.LumaCount + 2*block.info.ChromaCount
	return nil
}

func (d *Decoder) Stats() Stats {
	if d == nil {
		return Stats{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.statsLocked()
}

func (d *Decoder) statsLocked() Stats {
	blocks := 0
	if d.width > 0 && d.height > 0 {
		blocks = (d.width / BlockSize) * (d.height / BlockSize)
	}
	reconstructed := len(d.decoded)
	coverage := 0.0
	if blocks > 0 {
		coverage = float64(reconstructed) / float64(blocks)
	}
	rate := 0.0
	if len(d.packets) > 0 {
		// Count scalar Y/Cb/Cr measurements against 3 samples per pixel.
		rate = float64(d.measures) / float64(len(d.packets)*blockPixels*3)
	}
	return Stats{Width: d.width, Height: d.height, BlocksReceived: len(d.packets), BlocksExpected: blocks,
		BlocksReconstructed: reconstructed, BlocksConcealed: maxInt(0, blocks-reconstructed),
		EncodedBytes: d.bytes, Measurements: d.measures, SamplingRate: rate, Coverage: coverage}
}

type decodeJob struct{ block encodedBlock }
type decodeResult struct {
	index  int
	pixels []byte
	err    error
}

func (d *Decoder) Reconstruct(ctx context.Context, workers, iterations int) (*image.RGBA, Stats, error) {
	if d == nil {
		return nil, Stats{}, errors.New("BCSI decoder is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, Stats{}, err
	}
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers < 1 {
		workers = 1
	}
	if workers > 16 {
		workers = 16
	}
	if iterations <= 0 {
		iterations = 18
	}
	if iterations > 80 {
		return nil, Stats{}, errors.New("BCSI reconstruction iteration limit is 80")
	}
	d.mu.Lock()
	if d.width == 0 || len(d.packets) == 0 {
		d.mu.Unlock()
		return nil, Stats{}, errors.New("no BCSI blocks received")
	}
	jobs := make([]decodeJob, 0, len(d.packets)-len(d.decoded))
	for index, block := range d.packets {
		if _, done := d.decoded[index]; !done {
			jobs = append(jobs, decodeJob{block: block})
		}
	}
	statsBefore := d.statsLocked()
	d.mu.Unlock()
	if len(jobs) > 0 {
		jobCh := make(chan decodeJob)
		resultCh := make(chan decodeResult, len(jobs))
		var wg sync.WaitGroup
		if workers > len(jobs) {
			workers = len(jobs)
		}
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range jobCh {
					pixels, err := reconstructBlock(ctx, job.block, iterations)
					resultCh <- decodeResult{index: job.block.info.BlockIndex, pixels: pixels, err: err}
				}
			}()
		}
	feed:
		for _, job := range jobs {
			select {
			case <-ctx.Done():
				break feed
			case jobCh <- job:
			}
		}
		close(jobCh)
		wg.Wait()
		close(resultCh)
		var firstErr error
		d.mu.Lock()
		for result := range resultCh {
			if result.err != nil {
				if firstErr == nil {
					firstErr = result.err
				}
				continue
			}
			d.decoded[result.index] = result.pixels
		}
		statsBefore = d.statsLocked()
		d.mu.Unlock()
		if firstErr != nil && !errors.Is(firstErr, context.Canceled) {
			return nil, statsBefore, firstErr
		}
		if err := ctx.Err(); err != nil {
			return nil, statsBefore, err
		}
	}
	img, stats := d.Preview()
	return img, stats, nil
}

func (d *Decoder) Preview() (*image.RGBA, Stats) {
	if d == nil {
		return nil, Stats{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	stats := d.statsLocked()
	if d.width == 0 || d.height == 0 || len(d.decoded) == 0 {
		return nil, stats
	}
	cols, rows := d.width/BlockSize, d.height/BlockSize
	source := make([]int, cols*rows)
	for i := range source {
		source[i] = -1
	}
	queue := make([]int, 0, len(d.decoded))
	for index := range d.decoded {
		source[index] = index
		queue = append(queue, index)
	}
	// Multi-source BFS creates a nearest-recovered-block concealment map in
	// O(number of blocks), avoiding quadratic neighbour searches on large images.
	for head := 0; head < len(queue); head++ {
		index := queue[head]
		x, y := index%cols, index/cols
		neighbors := [4]int{index - 1, index + 1, index - cols, index + cols}
		valid := [4]bool{x > 0, x+1 < cols, y > 0, y+1 < rows}
		for i, next := range neighbors {
			if valid[i] && source[next] < 0 {
				source[next] = source[index]
				queue = append(queue, next)
			}
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, d.width, d.height))
	neutral := make([]byte, BlockSize*BlockSize*4)
	for i := 0; i < blockPixels; i++ {
		neutral[i*4], neutral[i*4+1], neutral[i*4+2], neutral[i*4+3] = 128, 128, 128, 255
	}
	for index := 0; index < cols*rows; index++ {
		pixels := d.decoded[index]
		if pixels == nil {
			if source[index] >= 0 {
				pixels = d.decoded[source[index]]
			} else {
				pixels = neutral
			}
		}
		bx, by := index%cols, index/cols
		for y := 0; y < BlockSize; y++ {
			dst := (by*BlockSize+y)*out.Stride + bx*BlockSize*4
			src := y * BlockSize * 4
			copy(out.Pix[dst:dst+BlockSize*4], pixels[src:src+BlockSize*4])
		}
	}
	stats = d.statsLocked()
	return out, stats
}

func reconstructBlock(ctx context.Context, block encodedBlock, iterations int) ([]byte, error) {
	values, err := readQuantized(block.payload, []int{block.info.LumaCount, block.info.ChromaCount, block.info.ChromaCount}, block.info.QuantStep)
	if err != nil {
		return nil, err
	}
	var planes [3][blockPixels]float64
	for channel := 0; channel < 3; channel++ {
		recovered, err := recoverChannelSeed(ctx, values[channel], block.info.BlockIndex, channel, block.info.QuantStep, iterations, block.info.Seed)
		if err != nil {
			return nil, err
		}
		copy(planes[channel][:], recovered[:])
	}
	out := make([]byte, blockPixels*4)
	for i := 0; i < blockPixels; i++ {
		y := uint8(clampInt(int(math.Round(planes[0][i]+128)), 0, 255))
		cb := uint8(clampInt(int(math.Round(planes[1][i]+128)), 0, 255))
		cr := uint8(clampInt(int(math.Round(planes[2][i]+128)), 0, 255))
		r, g, b := color.YCbCrToRGB(y, cb, cr)
		out[i*4], out[i*4+1], out[i*4+2], out[i*4+3] = r, g, b, 255
	}
	return out, nil
}

func readQuantized(payload []byte, counts []int, step int) ([][]float64, error) {
	out := make([][]float64, len(counts))
	offset := 0
	for channel, count := range counts {
		if offset >= len(payload) || int(payload[offset]) != count {
			return nil, errors.New("BCSI channel count mismatch")
		}
		offset++
		out[channel] = make([]float64, count)
		var previous int64
		for i := 0; i < count; i++ {
			delta, n := binary.Varint(payload[offset:])
			if n <= 0 {
				return nil, errors.New("invalid BCSI quantized data")
			}
			offset += n
			previous += delta
			out[channel][i] = float64(previous) * float64(step)
		}
	}
	if offset != len(payload) {
		return nil, errors.New("BCSI payload has trailing bytes")
	}
	return out, nil
}

func recoverChannelSeed(ctx context.Context, measurements []float64, blockIndex, channel, quantStep, iterations int, seed uint32) ([blockPixels]float64, error) {
	var zero [blockPixels]float64
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	perm, signs := sensingPermutation(blockIndex, channel, seed)
	rows := sensingRows(blockIndex, channel, seed)
	var y [blockPixels]float64
	for i := range measurements {
		y[rows[i]] = measurements[i]
	}
	fwht2D(y[:])
	var adjoint [blockPixels]float64
	for i := 0; i < blockPixels; i++ {
		adjoint[perm[i]] = y[i] * signs[i] / BlockSize
	}
	var coefficients [blockPixels]float64
	var result [blockPixels]float64
	haar2D(adjoint[:], false)
	copy(coefficients[:], adjoint[:])
	var spatial [blockPixels]float64
	var prediction [blockPixels]float64
	var residual [blockPixels]float64
	var gradient [blockPixels]float64
	lambda := math.Max(0.02, float64(quantStep)*0.02)
	for iteration := 0; iteration < iterations; iteration++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		copy(spatial[:], coefficients[:])
		haar2D(spatial[:], true)
		prediction = measureArray(spatial, blockIndex, channel, len(measurements), seed)
		clear(residual[:])
		for i := range measurements {
			residual[rows[i]] = prediction[i] - measurements[i]
		}
		fwht2D(residual[:])
		for i := 0; i < blockPixels; i++ {
			gradient[perm[i]] = residual[i] * signs[i] / BlockSize
		}
		haar2D(gradient[:], false)
		for i := range coefficients {
			v := coefficients[i] - 0.95*gradient[i]
			cut := 0.95 * lambda
			if v > cut {
				v -= cut
			} else if v < -cut {
				v += cut
			} else {
				v = 0
			}
			coefficients[i] = v
		}
	}
	copy(result[:], coefficients[:])
	haar2D(result[:], true)
	return result, nil
}

func measureArray(spatial [blockPixels]float64, blockIndex, channel, count int, seed uint32) [blockPixels]float64 {
	perm, signs := sensingPermutation(blockIndex, channel, seed)
	rows := sensingRows(blockIndex, channel, seed)
	var work [blockPixels]float64
	for i := range work {
		work[i] = spatial[perm[i]] * signs[i]
	}
	fwht2D(work[:])
	var out [blockPixels]float64
	for i := 0; i < count; i++ {
		out[i] = work[rows[i]] / BlockSize
	}
	return out
}

func haar1D(values []float64, inverse bool) {
	var tmp [BlockSize]float64
	const invSqrt2 = 0.7071067811865475244
	if !inverse {
		for n := len(values); n >= 2; n >>= 1 {
			for i := 0; i < n/2; i++ {
				a, b := values[2*i], values[2*i+1]
				tmp[i], tmp[n/2+i] = (a+b)*invSqrt2, (a-b)*invSqrt2
			}
			copy(values[:n], tmp[:n])
		}
		return
	}
	for n := 2; n <= len(values); n <<= 1 {
		for i := 0; i < n/2; i++ {
			a, d := values[i], values[n/2+i]
			tmp[2*i], tmp[2*i+1] = (a+d)*invSqrt2, (a-d)*invSqrt2
		}
		copy(values[:n], tmp[:n])
	}
}

func haar2D(values []float64, inverse bool) {
	var line [BlockSize]float64
	if !inverse {
		for n := BlockSize; n >= 2; n >>= 1 {
			for y := 0; y < n; y++ {
				row := values[y*BlockSize : y*BlockSize+n]
				haar1D(row, false)
			}
			for x := 0; x < n; x++ {
				for y := 0; y < n; y++ {
					line[y] = values[y*BlockSize+x]
				}
				haar1D(line[:n], false)
				for y := 0; y < n; y++ {
					values[y*BlockSize+x] = line[y]
				}
			}
		}
		return
	}
	for n := 2; n <= BlockSize; n <<= 1 {
		for x := 0; x < n; x++ {
			for y := 0; y < n; y++ {
				line[y] = values[y*BlockSize+x]
			}
			haar1D(line[:n], true)
			for y := 0; y < n; y++ {
				values[y*BlockSize+x] = line[y]
			}
		}
		for y := 0; y < n; y++ {
			row := values[y*BlockSize : y*BlockSize+n]
			haar1D(row, true)
		}
	}
}

func clampInt(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
