package bcsi

// This compact Haar-only codec is a benchmark reference used to compare the
// sensing codec against a transform/quantization/entropy-coding pipeline. It
// is intentionally not advertised as an on-air protocol or JPEG-2000 clone.

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
)

func EncodeHaarBaseline(src image.Image, quantStep int) ([]byte, error) {
	if src == nil {
		return nil, errors.New("wavelet baseline source image is nil")
	}
	if quantStep < 1 || quantStep > 32 {
		return nil, errors.New("wavelet baseline quantization step must be 1..32")
	}
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 || w%BlockSize != 0 || h%BlockSize != 0 || w > 65535 || h > 65535 {
		return nil, errors.New("wavelet baseline dimensions must be multiples of 8")
	}
	var raw bytes.Buffer
	var values [3][blockPixels]float64
	var scratch [binary.MaxVarintLen64]byte
	for by := 0; by < h/BlockSize; by++ {
		for bx := 0; bx < w/BlockSize; bx++ {
			for y := 0; y < BlockSize; y++ {
				for x := 0; x < BlockSize; x++ {
					c := color.RGBAModel.Convert(src.At(bounds.Min.X+bx*BlockSize+x, bounds.Min.Y+by*BlockSize+y)).(color.RGBA)
					Y, Cb, Cr := color.RGBToYCbCr(c.R, c.G, c.B)
					i := y*BlockSize + x
					values[0][i], values[1][i], values[2][i] = float64(Y)-128, float64(Cb)-128, float64(Cr)-128
				}
			}
			for channel := 0; channel < 3; channel++ {
				coefficients := values[channel]
				haar2D(coefficients[:], false)
				for _, value := range coefficients {
					q := int64(math.Round(value / float64(quantStep)))
					n := binary.PutVarint(scratch[:], q)
					raw.Write(scratch[:n])
				}
			}
		}
	}
	compressed := deflate(raw.Bytes())
	if compressed == nil {
		return nil, errors.New("wavelet baseline DEFLATE failed")
	}
	out := make([]byte, 9+len(compressed))
	copy(out[:4], "HWB1")
	binary.BigEndian.PutUint16(out[4:6], uint16(w))
	binary.BigEndian.PutUint16(out[6:8], uint16(h))
	out[8] = byte(quantStep)
	copy(out[9:], compressed)
	return out, nil
}

func DecodeHaarBaseline(encoded []byte) (*image.RGBA, error) {
	if len(encoded) < 10 || string(encoded[:4]) != "HWB1" {
		return nil, errors.New("invalid wavelet baseline stream")
	}
	w, h, step := int(binary.BigEndian.Uint16(encoded[4:6])), int(binary.BigEndian.Uint16(encoded[6:8])), int(encoded[8])
	if w <= 0 || h <= 0 || w%BlockSize != 0 || h%BlockSize != 0 || step < 1 || step > 32 {
		return nil, errors.New("invalid wavelet baseline header")
	}
	r := flate.NewReader(bytes.NewReader(encoded[9:]))
	defer r.Close()
	limit := int64(w*h*3*binary.MaxVarintLen64 + 1)
	raw, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, fmt.Errorf("decode wavelet baseline payload: %w", err)
	}
	if int64(len(raw)) >= limit {
		return nil, errors.New("wavelet baseline payload exceeds limit")
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	offset := 0
	var planes [3][blockPixels]float64
	for by := 0; by < h/BlockSize; by++ {
		for bx := 0; bx < w/BlockSize; bx++ {
			for channel := 0; channel < 3; channel++ {
				for i := 0; i < blockPixels; i++ {
					q, n := binary.Varint(raw[offset:])
					if n <= 0 {
						return nil, errors.New("truncated wavelet coefficient stream")
					}
					offset += n
					planes[channel][i] = float64(q * int64(step))
				}
				haar2D(planes[channel][:], true)
			}
			for y := 0; y < BlockSize; y++ {
				for x := 0; x < BlockSize; x++ {
					i := y*BlockSize + x
					Y := uint8(clampInt(int(math.Round(planes[0][i]+128)), 0, 255))
					Cb := uint8(clampInt(int(math.Round(planes[1][i]+128)), 0, 255))
					Cr := uint8(clampInt(int(math.Round(planes[2][i]+128)), 0, 255))
					r, g, b := color.YCbCrToRGB(Y, Cb, Cr)
					out.SetRGBA(bx*BlockSize+x, by*BlockSize+y, color.RGBA{R: r, G: g, B: b, A: 255})
				}
			}
		}
	}
	if offset != len(raw) {
		return nil, errors.New("wavelet baseline stream has trailing coefficients")
	}
	return out, nil
}
