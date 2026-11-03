// Package fec implements the shortened RS(255,223) code used by the ARCSSD
// image packet layer. The code uses GF(256) with primitive polynomial 0x187,
// 32 roots beginning at alpha^(112*11), and primitive step 11.
//
// The implementation is a clean-room encoder/decoder based on the public
// Reed-Solomon parameters documented by the upstream ARCSSD RS8 source.
package fec

import "errors"

const (
	RS8CodeSymbols   = 255
	RS8DataSymbols   = 223
	RS8ParitySymbols = 32
	RS8FirstRoot     = 112
	RS8Primitive     = 11
)

var alphaTo [255]byte
var indexOf [256]int16
var generatorLog = [33]byte{0x00, 0xF9, 0x3B, 0x42, 0x04, 0x2B, 0x7E, 0xFB, 0x61, 0x1E, 0x03, 0xD5, 0x32, 0x42, 0xAA, 0x05, 0x18, 0x05, 0xAA, 0x42, 0x32, 0xD5, 0x03, 0x1E, 0x61, 0xFB, 0x7E, 0x2B, 0x04, 0x42, 0x3B, 0xF9, 0x00}

func init() {
	for i := range indexOf {
		indexOf[i] = -1
	}
	v := uint16(1)
	for i := 0; i < 255; i++ {
		alphaTo[i] = byte(v)
		indexOf[byte(v)] = int16(i)
		v <<= 1
		if v&0x100 != 0 {
			v ^= 0x187
		}
	}
}

func mod255(x int) int {
	x %= 255
	if x < 0 {
		x += 255
	}
	return x
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return alphaTo[mod255(int(indexOf[a])+int(indexOf[b]))]
}

func gfDiv(a, b byte) byte {
	if a == 0 {
		return 0
	}
	if b == 0 {
		panic("fec: division by zero in GF(256)")
	}
	return alphaTo[mod255(int(indexOf[a])-int(indexOf[b]))]
}

func gfPow(a byte, n int) byte {
	if n == 0 {
		return 1
	}
	if a == 0 {
		return 0
	}
	return alphaTo[mod255(int(indexOf[a])*n)]
}

// RS8Parity returns the 32 parity symbols for exactly 223 data symbols.
func RS8Parity(data []byte) ([]byte, error) {
	if len(data) != RS8DataSymbols {
		return nil, errors.New("RS8 encoder requires exactly 223 data symbols")
	}
	parity := make([]byte, RS8ParitySymbols)
	for i := 0; i < RS8DataSymbols; i++ {
		feedback := indexOf[data[i]^parity[0]]
		if feedback >= 0 {
			for j := 1; j < RS8ParitySymbols; j++ {
				parity[j] ^= alphaTo[mod255(int(feedback)+int(generatorLog[RS8ParitySymbols-j]))]
			}
		}
		copy(parity, parity[1:])
		if feedback >= 0 {
			parity[RS8ParitySymbols-1] = alphaTo[mod255(int(feedback)+int(generatorLog[0]))]
		} else {
			parity[RS8ParitySymbols-1] = 0
		}
	}
	return parity, nil
}

// Syndromes computes the 32 syndromes for a 255-symbol codeword.
func Syndromes(codeword []byte) ([]byte, error) {
	if len(codeword) != RS8CodeSymbols {
		return nil, errors.New("RS8 codeword must contain exactly 255 symbols")
	}
	s := make([]byte, RS8ParitySymbols)
	for i := range s {
		root := alphaTo[mod255((RS8FirstRoot+i)*RS8Primitive)]
		var value byte
		for _, symbol := range codeword {
			value = gfMul(value, root) ^ symbol
		}
		s[i] = value
	}
	return s, nil
}

// ValidateRS8 reports whether codeword is a valid 255-symbol RS codeword.
func ValidateRS8(codeword []byte) bool {
	s, err := Syndromes(codeword)
	if err != nil {
		return false
	}
	for _, v := range s {
		if v != 0 {
			return false
		}
	}
	return true
}

// CorrectRS8 corrects up to 16 symbol errors in place. It returns the number
// of corrected symbols. The input is unchanged when the error count is beyond
// the code's correction capability or the received word is inconsistent.
func CorrectRS8(codeword []byte) (int, error) {
	if len(codeword) != RS8CodeSymbols {
		return 0, errors.New("RS8 codeword must contain exactly 255 symbols")
	}
	syndromes, _ := Syndromes(codeword)
	allZero := true
	for _, s := range syndromes {
		if s != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return 0, nil
	}

	locator, degree := berlekampMassey(syndromes)
	if degree == 0 || degree > RS8ParitySymbols/2 {
		return 0, errors.New("RS8 error count exceeds correction capability")
	}

	positions := make([]int, 0, degree)
	locations := make([]byte, 0, degree)
	for pos := 0; pos < RS8CodeSymbols; pos++ {
		// A symbol at position pos contributes X^(112+i) to syndrome i,
		// where X = alpha^(11*(254-pos)). The locator roots are X^-1.
		x := alphaTo[mod255(RS8Primitive*(RS8CodeSymbols-1-pos))]
		z := gfDiv(1, x)
		var value byte
		power := byte(1)
		for i := 0; i <= degree; i++ {
			value ^= gfMul(locator[i], power)
			power = gfMul(power, z)
		}
		if value == 0 {
			positions = append(positions, pos)
			locations = append(locations, x)
		}
	}
	if len(positions) != degree {
		return 0, errors.New("RS8 locator roots do not match the estimated error count")
	}

	magnitudes, err := solveErrorMagnitudes(syndromes, locations)
	if err != nil {
		return 0, err
	}
	candidate := append([]byte(nil), codeword...)
	for i, pos := range positions {
		candidate[pos] ^= magnitudes[i]
	}
	if !ValidateRS8(candidate) {
		return 0, errors.New("RS8 correction did not produce a valid codeword")
	}
	copy(codeword, candidate)
	return len(positions), nil
}

func berlekampMassey(s []byte) ([]byte, int) {
	c := make([]byte, RS8ParitySymbols+1)
	b := make([]byte, RS8ParitySymbols+1)
	c[0], b[0] = 1, 1
	degree, shift := 0, 1
	lastDiscrepancy := byte(1)

	for n := 0; n < len(s); n++ {
		d := s[n]
		for i := 1; i <= degree; i++ {
			d ^= gfMul(c[i], s[n-i])
		}
		if d == 0 {
			shift++
			continue
		}
		t := append([]byte(nil), c...)
		coef := gfDiv(d, lastDiscrepancy)
		for i := 0; i+shift < len(c); i++ {
			if b[i] != 0 {
				c[i+shift] ^= gfMul(coef, b[i])
			}
		}
		if 2*degree <= n {
			degree = n + 1 - degree
			b = t
			lastDiscrepancy = d
			shift = 1
		} else {
			shift++
		}
	}
	return c, degree
}

func solveErrorMagnitudes(s []byte, locations []byte) ([]byte, error) {
	n := len(locations)
	matrix := make([][]byte, n)
	for row := 0; row < n; row++ {
		matrix[row] = make([]byte, n+1)
		matrix[row][n] = s[row]
		for col, x := range locations {
			matrix[row][col] = gfPow(x, RS8FirstRoot+row)
		}
	}

	for col := 0; col < n; col++ {
		pivot := col
		for pivot < n && matrix[pivot][col] == 0 {
			pivot++
		}
		if pivot == n {
			return nil, errors.New("RS8 error magnitude matrix is singular")
		}
		matrix[col], matrix[pivot] = matrix[pivot], matrix[col]
		invPivot := gfDiv(1, matrix[col][col])
		for j := col; j <= n; j++ {
			matrix[col][j] = gfMul(matrix[col][j], invPivot)
		}
		for row := 0; row < n; row++ {
			if row == col || matrix[row][col] == 0 {
				continue
			}
			factor := matrix[row][col]
			for j := col; j <= n; j++ {
				matrix[row][j] ^= gfMul(factor, matrix[col][j])
			}
		}
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = matrix[i][n]
	}
	return out, nil
}
