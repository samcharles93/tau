package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
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
	Description: fmt.Sprintf("Execute a shell command. Uses PowerShell on Windows, bash on Linux/macOS. Returns stdout and stderr, truncated to the last %d lines or %s (whichever is hit first); when truncated, the full output is saved to a temp file whose path is included in the notice. Use for builds, tests, git, and other commands — prefer the dedicated grep, find, and read tools for searching and reading files.", DefaultMaxLines, FormatSize(DefaultMaxBytes)),
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

		shell, args := shellCommand(p.Command)
		cmd := exec.CommandContext(ctx, shell, args...)
		if cwd != "" {
			cmd.Dir = cwd
		}

		var stdout, stderr bytes.Buffer
		bw := &bridgeWriter{bridge: bridge}
		cmd.Stdout = io.MultiWriter(&stdout, bw)
		cmd.Stderr = io.MultiWriter(&stderr, bw)

		err := cmd.Run()

		var b strings.Builder
		if stdout.Len() > 0 {
			b.WriteString(stdout.String())
		}
		if stderr.Len() > 0 {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString("[stderr]\n")
			b.WriteString(stderr.String())
		}

		output := b.String()
		if output == "" {
			output = "(no output)"
		}

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
			// Check context cancellation first — a process may exit with
			// a non-zero code after the deadline, and we want to report
			// timeout rather than a misleading exit code.
			if ctx.Err() != nil {
				return Result{
					Content: fmt.Sprintf("[timeout after: %s]\n%s", timeout, content), IsError: true,
					ErrorKind: "timeout", Truncated: tr.Truncated, ResultBytes: len(output),
				}, nil
			}

			if exitErr, ok := err.(*exec.ExitError); ok {
				return Result{
					Content: fmt.Sprintf("[exit code: %d]\n%s", exitErr.ExitCode(), content), IsError: true,
					ErrorKind: "command_exit", Truncated: tr.Truncated, ResultBytes: len(output),
				}, nil
			}

			return Result{Content: fmt.Sprintf("error executing command: %v", err), IsError: true}, nil
		}

		return Result{Content: content, Truncated: tr.Truncated, ResultBytes: len(output)}, nil
	}
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
