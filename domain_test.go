package holes

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestIntParse(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{in: "0", want: 0},
		{in: "42", want: 42},
		{in: "-7", want: -7},
		{in: "+7", want: 7},
		{in: "007", want: 7},
		{in: "9223372036854775807", want: 9223372036854775807},
		{in: "9223372036854775808", wantErr: true},
		{in: "1.5", wantErr: true},
		{in: "", wantErr: true},
		{in: "abc", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Int{}.Parse(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Int.Parse(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Int.Parse(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestIntFormat(t *testing.T) {
	tests := []struct {
		width int
		in    int64
		want  string
	}{
		{width: 0, in: 7, want: "7"},
		{width: 0, in: -7, want: "-7"},
		{width: -3, in: 7, want: "7"},
		{width: 1, in: 7, want: "7"},
		{width: 4, in: 7, want: "0007"},
		{width: 4, in: 240, want: "0240"},
		{width: 4, in: 0, want: "0000"},
		{width: 4, in: 1234, want: "1234"},
		{width: 4, in: 10000, want: "10000"},
		{width: 4, in: -5, want: "-005"},
		{width: 2, in: -5, want: "-5"},
		{width: 20, in: math.MinInt64, want: "-9223372036854775808"},
		{width: 21, in: math.MaxInt64, want: "009223372036854775807"},
	}
	for _, tt := range tests {
		if got := (Int{Width: tt.width}).Format(tt.in); got != tt.want {
			t.Errorf("Int{Width: %d}.Format(%d) = %q, want %q", tt.width, tt.in, got, tt.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		parse   func(string) (int64, error)
		in      string
		wantMsg string
		wantIs  error
	}{
		{name: "int syntax", parse: Int{}.Parse, in: "x", wantMsg: `parse integer "x": invalid syntax`, wantIs: strconv.ErrSyntax},
		{name: "int range", parse: Int{}.Parse, in: "9223372036854775808", wantMsg: `parse integer "9223372036854775808": value out of range`, wantIs: strconv.ErrRange},
		{name: "date", parse: Date{}.Parse, in: "22/09/2026", wantMsg: `parse date: parsing time "22/09/2026" as "2006-01-02": cannot parse "22/09/2026" as "2006"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.parse(tt.in)
			if err == nil || err.Error() != tt.wantMsg {
				t.Errorf("Parse(%q) error = %v, want %q", tt.in, err, tt.wantMsg)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("Parse(%q) error = %v, want it to wrap %v", tt.in, err, tt.wantIs)
			}
		})
	}
}

func TestDate(t *testing.T) {
	tests := []struct {
		name    string
		layout  string
		in      string
		want    int64
		wantOut string
		wantErr bool
	}{
		{name: "epoch", in: "1970-01-01", want: 0, wantOut: "1970-01-01"},
		{name: "default layout", in: "2026-09-22", want: 20718, wantOut: "2026-09-22"},
		{name: "before epoch", in: "1969-12-31", want: -1, wantOut: "1969-12-31"},
		{name: "leap day", in: "2024-02-29", want: 19782, wantOut: "2024-02-29"},
		{name: "compact layout", layout: "20060102", in: "20260922", want: 20718, wantOut: "20260922"},
		{name: "time of day ignored", layout: time.RFC3339, in: "2026-09-22T23:59:59Z", want: 20718, wantOut: "2026-09-22T00:00:00Z"},
		{name: "zone keeps written date", layout: time.RFC3339, in: "2026-09-22T23:00:00-05:00", want: 20718, wantOut: "2026-09-22T00:00:00Z"},
		{name: "invalid day", in: "2026-02-30", wantErr: true},
		{name: "wrong layout", in: "22/09/2026", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Date{Layout: tt.layout}
			got, err := d.Parse(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Date{%q}.Parse(%q) error = %v, wantErr %v", tt.layout, tt.in, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("Date{%q}.Parse(%q) = %d, want %d", tt.layout, tt.in, got, tt.want)
			}
			if out := d.Format(got); out != tt.wantOut {
				t.Errorf("Date{%q}.Format(%d) = %q, want %q", tt.layout, got, out, tt.wantOut)
			}
		})
	}
}

// validLayouts identify a single day; invalidLayouts do not.
var (
	validLayouts   = []string{"", "2006-01-02", "20060102", time.RFC3339, "Jan _2 2006", "2006-002", "02/01/2006", "06-01-02", "Mon 2006-01-02"}
	invalidLayouts = []string{"2006-01", "01-02", "2006", "Jan 2", "15:04", "200612"}
)

func TestDateValidate(t *testing.T) {
	for _, layout := range validLayouts {
		if err := (Date{Layout: layout}).Validate(); err != nil {
			t.Errorf("Date{%q}.Validate() = %v, want nil", layout, err)
		}
	}
	for _, layout := range invalidLayouts {
		err := Date{Layout: layout}.Validate()
		if !errors.Is(err, ErrLayout) {
			t.Errorf("Date{%q}.Validate() = %v, want ErrLayout", layout, err)
		}
	}
}

func FuzzIntRoundTrip(f *testing.F) {
	for _, s := range []string{"0", "-1", "+5", "007", "9223372036854775807", "-9223372036854775808", "x"} {
		for _, width := range []int{0, 4, 25} {
			f.Add(s, width)
		}
	}
	f.Fuzz(func(t *testing.T, s string, width int) {
		if width > 64 {
			t.Skip() // Wider padding only adds zeros; keep allocations small.
		}
		d := Int{Width: width}
		p, err := d.Parse(s)
		if err != nil {
			t.Skip()
		}
		out := d.Format(p)
		if len(out) < width {
			t.Errorf("Int{Width: %d}.Format(%d) = %q, shorter than the width", width, p, out)
		}
		if q, err := d.Parse(out); err != nil || q != p {
			t.Errorf("round trip of %q with width %d: got %d, %v; want %d", s, width, q, err, p)
		}
	})
}

func FuzzDateRoundTrip(f *testing.F) {
	for _, s := range []string{"1970-01-01", "0001-01-01", "9999-12-31", "2024-02-29", "nope"} {
		f.Add(s, "")
	}
	leapDay := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
	for _, layout := range slices.Concat(validLayouts, invalidLayouts) {
		f.Add(leapDay.Format(layout), layout)
	}
	f.Fuzz(func(t *testing.T, s, layout string) {
		d := Date{Layout: layout}
		if d.Validate() != nil {
			t.Skip()
		}
		p, err := d.Parse(s)
		if err != nil {
			t.Skip()
		}
		if q, err := d.Parse(d.Format(p)); err != nil || q != p {
			t.Errorf("round trip of %q with layout %q: got %d, %v; want %d", s, layout, q, err, p)
		}
	})
}
