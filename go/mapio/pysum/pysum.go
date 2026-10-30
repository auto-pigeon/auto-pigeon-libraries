// Package pysum reproduces CPython's builtin sum() over floats.
//
// It exists as its own layer because several unrelated packages need it, and Go
// forbids the import cycle that sharing it through any of them would create.
//
// Python's builtin sum() is not a naive accumulator. Since CPython 3.12 its
// float fast path carries a Neumaier (Kahan-Babuška) compensation term and adds
// it back at the end, so sum() is materially more accurate than `total +=
// value` in a loop. Go's obvious translation of `sum(values)` is the naive loop,
// and over a few thousand terms the two disagree in the last places.
//
// That is not academic. Summing 1591 per-face interior areas for e1m3 gives
//
//	Python sum()   12187650.647769483
//	naive loop     12187650.647769505
//
// from bit-identical inputs, and the value lands close enough to the
// six-decimal rounding boundary that the number persisted with compensation
// differs from the one a naive loop persists.
//
// A persisted number defined with compensated summation uses Sum. A site defined
// as an explicit `total += value` loop stays a naive loop — changing either kind
// moves persisted values at rounding boundaries.
package pysum

import "math"

// Sum adds values the way CPython's builtin sum() does.
//
// The algorithm is CPython's, transcribed: keep a running total and a
// compensation term holding the low-order bits each addition dropped, choosing
// the subtraction order by magnitude so the term is exact, and add the
// compensation back once at the end.
func Sum(values []float64) float64 {
	var accumulator Accumulator
	for _, value := range values {
		accumulator.Add(value)
	}
	return accumulator.Total()
}

// Accumulator is Sum in streaming form: the same compensated addition, without
// materialising the slice.
//
// It exists because some sites sum generator-style sequences as well as lists —
// `sum(abs(values[…] - values[…]) for row in range(height))` in texture
// analysis, over as many terms as a texture has pixels. Building a slice per
// column merely to hand it to Sum would allocate megabytes to reproduce a
// number Python never stored either.
//
// The zero value is an empty sum. Feeding it the same values in the same order
// as Sum's slice gives bit-identical results, because it is the same algorithm
// — Sum is now written in terms of it, so the two cannot drift apart.
type Accumulator struct {
	total        float64
	compensation float64
}

// Add folds one term into the running sum.
func (a *Accumulator) Add(value float64) {
	sum := a.total + value
	if math.Abs(a.total) >= math.Abs(value) {
		a.compensation += (a.total - sum) + value
	} else {
		a.compensation += (value - sum) + a.total
	}
	a.total = sum
}

// Total is the compensated sum of everything added so far.
//
// CPython returns the running total alone when it is not finite: the
// compensation of an infinity is a NaN, and adding it would turn a genuine
// overflow into a NaN Python never produces.
func (a *Accumulator) Total() float64 {
	if math.IsInf(a.total, 0) || math.IsNaN(a.total) {
		return a.total
	}
	return a.total + a.compensation
}
