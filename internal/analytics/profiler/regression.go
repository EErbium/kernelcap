package profiler

import "math"

func OLSSlope(x, y []float64) (slope, intercept, rSquared float64) {
	n := len(x)
	if n < 2 || len(y) < n {
		return 0, 0, 0
	}

	var sumX, sumY, sumXY, sumX2, sumY2 float64

	for i := 0; i < n; i++ {
		sumX += x[i]
		sumY += y[i]
		sumXY += x[i] * y[i]
		sumX2 += x[i] * x[i]
		sumY2 += y[i] * y[i]
	}

	denom := float64(n)*sumX2 - sumX*sumX
	if math.Abs(denom) < 1e-15 {
		return 0, 0, 0
	}

	slope = (float64(n)*sumXY - sumX*sumY) / denom
	intercept = (sumY - slope*sumX) / float64(n)

	num := float64(n)*sumXY - sumX*sumY
	denomR := (float64(n)*sumX2 - sumX*sumX) * (float64(n)*sumY2 - sumY*sumY)
	if denomR <= 0 {
		rSquared = 0
	} else {
		r := num / math.Sqrt(denomR)
		rSquared = r * r
	}

	return slope, intercept, rSquared
}
