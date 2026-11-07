package gifmode

import (
	"image"
	"sort"

	"github.com/BH2VSQ/SATSSDV-Go/internal/image/ssdv"
)

const (
	maxMotionSearchMCUs = 6
	maxMotionVectors    = 32
	maxMotionSAD        = 8
)

type motionCell struct {
	dx, dy int8
	valid  bool
}

type motionRunKey struct {
	x, width int
	dx, dy   int8
}

// PredictMotion estimates integer-MCU translations from the previous composed
// frame to the current one. It returns a prediction canvas and a compact list
// of non-overlapping destination rectangles. Only high-confidence block
// matches are used; unmatched regions remain for normal SSDV residual coding.
func PredictMotion(previous, current *image.RGBA) (*image.RGBA, []ssdv.GIFMotionVector, int) {
	if previous == nil || current == nil || previous.Bounds().Dx() != current.Bounds().Dx() || previous.Bounds().Dy() != current.Bounds().Dy() {
		return cloneRGBA(current), nil, 0
	}
	bounds := current.Bounds()
	if bounds.Dx()%16 != 0 || bounds.Dy()%16 != 0 {
		return cloneRGBA(previous), nil, 0
	}
	widthMCUs, heightMCUs := bounds.Dx()/16, bounds.Dy()/16
	cells := make([]motionCell, widthMCUs*heightMCUs)
	for my := 0; my < heightMCUs; my++ {
		for mx := 0; mx < widthMCUs; mx++ {
			currentError := sampledBlockSAD(previous, current, mx, my, mx, my)
			// A block that has not changed at its own coordinate is not a motion
			// candidate. This is also the main defense against matching repeated
			// flat backgrounds to arbitrary nearby locations.
			if currentError <= maxMotionSAD {
				continue
			}
			bestError := currentError
			bestDX, bestDY := 0, 0
			for dy := -maxMotionSearchMCUs; dy <= maxMotionSearchMCUs; dy++ {
				for dx := -maxMotionSearchMCUs; dx <= maxMotionSearchMCUs; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					sx, sy := mx-dx, my-dy
					if sx < 0 || sy < 0 || sx >= widthMCUs || sy >= heightMCUs {
						continue
					}
					err := sampledBlockSAD(previous, current, sx, sy, mx, my)
					if err < bestError {
						bestError, bestDX, bestDY = err, dx, dy
					}
				}
			}
			// Require a near-exact source block and a decisive improvement over
			// the same-coordinate comparison. Quantized GIF colors are usually
			// exact; the tolerance permits tiny palette/resampling differences.
			if bestError <= maxMotionSAD && bestError*3 < currentError {
				cells[my*widthMCUs+mx] = motionCell{dx: int8(bestDX), dy: int8(bestDY), valid: true}
			}
		}
	}

	vectors := motionRuns(cells, widthMCUs, heightMCUs)
	if len(vectors) > maxMotionVectors {
		sort.Slice(vectors, func(i, j int) bool {
			a := int(vectors[i].Width) * int(vectors[i].Height)
			b := int(vectors[j].Width) * int(vectors[j].Height)
			if a != b {
				return a > b
			}
			if vectors[i].SourceY != vectors[j].SourceY {
				return vectors[i].SourceY < vectors[j].SourceY
			}
			return vectors[i].SourceX < vectors[j].SourceX
		})
		vectors = vectors[:maxMotionVectors]
	}
	predicted := cloneRGBA(previous)
	if len(vectors) == 0 {
		return predicted, nil, 0
	}
	snapshot := cloneRGBA(previous)
	predictedMCUs := 0
	for _, vector := range vectors {
		copyMotionRect(predicted, snapshot, vector)
		predictedMCUs += int(vector.Width) * int(vector.Height)
	}
	return predicted, vectors, predictedMCUs
}

// ApplyMotionVectors reconstructs the predicted canvas from a previously
// committed frame. Every vector copies from an immutable source snapshot so
// overlapping source/destination rectangles cannot cascade within one frame.
// Invalid vectors are ignored here; packet parsing rejects them before RX.
func ApplyMotionVectors(previous *image.RGBA, vectors []ssdv.GIFMotionVector) *image.RGBA {
	if previous == nil {
		return nil
	}
	predicted := cloneRGBA(previous)
	if len(vectors) == 0 {
		return predicted
	}
	snapshot := cloneRGBA(previous)
	widthMCUs, heightMCUs := previous.Bounds().Dx()/16, previous.Bounds().Dy()/16
	for _, vector := range vectors {
		if vector.Width == 0 || vector.Height == 0 || !motionVectorFits(vector, widthMCUs, heightMCUs) {
			continue
		}
		copyMotionRect(predicted, snapshot, vector)
	}
	return predicted
}

func motionVectorFits(v ssdv.GIFMotionVector, widthMCUs, heightMCUs int) bool {
	sx, sy := int(v.SourceX), int(v.SourceY)
	dx, dy := sx+int(v.DeltaX), sy+int(v.DeltaY)
	return sx+int(v.Width) <= widthMCUs && sy+int(v.Height) <= heightMCUs &&
		dx >= 0 && dy >= 0 && dx+int(v.Width) <= widthMCUs && dy+int(v.Height) <= heightMCUs &&
		(v.DeltaX != 0 || v.DeltaY != 0)
}

// sampledBlockSAD compares a 16x16 destination MCU against a source MCU in the
// previous frame using a 4x4 pixel sampling grid and RGB absolute difference.
func sampledBlockSAD(previous, current *image.RGBA, sourceX, sourceY, destX, destY int) int {
	sad := 0
	for sy := 2; sy < 16; sy += 4 {
		for sx := 2; sx < 16; sx += 4 {
			pr, pg, pb, _ := previous.At(previous.Rect.Min.X+sourceX*16+sx, previous.Rect.Min.Y+sourceY*16+sy).RGBA()
			cr, cg, cb, _ := current.At(current.Rect.Min.X+destX*16+sx, current.Rect.Min.Y+destY*16+sy).RGBA()
			sad += absInt(int(pr>>8) - int(cr>>8))
			sad += absInt(int(pg>>8) - int(cg>>8))
			sad += absInt(int(pb>>8) - int(cb>>8))
		}
	}
	return sad / (16 * 3)
}

func motionRuns(cells []motionCell, width, height int) []ssdv.GIFMotionVector {
	var vectors []ssdv.GIFMotionVector
	active := make(map[motionRunKey]int)
	for y := 0; y < height; y++ {
		current := make(map[motionRunKey]int)
		for x := 0; x < width; {
			cell := cells[y*width+x]
			if !cell.valid {
				x++
				continue
			}
			start := x
			for x < width {
				candidate := cells[y*width+x]
				if !candidate.valid || candidate.dx != cell.dx || candidate.dy != cell.dy {
					break
				}
				x++
			}
			sourceX := start - int(cell.dx)
			sourceY := y - int(cell.dy)
			key := motionRunKey{x: sourceX, width: x - start, dx: cell.dx, dy: cell.dy}
			if index, ok := active[key]; ok && int(vectors[index].SourceY)+int(vectors[index].Height) == sourceY {
				vectors[index].Height++
				current[key] = index
			} else {
				vectors = append(vectors, ssdv.GIFMotionVector{SourceX: uint8(sourceX), SourceY: uint8(sourceY), Width: uint8(x - start), Height: 1, DeltaX: cell.dx, DeltaY: cell.dy})
				current[key] = len(vectors) - 1
			}
		}
		active = current
	}
	return vectors
}

func copyMotionRect(destination, source *image.RGBA, vector ssdv.GIFMotionVector) {
	sx := source.Rect.Min.X + int(vector.SourceX)*16
	sy := source.Rect.Min.Y + int(vector.SourceY)*16
	dx := destination.Rect.Min.X + (int(vector.SourceX)+int(vector.DeltaX))*16
	dy := destination.Rect.Min.Y + (int(vector.SourceY)+int(vector.DeltaY))*16
	width, height := int(vector.Width)*16, int(vector.Height)*16
	for row := 0; row < height; row++ {
		srcOffset := (sy+row-source.Rect.Min.Y)*source.Stride + (sx-source.Rect.Min.X)*4
		dstOffset := (dy+row-destination.Rect.Min.Y)*destination.Stride + (dx-destination.Rect.Min.X)*4
		copy(destination.Pix[dstOffset:dstOffset+width*4], source.Pix[srcOffset:srcOffset+width*4])
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
