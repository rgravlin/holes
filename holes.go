// Package holes finds the values missing from a sequence.
//
// Values are mapped to int64 positions on a number line, for example by
// [Int] or [Date]. [Find] then reports every run of
// expected positions that is absent from the input as a [Gap].
package holes

import (
	"errors"
	"iter"
	"slices"
)

// ErrStep is returned by [Options.Validate] and [Find] when the step is
// negative.
var ErrStep = errors.New("step must be positive")

// ErrBounds is returned by [Options.Validate] and [Find] when From is greater
// than To.
var ErrBounds = errors.New("from must not be greater than to")

// Gap is an inclusive run of missing positions: First, First+Step, ..., Last.
type Gap struct {
	First int64
	Last  int64
	// Step is the distance between consecutive positions in the run.
	Step int64
	// Count is the number of missing positions in the run. It saturates at
	// MaxUint64 for the one run that cannot be counted: the entire int64
	// range with step 1. For that run, [Gap.Values] yields Count positions
	// and so stops at MaxInt64-1, one short of Last.
	Count uint64
}

// Values returns the gap's Count positions, in ascending order: every
// position from First to Last, except when Count saturates (see Count).
func (g Gap) Values() iter.Seq[int64] {
	return func(yield func(int64) bool) {
		for k := range g.Count {
			if !yield(add(g.First, k*uint64(g.Step))) { //nolint:gosec // G115: Step is positive by construction in Find.
				return
			}
		}
	}
}

// Options control which positions [Find] expects to see.
type Options struct {
	// Step is the distance between consecutive expected positions.
	// Zero means 1.
	Step int64
	// From, when set, is the first expected position. Positions before it
	// are ignored, and it is reported missing if the input starts later or
	// has no positions within the bounds.
	From *int64
	// To, when set, is the last expected position. Positions after it are
	// ignored, and missing positions up to it are reported. With no positions
	// within the bounds, To is reported missing (with From also set, the
	// whole range is).
	To *int64
}

// Validate returns [ErrStep] if the step is negative and [ErrBounds] if From
// is greater than To. [Find] calls it; callers can use it to reject options
// before gathering positions.
func (o Options) Validate() error {
	if o.Step < 0 {
		return ErrStep
	}
	if o.From != nil && o.To != nil && *o.From > *o.To {
		return ErrBounds
	}
	return nil
}

// Find reports the gaps in positions, in ascending order.
//
// positions may be unsorted and contain duplicates; Find does not modify it.
// Between two present positions a < b, the expected positions are
// a+step, a+2*step, ... below b, so the step is re-anchored at every value
// that is present.
func Find(positions []int64, opts Options) ([]Gap, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	step := opts.Step
	if step == 0 {
		step = 1
	}
	return gaps(sortedIn(positions, opts), uint64(step), opts), nil
}

// sortedIn returns a sorted, deduplicated copy of ps limited to the bounds.
func sortedIn(ps []int64, opts Options) []int64 {
	out := slices.DeleteFunc(slices.Clone(ps), func(p int64) bool {
		return (opts.From != nil && p < *opts.From) || (opts.To != nil && p > *opts.To)
	})
	slices.Sort(out)
	return slices.Compact(out)
}

func gaps(ps []int64, step uint64, opts Options) []Gap {
	var out []Gap
	if len(ps) == 0 {
		// With no data, the bounds are the only positions known to be expected.
		switch {
		case opts.From != nil && opts.To != nil:
			n := dist(*opts.From, *opts.To) / step
			out = append(out, Gap{First: *opts.From, Last: add(*opts.From, n*step), Step: int64(step), Count: satInc(n)}) //nolint:gosec // G115: step came from a positive int64.
		case opts.From != nil:
			out = append(out, run(*opts.From, 1, step))
		case opts.To != nil:
			out = append(out, run(*opts.To, 1, step))
		}
		return out
	}
	if opts.From != nil && ps[0] > *opts.From {
		// From itself is expected, so count it along with the positions before ps[0].
		out = append(out, run(*opts.From, (dist(*opts.From, ps[0])-1)/step+1, step))
	}
	for i := 1; i < len(ps); i++ {
		if n := (dist(ps[i-1], ps[i]) - 1) / step; n > 0 {
			out = append(out, run(add(ps[i-1], step), n, step))
		}
	}
	last := ps[len(ps)-1]
	if opts.To != nil && last < *opts.To {
		if n := dist(last, *opts.To) / step; n > 0 {
			out = append(out, run(add(last, step), n, step))
		}
	}
	return out
}

// run builds the gap of n positions starting at first.
func run(first int64, n, step uint64) Gap {
	return Gap{First: first, Last: add(first, (n-1)*step), Step: int64(step), Count: n} //nolint:gosec // G115: step came from a positive int64.
}

// dist returns b-a for a <= b without overflowing: the two's-complement
// difference is exact once read as unsigned.
func dist(a, b int64) uint64 {
	return uint64(b) - uint64(a) //nolint:gosec // G115: wrapping conversion is the intended arithmetic.
}

// add returns a+d. Callers guarantee the result fits in an int64; the
// unsigned arithmetic wraps to the correct value when d exceeds MaxInt64.
func add(a int64, d uint64) int64 {
	return int64(uint64(a) + d) //nolint:gosec // G115: wrapping conversion is the intended arithmetic.
}

// satInc returns n+1, saturating at MaxUint64. Only the full int64 range
// with step 1 has 2^64 positions, which a uint64 cannot count.
func satInc(n uint64) uint64 { return max(n, n+1) }
