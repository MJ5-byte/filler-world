package elo

import "math"

const K = 32

// Update returns the new ratings for a and b given a's actual score:
// 1 for a win, 0.5 for a draw, 0 for a loss.
func Update(ratingA, ratingB, scoreA float64) (float64, float64) {
	expectedA := 1 / (1 + math.Pow(10, (ratingB-ratingA)/400))
	delta := K * (scoreA - expectedA)
	return ratingA + delta, ratingB - delta
}
