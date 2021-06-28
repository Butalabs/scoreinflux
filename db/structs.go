package main

type CombinValue struct {
	// Players part of a combination we store the value for
	Players []string
	// Cumulated time played together for this combination
	Minutes int
	// Cumulated score for when this combination played together
	CumulatedScore float64
	// Averaged score per 90 minutes for this combination
	Score float64
	// Number of combinations a player as been part of
	CombinCount int // Only if len(Players) = 1
	// Actual Shapley Value of a player
	// It is actually only an estimation as not all the combinations are counted
	ShapleyValue float64 // Only if len(Players) = 1
}
