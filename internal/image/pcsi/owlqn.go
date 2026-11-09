package pcsi

import (
	"context"
	"errors"
	"math"
)

const (
	pcsiOWLQNLambda = 5.0
	owlqnHistory    = 8
)

type dct2DPlan struct {
	width, height int
	row, column   *dctPlan
	work          []float64
}

func newDCT2DPlan(width, height int) *dct2DPlan {
	return &dct2DPlan{
		width: width, height: height,
		row: newDCTPlan(width), column: newDCTPlan(height),
		work: make([]float64, width*height),
	}
}

func (p *dct2DPlan) transform(src, dst []float64, inverse bool) error {
	if p == nil || len(src) != p.width*p.height || len(dst) != len(src) {
		return errors.New("DCT plan dimensions do not match input")
	}
	copy(p.work, src)
	for y := 0; y < p.height; y++ {
		start := y * p.width
		p.row.transform(p.work[start:start+p.width], p.row.lineOut, inverse)
		copy(p.work[start:start+p.width], p.row.lineOut)
	}
	for x := 0; x < p.width; x++ {
		for y := 0; y < p.height; y++ {
			p.column.lineIn[y] = p.work[y*p.width+x]
		}
		p.column.transform(p.column.lineIn, p.column.lineOut, inverse)
		for y := 0; y < p.height; y++ {
			p.work[y*p.width+x] = p.column.lineOut[y]
		}
	}
	copy(dst, p.work)
	return nil
}

type owlqnHistoryPair struct {
	s, y []float64
	rho  float64
}

type pcsiObjective struct {
	width, height     int
	plan              *dct2DPlan
	positions         []int
	observations      []float64
	spatial           []float64
	residual          []float64
	gradient          []float64
	coefficientMatrix []float64
	matrixGradient    []float64
}

func newPCSIObjective(width, height int, positions []int, observations []uint8) (*pcsiObjective, error) {
	if width <= 0 || height <= 0 || len(positions) == 0 || len(positions) != len(observations) {
		return nil, errors.New("invalid PCSI channel measurements")
	}
	n := width * height
	if n/width != height {
		return nil, errors.New("PCSI image dimensions overflow")
	}
	obj := &pcsiObjective{
		width: width, height: height, plan: newDCT2DPlan(width, height),
		positions: append([]int(nil), positions...), observations: make([]float64, len(observations)),
		spatial: make([]float64, n), residual: make([]float64, n), gradient: make([]float64, n),
		coefficientMatrix: make([]float64, n), matrixGradient: make([]float64, n),
	}
	for i, position := range positions {
		if position < 0 || position >= n {
			return nil, errors.New("PCSI measurement index is outside image dimensions")
		}
		obj.observations[i] = float64(observations[i])
	}
	return obj, nil
}

func (o *pcsiObjective) evaluate(coefficients []float64, gradient bool) (float64, []float64, error) {
	if len(coefficients) != len(o.spatial) {
		return 0, nil, errors.New("PCSI coefficient vector has an invalid length")
	}
	// The reference PCSI solver flattens its coefficient vector as
	// reshape((width, height)).T. That is column-major with respect to the
	// 2-D DCT matrix, while the Go DCT plan consumes row-major matrices.
	// Convert explicitly in both directions; treating this vector as row-major
	// produces a valid-looking but spatially scrambled/noisy reconstruction.
	for x := 0; x < o.width; x++ {
		for y := 0; y < o.height; y++ {
			o.coefficientMatrix[y*o.width+x] = coefficients[x*o.height+y]
		}
	}
	if err := o.plan.transform(o.coefficientMatrix, o.spatial, true); err != nil {
		return 0, nil, err
	}
	clear(o.residual)
	var loss float64
	for i, position := range o.positions {
		delta := o.spatial[position] - o.observations[i]
		loss += delta * delta
		if gradient {
			o.residual[position] = 2 * delta
		}
	}
	if !gradient {
		return loss, nil, nil
	}
	if err := o.plan.transform(o.residual, o.matrixGradient, false); err != nil {
		return 0, nil, err
	}
	for x := 0; x < o.width; x++ {
		for y := 0; y < o.height; y++ {
			o.gradient[x*o.height+y] = o.matrixGradient[y*o.width+x]
		}
	}
	return loss, o.gradient, nil
}

// reconstructChannel matches the reference PCSI objective:
//
//	|| sampled(IDCT(coefficients)) - measurements ||^2 + 5*||coefficients||_1
//
// The minimizer is found with orthant-wise limited-memory BFGS. The input
// coefficients are reused between progressive preview updates.
func reconstructChannel(width, height int, columnIndices []int, observations []uint8, initial []float64, iterations int) ([]float64, []float64, error) {
	return reconstructChannelContext(context.Background(), width, height, columnIndices, observations, initial, iterations)
}

type owlqnIterationState struct {
	coefficients []float64
	candidate    []float64
	loss         float64
	gradient     []float64
	pseudo       []float64
	value        float64
	history      []owlqnHistoryPair
}

func reconstructChannelContext(ctx context.Context, width, height int, columnIndices []int, observations []uint8, initial []float64, iterations int) ([]float64, []float64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if iterations <= 0 {
		iterations = 24
	}
	if iterations > 500 {
		return nil, nil, errors.New("PCSI reconstruction iteration limit is 500")
	}
	positions := make([]int, len(columnIndices))
	for i, columnIndex := range columnIndices {
		if columnIndex < 0 || columnIndex >= width*height {
			return nil, nil, errors.New("PCSI measurement index is outside image dimensions")
		}
		x, y := columnIndex/height, columnIndex%height
		positions[i] = y*width + x
	}
	objective, err := newPCSIObjective(width, height, positions, observations)
	if err != nil {
		return nil, nil, err
	}
	state, err := newOWLQNIterationState(objective, width, height, observations, initial)
	if err != nil {
		return nil, nil, err
	}
	for iteration := 0; iteration < iterations; iteration++ {
		continued, stepErr := runOWLQNIteration(ctx, objective, state)
		if stepErr != nil {
			return nil, nil, stepErr
		}
		if !continued {
			break
		}
	}
	spatial := make([]float64, width*height)
	for column := 0; column < width; column++ {
		for row := 0; row < height; row++ {
			objective.coefficientMatrix[row*width+column] = state.coefficients[column*height+row]
		}
	}
	if err := objective.plan.transform(objective.coefficientMatrix, spatial, true); err != nil {
		return nil, nil, err
	}
	return spatial, state.coefficients, nil
}

func newOWLQNIterationState(objective *pcsiObjective, width, height int, observations []uint8, initial []float64) (*owlqnIterationState, error) {
	n := width * height
	coefficients := make([]float64, n)
	if len(initial) == n {
		copy(coefficients, initial)
	} else {
		// Seed the orthonormal DCT DC coefficient from the mean of the
		// received measurements. Starting every progressive preview at an
		// all-zero coefficient vector produces a strongly tinted/black image
		// until many expensive OWL-QN iterations have completed. The DC seed
		// gives missing regions a neutral channel average immediately while
		// leaving all measured pixels to the normal sparse reconstruction.
		var sum float64
		for _, sample := range observations {
			sum += float64(sample)
		}
		mean := sum / float64(len(observations))
		coefficients[0] = mean * math.Sqrt(float64(n))
	}
	loss, gradient, err := objective.evaluate(coefficients, true)
	if err != nil {
		return nil, err
	}
	return &owlqnIterationState{
		coefficients: coefficients,
		candidate:    make([]float64, len(coefficients)),
		loss:         loss,
		gradient:     gradient,
		pseudo:       owlqnPseudoGradient(coefficients, gradient, pcsiOWLQNLambda),
		value:        loss + pcsiOWLQNLambda*vectorL1Norm(coefficients),
		history:      make([]owlqnHistoryPair, 0, owlqnHistory),
	}, nil
}

func runOWLQNIteration(ctx context.Context, objective *pcsiObjective, state *owlqnIterationState) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if vectorInfNorm(state.pseudo) < 1e-4 {
		return false, nil
	}
	direction, orthant, slope := owlqnSearchDirection(state)
	if slope >= -1e-14 {
		return false, nil
	}
	candidate, nextLoss, nextGradient, nextPseudo, accepted, err := owlqnLineSearch(ctx, objective, state, direction, orthant, slope)
	if err != nil {
		return false, err
	}
	if !accepted {
		return false, nil
	}
	updateOWLQNHistory(state, candidate, nextPseudo)
	state.coefficients, state.candidate = candidate, state.coefficients
	state.loss = nextLoss
	state.gradient = nextGradient
	state.pseudo = nextPseudo
	state.value = nextLoss + pcsiOWLQNLambda*vectorL1Norm(candidate)
	return true, nil
}

func owlqnSearchDirection(state *owlqnIterationState) ([]float64, []float64, float64) {
	direction := owlqnDirection(state.pseudo, state.history)
	orthant := make([]float64, len(state.coefficients))
	for i := range state.coefficients {
		switch {
		case state.coefficients[i] > 0:
			orthant[i] = 1
		case state.coefficients[i] < 0:
			orthant[i] = -1
		case state.pseudo[i] < 0:
			orthant[i] = 1
		case state.pseudo[i] > 0:
			orthant[i] = -1
		}
		// Only coefficients currently at zero are constrained to enter
		// the selected orthant. A non-zero coefficient must be allowed to
		// move back toward zero; masking that direction freezes OWL-QN
		// immediately after an overshooting line-search step.
		if state.coefficients[i] == 0 && direction[i]*orthant[i] <= 0 {
			direction[i] = 0
		}
	}
	slope := vectorDot(state.pseudo, direction)
	if slope >= -1e-14 {
		for i := range direction {
			direction[i] = -state.pseudo[i]
			if state.coefficients[i] == 0 && direction[i]*orthant[i] <= 0 {
				direction[i] = 0
			}
		}
		slope = vectorDot(state.pseudo, direction)
	}
	return direction, orthant, slope
}

func owlqnLineSearch(ctx context.Context, objective *pcsiObjective, state *owlqnIterationState, direction, orthant []float64, slope float64) ([]float64, float64, []float64, []float64, bool, error) {
	candidate := state.candidate
	step := 1.0
	for search := 0; search < 28; search++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, nil, nil, false, err
		}
		for i := range state.coefficients {
			candidate[i] = state.coefficients[i] + step*direction[i]
			if candidate[i]*orthant[i] <= 0 {
				candidate[i] = 0
			}
		}
		nextLoss, nextGradient, err := objective.evaluate(candidate, true)
		if err != nil {
			return nil, 0, nil, nil, false, err
		}
		nextValue := nextLoss + pcsiOWLQNLambda*vectorL1Norm(candidate)
		if nextValue <= state.value+1e-4*step*slope {
			nextPseudo := owlqnPseudoGradient(candidate, nextGradient, pcsiOWLQNLambda)
			return candidate, nextLoss, nextGradient, nextPseudo, true, nil
		}
		step *= 0.5
	}
	return nil, 0, nil, nil, false, nil
}

func updateOWLQNHistory(state *owlqnIterationState, candidate, nextPseudo []float64) {
	n := len(state.coefficients)
	sVec := make([]float64, n)
	yVec := make([]float64, n)
	for i := range state.coefficients {
		sVec[i] = candidate[i] - state.coefficients[i]
		yVec[i] = nextPseudo[i] - state.pseudo[i]
	}
	curvature := vectorDot(sVec, yVec)
	if curvature <= 1e-12*math.Sqrt(vectorDot(sVec, sVec)*vectorDot(yVec, yVec)) {
		return
	}
	if len(state.history) == owlqnHistory {
		copy(state.history, state.history[1:])
		state.history = state.history[:len(state.history)-1]
	}
	state.history = append(state.history, owlqnHistoryPair{s: sVec, y: yVec, rho: 1 / curvature})
}

func owlqnPseudoGradient(x, gradient []float64, lambda float64) []float64 {
	out := make([]float64, len(x))
	for i, value := range x {
		g := gradient[i]
		switch {
		case value < 0:
			out[i] = g - lambda
		case value > 0:
			out[i] = g + lambda
		case g < -lambda:
			out[i] = g + lambda
		case g > lambda:
			out[i] = g - lambda
		default:
			out[i] = 0
		}
	}
	return out
}

func owlqnDirection(gradient []float64, history []owlqnHistoryPair) []float64 {
	q := append([]float64(nil), gradient...)
	alpha := make([]float64, len(history))
	for i := len(history) - 1; i >= 0; i-- {
		alpha[i] = history[i].rho * vectorDot(history[i].s, q)
		vectorAddScaled(q, history[i].y, -alpha[i])
	}
	gamma := 1.0
	if len(history) > 0 {
		last := history[len(history)-1]
		yy := vectorDot(last.y, last.y)
		if yy > 0 {
			gamma = vectorDot(last.s, last.y) / yy
		}
	}
	for i := range q {
		q[i] *= gamma
	}
	for i := range history {
		beta := history[i].rho * vectorDot(history[i].y, q)
		vectorAddScaled(q, history[i].s, alpha[i]-beta)
	}
	for i := range q {
		q[i] = -q[i]
	}
	return q
}

func vectorAddScaled(dst, src []float64, scale float64) {
	for i := range dst {
		dst[i] += scale * src[i]
	}
}

func vectorDot(a, b []float64) float64 {
	var sum float64
	for i, v := range a {
		sum += v * b[i]
	}
	return sum
}

func vectorL1Norm(values []float64) float64 {
	var sum float64
	for _, v := range values {
		sum += math.Abs(v)
	}
	return sum
}

func vectorInfNorm(values []float64) float64 {
	var largest float64
	for _, v := range values {
		if value := math.Abs(v); value > largest {
			largest = value
		}
	}
	return largest
}
