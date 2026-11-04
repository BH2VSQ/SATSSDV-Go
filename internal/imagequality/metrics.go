package imagequality

import (
	"image"
	"image/color"
	"math"
)

// PSNR computes RGB peak signal-to-noise ratio in decibels. A perfect match
// returns +Inf. Images must have identical bounds and non-empty dimensions.
func PSNR(reference, candidate image.Image) float64 {
	if !sameImageGeometry(reference, candidate) {
		return math.NaN()
	}
	bounds := reference.Bounds()
	var squaredError float64
	count := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			a := color.NRGBAModel.Convert(reference.At(x, y)).(color.NRGBA)
			b := color.NRGBAModel.Convert(candidate.At(x, y)).(color.NRGBA)
			for _, pair := range [][2]uint8{{a.R, b.R}, {a.G, b.G}, {a.B, b.B}} {
				d := float64(pair[0]) - float64(pair[1])
				squaredError += d * d
				count++
			}
		}
	}
	mse := squaredError / float64(count)
	if mse == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/mse)
}

// SSIM computes the mean 8x8-window structural similarity of image luminance.
// The constants follow the commonly used K1=0.01 and K2=0.03 definition.
func SSIM(reference, candidate image.Image) float64 {
	if !sameImageGeometry(reference, candidate) {
		return math.NaN()
	}
	bounds := reference.Bounds()
	const window = 8
	const c1 = (0.01 * 255) * (0.01 * 255)
	const c2 = (0.03 * 255) * (0.03 * 255)
	var total float64
	windows := 0
	for top := bounds.Min.Y; top < bounds.Max.Y; top += window {
		for left := bounds.Min.X; left < bounds.Max.X; left += window {
			w := minInt(window, bounds.Max.X-left)
			h := minInt(window, bounds.Max.Y-top)
			n := float64(w * h)
			var sumX, sumY float64
			var valuesX, valuesY [window * window]float64
			idx := 0
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					valuesX[idx] = luminance(reference.At(left+x, top+y))
					valuesY[idx] = luminance(candidate.At(left+x, top+y))
					sumX += valuesX[idx]
					sumY += valuesY[idx]
					idx++
				}
			}
			muX, muY := sumX/n, sumY/n
			var varianceX, varianceY, covariance float64
			for i := 0; i < idx; i++ {
				dx, dy := valuesX[i]-muX, valuesY[i]-muY
				varianceX += dx * dx
				varianceY += dy * dy
				covariance += dx * dy
			}
			denom := math.Max(1, n-1)
			varianceX /= denom
			varianceY /= denom
			covariance /= denom
			den := (muX*muX + muY*muY + c1) * (varianceX + varianceY + c2)
			if den > 0 {
				total += ((2*muX*muY + c1) * (2*covariance + c2)) / den
				windows++
			}
		}
	}
	if windows == 0 {
		return math.NaN()
	}
	return total / float64(windows)
}

func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 257
}

func sameImageGeometry(a, b image.Image) bool {
	return a != nil && b != nil && a.Bounds() == b.Bounds() && a.Bounds().Dx() > 0 && a.Bounds().Dy() > 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
