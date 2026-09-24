package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rgravlin/holes"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		stdin      string
		wantOut    string
		wantErr    string // substring of stderr
		wantStatus int
	}{
		{name: "nothing missing", stdin: "1\n2\n3\n", wantStatus: exitNone},
		{name: "empty input", stdin: "", wantStatus: exitNone},
		{name: "missing values", stdin: "1\n5\n3\n", wantOut: "2\n4\n", wantStatus: exitFound},
		{name: "blank lines and spaces", stdin: "  1\n\n 4 \n", wantOut: "2\n3\n", wantStatus: exitFound},
		{name: "byte-order mark stripped", stdin: "\ufeff1\n3\n", wantOut: "2\n", wantStatus: exitFound},
		{name: "byte-order mark only at start", stdin: "1\n\ufeff3\n", wantErr: "stdin:2: parse integer", wantStatus: exitError},
		{name: "ranges", args: []string{"-r"}, stdin: "1\n3\n9\n", wantOut: "2\n4..8\n", wantStatus: exitFound},
		{name: "count", args: []string{"-c"}, stdin: "1\n3\n9\n", wantOut: "6\n", wantStatus: exitFound},
		{name: "count when none", args: []string{"-count"}, stdin: "1\n2\n", wantOut: "0\n", wantStatus: exitNone},
		{name: "count saturates", args: []string{"-c", "-from", "-9223372036854775808", "-to", "9223372036854775807"}, wantOut: "18446744073709551615\n", wantStatus: exitFound},
		{name: "quiet", args: []string{"-q"}, stdin: "1\n3\n", wantStatus: exitFound},
		{name: "quiet wins over count", args: []string{"-q", "-c"}, stdin: "1\n3\n", wantStatus: exitFound},
		{name: "quiet wins over ranges", args: []string{"-r", "-q"}, stdin: "1\n3\n", wantStatus: exitFound},
		{name: "step", args: []string{"-step", "10"}, stdin: "0\n40\n", wantOut: "10\n20\n30\n", wantStatus: exitFound},
		{name: "from and to", args: []string{"-from", "0", "-to", "5"}, stdin: "2\n3\n", wantOut: "0\n1\n4\n5\n", wantStatus: exitFound},
		{name: "dates", args: []string{"-t", "date"}, stdin: "2026-09-01\n2026-09-04\n", wantOut: "2026-09-02\n2026-09-03\n", wantStatus: exitFound},
		{name: "dates across month", args: []string{"-type", "date", "-r"}, stdin: "2026-08-30\n2026-09-02\n", wantOut: "2026-08-31..2026-09-01\n", wantStatus: exitFound},
		{name: "date layout", args: []string{"-t", "date", "-layout", "20060102"}, stdin: "20260101\n20260103\n", wantOut: "20260102\n", wantStatus: exitFound},
		{name: "weekly dates", args: []string{"-t", "date", "-step", "7"}, stdin: "2026-09-01\n2026-09-22\n", wantOut: "2026-09-08\n2026-09-15\n", wantStatus: exitFound},
		{name: "last backup missing", args: []string{"-t", "date", "-to", "2026-09-22"}, stdin: "2026-09-20\n2026-09-21\n", wantOut: "2026-09-22\n", wantStatus: exitFound},
		{name: "no backups at all", args: []string{"-t", "date", "-to", "2026-09-22"}, stdin: "", wantOut: "2026-09-22\n", wantStatus: exitFound},
		{name: "backups stopped before from", args: []string{"-t", "date", "-from", "2026-09-20"}, stdin: "2026-09-01\n2026-09-02\n", wantOut: "2026-09-20\n", wantStatus: exitFound},
		{
			name:       "extract from filenames",
			args:       []string{"-t", "date", "-e", `\d{4}-\d{2}-\d{2}`},
			stdin:      "db-2026-09-01.tar.gz\nREADME\ndb-2026-09-03.tar.gz\n",
			wantOut:    "2026-09-02\n",
			wantStatus: exitFound,
		},
		{name: "extract group", args: []string{"-e", `frame_(\d+)\.exr`}, stdin: "frame_0001.exr\nframe_0004.exr\n", wantOut: "2\n3\n", wantStatus: exitFound},
		{name: "extract every match on a line", args: []string{"-e", `\d+`}, stdin: "1,2,5,7\n", wantOut: "3\n4\n6\n", wantStatus: exitFound},
		{name: "extract every match across lines", args: []string{"-e", `\d+`, "-r"}, stdin: "1 2\nnone\n9\n", wantOut: "3..8\n", wantStatus: exitFound},
		{name: "extract group of every match", args: []string{"-e", `#(\d+)`}, stdin: "fixes #3, #5 and 4\n", wantOut: "4\n", wantStatus: exitFound},
		{
			name:       "anchored extract takes one match per line",
			args:       []string{"-t", "date", "-e", `^\d{4}-\d{2}-\d{2}`},
			stdin:      "2026-09-20 retry at 2026-09-25\n2026-09-22 ok\n",
			wantOut:    "2026-09-21\n",
			wantStatus: exitFound,
		},
		{name: "width pads", args: []string{"-e", `frame_(\d+)\.exr`, "-width", "4"}, stdin: "frame_0001.exr\nframe_0004.exr\n", wantOut: "0002\n0003\n", wantStatus: exitFound},
		{name: "width with ranges", args: []string{"-r", "-width", "4", "-to", "12"}, stdin: "1\n5\n", wantOut: "0002..0004\n0006..0012\n", wantStatus: exitFound},
		{name: "width is a minimum", args: []string{"-width", "2"}, stdin: "98\n101\n", wantOut: "99\n100\n", wantStatus: exitFound},
		{name: "width leaves count alone", args: []string{"-c", "-width", "4"}, stdin: "1\n5\n", wantOut: "3\n", wantStatus: exitFound},
		{name: "bad value names line", stdin: "1\nx\n", wantErr: "stdin:2: parse integer", wantStatus: exitError},
		{name: "layout without day", args: []string{"-t", "date", "-layout", "2006-01"}, wantErr: "-layout", wantStatus: exitError},
		{name: "layout without year", args: []string{"-t", "date", "-layout", "01-02"}, wantErr: "-layout", wantStatus: exitError},
		{name: "negative width", args: []string{"-width", "-1"}, wantErr: "-width -1: must be between 0 and 20", wantStatus: exitError},
		{name: "width too large", args: []string{"-width", "21"}, wantErr: "-width 21: must be between 0 and 20", wantStatus: exitError},
		{name: "width needs int", args: []string{"-t", "date", "-width", "4"}, wantErr: "-width requires -type int", wantStatus: exitError},
		{name: "bad type", args: []string{"-t", "ip"}, wantErr: `-type "ip"`, wantStatus: exitError},
		{name: "layout needs date", args: []string{"-layout", "2006"}, wantErr: "-layout requires -type date", wantStatus: exitError},
		{name: "bad step", args: []string{"-step", "0"}, wantErr: "-step 0", wantStatus: exitError},
		{name: "bad from", args: []string{"-t", "date", "-from", "yesterday"}, wantErr: "-from: parse date", wantStatus: exitError},
		{name: "bad to", args: []string{"-t", "date", "-to", "yesterday"}, wantErr: "-to: parse date", wantStatus: exitError},
		{name: "from after to", args: []string{"-from", "5", "-to", "1"}, wantErr: "from must not be greater than to", wantStatus: exitError},
		{name: "bad regexp", args: []string{"-e", "("}, wantErr: "-extract:", wantStatus: exitError},
		{name: "ranges and count", args: []string{"-r", "-c"}, wantErr: "mutually exclusive", wantStatus: exitError},
		{name: "unknown flag", args: []string{"-nope"}, wantErr: "flag provided but not defined", wantStatus: exitError},
		{name: "missing file", args: []string{"does-not-exist"}, wantErr: "open does-not-exist", wantStatus: exitError},
		{name: "line too long", stdin: strings.Repeat("1", maxLine+1), wantErr: "lines are limited", wantStatus: exitError},
		{name: "version", args: []string{"-version"}, wantOut: "holes (devel)\n", wantStatus: exitNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := run(tt.args, strings.NewReader(tt.stdin), &stdout, &stderr)
			if status != tt.wantStatus {
				t.Errorf("run(%q) status = %d, want %d (stderr: %s)", tt.args, status, tt.wantStatus, stderr.String())
			}
			if got := stdout.String(); got != tt.wantOut {
				t.Errorf("run(%q) stdout = %q, want %q", tt.args, got, tt.wantOut)
			}
			if tt.wantErr == "" && stderr.Len() > 0 {
				t.Errorf("run(%q) unexpected stderr: %s", tt.args, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.wantErr) {
				t.Errorf("run(%q) stderr = %q, want it to contain %q", tt.args, stderr.String(), tt.wantErr)
			}
		})
	}
}

func TestRunHelp(t *testing.T) {
	for _, arg := range []string{"-h", "-help"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if status := run([]string{arg}, strings.NewReader(""), &stdout, &stderr); status != exitNone {
				t.Errorf("run(%q) status = %d, want %d", arg, status, exitNone)
			}
			if !strings.HasPrefix(stdout.String(), "Usage: holes") || !strings.Contains(stdout.String(), "-layout") {
				t.Errorf("run(%q) stdout = %q, want usage text with flag defaults", arg, stdout.String())
			}
			examples, flags := strings.Index(stdout.String(), "\nExamples:\n"), strings.Index(stdout.String(), "\nFlags:\n")
			if examples < 0 || flags < examples {
				t.Fatalf("run(%q) stdout = %q, want an Examples section before the Flags section", arg, stdout.String())
			}
			if n := strings.Count(stdout.String()[examples:flags], "\n  # "); n < 1 || n > 6 {
				t.Errorf("run(%q) has %d examples, want 1 to 6", arg, n)
			}
			if stderr.Len() > 0 {
				t.Errorf("run(%q) stderr = %q, want empty", arg, stderr.String())
			}
		})
	}
}

func TestRunUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := run([]string{"-nope"}, strings.NewReader(""), &stdout, &stderr); status != exitError {
		t.Errorf("status = %d, want %d", status, exitError)
	}
	if stdout.Len() > 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	got := stderr.String()
	if n := strings.Count(got, "flag provided but not defined"); n != 1 {
		t.Errorf("stderr reports the error %d times, want once: %q", n, got)
	}
	if strings.Contains(got, "Usage:") {
		t.Errorf("stderr = %q, want no usage text", got)
	}
	if want := "holes: flag provided but not defined: -nope\nrun 'holes -h' for usage\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestRunErrorMessages(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		stdin   string
		once    string // must appear exactly once in stderr
		wantErr string // substring of stderr
		wantNot string // must not appear in stderr
	}{
		{name: "integer", stdin: "x\n", once: `"x"`, wantErr: `stdin:1: parse integer "x": invalid syntax (use -e to extract`},
		{name: "integer out of range", stdin: "99999999999999999999\n", once: `"99999999999999999999"`, wantErr: "value out of range", wantNot: "use -e"},
		{name: "integer with extract", args: []string{"-e", `\S+`}, stdin: "x\n", once: `"x"`, wantErr: `invalid syntax`, wantNot: "use -e"},
		{name: "date", args: []string{"-t", "date"}, stdin: "nope\n", once: `"2006-01-02"`, wantErr: "stdin:1: parse date: parsing time", wantNot: "with layout"},
		{name: "date hint", args: []string{"-t", "date"}, stdin: "nope\n", once: "-layout", wantErr: "(check that -layout matches the input)"},
		{name: "missing file", args: []string{"does-not-exist"}, once: "does-not-exist", wantErr: "holes: open does-not-exist: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if status := run(tt.args, strings.NewReader(tt.stdin), &stdout, &stderr); status != exitError {
				t.Errorf("run(%q) status = %d, want %d", tt.args, status, exitError)
			}
			got := stderr.String()
			if n := strings.Count(got, tt.once); n != 1 {
				t.Errorf("run(%q) stderr = %q, contains %q %d times, want once", tt.args, got, tt.once, n)
			}
			if !strings.Contains(got, tt.wantErr) {
				t.Errorf("run(%q) stderr = %q, want it to contain %q", tt.args, got, tt.wantErr)
			}
			if tt.wantNot != "" && strings.Contains(got, tt.wantNot) {
				t.Errorf("run(%q) stderr = %q, want it not to contain %q", tt.args, got, tt.wantNot)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	info := func(version string, settings ...debug.BuildSetting) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Version: version}, Settings: settings}
	}
	rev := debug.BuildSetting{Key: "vcs.revision", Value: "0123456789abcdef"}
	tests := []struct {
		name string
		bi   *debug.BuildInfo
		ok   bool
		want string
	}{
		{name: "no build info", want: "holes (devel)"},
		{name: "no version", bi: info(""), ok: true, want: "holes (devel)"},
		{name: "release", bi: info("v1.2.3"), ok: true, want: "holes v1.2.3"},
		{name: "with commit", bi: info("v1.2.3", rev), ok: true, want: "holes v1.2.3 (commit 0123456789abcdef)"},
		{name: "clean tree", bi: info("v1.2.3", rev, debug.BuildSetting{Key: "vcs.modified", Value: "false"}), ok: true, want: "holes v1.2.3 (commit 0123456789abcdef)"},
		{name: "dirty tree", bi: info("(devel)", rev, debug.BuildSetting{Key: "vcs.modified", Value: "true"}), ok: true, want: "holes (devel) (commit 0123456789abcdef+dirty)"},
		{
			name: "pseudo-version already names the commit",
			bi:   info("v0.0.0-20260922133937-0123456789ab+dirty", rev, debug.BuildSetting{Key: "vcs.modified", Value: "true"}),
			ok:   true,
			want: "holes v0.0.0-20260922133937-0123456789ab+dirty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := version(tt.bi, tt.ok); got != tt.want {
				t.Errorf("version(%+v, %t) = %q, want %q", tt.bi, tt.ok, got, tt.want)
			}
		})
	}
}

// readerFunc adapts a function to [io.Reader].
type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestRunBadBoundsSkipsInput(t *testing.T) {
	stdin := readerFunc(func([]byte) (int, error) {
		t.Error("stdin read before the bounds were validated")
		return 0, io.EOF
	})
	var stdout, stderr bytes.Buffer
	if status := run([]string{"-from", "5", "-to", "1"}, stdin, &stdout, &stderr); status != exitError {
		t.Errorf("status = %d, want %d", status, exitError)
	}
	if want := "-from 5, -to 1"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
	}
}

func TestRunReadError(t *testing.T) {
	stdin := readerFunc(func([]byte) (int, error) { return 0, errors.New("device gone") })
	var stdout, stderr bytes.Buffer
	if status := run(nil, stdin, &stdout, &stderr); status != exitError {
		t.Errorf("status = %d, want %d", status, exitError)
	}
	got := stderr.String()
	if want := "stdin:1: read input: device gone"; !strings.Contains(got, want) {
		t.Errorf("stderr = %q, want it to contain %q", got, want)
	}
	if strings.Contains(got, "lines are limited") {
		t.Errorf("stderr = %q, want no line-length hint for an error unrelated to line length", got)
	}
}

func TestRunFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("files and stdin combined", func(t *testing.T) {
		a, b := write("a", "1\n2\n"), write("b", "6\n")
		var stdout, stderr bytes.Buffer
		status := run([]string{"-r", a, "-", b}, strings.NewReader("4\n"), &stdout, &stderr)
		if status != exitFound {
			t.Errorf("status = %d, want %d (stderr: %s)", status, exitFound, stderr.String())
		}
		if got, want := stdout.String(), "3\n5\n"; got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
	})

	t.Run("byte-order mark stripped from each file", func(t *testing.T) {
		a, b := write("bom-a", "\ufeff1\n"), write("bom-b", "\ufeff3\n")
		var stdout, stderr bytes.Buffer
		status := run([]string{a, b}, strings.NewReader(""), &stdout, &stderr)
		if status != exitFound {
			t.Errorf("status = %d, want %d (stderr: %s)", status, exitFound, stderr.String())
		}
		if got, want := stdout.String(), "2\n"; got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
	})

	t.Run("bad value names file and line", func(t *testing.T) {
		bad := write("bad", "1\n\nfoo\n")
		var stdout, stderr bytes.Buffer
		if status := run([]string{bad}, strings.NewReader(""), &stdout, &stderr); status != exitError {
			t.Errorf("status = %d, want %d", status, exitError)
		}
		if want := bad + ":3:"; !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
		}
	})
}

// failWriter is an [io.Writer] whose writes always fail.
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestRunWriteError(t *testing.T) {
	var evens strings.Builder
	for i := 0; i <= 10000; i += 2 {
		fmt.Fprintln(&evens, i)
	}
	tests := []struct {
		name  string
		args  []string
		stdin string
	}{
		// Outputs over bufio's 4096-byte buffer fail on a write, not the flush.
		{name: "values", stdin: "0\n100000\n"},
		{name: "ranges", args: []string{"-r"}, stdin: evens.String()},
		{name: "small output fails at flush", stdin: "1\n3\n"},
		{name: "count", args: []string{"-c"}, stdin: "1\n3\n"},
		{name: "version", args: []string{"-version"}},
		{name: "help", args: []string{"-h"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if status := run(tt.args, strings.NewReader(tt.stdin), failWriter{}, &stderr); status != exitError {
				t.Errorf("run(%q) status = %d, want %d", tt.args, status, exitError)
			}
			if want := "write output: disk full"; !strings.Contains(stderr.String(), want) {
				t.Errorf("run(%q) stderr = %q, want it to contain %q", tt.args, stderr.String(), want)
			}
		})
	}
}

// FuzzRun feeds arbitrary input, -e regexps and -from/-to bounds through run.
// It runs each case with -c and with -r, whose output stays small however
// large the gaps are, and checks that run never panics, uses stderr and exit
// status 2 only for errors, and that the two modes agree.
func FuzzRun(f *testing.F) {
	f.Add("1\n3\n", "", "", "", false)
	f.Add("\ufeff1\r\n\n  5 \n", "", "0", "9", false)
	f.Add("1\n\ufeff3\n", "", "", "", false)
	f.Add("frame_0001.exr\nframe_0004.exr\n", `frame_(\d+)\.exr`, "", "", false)
	f.Add("batches 1,2,3,5,8,9", `\d+`, "", "", false)
	f.Add("a1b2\n", `(x)?\d`, "", "", false)
	f.Add("x\n", "", "", "", false)
	f.Add("", "", "-9223372036854775808", "9223372036854775807", false)
	f.Add("1\n", "(", "5", "1", false)
	f.Add("db-2026-09-19.tar.gz\nnotes.txt\n", `\d{4}-\d{2}-\d{2}`, "", "2026-09-22", true)
	f.Add("2026-09-01\n2026-02-30\n", "", "", "", true)
	f.Add("0000-01-01\n9999-12-31\n", "", "", "", true)
	f.Fuzz(func(t *testing.T, stdin, extract, from, to string, date bool) {
		args := []string{"-e", extract, "-from", from, "-to", to}
		if date {
			args = append(args, "-t", "date")
		}
		cStatus, cOut, cErr := fuzzRun(t, slices.Concat(args, []string{"-c"}), stdin)
		rStatus, rOut, rErr := fuzzRun(t, slices.Concat(args, []string{"-r"}), stdin)
		if cStatus != rStatus || cErr != rErr {
			t.Fatalf("args %q: -c gave status %d, stderr %q; -r gave status %d, stderr %q", args, cStatus, cErr, rStatus, rErr)
		}
		if cStatus == exitError {
			return
		}
		count, err := strconv.ParseUint(strings.TrimSuffix(cOut, "\n"), 10, 64)
		if err != nil || !strings.HasSuffix(cOut, "\n") {
			t.Fatalf("args %q: -c stdout = %q, want one count line", args, cOut)
		}
		if (count == 0) != (cStatus == exitNone) {
			t.Fatalf("args %q: -c counted %d but exited %d", args, count, cStatus)
		}
		if (rOut == "") != (rStatus == exitNone) || (rOut != "" && !strings.HasSuffix(rOut, "\n")) {
			t.Fatalf("args %q: -r stdout = %q but exited %d", args, rOut, rStatus)
		}
		// Every gap has at least one missing value.
		var lines uint64
		for range strings.Lines(rOut) {
			lines++
		}
		if lines > count {
			t.Fatalf("args %q: -r printed %d lines (%q) for %d missing values", args, lines, rOut, count)
		}
	})
}

// fuzzRun runs holes and checks the invariants that hold for any input: exit
// status 0 or 1 with nothing on stderr, or 2 with one error on stderr and
// nothing on stdout.
func fuzzRun(t *testing.T, args []string, stdin string) (status int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	status = run(args, strings.NewReader(stdin), &out, &errOut)
	stdout, stderr = out.String(), errOut.String()
	switch status {
	case exitNone, exitFound:
		if stderr != "" {
			t.Fatalf("run(%q) status %d with stderr %q", args, status, stderr)
		}
	case exitError:
		if stdout != "" || !strings.HasPrefix(stderr, "holes: ") || !strings.HasSuffix(stderr, "\n") {
			t.Fatalf("run(%q) status 2 with stdout %q, stderr %q", args, stdout, stderr)
		}
	default:
		t.Fatalf("run(%q) status = %d, want 0, 1 or 2", args, status)
	}
	return status, stdout, stderr
}

// BenchmarkRun runs the CLI end to end. The read cases parse benchLines
// sorted lines with about one value in ten missing, and print only the count
// so that reading dominates. The print cases read nothing and print every
// value from -from to -to.
func BenchmarkRun(b *testing.B) {
	const benchLines = 200_000
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // G404: a fixed seed keeps benchmark inputs identical across runs.
	date := holes.Date{}
	var ints, files, dates strings.Builder
	for p := range int64(benchLines) {
		if r.IntN(10) == 0 {
			continue
		}
		fmt.Fprintf(&ints, "%d\n", p)
		fmt.Fprintf(&files, "frame_%08d.exr\n", p)
		fmt.Fprintf(&dates, "%s\n", date.Format(p))
	}

	for _, bb := range []struct {
		name  string
		args  []string
		stdin string
	}{
		{name: "read int", args: []string{"-c"}, stdin: ints.String()},
		{name: "read extract", args: []string{"-c", "-e", `frame_(\d+)\.exr`}, stdin: files.String()},
		{name: "read date", args: []string{"-c", "-t", "date"}, stdin: dates.String()},
		{name: "print int", args: []string{"-from", "0", "-to", strconv.Itoa(benchLines - 1)}},
		{name: "print date", args: []string{"-t", "date", "-from", date.Format(0), "-to", date.Format(benchLines - 1)}},
	} {
		b.Run(bb.name, func(b *testing.B) {
			b.SetBytes(int64(len(bb.stdin)))
			var stderr bytes.Buffer
			for b.Loop() {
				if status := run(bb.args, strings.NewReader(bb.stdin), io.Discard, &stderr); status != exitFound {
					b.Fatalf("run(%q) status = %d, want %d; stderr %q", bb.args, status, exitFound, stderr.String())
				}
			}
		})
	}
}
