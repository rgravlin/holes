package holes

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Int converts base-10 integers to positions for [Find]; a value's
// position is itself.
type Int struct {
	// Width is the minimum length of formatted values, reached by padding
	// with leading zeros; as with printf's %0Nd, a minus sign counts toward
	// it. Zero or negative means no padding. Parse accepts padded values.
	Width int
}

// Parse returns the position of s.
func (Int) Parse(s string) (int64, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		// NumError repeats the input; keep only its cause.
		if ne, ok := errors.AsType[*strconv.NumError](err); ok {
			err = ne.Err
		}
		return 0, fmt.Errorf("parse integer %q: %w", s, err)
	}
	return n, nil
}

// Format returns the text of position p, zero-padded to Width.
func (d Int) Format(p int64) string {
	s := strconv.FormatInt(p, 10)
	if len(s) >= d.Width {
		return s
	}
	zeros := strings.Repeat("0", d.Width-len(s))
	if p < 0 {
		return "-" + zeros + s[1:]
	}
	return zeros + s
}

// DefaultDateLayout is the layout [Date] uses when none is set (ISO 8601).
const DefaultDateLayout = time.DateOnly

const secondsPerDay = 24 * 60 * 60

// ErrLayout is returned by [Date.Validate] when the layout does not identify
// a single day.
var ErrLayout = errors.New("date layout must identify a single day")

// Date converts calendar dates to positions for [Find]; a date's position
// is its number of days since 1970-01-01. Any time of day or zone in the input is
// ignored: the date is taken as written.
//
// Parse and Format assume a layout that passes [Date.Validate]; with any
// other layout their results are meaningless.
//
// The supported range is years 0000 to 9999, which is what [time.Parse]
// accepts. Format of a position outside it is undefined: it may return text
// Parse rejects, and beyond about 10^14 days the conversion overflows.
type Date struct {
	// Layout is a [time] layout such as "2006-01-02" or "20060102". It must
	// identify a single day: it needs the year, month and day, or the year
	// and day of year. Empty means [DefaultDateLayout].
	Layout string
}

// validationDays exercise a layout: distinct years, months and days, a year
// boundary and a leap day.
var validationDays = [...]time.Time{
	time.Date(2001, 2, 3, 0, 0, 0, 0, time.UTC),
	time.Date(2001, 12, 31, 0, 0, 0, 0, time.UTC),
	time.Date(2002, 1, 1, 0, 0, 0, 0, time.UTC),
	time.Date(2000, 2, 29, 0, 0, 0, 0, time.UTC),
}

// Validate reports whether the layout identifies a single day, by checking
// that sample days survive Format then Parse unchanged. The error wraps
// [ErrLayout] and shows a failing example.
func (d Date) Validate() error {
	for _, day := range validationDays {
		p := day.Unix() / secondsPerDay
		s := d.Format(p)
		q, err := d.Parse(s)
		if err != nil {
			return fmt.Errorf("%w: %q formats %s as %q, which does not parse back: %w", ErrLayout, d.layout(), day.Format(time.DateOnly), s, err)
		}
		if q != p {
			return fmt.Errorf("%w: %q formats %s as %q", ErrLayout, d.layout(), day.Format(time.DateOnly), s)
		}
	}
	return nil
}

func (d Date) layout() string {
	if d.Layout == "" {
		return DefaultDateLayout
	}
	return d.Layout
}

// Parse returns the position of the date s.
func (d Date) Parse(s string) (int64, error) {
	t, err := time.Parse(d.layout(), s)
	if err != nil {
		// time.ParseError already names the value and the layout.
		return 0, fmt.Errorf("parse date: %w", err)
	}
	y, m, day := t.Date()
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC).Unix() / secondsPerDay, nil
}

// Format returns the date at position p.
func (d Date) Format(p int64) string {
	return time.Unix(p*secondsPerDay, 0).UTC().Format(d.layout())
}
