// Package ui centralizes terminal output: status messages, spinners, tables and
// progress bars. Everything degrades gracefully when stdout is not a TTY or when
// NO_COLOR is set, so piped/redirected output stays clean.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/mattn/go-isatty"
	"github.com/schollz/progressbar/v3"
)

var (
	// Out is the destination for normal output; overridable in tests.
	Out io.Writer = os.Stdout
	// Err is the destination for diagnostics.
	Err io.Writer = os.Stderr
)

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("VC_NO_COLOR") != "" {
		return false
	}
	f, ok := Out.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

func isTTY() bool {
	f, ok := Out.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}

const (
	cReset  = "\x1b[0m"
	cGreen  = "\x1b[32m"
	cYellow = "\x1b[33m"
	cRed    = "\x1b[31m"
	cCyan   = "\x1b[36m"
	cDim    = "\x1b[2m"
	cBold   = "\x1b[1m"
)

func paint(color, s string) string {
	if !colorEnabled() {
		return s
	}
	return color + s + cReset
}

// Success prints a green check line.
func Success(format string, a ...any) {
	fmt.Fprintln(Out, paint(cGreen, "✓ ")+fmt.Sprintf(format, a...))
}

// Info prints a plain informational line.
func Info(format string, a ...any) {
	fmt.Fprintln(Out, fmt.Sprintf(format, a...))
}

// Step prints a cyan arrow line for a step in progress.
func Step(format string, a ...any) {
	fmt.Fprintln(Out, paint(cCyan, "→ ")+fmt.Sprintf(format, a...))
}

// Warn prints a yellow warning to stderr.
func Warn(format string, a ...any) {
	fmt.Fprintln(Err, paint(cYellow, "! ")+fmt.Sprintf(format, a...))
}

// Errorf prints a red error line to stderr.
func Errorf(format string, a ...any) {
	fmt.Fprintln(Err, paint(cRed, "✗ ")+fmt.Sprintf(format, a...))
}

// Dim returns dimmed text (for hints).
func Dim(s string) string { return paint(cDim, s) }

// Bold returns bold text.
func Bold(s string) string { return paint(cBold, s) }

// Hint prints a dimmed help line, typically a suggested next command.
func Hint(format string, a ...any) {
	fmt.Fprintln(Out, Dim("  "+fmt.Sprintf(format, a...)))
}

// Spin runs fn while showing an animated spinner with msg. On a non-TTY it just
// prints the message once and runs fn (no animation, no spam). It prints a
// success/failure line based on fn's result and returns fn's error.
func Spin(msg string, fn func() error) error {
	if !isTTY() {
		Step("%s", msg)
		if err := fn(); err != nil {
			Errorf("%s: %v", msg, err)
			return err
		}
		return nil
	}
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithWriter(Out))
	s.Suffix = " " + msg
	s.Start()
	err := fn()
	s.Stop()
	if err != nil {
		Errorf("%s: %v", msg, err)
		return err
	}
	Success("%s", msg)
	return nil
}

// ProgressWriter returns an io.Writer that renders a download progress bar of
// the given total size (bytes). On a non-TTY it returns a no-op writer so logs
// stay clean. Wrap it around the destination of an io.Copy, or pass to io.Copy
// as a tee.
func ProgressWriter(total int64, description string) io.Writer {
	if !isTTY() {
		return io.Discard
	}
	return progressbar.NewOptions64(
		total,
		progressbar.OptionSetDescription(description),
		progressbar.OptionSetWriter(Out),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionClearOnFinish(),
	)
}

// Table renders aligned columns with a header. It computes column widths from
// the content and pads with two spaces between columns.
func Table(header []string, rows [][]string) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i := 0; i < len(widths) && i < len(row); i++ {
			if l := displayLen(row[i]); l > widths[i] {
				widths[i] = l
			}
		}
	}
	printRow(header, widths, true)
	for _, row := range rows {
		printRow(row, widths, false)
	}
}

func printRow(cols []string, widths []int, isHeader bool) {
	var b strings.Builder
	for i, w := range widths {
		cell := ""
		if i < len(cols) {
			cell = cols[i]
		}
		text := cell
		if isHeader {
			text = Bold(cell)
		}
		b.WriteString(text)
		// pad to width (account for the actual rune length, not styled length)
		pad := w - displayLen(cell)
		if i < len(widths)-1 {
			b.WriteString(strings.Repeat(" ", pad+2))
		}
	}
	fmt.Fprintln(Out, b.String())
}

func displayLen(s string) int {
	return len([]rune(s))
}
