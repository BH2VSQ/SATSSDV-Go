// SPDX-License-Identifier: GPL-3.0-or-later
//
// This 2400-bit/s CCSDS transmit modem is a Go translation of the
// BT=0.35 pulse-shaping and phase-accumulator path in the ARCSSD source
// supplied with SATSSDV-Go. Original source copyright 2015 Wei Mingchuan
// and HIT Research Center of Satellite Technology. See THIRD_PARTY_NOTICES.md.
package ccsds

import "math"

var arcssdSineTable = func() [1024]float64 {
	var table [1024]float64
	for i := range table {
		table[i] = math.Sin(2 * math.Pi * float64(i) / 1024)
	}
	return table
}()

// modulateARCSSD2400 reproduces the supplied ARCSSD CCSDS AFSK2400
// phase accumulator and its 25-tap BT=0.35 Gaussian pulse-shaping FIR.
func modulateARCSSD2400(data []byte) []float32 {
	var taps = [...]int32{2, 6, 19, 52, 132, 299, 607, 1105, 1804, 2643, 3471, 4087, 4316, 4087, 3471, 2643, 1804, 1105, 607, 299, 132, 52, 19, 6, 2}
	var delay [25]int32
	out := make([]float32, len(data)*8*8)
	phase := 0
	pos := 0
	for _, b := range data {
		for bit := 7; bit >= 0; bit-- {
			// Keep the ARCSSD wire polarity: a one uses DIVISION_INC
			// (-31 phase steps), while a zero uses its inverse (+31).
			// The previous Go port used +/-16 and inverted the symbol
			// mapping, producing a waveform that only matched its own RX.
			phaseDeviation := int32(31)
			if b&(1<<uint(bit)) != 0 {
				phaseDeviation = -31
			}
			for sample := 0; sample < 8; sample++ {
				copy(delay[:], delay[1:])
				delay[len(delay)-1] = phaseDeviation
				var accumulator int32
				for i := range taps {
					accumulator += delay[i] * taps[i]
				}
				shaped := (accumulator + 16384) >> 15
				phase = (phase + int(shaped) + 96) & 1023 // 1800 Hz centre frequency
				out[pos] = float32(0.9155273 * arcssdSineTable[phase])
				pos++
			}
		}
	}
	return out
}

// modulateARCSSD1200 mirrors the reference CCSDS 1200-baud phase accumulator.
// At 19.2 kHz the reference quantizes the center increment to 91 and the
// half-deviation to 26 table steps, then emits 16 samples per data bit.
func modulateARCSSD1200(data []byte) []float32 {
	out := make([]float32, len(data)*8*16)
	phase := 0
	pos := 0
	for _, value := range data {
		for bit := 7; bit >= 0; bit-- {
			deviation := 26
			if value&(1<<uint(bit)) != 0 {
				deviation = -26
			}
			increment := 91 + deviation
			for sample := 0; sample < 16; sample++ {
				phase = (phase + increment) & 1023
				out[pos] = float32(0.9155273 * arcssdSineTable[phase])
				pos++
			}
		}
	}
	return out
}
