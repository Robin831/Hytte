package lactate

import (
	"math"
	"sort"
)

// ThresholdMethod identifies a lactate threshold calculation method.
type ThresholdMethod string

const (
	MethodOBLA    ThresholdMethod = "OBLA"
	MethodDmax    ThresholdMethod = "Dmax"
	MethodModDmax ThresholdMethod = "ModDmax"
	MethodLogLog  ThresholdMethod = "Log-log"
	MethodExpDmax ThresholdMethod = "ExpDmax"
)

// DefaultOBLAThreshold is the standard OBLA threshold at 4.0 mmol/L.
const DefaultOBLAThreshold = 4.0

// ThresholdResult represents the outcome of a single threshold calculation.
type ThresholdResult struct {
	Method       ThresholdMethod `json:"method"`
	SpeedKmh     float64         `json:"speed_kmh"`
	LactateMmol  float64         `json:"lactate_mmol"`
	HeartRateBpm int             `json:"heart_rate_bpm"`
	Valid        bool            `json:"valid"`
	Reason       string          `json:"reason,omitempty"`
}

// point is an internal representation of a data point sorted by speed.
type point struct {
	speed   float64
	lactate float64
	hr      float64
}

// CalculateThresholds computes all five threshold methods for the given stages.
func CalculateThresholds(stages []Stage) []ThresholdResult {
	pts := sortedPoints(stages)
	return []ThresholdResult{
		calcOBLA(pts, DefaultOBLAThreshold),
		calcDmax(pts),
		calcModDmax(pts),
		calcLogLog(pts),
		calcExpDmax(pts),
	}
}

// sortedPoints converts stages to points sorted by ascending speed.
func sortedPoints(stages []Stage) []point {
	pts := make([]point, len(stages))
	for i, s := range stages {
		pts[i] = point{speed: s.SpeedKmh, lactate: s.LactateMmol, hr: float64(s.HeartRateBpm)}
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].speed < pts[j].speed })
	return pts
}

// interpolateAt linearly interpolates a value (lactate or HR) at a given speed.
func interpolateAt(pts []point, speed float64, getValue func(point) float64) float64 {
	if len(pts) == 0 {
		return 0
	}
	if speed <= pts[0].speed {
		return getValue(pts[0])
	}
	if speed >= pts[len(pts)-1].speed {
		return getValue(pts[len(pts)-1])
	}
	for i := 1; i < len(pts); i++ {
		if speed <= pts[i].speed {
			t := (speed - pts[i-1].speed) / (pts[i].speed - pts[i-1].speed)
			return getValue(pts[i-1]) + t*(getValue(pts[i])-getValue(pts[i-1]))
		}
	}
	return getValue(pts[len(pts)-1])
}

func interpolateHR(pts []point, speed float64) int {
	hr := interpolateAt(pts, speed, func(p point) float64 { return p.hr })
	return int(math.Round(hr))
}

func interpolateLactate(pts []point, speed float64) float64 {
	return interpolateAt(pts, speed, func(p point) float64 { return p.lactate })
}

func invalid(method ThresholdMethod, reason string) ThresholdResult {
	return ThresholdResult{Method: method, Valid: false, Reason: reason}
}

// --- OBLA ---

// calcOBLA finds the speed at which lactate crosses a fixed threshold (default 4.0 mmol/L)
// by linear interpolation between consecutive stages.
func calcOBLA(pts []point, threshold float64) ThresholdResult {
	method := MethodOBLA
	if len(pts) < 2 {
		return invalid(method, "need at least 2 stages")
	}

	for i := 1; i < len(pts); i++ {
		if pts[i-1].lactate <= threshold && pts[i].lactate >= threshold {
			if pts[i].lactate == pts[i-1].lactate {
				speed := (pts[i-1].speed + pts[i].speed) / 2
				return ThresholdResult{
					Method: method, SpeedKmh: speed, LactateMmol: threshold,
					HeartRateBpm: interpolateHR(pts, speed), Valid: true,
				}
			}
			t := (threshold - pts[i-1].lactate) / (pts[i].lactate - pts[i-1].lactate)
			speed := pts[i-1].speed + t*(pts[i].speed-pts[i-1].speed)
			return ThresholdResult{
				Method: method, SpeedKmh: speed, LactateMmol: threshold,
				HeartRateBpm: interpolateHR(pts, speed), Valid: true,
			}
		}
	}

	if pts[len(pts)-1].lactate < threshold {
		return invalid(method, "lactate never reaches threshold")
	}
	return invalid(method, "lactate starts above threshold")
}

// --- Dmax ---

// calcDmax fits a 3rd-degree polynomial to the lactate curve and finds the point
// with maximum perpendicular distance from the line connecting the first and last points.
func calcDmax(pts []point) ThresholdResult {
	method := MethodDmax
	if len(pts) < 4 {
		return invalid(method, "need at least 4 stages")
	}

	x, y := extractXY(pts)
	coeffs := fitPolynomial(x, y, 3)
	if coeffs == nil {
		return invalid(method, "polynomial fit failed")
	}

	evalFn := func(v float64) float64 { return evalPoly(coeffs, v) }
	speed, lac := dmaxOnCurve(evalFn, x[0], y[0], x[len(x)-1], y[len(y)-1], x[0], x[len(x)-1])
	if math.IsNaN(speed) {
		return invalid(method, "Dmax calculation failed")
	}

	return ThresholdResult{
		Method: method, SpeedKmh: speed, LactateMmol: lac,
		HeartRateBpm: interpolateHR(pts, speed), Valid: true,
	}
}

// --- Modified Dmax ---

// calcModDmax is like Dmax but draws the reference line from the point where
// lactate first rises above the baseline by 0.5 mmol/L instead of from the first point.
func calcModDmax(pts []point) ThresholdResult {
	method := MethodModDmax
	if len(pts) < 4 {
		return invalid(method, "need at least 4 stages")
	}

	x, y := extractXY(pts)
	coeffs := fitPolynomial(x, y, 3)
	if coeffs == nil {
		return invalid(method, "polynomial fit failed")
	}

	// Find the baseline: minimum lactate value.
	minLac := y[0]
	for _, v := range y {
		if v < minLac {
			minLac = v
		}
	}

	// Find the first point where lactate exceeds baseline + 0.5 mmol/L.
	const delta = 0.5
	startIdx := -1
	for i, v := range y {
		if v >= minLac+delta {
			startIdx = i
			break
		}
	}
	if startIdx < 0 || startIdx >= len(x)-1 {
		return invalid(method, "lactate never rises 0.5 mmol/L above baseline")
	}

	evalFn := func(v float64) float64 { return evalPoly(coeffs, v) }
	speed, lac := dmaxOnCurve(evalFn, x[startIdx], y[startIdx], x[len(x)-1], y[len(y)-1], x[startIdx], x[len(x)-1])
	if math.IsNaN(speed) {
		return invalid(method, "ModDmax calculation failed")
	}

	return ThresholdResult{
		Method: method, SpeedKmh: speed, LactateMmol: lac,
		HeartRateBpm: interpolateHR(pts, speed), Valid: true,
	}
}

// --- Log-log ---

// calcLogLog finds the lactate threshold using the log-log transformation method.
// It fits two intersecting regression lines to log(speed) vs log(lactate) and
// finds the breakpoint that minimizes total residual error.
func calcLogLog(pts []point) ThresholdResult {
	method := MethodLogLog
	if len(pts) < 4 {
		return invalid(method, "need at least 4 stages")
	}

	// All values must be positive for log transform.
	for _, p := range pts {
		if p.speed <= 0 || p.lactate <= 0 {
			return invalid(method, "all speed and lactate values must be positive")
		}
	}

	logX := make([]float64, len(pts))
	logY := make([]float64, len(pts))
	for i, p := range pts {
		logX[i] = math.Log(p.speed)
		logY[i] = math.Log(p.lactate)
	}

	n := len(pts)
	bestSSR := math.MaxFloat64
	bestBreak := -1

	// Try every possible split, requiring at least 2 points on each side.
	for bp := 2; bp <= n-2; bp++ {
		ssr1 := linearRegressionSSR(logX[:bp], logY[:bp])
		ssr2 := linearRegressionSSR(logX[bp:], logY[bp:])
		totalSSR := ssr1 + ssr2
		if totalSSR < bestSSR {
			bestSSR = totalSSR
			bestBreak = bp
		}
	}

	if bestBreak < 0 {
		return invalid(method, "could not find breakpoint")
	}

	// Find intersection of the two regression lines.
	a1, b1 := linearRegression(logX[:bestBreak], logY[:bestBreak])
	a2, b2 := linearRegression(logX[bestBreak:], logY[bestBreak:])

	if math.Abs(b1-b2) < 1e-12 {
		return invalid(method, "regression lines are parallel")
	}

	logSpeed := (a2 - a1) / (b1 - b2)
	speed := math.Exp(logSpeed)
	lac := interpolateLactate(pts, speed)

	// Sanity: result should be within the data range.
	if speed < pts[0].speed || speed > pts[len(pts)-1].speed {
		return invalid(method, "threshold outside data range")
	}

	return ThresholdResult{
		Method: method, SpeedKmh: speed, LactateMmol: lac,
		HeartRateBpm: interpolateHR(pts, speed), Valid: true,
	}
}

// --- ExpDmax ---

// calcExpDmax fits an exponential curve y = a·e^(b·x) + c to the lactate data
// and applies the Dmax method on this curve.
func calcExpDmax(pts []point) ThresholdResult {
	method := MethodExpDmax
	if len(pts) < 3 {
		return invalid(method, "need at least 3 stages")
	}

	x, y := extractXY(pts)
	a, b, c := fitExponential(x, y)
	if math.IsNaN(a) || math.IsNaN(b) || math.IsNaN(c) {
		return invalid(method, "exponential fit failed")
	}

	evalFn := func(v float64) float64 { return a*math.Exp(b*v) + c }
	speed, lac := dmaxOnCurve(evalFn, x[0], y[0], x[len(x)-1], y[len(y)-1], x[0], x[len(x)-1])
	if math.IsNaN(speed) {
		return invalid(method, "ExpDmax calculation failed")
	}

	return ThresholdResult{
		Method: method, SpeedKmh: speed, LactateMmol: lac,
		HeartRateBpm: interpolateHR(pts, speed), Valid: true,
	}
}

// --- Dmax helper ---

// dmaxOnCurve finds the point on the curve (defined by evalFn) between xMin and xMax
// that has maximum perpendicular distance from the line between (x0,y0) and (x1,y1).
// Returns the (speed, lactate) at maximum distance.
func dmaxOnCurve(evalFn func(float64) float64, x0, y0, x1, y1, xMin, xMax float64) (float64, float64) {
	// Line: from (x0,y0) to (x1,y1). Distance = |a*x + b*y + c| / sqrt(a^2 + b^2)
	// where a = y1-y0, b = x0-x1, c = x1*y0 - x0*y1
	lineA := y1 - y0
	lineB := x0 - x1
	lineC := x1*y0 - x0*y1
	lineDenom := math.Sqrt(lineA*lineA + lineB*lineB)
	if lineDenom < 1e-15 {
		return math.NaN(), math.NaN()
	}

	const steps = 1000
	dx := (xMax - xMin) / float64(steps)
	maxDist := -1.0
	bestX := math.NaN()

	for i := 0; i <= steps; i++ {
		xi := xMin + float64(i)*dx
		yi := evalFn(xi)
		dist := (lineA*xi + lineB*yi + lineC) / lineDenom
		// For lactate curves that bow upward, the curve is above the line,
		// so we want the maximum positive distance.
		if dist > maxDist {
			maxDist = dist
			bestX = xi
		}
	}

	if math.IsNaN(bestX) || maxDist <= 0 {
		return math.NaN(), math.NaN()
	}

	return bestX, evalFn(bestX)
}

// --- Math helpers ---

func extractXY(pts []point) ([]float64, []float64) {
	x := make([]float64, len(pts))
	y := make([]float64, len(pts))
	for i, p := range pts {
		x[i] = p.speed
		y[i] = p.lactate
	}
	return x, y
}

// evalPoly evaluates polynomial coeffs[0] + coeffs[1]*x + coeffs[2]*x^2 + ...
func evalPoly(coeffs []float64, x float64) float64 {
	result := 0.0
	xPow := 1.0
	for _, c := range coeffs {
		result += c * xPow
		xPow *= x
	}
	return result
}

// fitPolynomial fits a polynomial of the given degree to (x, y) data using least squares.
// Returns coefficients [c0, c1, c2, ...] where y = c0 + c1*x + c2*x^2 + ...
func fitPolynomial(x, y []float64, degree int) []float64 {
	n := len(x)
	m := degree + 1
	if n < m {
		return nil
	}

	// Build normal equations: (X^T X) * c = X^T y
	// X is the Vandermonde matrix.
	xtx := make([][]float64, m)
	xty := make([]float64, m)
	for i := range m {
		xtx[i] = make([]float64, m)
	}

	for k := range n {
		xi := 1.0
		for i := range m {
			xty[i] += xi * y[k]
			xj := 1.0
			for j := range m {
				xtx[i][j] += xi * xj
				xj *= x[k]
			}
			xi *= x[k]
		}
	}

	return solveLinearSystem(xtx, xty)
}

// solveLinearSystem solves Ax = b using Gaussian elimination with partial pivoting.
// Returns nil if the system is singular.
func solveLinearSystem(a [][]float64, b []float64) []float64 {
	n := len(b)
	// Create augmented matrix.
	aug := make([][]float64, n)
	for i := range n {
		aug[i] = make([]float64, n+1)
		copy(aug[i], a[i])
		aug[i][n] = b[i]
	}

	for col := range n {
		// Partial pivoting.
		maxRow := col
		maxVal := math.Abs(aug[col][col])
		for row := col + 1; row < n; row++ {
			if math.Abs(aug[row][col]) > maxVal {
				maxVal = math.Abs(aug[row][col])
				maxRow = row
			}
		}
		if maxVal < 1e-12 {
			return nil
		}
		aug[col], aug[maxRow] = aug[maxRow], aug[col]

		// Eliminate below.
		for row := col + 1; row < n; row++ {
			factor := aug[row][col] / aug[col][col]
			for j := col; j <= n; j++ {
				aug[row][j] -= factor * aug[col][j]
			}
		}
	}

	// Back substitution.
	x := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		x[i] = aug[i][n]
		for j := i + 1; j < n; j++ {
			x[i] -= aug[i][j] * x[j]
		}
		x[i] /= aug[i][i]
	}
	return x
}

// linearRegression fits y = intercept + slope*x and returns (intercept, slope).
func linearRegression(x, y []float64) (float64, float64) {
	n := float64(len(x))
	if n < 2 {
		return 0, 0
	}
	sumX, sumY, sumXX, sumXY := 0.0, 0.0, 0.0, 0.0
	for i := range x {
		sumX += x[i]
		sumY += y[i]
		sumXX += x[i] * x[i]
		sumXY += x[i] * y[i]
	}
	denom := n*sumXX - sumX*sumX
	if math.Abs(denom) < 1e-12 {
		return sumY / n, 0
	}
	slope := (n*sumXY - sumX*sumY) / denom
	intercept := (sumY - slope*sumX) / n
	return intercept, slope
}

// linearRegressionSSR returns the sum of squared residuals for a linear fit.
func linearRegressionSSR(x, y []float64) float64 {
	intercept, slope := linearRegression(x, y)
	ssr := 0.0
	for i := range x {
		r := y[i] - (intercept + slope*x[i])
		ssr += r * r
	}
	return ssr
}

// fitExponential fits y = a·exp(b·x) + c using grid search over c
// with linearized least squares for a and b.
func fitExponential(x, y []float64) (float64, float64, float64) {
	n := len(x)
	if n < 3 {
		return math.NaN(), math.NaN(), math.NaN()
	}

	minY := y[0]
	for _, v := range y {
		if v < minY {
			minY = v
		}
	}

	bestSSR := math.MaxFloat64
	var bestA, bestB, bestC float64

	// Search c from 0 to just below minY.
	const steps = 200
	for i := 0; i <= steps; i++ {
		tryC := minY * float64(i) / float64(steps+1)
		valid := true
		logY := make([]float64, n)
		for j := range n {
			diff := y[j] - tryC
			if diff <= 1e-12 {
				valid = false
				break
			}
			logY[j] = math.Log(diff)
		}
		if !valid {
			continue
		}

		intercept, slope := linearRegression(x, logY)
		tryA := math.Exp(intercept)
		tryB := slope

		ssr := 0.0
		for j := range n {
			pred := tryA*math.Exp(tryB*x[j]) + tryC
			r := y[j] - pred
			ssr += r * r
		}
		if ssr < bestSSR {
			bestSSR = ssr
			bestA, bestB, bestC = tryA, tryB, tryC
		}
	}

	if bestSSR == math.MaxFloat64 {
		return math.NaN(), math.NaN(), math.NaN()
	}
	return bestA, bestB, bestC
}
