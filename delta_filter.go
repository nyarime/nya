package nya

import "math"

// DeltaFilter applies delta encoding/decoding in-place.
func DeltaFilter(data []byte, distance int, encode bool) {
	if distance <= 0 {
		distance = 1
	}
	if len(data) <= distance {
		return
	}
	if encode {
		for i := len(data) - 1; i >= distance; i-- {
			data[i] -= data[i-distance]
		}
	} else {
		for i := distance; i < len(data); i++ {
			data[i] += data[i-distance]
		}
	}
}

// DetectDeltaDistance analyzes data and returns optimal delta distance.
// Returns 0 if delta encoding won't help.
func DetectDeltaDistance(data []byte) int {
	if len(data) < 64 {
		return 0
	}
	bestDist := 0
	bestScore := entropy(data)
	for _, d := range []int{1, 2, 4} {
		test := make([]byte, len(data))
		copy(test, data)
		DeltaFilter(test, d, true)
		score := entropy(test)
		if score < bestScore-0.5 {
			bestScore = score
			bestDist = d
		}
	}
	return bestDist
}

func entropy(data []byte) float64 {
	var freq [256]int
	for _, b := range data {
		freq[b]++
	}
	n := float64(len(data))
	var h float64
	for _, f := range freq {
		if f > 0 {
			p := float64(f) / n
			h -= p * math.Log2(p)
		}
	}
	return h
}
