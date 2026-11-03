package ssdv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/jpeg"
	"math"
	"strings"

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
}

// EncodeImage converts an image into canonical 256-byte SSDV packets. Input
// JPEG is generated as a baseline 4:2:0 stream, then its quantized DCT
// coefficients are transcoded into SSDV's fixed quantization tables and
// standard Huffman tables. Packet payloads are split on byte boundaries; the
// packet MCU marker is left at the sentinel because the stream decoder keeps
// the entropy state across contiguous packets. Packet-loss reconstruction
// metadata is intentionally deferred until MCU boundary indexing is added.
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

	// Generate a high-quality baseline source. The custom SSDV quantizers are
	// applied by the coefficient transcoder below, not by this intermediate
	// JPEG quality setting.
	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, src, &jpeg.Options{Quality: 100}); err != nil {
		return nil, fmt.Errorf("encode source JPEG: %w", err)
	}
	frame, err := parseBaselineJPEG(jpegBuf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("parse source JPEG: %w", err)
	}
	if frame.Width != width || frame.Height != height {
		return nil, errors.New("JPEG encoder changed source image dimensions")
	}
	if frame.Width/16 > 255 || frame.Height/16 > 255 {
		return nil, errors.New("SSDV dimensions exceed packet header limits")
	}
	entropy, err := transcodeJPEGScan(frame, cfg.Quality)
	if err != nil {
		return nil, fmt.Errorf("transcode JPEG scan: %w", err)
	}
	payloadSize := NoFECDataLength
	typ := byte(TypeNoFEC)
	if cfg.FEC {
		payloadSize = FECDataLength
		typ = TypeFEC
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
		if i == count-1 {
			packet[11] |= 0x04 // EOI
		}
		packet[12] = 0xff // MCU boundary sentinel; packet stream is contiguous.
		binary.BigEndian.PutUint16(packet[13:15], 0xffff)
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
	codes   map[byte]huffmanCode
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
	h.codes = make(map[byte]huffmanCode, len(symbols))
	code, k := 0, 0
	for bits := 1; bits <= 16; bits++ {
		count := int(counts[bits-1])
		if count > 0 {
			h.minCode[bits] = code
			h.maxCode[bits] = code + count - 1
			h.valPtr[bits] = k
			for j := 0; j < count; j++ {
				h.codes[symbols[k+j]] = huffmanCode{code: uint16(code + j), bits: uint8(bits)}
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
	v, ok := h.codes[symbol]
	if !ok {
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

func transcodeJPEGScan(f *jpegFrame, quality uint8) ([]byte, error) {
	if f.Width%16 != 0 || f.Height%16 != 0 {
		return nil, errors.New("SSDV JPEG dimensions must be multiples of 16")
	}
	dstQuant := [2][64]byte{}
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
	var reader jpegBitReader
	reader.data = f.scan
	var writer jpegBitWriter
	var srcDC, dstDC [3]int
	mcuCount := (f.Width / 16) * (f.Height / 16)
	for mcu := 0; mcu < mcuCount; mcu++ {
		for componentIndex, blockCount := range []int{4, 1, 1} {
			component := f.components[componentIndex]
			qsrc := f.quant[component.quantID]
			qdst := dstQuant[0]
			if componentIndex > 0 {
				qdst = dstQuant[1]
			}
			for block := 0; block < blockCount; block++ {
				var coeff [64]int
				dcCategory, err := reader.huffman(&f.dc[component.dcID])
				if err != nil {
					return nil, fmt.Errorf("MCU %d component %d DC: %w", mcu, componentIndex, err)
				}
				if dcCategory > 11 {
					return nil, errors.New("JPEG DC category exceeds baseline range")
				}
				delta, err := receiveExtend(&reader, int(dcCategory))
				if err != nil {
					return nil, err
				}
				srcDC[componentIndex] += delta
				newDC := roundedDivide(srcDC[componentIndex]*int(qsrc[0]), int(qdst[0]))
				dstDelta := newDC - dstDC[componentIndex]
				dstDC[componentIndex] = newDC
				dcOutCategory := category(dstDelta)
				if dcOutCategory > 11 {
					return nil, errors.New("transcoded JPEG DC category exceeds baseline range")
				}
				if err := writer.putHuffman(&f.dc[component.dcID], dcOutCategory); err != nil {
					return nil, err
				}
				writer.put(amplitude(dstDelta, dcOutCategory), dcOutCategory)

				k := 1
				for k < 64 {
					symbol, err := reader.huffman(&f.ac[component.acID])
					if err != nil {
						return nil, fmt.Errorf("MCU %d component %d AC: %w", mcu, componentIndex, err)
					}
					if symbol == 0 {
						break
					}
					if symbol == 0xf0 {
						k += 16
						if k > 64 {
							return nil, errors.New("JPEG AC zero run exceeds block")
						}
						continue
					}
					run, size := int(symbol>>4), int(symbol&0x0f)
					if size == 0 || size > 10 {
						return nil, errors.New("invalid JPEG AC coefficient category")
					}
					k += run
					if k >= 64 {
						return nil, errors.New("JPEG AC run exceeds block")
					}
					value, err := receiveExtend(&reader, size)
					if err != nil {
						return nil, err
					}
					coeff[k] = roundedDivide(value*int(qsrc[k]), int(qdst[k]))
					k++
				}
				// Re-encode the requantized block. Coefficients which rounded to
				// zero are naturally absorbed into the next run length.
				zeroRun := 0
				for k = 1; k < 64; k++ {
					if coeff[k] == 0 {
						zeroRun++
						continue
					}
					for zeroRun >= 16 {
						if err := writer.putHuffman(&f.ac[component.acID], 0xf0); err != nil {
							return nil, err
						}
						zeroRun -= 16
					}
					cat := category(coeff[k])
					if cat == 0 || cat > 10 {
						return nil, errors.New("transcoded JPEG AC category exceeds baseline range")
					}
					if err := writer.putHuffman(&f.ac[component.acID], byte(zeroRun<<4)|cat); err != nil {
						return nil, err
					}
					writer.put(amplitude(coeff[k], cat), cat)
					zeroRun = 0
				}
				if zeroRun > 0 {
					if err := writer.putHuffman(&f.ac[component.acID], 0); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	writer.flushOnes()
	return writer.out, nil
}

// DecodePacketsToJPEG validates, sorts and decodes a complete contiguous SSDV
// packet set. It currently reconstructs images produced as canonical 4:2:0
// streams. Packet-loss concealment is not attempted when an ID is missing.
func DecodePacketsToJPEG(rawPackets [][]byte) ([]byte, Packet, error) {
	if len(rawPackets) == 0 {
		return nil, Packet{}, errors.New("SSDV packet set is empty")
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
