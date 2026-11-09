// Package pcsi implements PCSI MMT image packetization and reconstruction.
package pcsi

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/BH2VSQ/SATSSDV-Go/internal/fec"
)

const (
	WirePacketSize    = 256
	PayloadDataSize   = 224
	PacketSync        = 0x55
	PacketType        = 0x76
	PayloadBytes      = 218
	PacketHeaderBytes = 7
	PCSIInfoBits      = (PayloadBytes - PacketHeaderBytes) * 8
)

// MaxPCSIReconstructionPixels prevents untrusted packet headers from causing
// multi-gigabyte DCT workspaces. The application quality presets are <= 640x480.
const MaxPCSIReconstructionPixels = 2_073_600

type Config struct {
	Callsign          string
	ImageID           uint8
	BitDepth          int
	ChromaCompression int
}
type Encoder struct {
	width       int
	height      int
	initialized bool
	cfg         Config
	pixels      []int
	y           []uint8
	cb          []uint8
	cr          []uint8
	ycbcrCount  int
	yCount      int
	packetCount int
	channelBits int
}
type PacketInfo struct {
	Callsign     string
	ImageID      uint8
	PacketNumber uint16
	Height       int
	Width        int
	YCbCrPixels  int
	ChannelBits  int
}

func NewEncoder(src image.Image, cfg Config) (*Encoder, error) {
	if src == nil {
		return nil, errors.New("PCSI source image is nil")
	}
	if cfg.BitDepth == 0 {
		cfg.BitDepth = 12
	}
	if cfg.ChromaCompression == 0 {
		cfg.ChromaCompression = 20
	}
	if cfg.BitDepth%3 != 0 || cfg.BitDepth < 12 || cfg.BitDepth > 24 {
		return nil, errors.New("PCSI bit depth must be 12, 15, 18, 21 or 24")
	}
	if cfg.ChromaCompression < 0 || cfg.ChromaCompression > 100 {
		return nil, errors.New("PCSI chroma compression must be 0..100")
	}
	if _, e := EncodeCallsignBase40(cfg.Callsign); e != nil {
		return nil, e
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w%16 != 0 || h%16 != 0 {
		return nil, errors.New("PCSI MMT image dimensions must be multiples of 16")
	}
	bits := cfg.BitDepth / 3
	numYCbCr := int(math.RoundToEven(float64(PCSIInfoBits) / (2 + float64(cfg.ChromaCompression)) * 3 / float64(cfg.BitDepth)))
	numY := int(3 * (float64(PCSIInfoBits)/float64(cfg.BitDepth) - float64(numYCbCr)))
	perPacket := numYCbCr + numY
	if perPacket <= 0 {
		return nil, errors.New("PCSI packet configuration leaves no pixels")
	}
	count := w * h / perPacket
	if count <= 0 {
		return nil, errors.New("PCSI image is too small for one complete packet")
	}
	if count > 65536 {
		return nil, errors.New("PCSI image requires more than 65536 packets")
	}
	// Convert the source to the reference BGR/YCrCb channel convention once.
	// The previous encoder repeated RGB->YCbCr conversion for every transmitted
	// packet, revisiting the same pixels during packet serialization. PCSI's
	// deterministic permutation lets us cache compact byte planes without
	// changing packet order or any on-wire bits.
	yPlane, cbPlane, crPlane := make([]uint8, w*h), make([]uint8, w*h), make([]uint8, w*h)
	rgba, rgbaOK := src.(*image.RGBA)
	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			var c color.RGBA
			if rgbaOK {
				p := rgba.Pix[(py+b.Min.Y-rgba.Rect.Min.Y)*rgba.Stride+(px+b.Min.X-rgba.Rect.Min.X)*4:]
				c = color.RGBA{R: p[0], G: p[1], B: p[2], A: p[3]}
			} else {
				c = color.RGBAModel.Convert(src.At(b.Min.X+px, b.Min.Y+py)).(color.RGBA)
			}
			// Keep the reference's BGR->YCrCb input ordering byte-for-byte.
			Y, Cb, Cr := color.RGBToYCbCr(c.B, c.G, c.R)
			offset := py*w + px
			yPlane[offset], cbPlane[offset], crPlane[offset] = Y, Cb, Cr
		}
	}
	return &Encoder{width: w, height: h, initialized: true, cfg: cfg, pixels: shufflePixels(w * h), y: yPlane, cb: cbPlane, cr: crPlane,
		ycbcrCount: numYCbCr, yCount: numY, packetCount: count, channelBits: bits}, nil
}
func (e *Encoder) PacketCount() int {
	if e == nil {
		return 0
	}
	return e.packetCount
}
func (e *Encoder) EncodePacket(packetNumber int) ([]byte, error) {
	if e == nil || !e.initialized {
		return nil, errors.New("PCSI encoder is not initialized")
	}
	if packetNumber < 0 || packetNumber >= e.packetCount {
		return nil, fmt.Errorf("packet number %d is outside 0..%d", packetNumber, e.packetCount-1)
	}
	call, e2 := EncodeCallsignBase40(e.cfg.Callsign)
	if e2 != nil {
		return nil, e2
	}
	w, h := e.width, e.height
	numPix := e.ycbcrCount + e.yCount
	start := packetNumber * numPix
	packet := make([]byte, WirePacketSize)
	packet[0] = PacketSync
	packet[1] = PacketType
	binary.BigEndian.PutUint32(packet[2:6], call)
	payload := packet[6 : 6+PayloadBytes]
	payload[0] = e.cfg.ImageID
	binary.BigEndian.PutUint16(payload[1:3], uint16(packetNumber))
	payload[3] = byte(h / 16)
	payload[4] = byte(w / 16)
	payload[5] = byte(e.ycbcrCount)
	payload[6] = byte(e.channelBits - 1)
	writer := bitWriter{dst: payload[PacketHeaderBytes:]}
	maxValue := (1 << e.channelBits) - 1
	for i := 0; i < e.ycbcrCount; i++ {
		idx := e.pixels[start+i]
		x := idx / h
		y := idx % h
		offset := y*w + x
		writer.put(scale(e.y[offset], maxValue), e.channelBits)
		// Preserve the reference MMT stream order: Y, Cr, Cb.
		writer.put(scale(e.cr[offset], maxValue), e.channelBits)
		writer.put(scale(e.cb[offset], maxValue), e.channelBits)
	}
	for i := 0; i < e.yCount; i++ {
		idx := e.pixels[start+e.ycbcrCount+i]
		x := idx / h
		y := idx % h
		offset := y*w + x
		writer.put(scale(e.y[offset], maxValue), e.channelBits)
	}
	parity, err := fec.RS8Parity(packet[1:224])
	if err != nil {
		return nil, err
	}
	copy(packet[224:], parity)
	return packet, nil
}
func scale(v uint8, max int) uint32 { return uint32(math.RoundToEven(float64(v) / 255 * float64(max))) }

type bitWriter struct {
	dst    []byte
	offset int
}

func (w *bitWriter) put(v uint32, n int) {
	for bit := n - 1; bit >= 0; bit-- {
		if w.offset >= len(w.dst)*8 {
			return
		}
		if v&(1<<bit) != 0 {
			w.dst[w.offset/8] |= 1 << uint(7-w.offset%8)
		}
		w.offset++
	}
}

type bitReader struct {
	src    []byte
	offset int
}

func (r *bitReader) get(n int) uint32 {
	var v uint32
	for i := 0; i < n; i++ {
		v <<= 1
		if r.offset < len(r.src)*8 && r.src[r.offset/8]&(1<<uint(7-r.offset%8)) != 0 {
			v |= 1
		}
		r.offset++
	}
	return v
}

// CorrectPacket returns a corrected copy of one PCSI MMT 256-byte packet.
// It preserves the caller's raw receive buffer and reports corrected symbols.
func CorrectPacket(packet []byte) ([]byte, int, error) {
	if len(packet) != WirePacketSize {
		return nil, 0, fmt.Errorf("PCSI packet must be %d bytes", WirePacketSize)
	}
	// The sync byte is outside the RS codeword. The type byte is inside it,
	// so only validate sync before correction and inspect type afterwards.
	if packet[0] != PacketSync {
		return nil, 0, errors.New("PCSI packet sync byte mismatch")
	}
	out := append([]byte(nil), packet...)
	corrected, err := fec.CorrectRS8(out[1:])
	if err != nil {
		return nil, 0, fmt.Errorf("PCSI RS8 correction failed: %w", err)
	}
	if !fec.ValidateRS8(out[1:]) {
		return nil, 0, errors.New("PCSI RS8 codeword remains invalid after correction")
	}
	if out[1] != PacketType {
		return nil, 0, errors.New("PCSI packet type mismatch after RS8 correction")
	}
	return out, corrected, nil
}

func ParsePacket(packet []byte) (PacketInfo, error) {
	var out PacketInfo
	if len(packet) != WirePacketSize {
		return out, fmt.Errorf("PCSI packet must be 256 bytes")
	}
	if packet[0] != PacketSync || packet[1] != PacketType {
		return out, errors.New("PCSI packet sync/type mismatch")
	}
	if !fec.ValidateRS8(packet[1:]) {
		return out, errors.New("PCSI RS8 validation failed")
	}
	payload := packet[6:224]
	out.Callsign = DecodeCallsignBase40(binary.BigEndian.Uint32(packet[2:6]))
	out.ImageID = payload[0]
	out.PacketNumber = binary.BigEndian.Uint16(payload[1:3])
	out.Height = int(payload[3]) * 16
	out.Width = int(payload[4]) * 16
	out.YCbCrPixels = int(payload[5])
	out.ChannelBits = int(payload[6]) + 1
	if out.Width < 16 || out.Height < 16 || out.Width > 4080 || out.Height > 4080 || out.ChannelBits < 4 || out.ChannelBits > 8 || out.YCbCrPixels <= 0 {
		return PacketInfo{}, errors.New("PCSI packet header fields are invalid")
	}
	if out.YCbCrPixels*3*out.ChannelBits >= PCSIInfoBits {
		return PacketInfo{}, errors.New("PCSI packet chroma count exceeds its payload bit budget")
	}
	perPacket := out.YCbCrPixels + (PCSIInfoBits-out.YCbCrPixels*3*out.ChannelBits)/out.ChannelBits
	if perPacket <= 0 || out.PacketNumber >= uint16((out.Width*out.Height)/perPacket) {
		return PacketInfo{}, errors.New("PCSI packet number is outside the image packet range")
	}
	return out, nil
}
func EncodeCallsignBase40(callsign string) (uint32, error) {
	s := strings.ToUpper(strings.TrimSpace(callsign))
	if len(s) == 0 || len(s) > 6 {
		return 0, errors.New("PCSI callsign must contain 1..6 characters")
	}
	var value uint64
	for i := len(s) - 1; i >= 0; i-- {
		r := s[i]
		var digit uint64
		switch {
		case r >= 'A' && r <= 'Z':
			digit = uint64(r - 'A' + 14)
		case r >= '0' && r <= '9':
			digit = uint64(r - '0' + 1)
		case r == ' ' || r == '-':
			digit = 0
		default:
			return 0, fmt.Errorf("unsupported PCSI callsign character %q", r)
		}
		value = value*40 + digit
	}
	if value > 0xffffffff {
		return 0, errors.New("PCSI callsign does not fit Base40 field")
	}
	return uint32(value), nil
}
func DecodeCallsignBase40(value uint32) string {
	if value == 0 {
		return ""
	}
	out := make([]byte, 0, 6)
	for value > 0 {
		d := value % 40
		value /= 40
		switch {
		case d == 0:
			out = append(out, '-')
		case d < 11:
			out = append(out, byte('0'+d-1))
		case d < 14:
			out = append(out, '-')
		default:
			out = append(out, byte('A'+d-14))
		}
	}
	return string(out)
}

func shufflePixels(n int) []int {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	seed := uint64(1)
	const mod = uint64(1 << 31)
	for i := n - 1; i >= 0; i-- {
		seed = (1103515245*seed + 12345) % mod
		j := int(seed % uint64(i+1))
		p[i], p[j] = p[j], p[i]
	}
	return p
}

// DecoderState stores PCSI MMT measurements in the deterministic shuffled
// pixel-index space used by the reference Python implementation. Pixel indices
// are column-major: index = x*height + y.
type DecoderState struct {
	Info       PacketInfo
	Pixels     []int
	Y, Cb, Cr  []uint8
	YSeen      []bool
	ChromaSeen []bool
	Seen       map[uint16]bool
	Received   int

	yCoefficients  []float64
	cbCoefficients []float64
	crCoefficients []float64
}

// Clone returns an independent decoder snapshot. It is used by the RX
// reconstruction worker so numerical optimization never blocks packet ingest.
func (d *DecoderState) Clone() *DecoderState {
	if d == nil {
		return nil
	}
	out := &DecoderState{Info: d.Info, Received: d.Received}
	// Pixel order is immutable after decoder construction and can be shared
	// safely with the worker snapshot. All mutable sample planes are copied.
	out.Pixels = d.Pixels
	out.Y = append([]uint8(nil), d.Y...)
	out.Cb = append([]uint8(nil), d.Cb...)
	out.Cr = append([]uint8(nil), d.Cr...)
	out.YSeen = append([]bool(nil), d.YSeen...)
	out.ChromaSeen = append([]bool(nil), d.ChromaSeen...)
	out.yCoefficients = append([]float64(nil), d.yCoefficients...)
	out.cbCoefficients = append([]float64(nil), d.cbCoefficients...)
	out.crCoefficients = append([]float64(nil), d.crCoefficients...)
	if d.Seen != nil {
		out.Seen = make(map[uint16]bool, len(d.Seen))
		for id, seen := range d.Seen {
			out.Seen[id] = seen
		}
	}
	return out
}

type ReconstructionStats struct {
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	PacketsReceived int     `json:"packetsReceived"`
	PacketsExpected int     `json:"packetsExpected"`
	LumaSamples     int     `json:"lumaSamples"`
	ChromaSamples   int     `json:"chromaSamples"`
	TotalPixels     int     `json:"totalPixels"`
	Coverage        float64 `json:"coverage"`
}

func NewDecoderState(info PacketInfo) *DecoderState {
	n := info.Width * info.Height
	if info.Width <= 0 || info.Height <= 0 || n <= 0 || n > MaxPCSIReconstructionPixels {
		return &DecoderState{Info: info}
	}
	return &DecoderState{
		Info: info, Pixels: shufflePixels(n),
		Y: make([]uint8, n), Cb: make([]uint8, n), Cr: make([]uint8, n),
		YSeen: make([]bool, n), ChromaSeen: make([]bool, n), Seen: map[uint16]bool{},
	}
}

// CopyWarmStartFrom replaces only the optimization coefficient vectors with
// the latest solver state from another snapshot. Receive measurements and
// packet bookkeeping remain owned by the live decoder.
func (d *DecoderState) CopyWarmStartFrom(source *DecoderState) {
	if d == nil || source == nil || d.Info.Width != source.Info.Width || d.Info.Height != source.Info.Height {
		return
	}
	d.yCoefficients = append(d.yCoefficients[:0], source.yCoefficients...)
	d.cbCoefficients = append(d.cbCoefficients[:0], source.cbCoefficients...)
	d.crCoefficients = append(d.crCoefficients[:0], source.crCoefficients...)
}

// Add validates and corrects one PCSI packet, then adds its sparse pixel
// measurements. Duplicate packets are ignored. The 32-byte RS correction is
// performed on a private copy so a caller's raw capture remains unchanged.
func (d *DecoderState) Add(packet []byte) error {
	if d == nil || len(d.Pixels) == 0 {
		return errors.New("PCSI decoder state is not initialized")
	}
	if len(packet) != WirePacketSize {
		return fmt.Errorf("PCSI packet must be %d bytes", WirePacketSize)
	}
	corrected, _, err := CorrectPacket(packet)
	if err != nil {
		return err
	}
	info, err := ParsePacket(corrected)
	if err != nil {
		return err
	}
	if info.Callsign != d.Info.Callsign || info.ImageID != d.Info.ImageID || info.Width != d.Info.Width || info.Height != d.Info.Height || info.ChannelBits != d.Info.ChannelBits || info.YCbCrPixels != d.Info.YCbCrPixels {
		return errors.New("PCSI packet belongs to a different image or encoding profile")
	}
	if d.Seen[info.PacketNumber] {
		return nil
	}

	payload := corrected[6:224]
	r := bitReader{src: payload[PacketHeaderBytes:]}
	bits := info.ChannelBits
	maxValue := (1 << bits) - 1
	scaleBack := func(v uint32) uint8 { return uint8(math.RoundToEven(float64(v) / float64(maxValue) * 255)) }
	chromaCount := info.YCbCrPixels
	yOnlyCount := (PCSIInfoBits - chromaCount*3*bits) / bits
	pixelsPerPacket := chromaCount + yOnlyCount
	start := int(info.PacketNumber) * pixelsPerPacket
	if start < 0 || start+pixelsPerPacket > len(d.Pixels) {
		return errors.New("PCSI packet sample range exceeds image dimensions")
	}
	for i := 0; i < chromaCount; i++ {
		idx := d.Pixels[start+i]
		if !d.YSeen[idx] {
			d.YSeen[idx] = true
		}
		d.Y[idx] = scaleBack(r.get(bits))
		// The reference decoder labels the second wire sample Cb and the
		// third Cr, even though the TX OpenCV conversion names its source
		// channels YCrCb. Preserve that reference's end-to-end mapping.
		d.Cb[idx] = scaleBack(r.get(bits))
		d.Cr[idx] = scaleBack(r.get(bits))
		d.ChromaSeen[idx] = true
	}
	for i := 0; i < yOnlyCount; i++ {
		idx := d.Pixels[start+chromaCount+i]
		d.Y[idx] = scaleBack(r.get(bits))
		d.YSeen[idx] = true
	}
	d.Seen[info.PacketNumber] = true
	d.Received++
	return nil
}

// Stats returns packet and sampling coverage without running the numerical
// reconstruction. It is safe to call while a separate worker reconstructs.
func (d *DecoderState) Stats() ReconstructionStats {
	if d == nil {
		return ReconstructionStats{}
	}
	stats := ReconstructionStats{
		Width: d.Info.Width, Height: d.Info.Height,
		PacketsReceived: d.Received,
		TotalPixels:     len(d.Pixels),
	}
	if len(d.Pixels) == 0 {
		return stats
	}
	perPacket := d.Info.YCbCrPixels + (PCSIInfoBits-d.Info.YCbCrPixels*3*d.Info.ChannelBits)/d.Info.ChannelBits
	if perPacket > 0 {
		stats.PacketsExpected = len(d.Pixels) / perPacket
	}
	for _, seen := range d.YSeen {
		if seen {
			stats.LumaSamples++
		}
	}
	for _, seen := range d.ChromaSeen {
		if seen {
			stats.ChromaSamples++
		}
	}
	stats.Coverage = float64(stats.LumaSamples) / float64(stats.TotalPixels)
	return stats
}

// Reconstruct runs a pure-Go OWL-QN L1-regularized DCT recovery
// for Y, Cb and Cr, returning a real RGB preview from received samples. The
// reference PCSI implementation uses OWL-QN with the same squared-residual
// plus L1 objective. This solver is independently implemented and must still
// be compared against reference vectors for final on-air validation.
func (d *DecoderState) Reconstruct(iterations int) (*image.RGBA, ReconstructionStats, error) {
	return d.ReconstructContext(context.Background(), iterations)
}

// ReconstructContext performs the same reconstruction while honoring caller
// cancellation between OWL-QN iterations.
func (d *DecoderState) ReconstructContext(ctx context.Context, iterations int) (*image.RGBA, ReconstructionStats, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, ReconstructionStats{}, err
	}
	if d == nil || len(d.Pixels) == 0 {
		return nil, ReconstructionStats{}, errors.New("PCSI decoder state is empty")
	}
	if iterations <= 0 {
		iterations = 24
	}
	if iterations > 500 {
		return nil, ReconstructionStats{}, errors.New("PCSI reconstruction iteration limit is 500")
	}
	stats := d.Stats()
	if stats.LumaSamples == 0 {
		return nil, stats, errors.New("PCSI image has no luma measurements")
	}
	if stats.TotalPixels > MaxPCSIReconstructionPixels {
		return nil, stats, fmt.Errorf("PCSI reconstruction is limited to %d pixels per image", MaxPCSIReconstructionPixels)
	}
	indicesY, valuesY := collectSamples(d.Pixels, d.Y, d.YSeen)
	y, yc, err := reconstructChannelContext(ctx, d.Info.Width, d.Info.Height, indicesY, valuesY, d.yCoefficients, iterations)
	if err != nil {
		return nil, stats, fmt.Errorf("reconstruct PCSI luma: %w", err)
	}

	var cb, cr, cbCoefficients, crCoefficients []float64
	if stats.ChromaSamples > 0 {
		indicesC, valuesCb := collectSamples(d.Pixels, d.Cb, d.ChromaSeen)
		_, valuesCr := collectSamples(d.Pixels, d.Cr, d.ChromaSeen)
		// Reconstruct chroma sequentially. The previous implementation ran all
		// three full-resolution OWL-QN solvers concurrently, tripling their DCT
		// workspaces and line-search buffers. This often pushed desktop memory
		// above 1 GiB and starved the audio/RX workers when other applications
		// were active. Sequential channel solving preserves the same objective,
		// iteration budget and warm-start values while bounding peak memory to
		// one optimizer workspace.
		cb, cbCoefficients, err = reconstructChannelContext(ctx, d.Info.Width, d.Info.Height, indicesC, valuesCb, d.cbCoefficients, iterations)
		if err != nil {
			return nil, stats, fmt.Errorf("reconstruct PCSI Cb: %w", err)
		}
		cr, crCoefficients, err = reconstructChannelContext(ctx, d.Info.Width, d.Info.Height, indicesC, valuesCr, d.crCoefficients, iterations)
		if err != nil {
			return nil, stats, fmt.Errorf("reconstruct PCSI Cr: %w", err)
		}
	} else {
		cb = make([]float64, stats.TotalPixels)
		cr = make([]float64, stats.TotalPixels)
		for i := range cb {
			cb[i], cr[i] = 128, 128
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, stats, err
	}
	d.yCoefficients, d.cbCoefficients, d.crCoefficients = yc, cbCoefficients, crCoefficients

	w, h := d.Info.Width, d.Info.Height
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for yy := 0; yy < h; yy++ {
			rowIndex := yy*w + x
			Y := clampByte(y[rowIndex])
			Cb := clampByte(cb[rowIndex])
			Cr := clampByte(cr[rowIndex])
			r, g, b := color.YCbCrToRGB(Y, Cb, Cr)
			out.SetRGBA(x, yy, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return out, stats, nil
}

func collectSamples(pixelOrder []int, values []uint8, seen []bool) ([]int, []uint8) {
	count := 0
	for _, pixel := range pixelOrder {
		if pixel < len(seen) && seen[pixel] {
			count++
		}
	}
	indices := make([]int, 0, count)
	measurements := make([]uint8, 0, count)
	for _, pixel := range pixelOrder {
		if pixel < len(seen) && seen[pixel] {
			indices = append(indices, pixel)
			measurements = append(measurements, values[pixel])
		}
	}
	return indices, measurements
}

func clampByte(v float64) uint8 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(math.Round(v))
}
