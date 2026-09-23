// Command holes prints the values missing from a sequence read from files or
// standard input.
//
// Exit status is 0 if nothing is missing, 1 if something is, and 2 on error.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"iter"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/rgravlin/holes"
)

// Exit statuses, following grep: found is "something matched".
const (
	exitNone  = 0
	exitFound = 1
	exitError = 2
)

// maxWidth caps -width: an int64 is at most 20 characters with its sign, so
// any wider padding only adds zeros.
const maxWidth = 20

// maxLine caps a single input line; longer lines are an error, not a hang.
const maxLine = 1 << 20

const usage = `Usage: holes [flags] [file ...]

Print the values missing from a sequence, one per line. Reads standard
input when no file (or "-") is given. Input need not be sorted.

Exit status: 0 nothing missing, 1 something missing, 2 error.
`

// examples come between the usage text and the flag list in the -h output;
// keep them in sync with the README.
const examples = `
Examples:
  # Did every nightly backup land? Alert if not.
  ls /backups | holes -q -t date -e '\d{4}-\d{2}-\d{2}' -to "$(date +%F)" \
    || echo 'backup missing' >&2

  # Which days have no log lines (service down, cron skipped)?
  holes -t date -e '^\d{4}-\d{2}-\d{2}' app.log

  # Which numbers are missing from a list on one line?
  echo 'batches 1,2,3,5,8,9' | holes -e '\d+' -r

  # How many order IDs are missing (deleted rows, rolled-back inserts)?
  psql -Atc 'select id from orders' shop | holes -c

  # Weekly reports named 20260901.pdf, 20260908.pdf, ...
  ls reports | holes -t date -layout 20060102 -e '(\d{8})\.pdf' -step 7

  # Missing render frames, padded like the file names (frame_0001.exr)
  ls render | holes -e 'frame_(\d+)\.exr' -from 1 -to 240 -width 4
`

type config struct {
	// output, when set, is printed instead of processing input (-h, -version).
	output string

	in     reader
	opts   holes.Options
	ranges bool
	count  bool
	quiet  bool
	files  []string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, err := parseArgs(args)
	if err != nil {
		return fail(stderr, err)
	}
	if cfg.output != "" {
		if _, err = io.WriteString(stdout, cfg.output); err != nil {
			return fail(stderr, fmt.Errorf("write output: %w", err))
		}
		return exitNone
	}

	positions, err := cfg.in.readAll(cfg.files, stdin)
	if err != nil {
		return fail(stderr, err)
	}
	gaps, err := holes.Find(positions, cfg.opts)
	if err != nil {
		return fail(stderr, err)
	}

	if !cfg.quiet {
		w := bufio.NewWriter(stdout)
		err = write(w, gaps, cfg)
		if err == nil {
			err = w.Flush()
		}
		if err != nil {
			return fail(stderr, fmt.Errorf("write output: %w", err))
		}
	}
	if len(gaps) > 0 {
		return exitFound
	}
	return exitNone
}

// fail reports err on stderr and returns the error exit status.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "holes: %v\n", err) //nolint:errcheck // stderr is the last resort; a failure there has nowhere to go.
	return exitError
}

// domain converts between text values and the positions holes.Find works on.
// holes.Int and holes.Date implement it.
type domain interface {
	// Parse returns the position of s.
	Parse(s string) (int64, error)
	// Format returns the text of position p.
	Format(p int64) string
}

// flags holds the command-line flags as given, before validation.
type flags struct {
	typ, layout, from, to, extract string
	step                           int64
	width                          int
	ranges, count, quiet, version  bool
}

func parseArgs(args []string) (config, error) {
	fs := flag.NewFlagSet("holes", flag.ContinueOnError)
	// run reports errors once; help goes to stdout via config.output.
	fs.SetOutput(io.Discard)

	var f flags
	fs.StringVar(&f.typ, "type", "int", "value type: int or date")
	fs.StringVar(&f.typ, "t", "int", "shorthand for -type")
	fs.StringVar(&f.layout, "layout", "", "date layout in Go time format (default "+holes.DefaultDateLayout+"); requires -type date")
	fs.IntVar(&f.width, "width", 0, "pad integers with leading zeros to at least this many characters, e.g. 4 for 0001; requires -type int")
	fs.Int64Var(&f.step, "step", 1, "distance between consecutive expected values (days for dates)")
	fs.StringVar(&f.from, "from", "", "first expected value; earlier input is ignored")
	fs.StringVar(&f.to, "to", "", "last expected value; later input is ignored")
	fs.StringVar(&f.extract, "extract", "", "regexp selecting every value in each line (each match's first group if any; anchor with ^ for one per line); lines without a match are skipped")
	fs.StringVar(&f.extract, "e", "", "shorthand for -extract")
	fs.BoolVar(&f.ranges, "ranges", false, "print each gap as first..last instead of every value")
	fs.BoolVar(&f.ranges, "r", false, "shorthand for -ranges")
	fs.BoolVar(&f.count, "count", false, "print only the number of missing values")
	fs.BoolVar(&f.count, "c", false, "shorthand for -count")
	fs.BoolVar(&f.quiet, "quiet", false, "print nothing, even with -count or -ranges; report through the exit status only")
	fs.BoolVar(&f.quiet, "q", false, "shorthand for -quiet")
	fs.BoolVar(&f.version, "version", false, "print the version and exit")

	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		var b strings.Builder
		b.WriteString(usage)
		b.WriteString(examples)
		b.WriteString("\nFlags:\n")
		fs.SetOutput(&b)
		fs.PrintDefaults()
		return config{output: b.String()}, nil
	}
	if err != nil {
		return config{}, fmt.Errorf("%w\nrun 'holes -h' for usage", err)
	}
	if f.version {
		return config{output: version(debug.ReadBuildInfo()) + "\n"}, nil
	}
	return f.config(fs.Args())
}

// config validates the flags and builds the configuration for reading files.
func (f flags) config(files []string) (config, error) {
	d, hint, err := f.domain()
	if err != nil {
		return config{}, err
	}
	if f.step <= 0 {
		return config{}, fmt.Errorf("-step %d: must be positive", f.step)
	}
	if f.ranges && f.count {
		return config{}, errors.New("-ranges and -count are mutually exclusive")
	}
	opts := holes.Options{Step: f.step}
	if opts.From, err = bound(d, "-from", f.from); err != nil {
		return config{}, err
	}
	if opts.To, err = bound(d, "-to", f.to); err != nil {
		return config{}, err
	}
	if err = opts.Validate(); err != nil {
		return config{}, fmt.Errorf("-from %s, -to %s: %w", f.from, f.to, err)
	}
	var extract *regexp.Regexp
	if f.extract != "" {
		if extract, err = regexp.Compile(f.extract); err != nil {
			return config{}, fmt.Errorf("-extract: %w", err)
		}
	}
	return config{
		in:     reader{domain: d, extract: extract, hint: hint},
		opts:   opts,
		ranges: f.ranges,
		count:  f.count,
		quiet:  f.quiet,
		files:  files,
	}, nil
}

// domain returns the domain selected by -type and -layout, and the hint to
// append to its parse errors.
func (f flags) domain() (domain, string, error) {
	switch f.typ {
	case "int":
		if f.layout != "" {
			return nil, "", errors.New("-layout requires -type date")
		}
		if f.width < 0 || f.width > maxWidth {
			return nil, "", fmt.Errorf("-width %d: must be between 0 and %d", f.width, maxWidth)
		}
		d := holes.Int{Width: f.width}
		if f.extract == "" {
			return d, " (use -e to extract the numbers from each line)", nil
		}
		return d, "", nil
	case "date":
		if f.width != 0 {
			return nil, "", errors.New("-width requires -type int")
		}
		d := holes.Date{Layout: f.layout}
		if err := d.Validate(); err != nil {
			return nil, "", fmt.Errorf("-layout: %w", err)
		}
		return d, " (check that -layout matches the input)", nil
	default:
		return nil, "", fmt.Errorf("-type %q: want int or date", f.typ)
	}
}

// bound parses an optional -from/-to value; empty means unset.
func bound(d domain, name, s string) (*int64, error) {
	if s == "" {
		return nil, nil
	}
	p, err := d.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &p, nil
}

// reader turns input lines into positions.
type reader struct {
	domain  domain
	extract *regexp.Regexp
	// hint, if set, is appended to value parse errors to suggest a fix.
	hint string
}

// values yields the values in a line: the whole line, or with -extract every
// match (its first group, if the regexp has one).
func (r reader) values(line string) iter.Seq[string] {
	return func(yield func(string) bool) {
		if r.extract == nil {
			yield(line)
			return
		}
		for _, m := range r.extract.FindAllStringSubmatch(line, -1) {
			v := m[0]
			if len(m) > 1 {
				v = m[1]
			}
			if !yield(v) {
				return
			}
		}
	}
}

func (r reader) readAll(files []string, stdin io.Reader) ([]int64, error) {
	if len(files) == 0 {
		files = []string{"-"}
	}
	var ps []int64
	for _, name := range files {
		var err error
		if name == "-" {
			ps, err = r.read(ps, stdin, "stdin")
		} else {
			ps, err = r.readFile(ps, name)
		}
		if err != nil {
			return nil, err
		}
	}
	return ps, nil
}

func (r reader) readFile(ps []int64, name string) ([]int64, error) {
	f, err := os.Open(name) //nolint:gosec // G304: reading files named on the command line is the point.
	if err != nil {
		return nil, err // *os.PathError names the operation and file, here and from Close.
	}
	ps, err = r.read(ps, f, name)
	return ps, errors.Join(err, f.Close())
}

// read appends the position of every value in src to ps. A UTF-8
// byte-order mark at the start of src is ignored.
func (r reader) read(ps []int64, src io.Reader, name string) ([]int64, error) {
	sc := bufio.NewScanner(src)
	sc.Buffer(nil, maxLine)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if line == 1 {
			// Editors on Windows often start UTF-8 files with a byte-order mark.
			text = strings.TrimPrefix(text, "\ufeff")
		}
		for v := range r.values(strings.TrimSpace(text)) {
			if v == "" {
				continue
			}
			p, err := r.domain.Parse(v)
			if err != nil {
				hint := r.hint
				if errors.Is(err, strconv.ErrRange) {
					// The value is a number, just too large; -e would not help.
					hint = ""
				}
				return nil, fmt.Errorf("%s:%d: %w%s", name, line, err, hint)
			}
			ps = append(ps, p)
		}
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("%s:%d: read input (lines are limited to %d bytes): %w", name, line+1, maxLine, err)
		}
		return nil, fmt.Errorf("%s:%d: read input: %w", name, line+1, err)
	}
	return ps, nil
}

func write(w io.Writer, gaps []holes.Gap, cfg config) error {
	if cfg.count {
		// Cannot overflow: gaps never overlap present values, so with any
		// value present at most 2^64-1 positions are missing, and with none
		// there is a single gap whose Count already saturates.
		var total uint64
		for _, g := range gaps {
			total += g.Count
		}
		_, err := fmt.Fprintln(w, total)
		return err
	}
	for _, g := range gaps {
		if cfg.ranges {
			s := cfg.in.domain.Format(g.First)
			if g.Count > 1 {
				s += ".." + cfg.in.domain.Format(g.Last)
			}
			if _, err := fmt.Fprintln(w, s); err != nil {
				return err
			}
			continue
		}
		for p := range g.Values() {
			if _, err := fmt.Fprintln(w, cfg.in.domain.Format(p)); err != nil {
				return err
			}
		}
	}
	return nil
}

// version describes the build from its module version and, when the
// toolchain recorded it, the VCS commit (with +dirty for uncommitted changes).
// A pseudo-version already names the commit, so it is not repeated.
func version(bi *debug.BuildInfo, ok bool) string {
	if !ok {
		return "holes (devel)"
	}
	v := bi.Main.Version
	if v == "" {
		v = "(devel)"
	}
	var rev string
	var dirty bool
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	// Pseudo-versions end in the first 12 characters of the commit hash.
	if rev == "" || strings.Contains(v, rev[:min(12, len(rev))]) {
		return "holes " + v
	}
	if dirty {
		rev += "+dirty"
	}
	return fmt.Sprintf("holes %s (commit %s)", v, rev)
}
