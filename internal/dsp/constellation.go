package dsp

// ConstellationSample is one recovered symbol-space sample. Real and Imag
// describe the detector's actual two-dimensional decision variables; they are
// not FFT-bin coordinates.
type ConstellationSample struct {
	Real float64 `json:"real"`
	Imag float64 `json:"imag"`
}
