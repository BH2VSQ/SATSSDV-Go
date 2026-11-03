// SPDX-License-Identifier: GPL-3.0-or-later
//
// This is a Go translation of the 2400-bit/s quadrature receive filter and
// symbol timing logic in the ARCSSD source supplied with SATSSDV-Go.
// Original source copyright 2015 Wei Mingchuan and HIT Research Center of
// Satellite Technology. See THIRD_PARTY_NOTICES.md.
package ccsds

import (
	"errors"
	"math"
)

// ARCSSD2400Demodulator implements the 19.2 kHz, 2400 bit/s receiver used by
// the supplied ARCSSD CCSDS modem. It keeps FIR, carrier and bit-clock state
// across Feed calls, so audio device block boundaries do not reset acquisition.
type ARCSSD2400Demodulator struct {
	phase       int
	phaseClock  int
	sampledBits uint8
	bitsInByte  uint8
	currentByte byte
	filterI     [61]float64
	filterQ     [61]float64
	delayI      [8]float64
	delayQ      [8]float64
}

var arcssd2400ReceiverLPF = [...]float64{
	-2.045027e-04, 5.309061e-03, 9.739672e-03, 1.178566e-02,
	1.070174e-02, 6.541650e-03, 2.045215e-04, -6.741124e-03,
	-1.242540e-02, -1.515481e-02, -1.388318e-02, -8.552441e-03,
	-2.045361e-04, 9.183787e-03, 1.712491e-02, 2.121866e-02,
	1.979097e-02, 1.241896e-02, 2.045465e-04, -1.429075e-02,
	-2.746302e-02, -3.536657e-02, -3.455941e-02, -2.291310e-02,
	-2.045528e-04, 3.165351e-02, 6.881337e-02, 1.061029e-01,
	1.379343e-01, 1.593320e-01, 1.668712e-01, 1.593320e-01,
	1.379343e-01, 1.061029e-01, 6.881337e-02, 3.165351e-02,
	-2.045528e-04, -2.291310e-02, -3.455941e-02, -3.536657e-02,
	-2.746302e-02, -1.429075e-02, 2.045465e-04, 1.241896e-02,
	1.979097e-02, 2.121866e-02, 1.712491e-02, 9.183787e-03,
	-2.045361e-04, -8.552441e-03, -1.388318e-02, -1.515481e-02,
	-1.242540e-02, -6.741124e-03, 2.045215e-04, 6.541650e-03,
	1.070174e-02, 1.178566e-02, 9.739672e-03, 5.309061e-03,
	-2.045027e-04,
}

// Reset clears all carrier, filter and timing state.
func (d *ARCSSD2400Demodulator) Reset() {
	*d = ARCSSD2400Demodulator{}
}

// Feed returns recovered MSB-first channel bits. The returned bits may be
// passed to FindDataUnitsInBits after enough samples have accumulated.
func (d *ARCSSD2400Demodulator) Feed(samples []float32, sampleRate int) ([]bool, error) {
	if sampleRate != 19200 {
		return nil, errors.New("ARCSSD CCSDS 2400 demodulator requires 19200 Hz input")
	}
	bits := make([]bool, 0, len(samples)/8+1)
	for _, sample := range samples {
		// The reference receiver mixes the signal down from 1800 Hz with
		// a 1024-entry quadrature oscillator.
		q := math.Sin(2*math.Pi*float64(d.phase)/1024) * float64(sample)
		i := math.Sin(2*math.Pi*float64((d.phase+256)&1023)/1024) * float64(sample)
		d.phase = (d.phase + 96) & 1023

		copy(d.filterI[:], d.filterI[1:])
		copy(d.filterQ[:], d.filterQ[1:])
		d.filterI[60], d.filterQ[60] = i, q
		var fi, fq float64
		for k, tap := range arcssd2400ReceiverLPF {
			fi += d.filterI[k] * tap
			fq += d.filterQ[k] * tap
		}
		demod := fi*d.delayQ[0] - fq*d.delayI[0]
		copy(d.delayI[:], d.delayI[1:])
		copy(d.delayQ[:], d.delayQ[1:])
		d.delayI[7], d.delayQ[7] = fi, fq

		d.sampledBits <<= 1
		if demod <= 0 {
			d.sampledBits |= 1
		}
		if ((d.sampledBits ^ (d.sampledBits >> 1)) & 1) != 0 {
			if d.phaseClock < 32 {
				d.phaseClock++
			} else {
				d.phaseClock--
			}
		}
		d.phaseClock += 8
		if d.phaseClock >= 64 {
			d.phaseClock %= 64
			v := d.sampledBits & 0x07
			bit := v == 0x07 || v == 0x06 || v == 0x05 || v == 0x03
			d.currentByte <<= 1
			if bit {
				d.currentByte |= 1
			}
			d.bitsInByte++
			if d.bitsInByte == 8 {
				for k := 7; k >= 0; k-- {
					bits = append(bits, d.currentByte&(1<<uint(k)) != 0)
				}
				d.currentByte = 0
				d.bitsInByte = 0
			}
		}
	}
	return bits, nil
}
