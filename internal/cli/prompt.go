package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// ErrPromptCanceled is returned by the prompt helpers in this file when ctx
// is canceled (e.g. Ctrl+C, wired to signal cancellation upstream) while
// waiting for input.
var ErrPromptCanceled = errors.New("prompt canceled")

// SelectOption is one entry in a numbered selection list presented by
// Select.
type SelectOption struct {
	Label string // shown to the user
	Value string // returned when this option is chosen
}

// Select renders options as a numbered list on w, prompts on w, and reads a
// numeric choice from r, re-prompting on non-numeric or out-of-range input.
// It returns ErrPromptCanceled if ctx is canceled before a valid choice is
// read. There is no portable way to interrupt a blocking read on r once
// started, so a background goroutine reading it may outlive a canceled
// Select call; that's fine here since the caller is expected to exit shortly
// after cancellation.
func Select(ctx context.Context, w io.Writer, r io.Reader, title string, options []SelectOption) (SelectOption, error) {
	if len(options) == 0 {
		return SelectOption{}, errors.New("no options to select from")
	}
	if strings.TrimSpace(title) != "" {
		_, _ = fmt.Fprintln(w, styleBold(title))
	}
	for i, opt := range options {
		_, _ = fmt.Fprintf(w, "  %d) %s\n", i+1, opt.Label)
	}

	scanner := bufio.NewScanner(r)
	for {
		fmt.Fprint(w, "> ")

		lines := make(chan string, 1)
		errs := make(chan error, 1)
		go func() {
			if scanner.Scan() {
				lines <- scanner.Text()
				return
			}
			if err := scanner.Err(); err != nil {
				errs <- err
				return
			}
			errs <- io.EOF
		}()

		select {
		case <-ctx.Done():
			return SelectOption{}, ErrPromptCanceled
		case err := <-errs:
			return SelectOption{}, err
		case line := <-lines:
			n, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || n < 1 || n > len(options) {
				fmt.Fprintf(w, "please enter a number between 1 and %d\n", len(options))
				continue
			}
			return options[n-1], nil
		}
	}
}

// readPassword is term.ReadPassword by default; overridable in tests since
// term.ReadPassword needs a real terminal fd.
var readPassword = term.ReadPassword

// ReadSecret prompts on w and reads a line of hidden (non-echoed) input from
// the terminal referenced by fd — typically int(os.Stdin.Fd()). It returns
// ErrPromptCanceled if ctx is canceled first. fd must refer to a real
// terminal; there is no echo-free fallback for piped input, so
// non-interactive callers should use a separate flag or env-var path
// instead of this function.
func ReadSecret(ctx context.Context, w io.Writer, fd int, prompt string) (string, error) {
	if strings.TrimSpace(prompt) != "" {
		fmt.Fprint(w, prompt)
	}

	type result struct {
		value string
		err   error
	}
	done := make(chan result, 1)
	read := readPassword // captured once: a leaked goroutine past cancellation must not race a later reassignment of the package var
	go func() {
		b, err := read(fd)
		done <- result{value: string(b), err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ErrPromptCanceled
	case res := <-done:
		_, _ = fmt.Fprintln(w)
		if res.err != nil {
			return "", res.err
		}
		return res.value, nil
	}
}

// noColor reports whether ANSI styling should be suppressed, per the
// https://no-color.org convention: any non-empty NO_COLOR value disables it,
// regardless of content.
func noColor() bool {
	return os.Getenv("NO_COLOR") != ""
}

// styleBold wraps s in an ANSI bold escape, or returns it unchanged when
// noColor() is true.
func styleBold(s string) string {
	if noColor() {
		return s
	}
	return "\x1b[1m" + s + "\x1b[0m"
}
