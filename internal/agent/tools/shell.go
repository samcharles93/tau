package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	defaultShellTimeout = 120 * time.Second
	maxShellTimeout     = 10 * time.Minute
)

// ShellParams are the parameters for the shell tool.
type ShellParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"` // seconds, defaults to 120
}

var shellSchema = Schema{
	Name:        "shell",
	Description: fmt.Sprintf("Execute a shell command. Uses PowerShell on Windows, bash on Linux/macOS. Returns stdout and stderr, truncated to the last %d lines or %s (whichever is hit first); when truncated, the full output is saved to a temp file whose path is included in the notice. Use for builds, tests, git, and other commands - prefer the dedicated grep, find, and read tools for searching and reading files.", DefaultMaxLines, FormatSize(DefaultMaxBytes)),
	Parameters: json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "The shell command to execute"
			},
			"timeout": {
				"type": "integer",
				"description": "Timeout in seconds. Defaults to 120."
			}
		},
		"required": ["command"]
	}`),
}

// NewShellTool creates the built-in shell execution tool.
func NewShellTool(cwd string, mq *MutationQueue) Tool {
	return Tool{
		Schema:  shellSchema,
		Source:  "builtin",
		Execute: makeShellExecutor(cwd, mq),
	}
}

type bridgeWriter struct {
	bridge UIBridge
}

func (w *bridgeWriter) Write(p []byte) (n int, err error) {
	w.bridge.Log(string(p))
	return len(p), nil
}

func makeShellExecutor(cwd string, mq *MutationQueue) Executor {
	return func(ctx context.Context, params json.RawMessage, bridge UIBridge) (Result, error) {
		var p ShellParams
		if err := json.Unmarshal(params, &p); err != nil {
			return Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
		}

		p.Command = strings.TrimSpace(p.Command)
		if p.Command == "" {
			return Result{Content: "command is required", IsError: true}, nil
		}

		timeout := defaultShellTimeout
		if p.Timeout > 0 {
			timeout = time.Duration(p.Timeout) * time.Second
		}
		if timeout > maxShellTimeout {
			timeout = maxShellTimeout
		}

		if cwd != "" {
			info, err := os.Stat(cwd)
			if err != nil || !info.IsDir() {
				return Result{Content: fmt.Sprintf("invalid cwd %q", cwd), IsError: true}, nil
			}
		}

		// Serialize with file-mutation tools: shell commands may modify
		// files that write/edit are working on.
		if mq != nil {
			mq.GlobalLock()
			defer mq.GlobalUnlock()
		}

		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		stdout, stderr, err := runShellCommand(ctx, p.Command, cwd, bridge)
		return shellResult(p.Command, timeout, ctx.Err() != nil, stdout, stderr, err), nil
	}
}

// runShellCommand executes the command as a process-group leader so
// cancellation kills the whole group, not just the shell (which would orphan
// children like make/npm/pipelines - tau-iwh).
func runShellCommand(ctx context.Context, command, cwd string, bridge UIBridge) (stdout, stderr string, err error) {
	shell, args := shellCommand(command)
	cmd := exec.CommandContext(ctx, shell, args...)
	setProcessGroup(cmd)
	cmd.Cancel = func() error {
		return signalProcessGroup(cmd, syscall.SIGKILL)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}

	var outBuf, errBuf bytes.Buffer
	bw := &bridgeWriter{bridge: bridge}
	cmd.Stdout = io.MultiWriter(&outBuf, bw)
	cmd.Stderr = io.MultiWriter(&errBuf, bw)

	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// shellResult formats a finished shell command's output and exit status.
// timedOut reports whether the command's context expired (checked first - a
// process may exit non-zero after the deadline, and we want to report timeout
// rather than a misleading exit code).
func shellResult(command string, timeout time.Duration, timedOut bool, stdout, stderr string, err error) Result {
	var b strings.Builder
	if stdout != "" {
		b.WriteString(stdout)
	}
	if stderr != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[stderr]\n")
		b.WriteString(stderr)
	}

	output := b.String()
	if output == "" {
		output = "(no output)"
	}

	// Collapse before truncation, so a long passing test run is reduced to
	// its summaries rather than tail-truncated into a partial log.
	output, _ = collapseGoTestOutput(command, output, err == nil)

	tr := TruncateTail(output, DefaultMaxLines, DefaultMaxBytes)
	content := tr.Content
	if tr.Truncated {
		// Save the full output so the model can grep/tail it instead of
		// re-running an expensive command.
		if path, saveErr := saveFullOutput(output); saveErr == nil {
			content += "\n[full output saved to: " + path + "]"
		}
	}

	if err != nil {
		return shellErrorResult(err, timeout, timedOut, content, tr.Truncated, len(output))
	}
	return Result{Content: content, Truncated: tr.Truncated, ResultBytes: len(output)}
}

// shellErrorResult maps a command failure to a Result: timeout beats exit
// code, exit code beats generic error.
func shellErrorResult(err error, timeout time.Duration, timedOut bool, content string, truncated bool, resultBytes int) Result {
	if timedOut {
		return Result{
			Content: fmt.Sprintf("[timeout after: %s]\n%s", timeout, content), IsError: true,
			ErrorKind: "timeout", Truncated: truncated, ResultBytes: resultBytes,
		}
	}
	exitErr := &exec.ExitError{}
	if errors.As(err, &exitErr) {
		return Result{
			Content: fmt.Sprintf("[exit code: %d]\n%s", exitErr.ExitCode(), content), IsError: true,
			ErrorKind: "command_exit", Truncated: truncated, ResultBytes: resultBytes,
		}
	}
	return Result{Content: fmt.Sprintf("error executing command: %v", err), IsError: true}
}

// goTestSummaryRe matches cmd/go's per-package result lines: "ok", "FAIL" and
// "?" (no test files). Note it must NOT match "---", which prefixes per-test
// result lines ("--- PASS: TestX") - exactly the noise being collapsed. A
// package that fails to build reports as "FAIL <pkg> [build failed]", so the
// package-level failure case is already covered by the FAIL alternative.
var goTestSummaryRe = regexp.MustCompile(`^(ok|FAIL|\?)\s`)

// goTestInvocationRe matches a `go test` invocation, allowing for a leading
// `cd ... &&` and for flags between `go` and `test`. It deliberately does not
// match wrappers like gotestsum, whose output format differs.
var goTestInvocationRe = regexp.MustCompile(`(^|[;&|]\s*)go\s+test\b`)

// collapseGoTestOutput reduces a PASSING `go test` run to its per-package
// summary lines. A single `go test ./internal/tui2/... -v` in the analysed
// sessions took 14.2s and returned 11,044 tokens - 11% of all shell result
// tokens in one call - despite every test having passed, where the only
// information the model needed was "they passed".
//
// Failing runs are returned verbatim: on failure the per-test detail is the
// entire point, and collapsing it would force a re-run to recover it.
func collapseGoTestOutput(command, output string, succeeded bool) (string, bool) {
	if !succeeded || !goTestInvocationRe.MatchString(command) {
		return output, false
	}

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if goTestSummaryRe.MatchString(l) {
			kept = append(kept, l)
		}
	}

	// Nothing recognisable to keep, or nothing to gain: leave it alone rather
	// than returning an empty result or appending a pointless notice.
	if len(kept) == 0 || len(kept) == len(lines) {
		return output, false
	}

	return fmt.Sprintf(
		"%s\n\n[%d lines of passing per-test output collapsed; re-run with -v and a narrower package to see it]",
		strings.Join(kept, "\n"), len(lines)-len(kept),
	), true
}

// saveFullOutput writes the complete, untruncated command output to a temp
// file and returns its path.
func saveFullOutput(output string) (string, error) {
	f, err := os.CreateTemp("", "tau-shell-*.log")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(output); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// shellCommand returns the shell binary and arguments for the current platform.
func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		// Prefer PowerShell 7 (pwsh), fall back to Windows PowerShell.
		shell := "powershell.exe"
		if _, err := exec.LookPath("pwsh"); err == nil {
			shell = "pwsh"
		}
		return shell, []string{
			"-NoProfile",
			"-NonInteractive",
			"-ExecutionPolicy", "Bypass",
			"-Command", command,
		}
	}
	return "bash", []string{"-c", command}
}
