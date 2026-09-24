package holes

import (
	"errors"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestFind(t *testing.T) {
	tests := []struct {
		name string
		in   []int64
		opts Options
		want []Gap
	}{
		{name: "empty", in: nil, want: nil},
		{name: "single", in: []int64{5}, want: nil},
		{name: "contiguous", in: []int64{1, 2, 3}, want: nil},
		{name: "one missing", in: []int64{1, 3}, want: []Gap{{First: 2, Last: 2, Step: 1, Count: 1}}},
		{name: "run missing", in: []int64{1, 5}, want: []Gap{{First: 2, Last: 4, Step: 1, Count: 3}}},
		{name: "unsorted with duplicates", in: []int64{9, 1, 3, 3, 1}, want: []Gap{{First: 2, Last: 2, Step: 1, Count: 1}, {First: 4, Last: 8, Step: 1, Count: 5}}},
		{name: "negative", in: []int64{-3, 1}, want: []Gap{{First: -2, Last: 0, Step: 1, Count: 3}}},
		{name: "zero step means one", in: []int64{1, 3}, opts: Options{Step: 0}, want: []Gap{{First: 2, Last: 2, Step: 1, Count: 1}}},
		{name: "step", in: []int64{0, 5, 20}, opts: Options{Step: 5}, want: []Gap{{First: 10, Last: 15, Step: 5, Count: 2}}},
		{name: "step re-anchors at off-grid value", in: []int64{0, 7, 20}, opts: Options{Step: 5}, want: []Gap{{First: 5, Last: 5, Step: 5, Count: 1}, {First: 12, Last: 17, Step: 5, Count: 2}}},
		{name: "step larger than gap", in: []int64{0, 3}, opts: Options{Step: 5}, want: nil},
		{name: "from before data", in: []int64{5, 6}, opts: Options{From: new(int64(2))}, want: []Gap{{First: 2, Last: 4, Step: 1, Count: 3}}},
		{name: "from equals first", in: []int64{2, 3}, opts: Options{From: new(int64(2))}, want: nil},
		{name: "from drops earlier input", in: []int64{0, 5, 7}, opts: Options{From: new(int64(4))}, want: []Gap{{First: 4, Last: 4, Step: 1, Count: 1}, {First: 6, Last: 6, Step: 1, Count: 1}}},
		{name: "to after data", in: []int64{1, 2}, opts: Options{To: new(int64(4))}, want: []Gap{{First: 3, Last: 4, Step: 1, Count: 2}}},
		{name: "to drops later input", in: []int64{1, 3, 9}, opts: Options{To: new(int64(3))}, want: []Gap{{First: 2, Last: 2, Step: 1, Count: 1}}},
		{name: "to with step not on grid", in: []int64{0}, opts: Options{Step: 5, To: new(int64(12))}, want: []Gap{{First: 5, Last: 10, Step: 5, Count: 2}}},
		{name: "no data with both bounds", in: nil, opts: Options{From: new(int64(1)), To: new(int64(3))}, want: []Gap{{First: 1, Last: 3, Step: 1, Count: 3}}},
		{name: "all input out of bounds", in: []int64{100}, opts: Options{From: new(int64(1)), To: new(int64(2))}, want: []Gap{{First: 1, Last: 2, Step: 1, Count: 2}}},
		{name: "no data with only from", in: nil, opts: Options{From: new(int64(1))}, want: []Gap{{First: 1, Last: 1, Step: 1, Count: 1}}},
		{name: "no data with only to", in: nil, opts: Options{Step: 7, To: new(int64(3))}, want: []Gap{{First: 3, Last: 3, Step: 7, Count: 1}}},
		{name: "all input before from", in: []int64{1, 2}, opts: Options{From: new(int64(20))}, want: []Gap{{First: 20, Last: 20, Step: 1, Count: 1}}},
		{name: "all input after to", in: []int64{10}, opts: Options{To: new(int64(4))}, want: []Gap{{First: 4, Last: 4, Step: 1, Count: 1}}},
		{name: "full int64 span", in: []int64{math.MinInt64, math.MaxInt64}, want: []Gap{{First: math.MinInt64 + 1, Last: math.MaxInt64 - 1, Step: 1, Count: math.MaxUint64 - 1}}},
		{
			name: "whole range missing saturates count",
			opts: Options{From: new(int64(math.MinInt64)), To: new(int64(math.MaxInt64))},
			want: []Gap{{First: math.MinInt64, Last: math.MaxInt64, Step: 1, Count: math.MaxUint64}},
		},
		{name: "huge step", in: []int64{math.MinInt64, math.MaxInt64}, opts: Options{Step: math.MaxInt64}, want: []Gap{{First: -1, Last: math.MaxInt64 - 1, Step: math.MaxInt64, Count: 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := slices.Clone(tt.in)
			got, err := Find(tt.in, tt.opts)
			if err != nil {
				t.Fatalf("Find(%v, %+v) error: %v", tt.in, tt.opts, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Find(%v, %+v) = %v, want %v", tt.in, tt.opts, got, tt.want)
			}
			if !slices.Equal(in, tt.in) {
				t.Errorf("Find modified its input: %v, was %v", tt.in, in)
			}
		})
	}
}

func TestGapValuesStopsEarly(t *testing.T) {
	g := Gap{First: 1, Last: 10, Step: 1, Count: 10}
	var got []int64
	for p := range g.Values() {
		got = append(got, p)
		if len(got) == 2 {
			break
		}
	}
	if want := []int64{1, 2}; !slices.Equal(got, want) {
		t.Errorf("Values() with break after 2 = %v, want %v", got, want)
	}
}

func TestFindErrors(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want error
	}{
		{name: "negative step", opts: Options{Step: -1}, want: ErrStep},
		{name: "from after to", opts: Options{From: new(int64(3)), To: new(int64(2))}, want: ErrBounds},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Find([]int64{1}, tt.opts); !errors.Is(err, tt.want) {
				t.Errorf("Find(%+v) error = %v, want %v", tt.opts, err, tt.want)
			}
		})
	}
}

func TestOptionsValidate(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want error
	}{
		{name: "zero value", opts: Options{}},
		{name: "step zero means one", opts: Options{Step: 0, From: new(int64(1)), To: new(int64(2))}},
		{name: "positive step", opts: Options{Step: 7}},
		{name: "from equals to", opts: Options{From: new(int64(3)), To: new(int64(3))}},
		{name: "only from", opts: Options{From: new(int64(3))}},
		{name: "only to", opts: Options{To: new(int64(-3))}},
		{name: "negative step", opts: Options{Step: -1}, want: ErrStep},
		{name: "from after to", opts: Options{From: new(int64(3)), To: new(int64(2))}, want: ErrBounds},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if tt.want == nil && err != nil {
				t.Errorf("%+v.Validate() = %v, want nil", tt.opts, err)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("%+v.Validate() = %v, want %v", tt.opts, err, tt.want)
			}
		})
	}
}

// FuzzFind checks Find against a brute-force reference on small values,
// shifted by offset so the overflow-safe arithmetic is exercised near the
// ends of the int64 range.
func FuzzFind(f *testing.F) {
	for _, offset := range []int64{0, math.MaxInt64 - 127, math.MinInt64 + 128} {
		f.Add([]byte{129, 133, 131}, int8(1), int8(0), int8(0), false, false, offset)
		f.Add([]byte{128, 135, 148}, int8(5), int8(-3), int8(30), true, true, offset)
		f.Add([]byte{}, int8(2), int8(-5), int8(5), true, true, offset)
		f.Add([]byte{0, 255}, int8(1), int8(-128), int8(127), true, true, offset)
	}
	f.Fuzz(func(t *testing.T, data []byte, step, from, to int8, hasFrom, hasTo bool, offset int64) {
		if step <= 0 || (hasFrom && hasTo && from > to) {
			t.Skip()
		}
		if offset < math.MinInt64+128 || offset > math.MaxInt64-127 {
			t.Skip()
		}
		in := make([]int64, len(data))
		shifted := make([]int64, len(data))
		for i, b := range data {
			in[i] = int64(b) - 128
			shifted[i] = in[i] + offset
		}
		opts := Options{Step: int64(step)}
		shiftedOpts := opts
		if hasFrom {
			opts.From = new(int64(from))
			shiftedOpts.From = new(int64(from) + offset)
		}
		if hasTo {
			opts.To = new(int64(to))
			shiftedOpts.To = new(int64(to) + offset)
		}
		got, err := Find(shifted, shiftedOpts)
		if err != nil {
			t.Fatalf("Find(%v, %+v) error: %v", shifted, shiftedOpts, err)
		}
		var flat []int64
		for _, g := range got {
			if g.Step != int64(step) {
				t.Fatalf("gap %+v has step %d, want %d", g, g.Step, step)
			}
			vals := slices.Collect(g.Values())
			if uint64(len(vals)) != g.Count || vals[0] != g.First || vals[len(vals)-1] != g.Last {
				t.Fatalf("gap %+v: Values() = %v", g, vals)
			}
			for _, v := range vals {
				flat = append(flat, v-offset)
			}
		}
		if want := reference(in, opts); !slices.Equal(flat, want) {
			t.Errorf("Find(%v, %+v) with offset %d: missing (unshifted) = %v, want %v", shifted, shiftedOpts, offset, flat, want)
		}
	})
}

// reference is the obvious O(range) definition of the missing positions.
func reference(in []int64, opts Options) []int64 {
	var ps []int64
	for _, p := range in {
		if (opts.From == nil || p >= *opts.From) && (opts.To == nil || p <= *opts.To) {
			ps = append(ps, p)
		}
	}
	slices.Sort(ps)
	ps = slices.Compact(ps)
	var out []int64
	if len(ps) == 0 {
		switch {
		case opts.From != nil && opts.To != nil:
			for p := *opts.From; p <= *opts.To; p += opts.Step {
				out = append(out, p)
			}
		case opts.From != nil:
			out = append(out, *opts.From)
		case opts.To != nil:
			out = append(out, *opts.To)
		}
		return out
	}
	if opts.From != nil {
		for p := *opts.From; p < ps[0]; p += opts.Step {
			out = append(out, p)
		}
	}
	for i := 1; i < len(ps); i++ {
		for p := ps[i-1] + opts.Step; p < ps[i]; p += opts.Step {
			out = append(out, p)
		}
	}
	if opts.To != nil {
		for p := ps[len(ps)-1] + opts.Step; p <= *opts.To; p += opts.Step {
			out = append(out, p)
		}
	}
	return out
}

// BenchmarkFind runs Find on a million positions with about one in ten
// missing, given in order and shuffled; sorting dominates the shuffled case.
func BenchmarkFind(b *testing.B) {
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // G404: a fixed seed keeps benchmark inputs identical across runs.
	var sorted []int64
	for p := range int64(1_000_000) {
		if r.IntN(10) != 0 {
			sorted = append(sorted, p)
		}
	}
	shuffled := slices.Clone(sorted)
	r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	for _, bb := range []struct {
		name string
		ps   []int64
	}{
		{name: "sorted", ps: sorted},
		{name: "shuffled", ps: shuffled},
	} {
		b.Run(bb.name, func(b *testing.B) {
			for b.Loop() {
				if _, err := Find(bb.ps, Options{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
