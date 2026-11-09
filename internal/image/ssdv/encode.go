package ssdv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/BH2VSQ/SATSSDV-Go/internal/fec"
)

const (
	DefaultJPEGQuality = 4
	FECDataLength      = 205
	NoFECDataLength    = 237
)

var (
	// SSDV's canonical quantization tables differ slightly from the default
	// JPEG tables. The encoder therefore transcodes DCT coefficients from the
	// source JPEG quantizers into these wire-defined tables.
	ssdvLumaQuant = [64]byte{
		16, 12, 12, 14, 12, 10, 16, 14, 14, 14, 18, 18, 16, 20, 24, 40,
		26, 24, 22, 22, 24, 50, 36, 38, 30, 40, 58, 52, 62, 60, 58, 52,
		56, 56, 64, 72, 92, 78, 64, 68, 88, 70, 56, 56, 80, 110, 82, 88,
		96, 98, 104, 104, 104, 62, 78, 114, 122, 112, 100, 120, 92, 102, 104, 100,
	}
	ssdvChromaQuant = [64]byte{
		18, 18, 18, 22, 22, 22, 48, 26, 26, 48, 100, 66, 56, 66, 100, 100,
		100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100,
		100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100,
		100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100, 100,
	}
	ssdvQualityScale = [8]int{5000, 357, 172, 116, 100, 58, 28, 0}
)

// EncodeConfig defines the canonical SSDV packet stream. FEC is enabled by
// default because it is the interoperable normal SSDV packet type.
type EncodeConfig struct {
	Callsign string
	ImageID  uint8
	Quality  uint8 // SSDV JPEG quantizer level 0..7; distinct from image dimensions.
	FEC      bool
	// GIFMode and Keyframe use the two reserved SSDV flag bits to identify
	// SATSSDV-Go's non-standard incremental GIF extension. Standard SSDV
	// packets remain byte-for-byte unchanged when GIFMode is false.
	GIFMode  bool
	Keyframe bool
}

// EncodeImage converts an image into canonical 256-byte SSDV packets. It
// transcodes baseline JPEG coefficients into SSDV tables, byte-aligns the
// first complete MCU in each payload, resets destination DC predictors at
// those boundaries, and writes the standard MCU offset/index fields. This
// makes each packet independently useful to progressive and loss-tolerant RX.
// sparsePreviewTemplate caches the immutable 16x16 JPEG scaffold used when
// reconstructing SSDV MCU strips. GIF RX may decode hundreds or thousands of
// short sparse segments per animation; rebuilding and reparsing this template
// for every segment needlessly burns CPU and can let the RX packet queue lag.
type sparsePreviewTemplateCache struct {
	once      sync.Once
	data      []byte
	frame     *jpegFrame
	headerEnd int
	err       error
}

var sharedSparsePreviewTemplate sparsePreviewTemplateCache

func getSparsePreviewTemplate() ([]byte, *jpegFrame, int, error) {
	sharedSparsePreviewTemplate.once.Do(func() {
		var template bytes.Buffer
		if err := jpeg.Encode(&template, image.NewRGBA(image.Rect(0, 0, 16, 16)), &jpeg.Options{Quality: 100}); err != nil {
			sharedSparsePreviewTemplate.err = fmt.Errorf("create SSDV block JPEG template: %w", err)
			return
		}
		sharedSparsePreviewTemplate.data = append([]byte(nil), template.Bytes()...)
		sharedSparsePreviewTemplate.frame, sharedSparsePreviewTemplate.err = parseBaselineJPEG(sharedSparsePreviewTemplate.data)
		if sharedSparsePreviewTemplate.err != nil {
			sharedSparsePreviewTemplate.err = fmt.Errorf("parse SSDV block JPEG template: %w", sharedSparsePreviewTemplate.err)
			return
		}
		sharedSparsePreviewTemplate.headerEnd, sharedSparsePreviewTemplate.err = jpegHeaderEnd(sharedSparsePreviewTemplate.data)
		if sharedSparsePreviewTemplate.err != nil {
			sharedSparsePreviewTemplate.err = fmt.Errorf("read SSDV block JPEG template header: %w", sharedSparsePreviewTemplate.err)
		}
	})
	return sharedSparsePreviewTemplate.data, sharedSparsePreviewTemplate.frame, sharedSparsePreviewTemplate.headerEnd, sharedSparsePreviewTemplate.err
}

func EncodeImage(src image.Image, cfg EncodeConfig) ([][]byte, error) {
	if src == nil {
		return nil, errors.New("SSDV source image is nil")
	}
	if cfg.Quality > 7 {
		return nil, errors.New("SSDV quality must be 0..7")
	}
	call, err := EncodeBase40(cfg.Callsign)
	if err != nil {
		return nil, err
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < 16 || height < 16 || width > 4080 || height > 4080 || width%16 != 0 || height%16 != 0 {
		return nil, errors.New("SSDV dimensions must be multiples of 16 in the range 16..4080")
	}

	// Generate a baseline source and transcode it to SSDV's canonical tables.
	// SSDV quality levels 0..7 already discard substantial high-frequency
	// detail. A quality-aware source JPEG prefilter avoids spending radio
	// payload on coefficients that the SSDV quantizer will remove anyway.
	// The selected source quality is an internal rate/quality tradeoff; the
	// on-air SSDV quality flag and canonical quantization tables stay intact.
	// If a pathological high-entropy MCU still exceeds one packet, the source
	// prefilter is stepped down further until each restart interval fits.
	payloadSize := NoFECDataLength
	typ := byte(TypeNoFEC)
	if cfg.FEC {
		payloadSize = FECDataLength
		typ = TypeFEC
	}
	sourceQualities := sourceJPEGQualityCandidates(cfg.Quality)
	var entropy []byte
	var mcuBoundaries map[int]packetMCUBoundary
	var frame *jpegFrame
	for _, sourceQuality := range sourceQualities {
		var jpegBuf bytes.Buffer
		if err := jpeg.Encode(&jpegBuf, src, &jpeg.Options{Quality: sourceQuality}); err != nil {
			return nil, fmt.Errorf("encode source JPEG: %w", err)
		}
		candidate, err := parseBaselineJPEG(jpegBuf.Bytes())
		if err != nil {
			return nil, fmt.Errorf("parse source JPEG: %w", err)
		}
		if candidate.Width != width || candidate.Height != height {
			return nil, errors.New("JPEG encoder changed source image dimensions")
		}
		if candidate.Width/16 > 255 || candidate.Height/16 > 255 {
			return nil, errors.New("SSDV dimensions exceed packet header limits")
		}
		candidateEntropy, boundaries, encodeErr := transcodeJPEGScanPacketized(candidate, cfg.Quality, payloadSize)
		if encodeErr == nil {
			frame, entropy, mcuBoundaries = candidate, candidateEntropy, boundaries
			break
		}
		if !strings.Contains(encodeErr.Error(), "one SSDV MCU exceeds the packet payload") {
			return nil, fmt.Errorf("transcode packetized JPEG scan: %w", encodeErr)
		}
	}
	if frame == nil {
		return nil, errors.New("could not fit a complete SSDV MCU into a packet payload")
	}
	if len(entropy) == 0 {
		return nil, errors.New("SSDV entropy stream is empty")
	}
	count := (len(entropy) + payloadSize - 1) / payloadSize
	if count == 0 || count > math.MaxUint16+1 {
		return nil, errors.New("SSDV image packet count exceeds 16-bit sequence range")
	}

	packets := make([][]byte, count)
	for i := 0; i < count; i++ {
		packet := make([]byte, PacketSize)
		packet[0], packet[1] = SyncWord, typ
		binary.BigEndian.PutUint32(packet[2:6], call)
		packet[6] = cfg.ImageID
		binary.BigEndian.PutUint16(packet[7:9], uint16(i))
		packet[9], packet[10] = byte(width/16), byte(height/16)
		packet[11] = ((cfg.Quality - 4) & 7) << 3 // MCU mode 0 = 4:2:0.
		if cfg.GIFMode {
			packet[11] |= 0x80 // reserved SSDV flag: SATSSDV-Go GIF extension
			if cfg.Keyframe {
				packet[11] |= 0x40
			}
		}
		if i == count-1 {
			packet[11] |= 0x04 // EOI
		}
		packet[12] = 0xff
		binary.BigEndian.PutUint16(packet[13:15], 0xffff)
		if boundary, ok := mcuBoundaries[i]; ok {
			packet[12] = boundary.offset
			binary.BigEndian.PutUint16(packet[13:15], boundary.index)
		}
		start := i * payloadSize
		end := start + payloadSize
		if end > len(entropy) {
			end = len(entropy)
		}
		copy(packet[15:15+payloadSize], entropy[start:end])
		fillSSDVNoise(packet[15+end-start : 15+payloadSize])
		crcAt, crcLen := 252, 251
		if cfg.FEC {
			crcAt, crcLen = 220, 219
		}
		binary.BigEndian.PutUint32(packet[crcAt:crcAt+4], crc32.ChecksumIEEE(packet[1:1+crcLen]))
		if cfg.FEC {
			parity, err := fec.RS8Parity(packet[1:224])
			if err != nil {
				return nil, err
			}
			copy(packet[224:], parity)
		}
		packets[i] = packet
	}
	return packets, nil
}

// sourceJPEGQualityCandidates maps the wire quantizer to a small, monotonic
// intermediate JPEG prefilter ladder. The default SSDV level 4 starts at 80;
// the canonical SSDV quantizer remains the final and interoperable quality
// control. Lower wire quality permits a stronger prefilter, while levels 6-7
// retain almost all source detail.
func sourceJPEGQualityCandidates(quality uint8) []int {
	preferred := 80
	switch {
	case quality <= 2:
		preferred = 68
	case quality == 3:
		preferred = 76
	case quality == 4:
		preferred = 80
	case quality == 5:
		preferred = 88
	case quality == 6:
		preferred = 92
	case quality == 7:
		preferred = 96
	}
	candidates := make([]int, 0, (preferred-60)/4+1)
	for q := preferred; q >= 60; q -= 8 {
		candidates = append(candidates, q)
	}
	if candidates[len(candidates)-1] != 60 {
		candidates = append(candidates, 60)
	}
	return candidates
}

// EncodeBase40 uses the SSDV Base40 alphabet and byte order.
func EncodeBase40(callsign string) (uint32, error) {
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	if callsign == "" || len(callsign) > 6 {
		return 0, errors.New("SSDV callsign must contain 1..6 ASCII characters")
	}
	var value uint64
	for i := len(callsign) - 1; i >= 0; i-- {
		var digit uint64
		switch c := callsign[i]; {
		case c >= 'A' && c <= 'Z':
			digit = uint64(c-'A') + 14
		case c >= '0' && c <= '9':
			digit = uint64(c-'0') + 1
		case c == ' ' || c == '-':
			digit = 0
		default:
			return 0, fmt.Errorf("invalid SSDV callsign character %q", callsign[i])
		}
		value = value*40 + digit
	}
	if value > math.MaxUint32 {
		return 0, errors.New("SSDV callsign exceeds Base40 range")
	}
	return uint32(value), nil
}

func fillSSDVNoise(dst []byte) {
	var value byte
	for i := range dst {
		value = value*245 + 45
		dst[i] = value
	}
}

type jpegComponent struct {
	id, h, v, quantID byte
	dcID, acID        byte
}
type jpegHuffman struct {
	minCode [17]int
	maxCode [17]int
	valPtr  [17]int
	symbols []byte
	codes   [256]huffmanCode
	valid   [256]bool
}
type huffmanCode struct {
	code uint16
	bits uint8
}
type jpegFrame struct {
	Width, Height int
	components    []jpegComponent
	quant         [4][64]byte
	quantSet      [4]bool
	dc            [4]jpegHuffman
	ac            [4]jpegHuffman
	dcSet         [4]bool
	acSet         [4]bool
	scan          []byte
}

func parseBaselineJPEG(data []byte) (*jpegFrame, error) {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return nil, errors.New("missing JPEG SOI")
	}
	f := &jpegFrame{}
	for off := 2; off < len(data); {
		for off < len(data) && data[off] != 0xff {
			off++
		}
		for off < len(data) && data[off] == 0xff {
			off++
		}
		if off >= len(data) {
			break
		}
		marker := data[off]
		off++
		if marker == 0xd9 {
			break
		}
		if marker == 0xd8 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if off+2 > len(data) {
			return nil, errors.New("truncated JPEG marker length")
		}
		length := int(binary.BigEndian.Uint16(data[off : off+2]))
		if length < 2 || off+length > len(data) {
			return nil, errors.New("invalid JPEG marker length")
		}
		segment := data[off+2 : off+length]
		off += length
		switch marker {
		case 0xdb:
			if err := f.parseQuant(segment); err != nil {
				return nil, err
			}
		case 0xc0:
			if err := f.parseFrame(segment); err != nil {
				return nil, err
			}
		case 0xc4:
			if err := f.parseHuffman(segment); err != nil {
				return nil, err
			}
		case 0xdd:
			return nil, errors.New("JPEG restart intervals are not supported by the first SSDV encoder")
		case 0xda:
			if err := f.parseScanHeader(segment); err != nil {
				return nil, err
			}
			var errScan error
			f.scan, errScan = extractJPEGEntropy(data[off:])
			if errScan != nil {
				return nil, errScan
			}
			if len(f.scan) == 0 {
				return nil, errors.New("JPEG scan is empty")
			}
			return f, nil
		case 0xc2:
			return nil, errors.New("progressive JPEG is not supported; use baseline JPEG")
		}
	}
	return nil, errors.New("JPEG SOS scan header was not found")
}

func (f *jpegFrame) parseQuant(segment []byte) error {
	for len(segment) > 0 {
		info := segment[0]
		segment = segment[1:]
		precision, id := info>>4, info&0x0f
		if precision != 0 || id > 3 || len(segment) < 64 {
			return errors.New("unsupported or truncated JPEG quantization table")
		}
		copy(f.quant[id][:], segment[:64])
		f.quantSet[id] = true
		segment = segment[64:]
	}
	return nil
}

func (f *jpegFrame) parseFrame(segment []byte) error {
	if len(segment) < 6 || segment[0] != 8 {
		return errors.New("only 8-bit baseline JPEG is supported")
	}
	f.Height = int(binary.BigEndian.Uint16(segment[1:3]))
	f.Width = int(binary.BigEndian.Uint16(segment[3:5]))
	n := int(segment[5])
	if n != 3 || len(segment) != 6+n*3 {
		return errors.New("SSDV source JPEG must have three YCbCr components")
	}
	f.components = make([]jpegComponent, n)
	for i := 0; i < n; i++ {
		base := 6 + i*3
		f.components[i] = jpegComponent{id: segment[base], h: segment[base+1] >> 4, v: segment[base+1] & 0x0f, quantID: segment[base+2]}
	}
	if f.components[0].h != 2 || f.components[0].v != 2 || f.components[1].h != 1 || f.components[1].v != 1 || f.components[2].h != 1 || f.components[2].v != 1 {
		return errors.New("SSDV encoder currently requires 4:2:0 JPEG sampling (Y=2x2, Cb/Cr=1x1)")
	}
	return nil
}

func (f *jpegFrame) parseHuffman(segment []byte) error {
	for len(segment) > 0 {
		if len(segment) < 17 {
			return errors.New("truncated JPEG Huffman table")
		}
		info := segment[0]
		class, id := info>>4, info&0x0f
		if class > 1 || id > 3 {
			return errors.New("unsupported JPEG Huffman table selector")
		}
		var counts [16]byte
		copy(counts[:], segment[1:17])
		n := 0
		for _, c := range counts {
			n += int(c)
		}
		if n == 0 || len(segment) < 17+n {
			return errors.New("invalid JPEG Huffman symbol count")
		}
		table, err := makeHuffman(counts, append([]byte(nil), segment[17:17+n]...))
		if err != nil {
			return err
		}
		if class == 0 {
			f.dc[id], f.dcSet[id] = table, true
		} else {
			f.ac[id], f.acSet[id] = table, true
		}
		segment = segment[17+n:]
	}
	return nil
}

func (f *jpegFrame) parseScanHeader(segment []byte) error {
	if len(segment) < 4 {
		return errors.New("truncated JPEG SOS")
	}
	n := int(segment[0])
	if n != 3 || len(segment) != 1+n*2+3 {
		return errors.New("SSDV requires a three-component sequential JPEG scan")
	}
	for i := 0; i < n; i++ {
		id, sel := segment[1+i*2], segment[2+i*2]
		found := false
		for j := range f.components {
			if f.components[j].id == id {
				f.components[j].dcID, f.components[j].acID = sel>>4, sel&0x0f
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("JPEG scan references unknown component %d", id)
		}
	}
	if segment[len(segment)-3] != 0 || segment[len(segment)-2] != 63 || segment[len(segment)-1] != 0 {
		return errors.New("only baseline sequential JPEG scans are supported")
	}
	for _, c := range f.components {
		if c.quantID > 3 || !f.quantSet[c.quantID] || !f.dcSet[c.dcID] || !f.acSet[c.acID] {
			return errors.New("JPEG scan is missing a quantization or Huffman table")
		}
	}
	return nil
}

func extractJPEGEntropy(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		b := data[i]
		if b != 0xff {
			out = append(out, b)
			continue
		}
		if i+1 >= len(data) {
			return nil, errors.New("truncated JPEG entropy marker")
		}
		next := data[i+1]
		switch next {
		case 0x00:
			out = append(out, 0xff)
			i++
		case 0xff:
			// Fill bytes are not part of the entropy stream.
			for i+1 < len(data) && data[i+1] == 0xff {
				i++
			}
		default:
			if next >= 0xd0 && next <= 0xd7 {
				return nil, errors.New("JPEG restart markers are not supported")
			}
			return out, nil
		}
	}
	return nil, errors.New("JPEG entropy stream has no terminating marker")
}

func makeHuffman(counts [16]byte, symbols []byte) (jpegHuffman, error) {
	var h jpegHuffman
	h.symbols = symbols
	code, k := 0, 0
	for bits := 1; bits <= 16; bits++ {
		count := int(counts[bits-1])
		if count > 0 {
			h.minCode[bits] = code
			h.maxCode[bits] = code + count - 1
			h.valPtr[bits] = k
			for j := 0; j < count; j++ {
				symbol := symbols[k+j]
				h.codes[symbol] = huffmanCode{code: uint16(code + j), bits: uint8(bits)}
				h.valid[symbol] = true
			}
			k += count
			code += count
		} else {
			h.minCode[bits] = -1
			h.maxCode[bits] = -1
		}
		code <<= 1
	}
	if k != len(symbols) {
		return h, errors.New("JPEG Huffman table count mismatch")
	}
	return h, nil
}

type jpegBitReader struct {
	data []byte
	bit  int
}

func (r *jpegBitReader) readBit() (int, error) {
	if r.bit >= len(r.data)*8 {
		return 0, errors.New("JPEG entropy stream ended early")
	}
	v := (r.data[r.bit/8] >> uint(7-r.bit%8)) & 1
	r.bit++
	return int(v), nil
}
func (r *jpegBitReader) readBits(n int) (int, error) {
	v := 0
	for i := 0; i < n; i++ {
		b, err := r.readBit()
		if err != nil {
			return 0, err
		}
		v = v<<1 | b
	}
	return v, nil
}
func (r *jpegBitReader) huffman(h *jpegHuffman) (byte, error) {
	code := 0
	for bits := 1; bits <= 16; bits++ {
		b, err := r.readBit()
		if err != nil {
			return 0, err
		}
		code = code<<1 | b
		if h.maxCode[bits] >= 0 && code >= h.minCode[bits] && code <= h.maxCode[bits] {
			idx := h.valPtr[bits] + code - h.minCode[bits]
			if idx < 0 || idx >= len(h.symbols) {
				return 0, errors.New("JPEG Huffman index out of range")
			}
			return h.symbols[idx], nil
		}
	}
	return 0, errors.New("invalid JPEG Huffman code")
}

type jpegBitWriter struct {
	out   []byte
	value uint8
	used  uint8
}

func (w *jpegBitWriter) put(value uint32, n uint8) {
	for i := int(n) - 1; i >= 0; i-- {
		w.value = w.value<<1 | uint8((value>>uint(i))&1)
		w.used++
		if w.used == 8 {
			w.out = append(w.out, w.value)
			w.value, w.used = 0, 0
		}
	}
}
func (w *jpegBitWriter) flushOnes() {
	if w.used > 0 {
		for w.used != 0 {
			w.put(1, 1)
		}
	}
}

func receiveExtend(r *jpegBitReader, n int) (int, error) {
	if n == 0 {
		return 0, nil
	}
	v, err := r.readBits(n)
	if err != nil {
		return 0, err
	}
	if v < (1 << (n - 1)) {
		v -= (1 << n) - 1
	}
	return v, nil
}
func category(value int) uint8 {
	if value < 0 {
		value = -value
	}
	var n uint8
	for value > 0 {
		n++
		value >>= 1
	}
	return n
}
func amplitude(value int, n uint8) uint32 {
	if n == 0 {
		return 0
	}
	if value < 0 {
		return uint32(value-1) & ((1 << n) - 1)
	}
	return uint32(value) & ((1 << n) - 1)
}
func (w *jpegBitWriter) putHuffman(h *jpegHuffman, symbol byte) error {
	v := h.codes[symbol]
	if !h.valid[symbol] {
		return fmt.Errorf("JPEG output Huffman symbol 0x%02x is unavailable", symbol)
	}
	w.put(uint32(v.code), v.bits)
	return nil
}
func roundedDivide(v, divisor int) int {
	if divisor <= 0 {
		return 0
	}
	v2 := v * 2 / divisor
	if v2&1 != 0 {
		if v > 0 {
			v2++
		} else {
			v2--
		}
	}
	return v2 / 2
}

type packetMCUBoundary struct {
	offset uint8
	index  uint16
}

func scaledQuantForQuality(quality uint8) [2][64]byte {
	var dstQuant [2][64]byte
	for i := 0; i < 64; i++ {
		for table, base := range [2]*[64]byte{&ssdvLumaQuant, &ssdvChromaQuant} {
			v := (int(base[i])*ssdvQualityScale[quality] + 50) / 100
			if v < 1 {
				v = 1
			}
			if v > 255 {
				v = 255
			}
			dstQuant[table][i] = byte(v)
		}
	}
	return dstQuant
}

func transcodeJPEGScan(f *jpegFrame, quality uint8) ([]byte, error) {
	if f.Width%16 != 0 || f.Height%16 != 0 {
		return nil, errors.New("SSDV JPEG dimensions must be multiples of 16")
	}
	out, _, _, err := transcodeJPEGScanMCUs(f, quality, (f.Width/16)*(f.Height/16), false, 0, nil)
	return out, err
}

// transcodeJPEGScanMCUs converts up to maxMCUs from a baseline JPEG entropy
// stream. In partial mode, a truncated final MCU is rolled back, allowing a
// packet receiver to render every complete block before a loss boundary.
// packetPayloadSize=0 keeps one continuous JPEG stream. A positive value
// inserts byte-aligned MCU restart points at SSDV payload boundaries and
// resets the destination DC predictors at each such point. restartAt is used
// by the inverse path to reset input SSDV DC predictors while preserving one
// continuous output JPEG scan.
func transcodeJPEGScanMCUs(f *jpegFrame, quality uint8, maxMCUs int, partial bool, packetPayloadSize int, restartAt map[int]int) ([]byte, map[int]packetMCUBoundary, int, error) {
	if f == nil || quality > 7 || maxMCUs < 0 || packetPayloadSize < 0 {
		return nil, nil, 0, errors.New("invalid SSDV entropy transcode parameters")
	}
	if f.Width%16 != 0 || f.Height%16 != 0 {
		return nil, nil, 0, errors.New("SSDV JPEG dimensions must be multiples of 16")
	}
	if maxMCUs == 0 {
		return []byte{}, map[int]packetMCUBoundary{}, 0, nil
	}
	dstQuant := scaledQuantForQuality(quality)
	reader := jpegBitReader{data: f.scan}
	var writer jpegBitWriter
	var srcDC, dstDC [3]int
	boundaries := make(map[int]packetMCUBoundary)
	if packetPayloadSize > 0 {
		boundaries[0] = packetMCUBoundary{offset: 0, index: 0}
	}
	nextPacketBoundary := packetPayloadSize
	decodedMCUs := 0
	for mcu := 0; mcu < maxMCUs; mcu++ {
		if byteOffset, ok := restartAt[mcu]; ok {
			// Skip the tail of the preceding packet's partial MCU plus one-fill
			// alignment bits, then reset input SSDV DC predictors at this marker.
			reader.bit = byteOffset * 8
			srcDC = [3]int{}
		}
		readerBefore, writerBefore := reader, writer
		srcBefore, dstBefore := srcDC, dstDC
		if packetPayloadSize > 0 && mcu > 0 {
			bitPosition := len(writer.out)*8 + int(writer.used)
			if bitPosition >= nextPacketBoundary*8 {
				// Align the first complete MCU after each payload boundary. Bytes
				// before this offset in the new packet finish the preceding MCU;
				// preserving them keeps the full image stream lossless.
				writer.flushOnes()
				packetID := len(writer.out) / packetPayloadSize
				packetOffset := len(writer.out) - packetID*packetPayloadSize
				if packetID > 65535 || packetOffset >= packetPayloadSize || packetOffset > 255 {
					return nil, nil, decodedMCUs, errors.New("SSDV MCU boundary exceeds packet field range")
				}
				boundaries[packetID] = packetMCUBoundary{offset: uint8(packetOffset), index: uint16(mcu)}
				dstDC = [3]int{}
				nextPacketBoundary = (packetID + 1) * packetPayloadSize
			}
		}

		mcuErr := transcodeJPEGMCU(f, &reader, &writer, &srcDC, &dstDC, dstQuant, mcu)
		if mcuErr != nil {
			if partial && decodedMCUs > 0 {
				reader, writer, srcDC, dstDC = readerBefore, writerBefore, srcBefore, dstBefore
				break
			}
			return nil, nil, decodedMCUs, mcuErr
		}
		decodedMCUs++
	}
	writer.flushOnes()
	return writer.out, boundaries, decodedMCUs, nil
}

func transcodeJPEGMCU(
	frame *jpegFrame,
	reader *jpegBitReader,
	writer *jpegBitWriter,
	sourceDC, destinationDC *[3]int,
	destinationQuant [2][64]byte,
	mcu int,
) error {
	for componentIndex, blockCount := range [...]int{4, 1, 1} {
		component := frame.components[componentIndex]
		sourceQuant := frame.quant[component.quantID]
		outputQuant := destinationQuant[0]
		if componentIndex > 0 {
			outputQuant = destinationQuant[1]
		}
		for block := 0; block < blockCount; block++ {
			var coefficients [64]int
			dcCategory, err := reader.huffman(&frame.dc[component.dcID])
			if err != nil {
				return fmt.Errorf("MCU %d component %d DC: %w", mcu, componentIndex, err)
			}
			if dcCategory > 11 {
				return errors.New("JPEG DC category exceeds baseline range")
			}
			delta, err := receiveExtend(reader, int(dcCategory))
			if err != nil {
				return err
			}
			sourceDC[componentIndex] += delta
			newDC := roundedDivide(sourceDC[componentIndex]*int(sourceQuant[0]), int(outputQuant[0]))
			destinationDelta := newDC - destinationDC[componentIndex]
			destinationDC[componentIndex] = newDC
			dcOutputCategory := category(destinationDelta)
			if dcOutputCategory > 11 {
				return errors.New("transcoded JPEG DC category exceeds baseline range")
			}
			if err := writer.putHuffman(&frame.dc[component.dcID], dcOutputCategory); err != nil {
				return err
			}
			writer.put(amplitude(destinationDelta, dcOutputCategory), dcOutputCategory)

			coefficientIndex := 1
			for coefficientIndex < 64 {
				symbol, err := reader.huffman(&frame.ac[component.acID])
				if err != nil {
					return fmt.Errorf("MCU %d component %d AC: %w", mcu, componentIndex, err)
				}
				if symbol == 0 {
					break
				}
				if symbol == 0xf0 {
					coefficientIndex += 16
					if coefficientIndex > 64 {
						return errors.New("JPEG AC zero run exceeds block")
					}
					continue
				}
				run, size := int(symbol>>4), int(symbol&0x0f)
				if size == 0 || size > 10 {
					return errors.New("invalid JPEG AC coefficient category")
				}
				coefficientIndex += run
				if coefficientIndex >= 64 {
					return errors.New("JPEG AC run exceeds block")
				}
				value, err := receiveExtend(reader, size)
				if err != nil {
					return err
				}
				coefficients[coefficientIndex] = roundedDivide(value*int(sourceQuant[coefficientIndex]), int(outputQuant[coefficientIndex]))
				coefficientIndex++
			}

			zeroRun := 0
			for coefficientIndex = 1; coefficientIndex < 64; coefficientIndex++ {
				if coefficients[coefficientIndex] == 0 {
					zeroRun++
					continue
				}
				for zeroRun >= 16 {
					if err := writer.putHuffman(&frame.ac[component.acID], 0xf0); err != nil {
						return err
					}
					zeroRun -= 16
				}
				categoryValue := category(coefficients[coefficientIndex])
				if categoryValue == 0 || categoryValue > 10 {
					return errors.New("transcoded JPEG AC category exceeds baseline range")
				}
				if err := writer.putHuffman(&frame.ac[component.acID], byte(zeroRun<<4)|categoryValue); err != nil {
					return err
				}
				writer.put(amplitude(coefficients[coefficientIndex], categoryValue), categoryValue)
				zeroRun = 0
			}
			if zeroRun > 0 {
				if err := writer.putHuffman(&frame.ac[component.acID], 0); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func transcodeJPEGScanPacketized(f *jpegFrame, quality uint8, payloadSize int) ([]byte, map[int]packetMCUBoundary, error) {
	if f.Width%16 != 0 || f.Height%16 != 0 {
		return nil, nil, errors.New("SSDV JPEG dimensions must be multiples of 16")
	}
	out, boundaries, _, err := transcodeJPEGScanMCUs(f, quality, (f.Width/16)*(f.Height/16), false, payloadSize, nil)
	return out, boundaries, err
}

func decodeJPEGEntropyStrip(templateBytes []byte, headerEnd int, quant [2][64]byte, encoded []byte, count int) (image.Image, error) {
	if count <= 0 {
		return nil, errors.New("MCU strip must contain at least one block")
	}
	header := append([]byte(nil), templateBytes[:headerEnd]...)
	if err := patchJPEGHeader(header, 16, count*16, quant); err != nil {
		return nil, err
	}
	jpegData := make([]byte, 0, len(header)+len(encoded)+16)
	jpegData = append(jpegData, header...)
	for _, b := range encoded {
		jpegData = append(jpegData, b)
		if b == 0xff {
			jpegData = append(jpegData, 0x00)
		}
	}
	jpegData = append(jpegData, 0xff, 0xd9)
	return jpeg.Decode(bytes.NewReader(jpegData))
}

func decodeMCUStrip(templateBytes []byte, templateFrame *jpegFrame, headerEnd int, quant [2][64]byte, quality uint8, entropy []byte, count int) (image.Image, error) {
	if count <= 0 {
		return nil, errors.New("MCU strip must contain at least one block")
	}
	frame := *templateFrame
	frame.Width, frame.Height, frame.scan = 16, count*16, entropy
	frame.quant[0], frame.quant[1] = quant[0], quant[1]
	frame.quantSet[0], frame.quantSet[1] = true, true
	encoded, _, decodedMCUs, err := transcodeJPEGScanMCUs(&frame, quality, count, false, 0, nil)
	if err != nil {
		return nil, err
	}
	if decodedMCUs != count {
		return nil, fmt.Errorf("decoded %d MCUs from strip, want %d", decodedMCUs, count)
	}
	return decodeJPEGEntropyStrip(templateBytes, headerEnd, quant, encoded, count)
}

// DecodePacketsToPreview reconstructs independent SSDV MCU blocks from any
// set of valid packets. Missing packets leave visibly damaged regions instead
// of forcing the image to wait for a contiguous top-to-bottom byte stream.
// Returned block count is the number of complete MCUs decoded into the canvas.
func DecodePacketsToPreview(rawPackets [][]byte) (*image.RGBA, Packet, int, error) {
	if len(rawPackets) == 0 {
		return nil, Packet{}, 0, errors.New("SSDV packet set is empty")
	}
	// Older SSDV senders may provide a continuous JPEG entropy stream without
	// per-packet MCU restart metadata. A GIF recovery keyframe can deliberately
	// fall back to that standard full-image format; decode it as a complete
	// JPEG rather than trying to interpret it as sparse MCU segments.
	legacyStream := true
	for _, raw := range rawPackets {
		packet, err := ParsePacket(raw)
		if err != nil {
			return nil, Packet{}, 0, err
		}
		if packet.MCUOffset != 0xff && packet.MCUIndex != 0xffff {
			legacyStream = false
			break
		}
	}
	if legacyStream {
		jpegData, metadata, err := DecodePacketsToJPEG(rawPackets)
		if err != nil {
			return nil, Packet{}, 0, err
		}
		decoded, err := jpeg.Decode(bytes.NewReader(jpegData))
		if err != nil {
			return nil, Packet{}, 0, fmt.Errorf("decode legacy SSDV JPEG preview: %w", err)
		}
		canvas := image.NewRGBA(image.Rect(0, 0, metadata.Width, metadata.Height))
		draw.Draw(canvas, canvas.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
		return canvas, metadata, int(metadata.MCUCount), nil
	}
	return decodePacketsToPreviewInto(nil, rawPackets, true, false)
}

// UpdatePacketsPreview overlays only the MCU blocks made newly decodable by
// the supplied packet and its immediate neighbours. It keeps existing pixels
// from earlier packets, so live RX no longer rebuilds every previously received
// packet for every new SSDV frame (an O(n^2) decode/allocation pattern).
func UpdatePacketsPreview(canvas *image.RGBA, rawPackets [][]byte) (*image.RGBA, Packet, int, error) {
	if canvas == nil {
		return nil, Packet{}, 0, errors.New("SSDV incremental preview canvas is nil")
	}
	return decodePacketsToPreviewInto(canvas, rawPackets, false, false)
}

// UpdatePacketsPreviewSparse applies only MCU segments delimited by adjacent
// SSDV packet restart points. It is intended for the GIF extension, where
// packet IDs may be sparse because unchanged image segments are omitted.
// An isolated non-EOI packet is not decoded without an explicit end boundary.
func UpdatePacketsPreviewSparse(canvas *image.RGBA, rawPackets [][]byte) (*image.RGBA, Packet, int, error) {
	if canvas == nil {
		return nil, Packet{}, 0, errors.New("SSDV sparse preview canvas is nil")
	}
	return decodePacketsToPreviewInto(canvas, rawPackets, false, true)
}

type ssdvPreviewDecode struct {
	canvas        *image.RGBA
	first         Packet
	byID          map[uint16]Packet
	ids           []int
	covered       []bool
	widthMCUs     int
	totalMCUs     int
	payloadSize   int
	templateBytes []byte
	templateFrame *jpegFrame
	headerEnd     int
	quant         [2][64]byte
	decodedTotal  int
}

func decodePacketsToPreviewInto(canvas *image.RGBA, rawPackets [][]byte, fillMissing, sparseOnly bool) (*image.RGBA, Packet, int, error) {
	if len(rawPackets) == 0 {
		return nil, Packet{}, 0, errors.New("SSDV packet set is empty")
	}
	first, byID, err := parsePreviewPacketSet(rawPackets)
	if err != nil {
		return nil, Packet{}, 0, err
	}
	if canvas == nil {
		canvas = image.NewRGBA(image.Rect(0, 0, first.Width, first.Height))
		draw.Draw(canvas, canvas.Bounds(), missingMCUFill(), image.Point{}, draw.Src)
	} else if canvas.Bounds().Dx() != first.Width || canvas.Bounds().Dy() != first.Height {
		return nil, first, 0, errors.New("SSDV preview canvas dimensions do not match packet image")
	}

	state, err := newSSDVPreviewDecode(canvas, first, byID)
	if err != nil {
		return nil, Packet{}, 0, err
	}
	if sparseOnly {
		state.decodeSparseSegments()
		if state.decodedTotal == 0 {
			return nil, first, 0, errors.New("no complete SSDV MCU update segments are currently decodable")
		}
		return canvas, first, state.decodedTotal, nil
	}
	state.decodeRestartPacketSegments()
	state.decodeAdjacentPacketSegments()
	if state.decodedTotal == 0 {
		return nil, first, 0, errors.New("no complete SSDV MCU blocks are currently decodable")
	}
	if fillMissing {
		state.fillMissingBlocks()
	}
	return canvas, first, state.decodedTotal, nil
}

func parsePreviewPacketSet(rawPackets [][]byte) (Packet, map[uint16]Packet, error) {
	byID := make(map[uint16]Packet, len(rawPackets))
	var first Packet
	for index, raw := range rawPackets {
		packet, err := ParsePacket(raw)
		if err != nil {
			return Packet{}, nil, fmt.Errorf("SSDV preview packet %d: %w", index, err)
		}
		if index == 0 {
			first = packet
		} else if packet.Callsign != first.Callsign || packet.ImageID != first.ImageID ||
			packet.Width != first.Width || packet.Height != first.Height || packet.Quality != first.Quality ||
			packet.FEC != first.FEC || packet.Subsampling != first.Subsampling {
			return Packet{}, nil, fmt.Errorf("SSDV preview packet %d belongs to a different image", index)
		}
		if packet.Subsampling != 0 {
			return Packet{}, nil, errors.New("SSDV block preview currently supports 4:2:0 MCU mode only")
		}
		if existing, ok := byID[packet.PacketID]; ok {
			if existing.Raw != packet.Raw {
				return Packet{}, nil, fmt.Errorf("conflicting duplicate SSDV packet ID %d", packet.PacketID)
			}
			continue
		}
		byID[packet.PacketID] = packet
	}
	return first, byID, nil
}

func newSSDVPreviewDecode(canvas *image.RGBA, first Packet, byID map[uint16]Packet) (*ssdvPreviewDecode, error) {
	widthMCUs := first.Width / 16
	totalMCUs := widthMCUs * (first.Height / 16)
	templateBytes, templateFrame, headerEnd, err := getSparsePreviewTemplate()
	if err != nil {
		return nil, err
	}
	quant := scaledQuantTables(first.Quality)
	templateFrame.quant[0], templateFrame.quant[1] = quant[0], quant[1]
	templateFrame.quantSet[0], templateFrame.quantSet[1] = true, true
	payloadSize := NoFECDataLength
	if first.FEC {
		payloadSize = FECDataLength
	}
	ids := make([]int, 0, len(byID))
	for id := range byID {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	return &ssdvPreviewDecode{
		canvas: canvas, first: first, byID: byID, ids: ids,
		covered: make([]bool, totalMCUs), widthMCUs: widthMCUs, totalMCUs: totalMCUs,
		payloadSize: payloadSize, templateBytes: templateBytes, templateFrame: templateFrame,
		headerEnd: headerEnd, quant: quant,
	}, nil
}

func (state *ssdvPreviewDecode) placeStrip(strip image.Image, startIndex, count int) {
	for local := 0; local < count; local++ {
		index := startIndex + local
		if index < 0 || index >= state.totalMCUs {
			break
		}
		x := (index % state.widthMCUs) * 16
		y := (index / state.widthMCUs) * 16
		source := image.Rect(0, local*16, 16, local*16+16)
		destination := image.Rect(x, y, x+16, y+16)
		draw.Draw(state.canvas, destination, strip, source.Min, draw.Src)
		if !state.covered[index] {
			state.covered[index] = true
			state.decodedTotal++
		}
	}
}

func (state *ssdvPreviewDecode) decodeSparseSegments() {
	for _, id := range state.ids {
		current := state.byID[uint16(id)]
		if current.MCUOffset == 0xff || current.MCUIndex == 0xffff {
			continue
		}
		endID, endOffset, endMCU, bounded, ok := state.findSparseSegmentEnd(id, current)
		if !ok || !state.packetRangeIsContiguous(uint16(id), endID) {
			continue
		}
		segment := state.buildSparseSegment(uint16(id), endID, current, endOffset, bounded)
		if len(segment) == 0 {
			continue
		}
		count := endMCU - int(current.MCUIndex)
		if count <= 0 {
			continue
		}
		strip, err := decodeMCUStrip(state.templateBytes, state.templateFrame, state.headerEnd, state.quant, state.first.Quality, segment, count)
		if err == nil {
			state.placeStrip(strip, int(current.MCUIndex), count)
		}
	}
}

func (state *ssdvPreviewDecode) findSparseSegmentEnd(id int, current Packet) (uint16, int, int, bool, bool) {
	for _, candidateID := range state.ids {
		if candidateID <= id {
			continue
		}
		candidate := state.byID[uint16(candidateID)]
		if candidate.MCUOffset != 0xff && candidate.MCUIndex != 0xffff && candidate.MCUIndex > current.MCUIndex {
			return uint16(candidateID), int(candidate.MCUOffset), int(candidate.MCUIndex), true, true
		}
	}
	for _, candidateID := range state.ids {
		if candidateID < id {
			continue
		}
		if state.byID[uint16(candidateID)].EOI {
			return uint16(candidateID), 0, state.totalMCUs, false, true
		}
	}
	return 0, 0, 0, false, false
}

func (state *ssdvPreviewDecode) packetRangeIsContiguous(start, end uint16) bool {
	for packetID := int(start); packetID <= int(end); packetID++ {
		if _, exists := state.byID[uint16(packetID)]; !exists {
			return false
		}
	}
	return true
}

func (state *ssdvPreviewDecode) buildSparseSegment(startID, endID uint16, current Packet, endOffset int, bounded bool) []byte {
	segment := make([]byte, 0, (int(endID)-int(startID)+1)*state.payloadSize)
	for packetID := int(startID); packetID <= int(endID); packetID++ {
		part := state.byID[uint16(packetID)]
		from, to := 0, state.payloadSize
		if uint16(packetID) == startID {
			from = 15 + int(current.MCUOffset) - 15
		}
		if bounded && uint16(packetID) == endID {
			to = endOffset
		}
		if from < 0 || from > state.payloadSize || to < from || to > state.payloadSize {
			return nil
		}
		segment = append(segment, part.Raw[15+from:15+to]...)
	}
	return segment
}

func (state *ssdvPreviewDecode) decodeRestartPacketSegments() {
	for _, id := range state.ids {
		packet := state.byID[uint16(id)]
		if packet.MCUOffset == 0xff || packet.MCUIndex == 0xffff {
			continue
		}
		if next, ok := state.byID[uint16(id+1)]; ok && next.MCUOffset != 0xff &&
			next.MCUIndex != 0xffff && next.MCUIndex > packet.MCUIndex {
			continue
		}
		start := 15 + int(packet.MCUOffset)
		end := 15 + state.payloadSize
		if start >= end || int(packet.MCUIndex) >= state.totalMCUs {
			continue
		}
		maxMCUs := state.totalMCUs - int(packet.MCUIndex)
		frame := *state.templateFrame
		frame.Width, frame.Height, frame.scan = 16, maxMCUs*16, packet.Raw[start:end]
		frame.quant[0], frame.quant[1] = state.quant[0], state.quant[1]
		frame.quantSet[0], frame.quantSet[1] = true, true
		entropy, _, decodedMCUs, err := transcodeJPEGScanMCUs(&frame, state.first.Quality, maxMCUs, true, 0, nil)
		if err != nil || decodedMCUs == 0 {
			continue
		}
		strip, err := decodeJPEGEntropyStrip(state.templateBytes, state.headerEnd, state.quant, entropy, decodedMCUs)
		if err == nil {
			state.placeStrip(strip, int(packet.MCUIndex), decodedMCUs)
		}
	}
}

func (state *ssdvPreviewDecode) decodeAdjacentPacketSegments() {
	for _, id := range state.ids {
		current := state.byID[uint16(id)]
		if current.MCUOffset == 0xff || current.MCUIndex == 0xffff {
			continue
		}
		next, ok := state.byID[uint16(id+1)]
		if !ok || next.MCUOffset == 0xff || next.MCUIndex == 0xffff || next.MCUIndex <= current.MCUIndex {
			continue
		}
		startOffset, endOffset := int(current.MCUOffset), int(next.MCUOffset)
		if startOffset >= state.payloadSize || endOffset > state.payloadSize {
			continue
		}
		segment := make([]byte, 0, (state.payloadSize-startOffset)+endOffset)
		segment = append(segment, current.Raw[15+startOffset:15+state.payloadSize]...)
		segment = append(segment, next.Raw[15:15+endOffset]...)
		count := int(next.MCUIndex - current.MCUIndex)
		strip, err := decodeMCUStrip(state.templateBytes, state.templateFrame, state.headerEnd, state.quant, state.first.Quality, segment, count)
		if err == nil {
			state.placeStrip(strip, int(current.MCUIndex), count)
		}
	}
}

func (state *ssdvPreviewDecode) fillMissingBlocks() {
	fill := missingMCUFill()
	for index, covered := range state.covered {
		if covered {
			continue
		}
		x := (index % state.widthMCUs) * 16
		y := (index / state.widthMCUs) * 16
		draw.Draw(state.canvas, image.Rect(x, y, x+16, y+16), fill, image.Point{}, draw.Src)
	}
}

func missingMCUFill() *image.Uniform {
	return &image.Uniform{C: color.RGBA{R: 218, G: 220, B: 222, A: 255}}
}

// DecodePacketsToJPEG validates, sorts and decodes a complete contiguous SSDV
// packet set. It currently reconstructs images produced as canonical 4:2:0
// streams. Packet-loss concealment is not attempted when an ID is missing.
func DecodePacketsToJPEG(rawPackets [][]byte) ([]byte, Packet, error) {
	if len(rawPackets) == 0 {
		return nil, Packet{}, errors.New("SSDV packet set is empty")
	}
	for _, raw := range rawPackets {
		packet, err := ParsePacket(raw)
		if err != nil {
			return nil, Packet{}, err
		}
		if packet.MCUOffset != 0xff && packet.MCUIndex != 0xffff {
			return decodeMCUPacketSetToJPEG(rawPackets)
		}
	}
	byID := make(map[uint16]Packet, len(rawPackets))
	var first Packet
	for i, raw := range rawPackets {
		packet, err := ParsePacket(raw)
		if err != nil {
			return nil, Packet{}, fmt.Errorf("SSDV packet %d: %w", i, err)
		}
		if i == 0 {
			first = packet
		} else if packet.Callsign != first.Callsign || packet.ImageID != first.ImageID || packet.Width != first.Width || packet.Height != first.Height || packet.Quality != first.Quality || packet.FEC != first.FEC || packet.Subsampling != first.Subsampling {
			return nil, Packet{}, fmt.Errorf("SSDV packet %d belongs to a different image or codec configuration", i)
		}
		if packet.Subsampling != 0 {
			return nil, Packet{}, errors.New("SSDV JPEG reconstruction currently supports 4:2:0 MCU mode only")
		}
		if existing, ok := byID[packet.PacketID]; ok {
			if existing.Raw != packet.Raw {
				return nil, Packet{}, fmt.Errorf("conflicting duplicate SSDV packet ID %d", packet.PacketID)
			}
			continue
		}
		byID[packet.PacketID] = packet
	}
	var finalID uint16
	foundEOI := false
	for id, packet := range byID {
		if packet.EOI {
			if foundEOI {
				return nil, Packet{}, errors.New("SSDV packet set contains multiple EOI markers")
			}
			foundEOI, finalID = true, id
		}
	}
	if !foundEOI {
		return nil, Packet{}, errors.New("SSDV packet set has no EOI packet")
	}
	if int(finalID)+1 != len(byID) {
		return nil, Packet{}, errors.New("SSDV packet set has packets after EOI or a missing packet ID")
	}
	payloadSize := NoFECDataLength
	if first.FEC {
		payloadSize = FECDataLength
	}
	stream := make([]byte, 0, len(byID)*payloadSize)
	for id := uint16(0); ; id++ {
		packet, ok := byID[id]
		if !ok {
			return nil, Packet{}, fmt.Errorf("SSDV packet ID %d is missing; cannot rebuild the contiguous JPEG scan", id)
		}
		if packet.EOI != (id == finalID) {
			return nil, Packet{}, fmt.Errorf("SSDV EOI flag is inconsistent at packet ID %d", id)
		}
		stream = append(stream, packet.Raw[15:15+payloadSize]...)
		if id == finalID {
			break
		}
	}

	// Obtain the baseline JPEG's canonical Huffman tables from Go's encoder,
	// then replace its quantizers and dimensions with values carried by SSDV.
	var template bytes.Buffer
	blank := image.NewRGBA(image.Rect(0, 0, 16, 16))
	if err := jpeg.Encode(&template, blank, &jpeg.Options{Quality: 100}); err != nil {
		return nil, Packet{}, fmt.Errorf("create JPEG header template: %w", err)
	}
	frame, err := parseBaselineJPEG(template.Bytes())
	if err != nil {
		return nil, Packet{}, fmt.Errorf("parse JPEG header template: %w", err)
	}
	frame.Width, frame.Height, frame.scan = first.Width, first.Height, stream
	targetTables := scaledQuantTables(first.Quality)
	frame.quant[0], frame.quant[1] = targetTables[0], targetTables[1]
	frame.quantSet[0], frame.quantSet[1] = true, true

	// Decode exactly the number of MCU blocks specified by the SSDV header.
	// This consumes the final packet's entropy and naturally discards its
	// deterministic PRNG padding bytes.
	entropy, err := transcodeJPEGScan(frame, first.Quality)
	if err != nil {
		return nil, Packet{}, fmt.Errorf("reconstruct SSDV JPEG entropy: %w", err)
	}
	headerEnd, err := jpegHeaderEnd(template.Bytes())
	if err != nil {
		return nil, Packet{}, err
	}
	header := append([]byte(nil), template.Bytes()[:headerEnd]...)
	if err := patchJPEGHeader(header, first.Width, first.Height, targetTables); err != nil {
		return nil, Packet{}, err
	}
	out := make([]byte, 0, len(header)+len(entropy)+32)
	out = append(out, header...)
	for _, b := range entropy {
		out = append(out, b)
		if b == 0xff {
			out = append(out, 0x00)
		}
	}
	out = append(out, 0xff, 0xd9)
	return out, first, nil
}

func decodeMCUPacketSetToJPEG(rawPackets [][]byte) ([]byte, Packet, error) {
	byID := make(map[uint16]Packet, len(rawPackets))
	var first Packet
	var finalID uint16
	foundEOI := false
	for i, raw := range rawPackets {
		packet, err := ParsePacket(raw)
		if err != nil {
			return nil, Packet{}, fmt.Errorf("SSDV packet %d: %w", i, err)
		}
		if i == 0 {
			first = packet
		} else if packet.Callsign != first.Callsign || packet.ImageID != first.ImageID || packet.Width != first.Width || packet.Height != first.Height || packet.Quality != first.Quality || packet.FEC != first.FEC || packet.Subsampling != first.Subsampling {
			return nil, Packet{}, fmt.Errorf("SSDV packet %d belongs to a different image", i)
		}
		if old, ok := byID[packet.PacketID]; ok {
			if old.Raw != packet.Raw {
				return nil, Packet{}, fmt.Errorf("conflicting duplicate SSDV packet ID %d", packet.PacketID)
			}
			continue
		}
		byID[packet.PacketID] = packet
		if packet.EOI {
			foundEOI, finalID = true, packet.PacketID
		}
	}
	if !foundEOI || int(finalID)+1 != len(byID) {
		return nil, Packet{}, errors.New("SSDV packet set is incomplete")
	}
	payloadSize := NoFECDataLength
	if first.FEC {
		payloadSize = FECDataLength
	}
	stream := make([]byte, 0, len(byID)*payloadSize)
	restartAt := make(map[int]int, len(byID))
	for id := uint16(0); ; id++ {
		packet, ok := byID[id]
		if !ok {
			return nil, Packet{}, fmt.Errorf("SSDV packet ID %d is missing", id)
		}
		if packet.EOI != (id == finalID) {
			return nil, Packet{}, fmt.Errorf("SSDV EOI flag is inconsistent at packet ID %d", id)
		}
		if packet.MCUOffset != 0xff && packet.MCUIndex != 0xffff {
			restartAt[int(packet.MCUIndex)] = int(id)*payloadSize + int(packet.MCUOffset)
		}
		stream = append(stream, packet.Raw[15:15+payloadSize]...)
		if id == finalID {
			break
		}
	}
	var template bytes.Buffer
	if err := jpeg.Encode(&template, image.NewRGBA(image.Rect(0, 0, 16, 16)), &jpeg.Options{Quality: 100}); err != nil {
		return nil, Packet{}, fmt.Errorf("create JPEG header template: %w", err)
	}
	frame, err := parseBaselineJPEG(template.Bytes())
	if err != nil {
		return nil, Packet{}, fmt.Errorf("parse JPEG header template: %w", err)
	}
	frame.Width, frame.Height, frame.scan = first.Width, first.Height, stream
	targetTables := scaledQuantTables(first.Quality)
	frame.quant[0], frame.quant[1] = targetTables[0], targetTables[1]
	frame.quantSet[0], frame.quantSet[1] = true, true
	entropy, _, decodedMCUs, err := transcodeJPEGScanMCUs(frame, first.Quality, int(first.MCUCount), false, 0, restartAt)
	if err != nil {
		return nil, Packet{}, fmt.Errorf("reconstruct SSDV JPEG entropy: %w", err)
	}
	if decodedMCUs != int(first.MCUCount) {
		return nil, Packet{}, fmt.Errorf("reconstructed %d MCU blocks, want %d", decodedMCUs, first.MCUCount)
	}
	headerEnd, err := jpegHeaderEnd(template.Bytes())
	if err != nil {
		return nil, Packet{}, err
	}
	header := append([]byte(nil), template.Bytes()[:headerEnd]...)
	if err := patchJPEGHeader(header, first.Width, first.Height, targetTables); err != nil {
		return nil, Packet{}, err
	}
	out := make([]byte, 0, len(header)+len(entropy)+32)
	out = append(out, header...)
	for _, b := range entropy {
		out = append(out, b)
		if b == 0xff {
			out = append(out, 0x00)
		}
	}
	out = append(out, 0xff, 0xd9)
	return out, first, nil
}

// DecodePacketPrefixToJPEG reconstructs the longest decodable top-to-bottom
// MCU-row prefix from a contiguous SSDV packet prefix. Unlike
// DecodePacketsToJPEG, it does not require an EOI packet or a complete image;
// it is intended for incremental RX previews only. Every supplied packet is
// still RS-corrected and CRC-validated by ParsePacket before use.
func DecodePacketPrefixToJPEG(rawPackets [][]byte) ([]byte, Packet, int, error) {
	if len(rawPackets) == 0 {
		return nil, Packet{}, 0, errors.New("SSDV packet prefix is empty")
	}
	for _, raw := range rawPackets {
		packet, err := ParsePacket(raw)
		if err != nil {
			return nil, Packet{}, 0, err
		}
		if packet.MCUOffset != 0xff && packet.MCUIndex != 0xffff {
			preview, metadata, _, decodeErr := DecodePacketsToPreview(rawPackets)
			if decodeErr != nil {
				return nil, metadata, 0, decodeErr
			}
			var out bytes.Buffer
			if encodeErr := jpeg.Encode(&out, preview, &jpeg.Options{Quality: 96}); encodeErr != nil {
				return nil, metadata, 0, encodeErr
			}
			return out.Bytes(), metadata, metadata.Height, nil
		}
	}
	byID := make(map[uint16]Packet, len(rawPackets))
	var first Packet
	for i, raw := range rawPackets {
		packet, err := ParsePacket(raw)
		if err != nil {
			return nil, Packet{}, 0, fmt.Errorf("SSDV prefix packet %d: %w", i, err)
		}
		if i == 0 {
			first = packet
		} else if packet.Callsign != first.Callsign || packet.ImageID != first.ImageID ||
			packet.Width != first.Width || packet.Height != first.Height || packet.Quality != first.Quality ||
			packet.FEC != first.FEC || packet.Subsampling != first.Subsampling {
			return nil, Packet{}, 0, fmt.Errorf("SSDV prefix packet %d belongs to a different image or codec configuration", i)
		}
		if packet.Subsampling != 0 {
			return nil, Packet{}, 0, errors.New("SSDV JPEG reconstruction currently supports 4:2:0 MCU mode only")
		}
		if existing, ok := byID[packet.PacketID]; ok {
			if existing.Raw != packet.Raw {
				return nil, Packet{}, 0, fmt.Errorf("conflicting duplicate SSDV packet ID %d", packet.PacketID)
			}
			continue
		}
		byID[packet.PacketID] = packet
	}
	payloadSize := NoFECDataLength
	if first.FEC {
		payloadSize = FECDataLength
	}
	stream := make([]byte, 0, len(byID)*payloadSize)
	prefixPackets := make([]Packet, 0, len(byID))
	for id := uint16(0); ; id++ {
		packet, ok := byID[id]
		if !ok {
			break
		}
		if packet.EOI {
			// A full EOI packet may contain deterministic padding. Use the
			// strict full-image decoder when the caller has the entire image.
			break
		}
		stream = append(stream, packet.Raw[15:15+payloadSize]...)
		prefixPackets = append(prefixPackets, packet)
	}
	if len(prefixPackets) == 0 {
		return nil, Packet{}, 0, errors.New("SSDV prefix has no contiguous non-EOI packet from packet ID 0")
	}

	var template bytes.Buffer
	blank := image.NewRGBA(image.Rect(0, 0, 16, 16))
	if err := jpeg.Encode(&template, blank, &jpeg.Options{Quality: 100}); err != nil {
		return nil, Packet{}, 0, fmt.Errorf("create JPEG header template: %w", err)
	}
	frame, err := parseBaselineJPEG(template.Bytes())
	if err != nil {
		return nil, Packet{}, 0, fmt.Errorf("parse JPEG header template: %w", err)
	}
	targetTables := scaledQuantTables(first.Quality)
	frame.Width = first.Width
	frame.scan = stream
	frame.quant[0], frame.quant[1] = targetTables[0], targetTables[1]
	frame.quantSet[0], frame.quantSet[1] = true, true

	headerEnd, err := jpegHeaderEnd(template.Bytes())
	if err != nil {
		return nil, Packet{}, 0, err
	}
	maxRows := first.Height / 16
	// Work down from the largest possible crop. A failed row count means the
	// received entropy stream ended inside an MCU; the next smaller whole-row
	// candidate is safe to render without inventing or padding image data.
	for rows := maxRows; rows >= 1; rows-- {
		height := rows * 16
		frame.Height = height
		entropy, decodeErr := transcodeJPEGScan(frame, first.Quality)
		if decodeErr != nil {
			continue
		}
		header := append([]byte(nil), template.Bytes()[:headerEnd]...)
		if err := patchJPEGHeader(header, first.Width, height, targetTables); err != nil {
			return nil, Packet{}, 0, err
		}
		out := make([]byte, 0, len(header)+len(entropy)+32)
		out = append(out, header...)
		for _, b := range entropy {
			out = append(out, b)
			if b == 0xff {
				out = append(out, 0x00)
			}
		}
		out = append(out, 0xff, 0xd9)
		return out, first, height, nil
	}
	return nil, first, 0, errors.New("SSDV prefix does not yet contain one complete MCU row")
}

func scaledQuantTables(quality uint8) [2][64]byte {
	var result [2][64]byte
	for table, base := range [2]*[64]byte{&ssdvLumaQuant, &ssdvChromaQuant} {
		for i, coefficient := range base {
			v := (int(coefficient)*ssdvQualityScale[quality] + 50) / 100
			if v < 1 {
				v = 1
			}
			if v > 255 {
				v = 255
			}
			result[table][i] = byte(v)
		}
	}
	return result
}

func jpegHeaderEnd(data []byte) (int, error) {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 0, errors.New("invalid JPEG header template")
	}
	for off := 2; off < len(data); {
		for off < len(data) && data[off] != 0xff {
			off++
		}
		for off < len(data) && data[off] == 0xff {
			off++
		}
		if off >= len(data) {
			break
		}
		marker := data[off]
		off++
		if marker == 0xd8 || marker == 0xd9 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if off+2 > len(data) {
			return 0, errors.New("truncated JPEG header marker")
		}
		length := int(binary.BigEndian.Uint16(data[off : off+2]))
		if length < 2 || off+length > len(data) {
			return 0, errors.New("invalid JPEG header marker length")
		}
		if marker == 0xda {
			return off + length, nil
		}
		off += length
	}
	return 0, errors.New("JPEG SOS marker missing from header template")
}

func patchJPEGHeader(header []byte, width, height int, tables [2][64]byte) error {
	for off := 2; off < len(header); {
		for off < len(header) && header[off] != 0xff {
			off++
		}
		for off < len(header) && header[off] == 0xff {
			off++
		}
		if off >= len(header) {
			break
		}
		marker := header[off]
		off++
		if marker == 0xd8 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if off+2 > len(header) {
			return errors.New("truncated JPEG header while patching")
		}
		length := int(binary.BigEndian.Uint16(header[off : off+2]))
		if length < 2 || off+length > len(header) {
			return errors.New("invalid JPEG header segment while patching")
		}
		segmentStart, segmentEnd := off+2, off+length
		switch marker {
		case 0xdb:
			pos := segmentStart
			for pos < segmentEnd {
				info := header[pos]
				pos++
				if info>>4 != 0 || segmentEnd-pos < 64 {
					return errors.New("unsupported JPEG quantizer in header template")
				}
				id := info & 0x0f
				if id <= 1 {
					copy(header[pos:pos+64], tables[id][:])
				}
				pos += 64
			}
		case 0xc0:
			if segmentEnd-segmentStart < 5 {
				return errors.New("truncated JPEG SOF0 header")
			}
			binary.BigEndian.PutUint16(header[segmentStart+1:segmentStart+3], uint16(height))
			binary.BigEndian.PutUint16(header[segmentStart+3:segmentStart+5], uint16(width))
		}
		if marker == 0xda {
			return nil
		}
		off += length
	}
	return errors.New("JPEG SOS marker missing while patching header")
}

// GIFFrameControlOptions contains the typed fields shared by SGV1 and SGV2
// frame boundary packets. MotionVectors is used only by SGV2.
type GIFFrameControlOptions struct {
	Callsign      string
	ImageID       uint8
	Width         int
	Height        int
	Quality       uint8
	DelayCS       uint16
	DataPackets   uint16
	FrameStart    bool
	FrameEnd      bool
	StreamStart   bool
	StreamEnd     bool
	Keyframe      bool
	MotionVectors []GIFMotionVector
}

// NewGIFControlPacket is the legacy frame-close constructor retained for
// compatibility with early SATSSDV-Go GIF prototypes.
func NewGIFControlPacket(options GIFFrameControlOptions) ([]byte, error) {
	options.DataPackets = 0
	options.FrameStart = false
	options.FrameEnd = true
	return NewGIFFrameControlPacket(options)
}

// NewGIFFrameControlPacket creates a versioned frame-boundary packet. A frame
// open marker (PacketID 0xfffe) carries the expected number of image-data
// packets, allowing RX to show an accurate live packet counter before frame
// close. A frame close marker (PacketID 0xffff) commits the composed frame.
// Repeated copies are identical and are removed by the transport deduplicator.
func NewGIFFrameControlPacket(options GIFFrameControlOptions) ([]byte, error) {
	width, height := options.Width, options.Height
	quality := options.Quality
	frameStart, frameEnd := options.FrameStart, options.FrameEnd
	callsign, imageID := options.Callsign, options.ImageID
	keyframe, streamStart, streamEnd := options.Keyframe, options.StreamStart, options.StreamEnd
	delayCS, dataPackets := options.DelayCS, options.DataPackets
	if width < 16 || height < 16 || width > 4080 || height > 4080 || width%16 != 0 || height%16 != 0 {
		return nil, errors.New("GIF control packet dimensions must be SSDV-aligned multiples of 16")
	}
	if quality > 7 {
		return nil, errors.New("GIF control packet quality must be 0..7")
	}
	if frameStart == frameEnd {
		return nil, errors.New("GIF frame control must be exactly one of frame-start or frame-end")
	}
	call, err := EncodeBase40(callsign)
	if err != nil {
		return nil, err
	}
	packet := make([]byte, PacketSize)
	packet[0], packet[1] = SyncWord, TypeFEC
	binary.BigEndian.PutUint32(packet[2:6], call)
	packet[6] = imageID
	if frameStart {
		binary.BigEndian.PutUint16(packet[7:9], 0xfffe)
	} else {
		binary.BigEndian.PutUint16(packet[7:9], 0xffff)
	}
	packet[9], packet[10] = byte(width/16), byte(height/16)
	packet[11] = (((quality - 4) & 7) << 3) | 0x04 | 0x80
	if keyframe {
		packet[11] |= 0x40
	}
	packet[12] = 0xff
	binary.BigEndian.PutUint16(packet[13:15], 0xffff)
	binary.BigEndian.PutUint16(packet[15:17], delayCS)
	fillSSDVNoise(packet[17:220])
	copy(packet[17:21], []byte("SGV1"))
	// This byte is protocol metadata, not opaque padding. Clear it before
	// setting flags; otherwise the random filler would spuriously assert
	// GIFStart/GIFEnd/frame-boundary bits and reset RX sessions unpredictably.
	packet[21] = 0
	if streamStart {
		packet[21] |= 0x01
	}
	if streamEnd {
		packet[21] |= 0x02
	}
	if frameStart {
		packet[21] |= 0x04
	}
	if frameEnd {
		packet[21] |= 0x08
	}
	binary.BigEndian.PutUint16(packet[22:24], dataPackets)
	binary.BigEndian.PutUint32(packet[220:224], crc32.ChecksumIEEE(packet[1:220]))
	parity, err := fec.RS8Parity(packet[1:224])
	if err != nil {
		return nil, err
	}
	copy(packet[224:], parity)
	return packet, nil
}

// NewGIFFrameControlPacketV2 builds the SGV2 frame-control extension. It keeps
// the SGV1 magic, base flags and data-packet count at their original offsets so
// SGV1 parsers can still recognize the frame boundaries and ignore the new
// motion payload. New receivers additionally apply the MCU motion vectors.
func NewGIFFrameControlPacketV2(options GIFFrameControlOptions) ([]byte, error) {
	width, height := options.Width, options.Height
	vectors := options.MotionVectors
	if len(vectors) > 32 {
		return nil, errors.New("SGV2 motion extension supports at most 32 vectors per frame")
	}
	packet, err := NewGIFFrameControlPacket(options)
	if err != nil {
		return nil, err
	}
	if len(vectors) > 0 && (!options.FrameStart || options.Keyframe) {
		return nil, errors.New("SGV2 motion vectors are only valid on delta frame-open controls")
	}
	if !validGIFMotionSet(vectors, width, height) {
		return nil, errors.New("SGV2 motion vector set is invalid or overlaps")
	}
	packet[21] |= 0x10 // SGV2 extension-present flag; legacy low bits are unchanged.
	packet[24] = 2
	packet[25] = byte(len(vectors))
	packet[26] = 6
	for i, vector := range vectors {
		base := 27 + i*6
		packet[base] = vector.SourceX
		packet[base+1] = vector.SourceY
		packet[base+2] = vector.Width
		packet[base+3] = vector.Height
		packet[base+4] = byte(vector.DeltaX)
		packet[base+5] = byte(vector.DeltaY)
	}
	// The extension lies in the control packet's reserved bytes; recompute both
	// integrity layers after writing it.
	binary.BigEndian.PutUint32(packet[220:224], crc32.ChecksumIEEE(packet[1:220]))
	parity, err := fec.RS8Parity(packet[1:224])
	if err != nil {
		return nil, err
	}
	copy(packet[224:], parity)
	return packet, nil
}
