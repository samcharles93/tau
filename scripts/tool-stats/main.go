// Command tool-stats analyses tau session files and reports how the agent's
// tools are actually used: call counts, result sizes, estimated token cost,
// and error rates per tool, plus a breakdown of what shell commands run.
//
// Usage:
//
//	go run ./scripts/tool-stats [--sessions-dir <dir>] [--metrics-dir <dir>] [--output <file.html>] [--json]
//
// It reads every *.jsonl and *.jsonl.tmp session file, prints a summary table
// to stdout, and writes a self-contained HTML report.
//
// New sessions persist structured tool-result status. Legacy sessions fall
// back to conservative tool-specific result-text classification. When a
// metrics.jsonl file exists, duration events add an independent live view.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/samcharles93/tau/internal/agent/tools"
	"github.com/samcharles93/tau/internal/chat"
	tauconfig "github.com/samcharles93/tau/internal/config"
)

// bucketLabels are histogram buckets over estimated result tokens.
var bucketLabels = []string{
	"0-50", "50-100", "100-250", "250-500", "500-1000",
	"1000-2000", "2000-4000", "4000-8000", "8000-16000", "16000-32000", "32000+",
}

var bucketUpper = []int{50, 100, 250, 500, 1000, 2000, 4000, 8000, 16000, 32000}

// errorPatterns match the error shapes tau's builtin tools produce. Checked
// against the start of the result content.
var errorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(invalid (parameters|arguments|cwd|path|regex|pattern|exclude)|error[ :])`),
	regexp.MustCompile(`^(grep|find|patch) (error|failed)`),
	regexp.MustCompile(`^\[(exit code|timeout after)`),
	regexp.MustCompile(`^(path|pattern|command|query|at least one edit) (is required|cannot be empty|escapes working directory|must be a directory|is a directory)`),
	regexp.MustCompile(`^(file too large|file appears to be binary|offset \d+ exceeds)`),
	regexp.MustCompile(`^edit \d+: `),
	regexp.MustCompile(`^edits \d+ and \d+ overlap`),
	regexp.MustCompile(`^no changes made to `),
	regexp.MustCompile(`(not found in catalog|documentation file not found|escaping docs sandbox)`),
	regexp.MustCompile(`^(old_text|unified_diff|skill name|tool name) `),
	regexp.MustCompile(`^plugin executor not available`),
}

type toolStat struct {
	Name              string   `json:"name"`
	Aliases           []string `json:"aliases,omitempty"`
	Calls             int      `json:"calls"`
	Results           int      `json:"results"`
	Errors            int      `json:"errors"`
	StructuredResults int      `json:"structuredResults"`
	Tokens            int      `json:"estimatedTokens"`
	Samples           []int    `json:"-"`
	Histogram         []int    `json:"histogram"`
	ErrRate           float64  `json:"errorRate"`
	Median            int      `json:"median"`
	P90               int      `json:"p90"`
	P95               int      `json:"p95"`
	P99               int      `json:"p99"`
	Max               int      `json:"max"`
	// ErrorSamples holds up to three distinct error messages for the report.
	ErrorSamples []string `json:"errorSamples,omitempty"`

	// Metrics-derived fields, populated when a metrics.jsonl join is
	// available. MetricCalls can differ from Calls: metrics only cover
	// sessions run with metrics.dir configured.
	MetricCalls    int     `json:"metricCalls,omitempty"`
	MetricErrors   int     `json:"metricErrors,omitempty"`
	MetricErrRate  float64 `json:"metricErrorRate,omitempty"`
	DurationMedian int     `json:"durationMedianMs,omitempty"`
	DurationP90    int     `json:"durationP90Ms,omitempty"`
	DurationMax    int     `json:"durationMaxMs,omitempty"`

	durations []int
	aliases   map[string]bool
}

type reportData struct {
	GeneratedAt      string `json:"generatedAt"`
	SessionsDir      string `json:"sessionsDir"`
	MetricsPath      string `json:"metricsPath,omitempty"`
	Files            int    `json:"files"`
	UniqueSessions   int    `json:"uniqueSessions"`
	UnmatchedCalls   int    `json:"unmatchedCalls,omitempty"`
	UnmatchedResults int    `json:"unmatchedResults,omitempty"`
	ParseErrors      int    `json:"parseErrors"`
	TotalCalls       int    `json:"totalCalls"`
	TotalTokens      int    `json:"totalTokens"`
	// MetricsSkipped counts metric events for sessions not present in the
	// sessions dir (excluded so both sources describe the same population).
	MetricsSkipped int            `json:"metricsSkipped,omitempty"`
	Buckets        []string       `json:"bucketLabels"`
	Tools          []*toolStat    `json:"tools"`
	Shell          []*toolStat    `json:"shellCommands"`
	Outliers       []toolOutlier  `json:"outliers,omitempty"`
	RepeatedReads  []repeatedRead `json:"repeatedReads,omitempty"`
	RepeatedCalls  []repeatedCall `json:"repeatedCalls,omitempty"`
	ErrorBreakdown []errorBucket  `json:"errorBreakdown,omitempty"`
}

type toolOutlier struct {
	SessionID string `json:"sessionId"`
	File      string `json:"file"`
	Tool      string `json:"tool"`
	Tokens    int    `json:"tokens"`
	Args      string `json:"args,omitempty"`
	Result    string `json:"result,omitempty"`
	ErrorKind string `json:"errorKind,omitempty"`
}

type repeatedRead struct {
	SessionID       string  `json:"sessionId"`
	File            string  `json:"file"`
	Path            string  `json:"path"`
	Calls           int     `json:"calls"`
	Tokens          int     `json:"tokens"`
	ExactDuplicates int     `json:"exactDuplicates"`
	UniqueLines     int     `json:"uniqueLines,omitempty"`
	OverlapRatio    float64 `json:"overlapRatio,omitempty"`
}

type repeatedCall struct {
	SessionID string `json:"sessionId"`
	Tool      string `json:"tool"`
	Args      string `json:"args"`
	MaxStreak int    `json:"maxStreak"`
}

type errorBucket struct {
	Tool    string   `json:"tool"`
	Kind    string   `json:"kind"`
	Count   int      `json:"count"`
	Samples []string `json:"samples,omitempty"`
}

type pendingCall struct {
	stat            *toolStat
	tool            string
	args            string
	readPath        string
	readOffset      int
	readLimit       int
	readFingerprint string
	sessionID       string
	file            string
	primary         bool
}

type readUse struct {
	calls           int
	tokens          int
	exactDuplicates int
	fingerprints    map[string]int
	lines           map[int]bool
	requestedLines  int
}

// sessionMessage is the subset of a persisted chat message the script needs.
type sessionMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id"`
	ToolResult *struct {
		ToolName    string        `json:"tool_name"`
		Status      string        `json:"status"`
		ErrorKind   string        `json:"error_kind"`
		Duration    time.Duration `json:"duration"`
		Truncated   bool          `json:"truncated"`
		ResultBytes int           `json:"result_bytes"`
	} `json:"tool_result"`
	ToolCalls []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

func main() {
	defaultDir := filepath.Join(os.Getenv("HOME"), ".config", "tau", "sessions")
	sessionsDir := flag.String("sessions-dir", defaultDir, "directory containing tau session .jsonl files")
	metricsDir := flag.String("metrics-dir", "", "directory containing metrics.jsonl (default: metrics.dir from the tau config)")
	output := flag.String("output", filepath.Join(os.TempDir(), "tau-tool-stats.html"), "path for the HTML report")
	asJSON := flag.Bool("json", false, "print the aggregated data as JSON to stdout instead of a table")
	flag.Parse()

	if *metricsDir == "" {
		if cfg, err := tauconfig.LoadConfigAllowEmpty(); err == nil {
			*metricsDir = cfg.Metrics.Dir
		}
	}

	data, err := analyse(*sessionsDir, *metricsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(data)
	} else {
		printSummary(data)
	}

	if err := writeHTML(*output, data); err != nil {
		fmt.Fprintln(os.Stderr, "error writing report:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "\nHTML report: %s\n", *output)
}

func analyse(dir, metricsDir string) (*reportData, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading sessions dir: %w", err)
	}

	tools := map[string]*toolStat{}
	shell := map[string]*toolStat{}
	data := &reportData{
		GeneratedAt: time.Now().Format(time.RFC3339),
		SessionsDir: dir,
		Buckets:     bucketLabels,
	}
	sessionIDs := map[string]bool{}
	entryNames := make(map[string]bool, len(entries))
	for _, entry := range entries {
		entryNames[entry.Name()] = true
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !(strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".jsonl.tmp")) {
			continue
		}
		if strings.HasSuffix(name, ".jsonl.tmp") && entryNames[strings.TrimSuffix(name, ".tmp")] {
			continue
		}
		data.Files++
		sessionIDs[sessionIDFromFilename(name)] = true
		if err := analyseFile(filepath.Join(dir, name), tools, shell, data); err != nil {
			data.ParseErrors++
		}
	}
	data.UniqueSessions = len(sessionIDs)

	if metricsDir != "" {
		joinMetrics(filepath.Join(metricsDir, "metrics.jsonl"), sessionIDs, tools, data)
	}

	finalize := func(m map[string]*toolStat) []*toolStat {
		out := make([]*toolStat, 0, len(m))
		for _, t := range m {
			sort.Ints(t.Samples)
			t.Histogram = histogram(t.Samples)
			if n := len(t.Samples); n > 0 {
				t.Median = t.Samples[n/2]
				t.P90 = percentile(t.Samples, 90)
				t.P95 = percentile(t.Samples, 95)
				t.P99 = percentile(t.Samples, 99)
				t.Max = t.Samples[n-1]
			}
			if t.Calls > 0 {
				t.ErrRate = float64(t.Errors) / float64(t.Calls)
			}
			if n := len(t.durations); n > 0 {
				sort.Ints(t.durations)
				t.DurationMedian = t.durations[n/2]
				t.DurationP90 = t.durations[min(n*9/10, n-1)]
				t.DurationMax = t.durations[n-1]
			}
			if t.MetricCalls > 0 {
				t.MetricErrRate = float64(t.MetricErrors) / float64(t.MetricCalls)
			}
			if len(t.aliases) > 0 {
				for alias := range t.aliases {
					if alias != t.Name {
						t.Aliases = append(t.Aliases, alias)
					}
				}
				sort.Strings(t.Aliases)
			}
			out = append(out, t)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Calls > out[j].Calls })
		return out
	}

	data.Tools = finalize(tools)
	data.Shell = finalize(shell)
	for _, t := range data.Tools {
		data.TotalCalls += t.Calls
		data.TotalTokens += t.Tokens
	}
	sort.Slice(data.Outliers, func(i, j int) bool { return data.Outliers[i].Tokens > data.Outliers[j].Tokens })
	if len(data.Outliers) > 30 {
		data.Outliers = data.Outliers[:30]
	}
	sort.Slice(data.RepeatedReads, func(i, j int) bool {
		if data.RepeatedReads[i].Tokens == data.RepeatedReads[j].Tokens {
			return data.RepeatedReads[i].Calls > data.RepeatedReads[j].Calls
		}
		return data.RepeatedReads[i].Tokens > data.RepeatedReads[j].Tokens
	})
	if len(data.RepeatedReads) > 30 {
		data.RepeatedReads = data.RepeatedReads[:30]
	}
	sort.Slice(data.RepeatedCalls, func(i, j int) bool { return data.RepeatedCalls[i].MaxStreak > data.RepeatedCalls[j].MaxStreak })
	if len(data.RepeatedCalls) > 30 {
		data.RepeatedCalls = data.RepeatedCalls[:30]
	}
	aggregatedErrors := map[string]*errorBucket{}
	for _, bucket := range data.ErrorBreakdown {
		key := bucket.Tool + "\x00" + bucket.Kind
		aggregated := aggregatedErrors[key]
		if aggregated == nil {
			aggregated = &errorBucket{Tool: bucket.Tool, Kind: bucket.Kind}
			aggregatedErrors[key] = aggregated
		}
		aggregated.Count += bucket.Count
		for _, sample := range bucket.Samples {
			if len(aggregated.Samples) < 3 && !slices.Contains(aggregated.Samples, sample) {
				aggregated.Samples = append(aggregated.Samples, sample)
			}
		}
	}
	data.ErrorBreakdown = data.ErrorBreakdown[:0]
	for _, bucket := range aggregatedErrors {
		data.ErrorBreakdown = append(data.ErrorBreakdown, *bucket)
	}
	sort.Slice(data.ErrorBreakdown, func(i, j int) bool { return data.ErrorBreakdown[i].Count > data.ErrorBreakdown[j].Count })
	return data, nil
}

// sessionIDFromFilename extracts the session ID from a session filename of
// the form <timestamp>_<session-id>.jsonl[.tmp].
func sessionIDFromFilename(name string) string {
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".tmp"), ".jsonl")
	if _, id, ok := strings.Cut(name, "_"); ok {
		return id
	}
	return name
}

// joinMetrics reads tool.<name>.duration events from metrics.jsonl and folds
// ground-truth error status and durations into the per-tool stats. Events
// from sessions not present in the sessions dir are skipped so the metrics
// columns describe the same population as the session-derived columns.
// Missing or unreadable files are fine — the join is best-effort.
func joinMetrics(path string, sessionIDs map[string]bool, tools map[string]*toolStat, data *reportData) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	data.MetricsPath = path

	sc := bufio.NewScanner(f)
	seenCalls := map[string]bool{}
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e chat.MetricEvent
		if err := json.Unmarshal(line, &e); err != nil {
			data.ParseErrors++
			continue
		}
		if e.Category != chat.MetricCategoryTool || !strings.HasSuffix(e.Name, ".duration") {
			continue
		}
		rawToolName := e.Labels["tool"]
		if rawToolName == "" {
			continue
		}
		toolName := normalizeToolName(rawToolName)
		if !sessionIDs[e.SessionID] {
			data.MetricsSkipped++
			continue
		}
		callID := e.CallID
		if callID == "" {
			callID = e.Labels["call_id"]
		}
		if callID != "" {
			requestID := e.RequestID
			if requestID == "" {
				requestID = e.Labels["request_id"]
			}
			key := e.SessionID + "\x00" + requestID + "\x00" + callID + "\x00" + toolName
			if seenCalls[key] {
				continue
			}
			seenCalls[key] = true
		}

		t, ok := tools[toolName]
		if !ok {
			t = &toolStat{Name: toolName}
			tools[toolName] = t
		}
		if rawToolName != toolName {
			if t.aliases == nil {
				t.aliases = map[string]bool{}
			}
			t.aliases[rawToolName] = true
		}
		t.MetricCalls++
		if e.Labels["status"] == "error" {
			t.MetricErrors++
		}
		t.durations = append(t.durations, int(e.Value))
	}
	if sc.Err() != nil {
		data.ParseErrors++
	}
}

func analyseFile(path string, tools, shell map[string]*toolStat, data *reportData) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fileName := filepath.Base(path)
	sessionID := sessionIDFromFilename(fileName)

	// callID → tool call metadata the result should be attributed to.
	pending := map[string]pendingCall{}
	readUses := map[string]readUse{}
	errorBuckets := map[string]*errorBucket{}
	lastFingerprint := ""
	streak := 0
	maxStreaks := map[string]int{}
	fingerprintCalls := map[string]pendingCall{}

	get := func(m map[string]*toolStat, name string, rawAliases ...string) *toolStat {
		t, ok := m[name]
		if !ok {
			t = &toolStat{Name: name}
			m[name] = t
		}
		for _, raw := range rawAliases {
			if raw == "" || raw == name {
				continue
			}
			if t.aliases == nil {
				t.aliases = map[string]bool{}
			}
			t.aliases[raw] = true
		}
		return t
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg sessionMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			data.ParseErrors++
			continue
		}

		switch msg.Role {
		case "assistant":
			for _, tc := range msg.ToolCalls {
				toolName := normalizeToolName(tc.Function.Name)
				if toolName == "" {
					toolName = "(malformed tool call)"
				}
				readPath, readOffset, readLimit := readArgs(tc.Function.Arguments)
				t := get(tools, toolName, tc.Function.Name)
				t.Calls++
				args := summarizeArgs(tc.Function.Arguments)
				fingerprint := toolName + "\x00" + args
				if fingerprint == lastFingerprint {
					streak++
				} else {
					lastFingerprint, streak = fingerprint, 1
				}
				maxStreaks[fingerprint] = max(maxStreaks[fingerprint], streak)
				pending[tc.ID] = pendingCall{
					stat:            t,
					tool:            toolName,
					args:            args,
					readPath:        readPath,
					readOffset:      readOffset,
					readLimit:       readLimit,
					readFingerprint: fmt.Sprintf("%s:%d:%d", readPath, readOffset, readLimit),
					sessionID:       sessionID,
					file:            fileName,
					primary:         true,
				}
				fingerprintCalls[fingerprint] = pending[tc.ID]

				if toolName == "shell" {
					var args struct {
						Command string `json:"command"`
					}
					if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil && args.Command != "" {
						s := get(shell, shellLabel(args.Command))
						s.Calls++
						pending[tc.ID+"\x00shell"] = pendingCall{
							stat:      s,
							tool:      s.Name,
							args:      firstLine(args.Command, 180),
							sessionID: sessionID,
							file:      fileName,
						}
					}
				}
			}
		case "tool":
			matched := false
			record := func(call pendingCall) {
				t := call.stat
				t.Results++
				tokens := (len(msg.Content) + 3) / 4
				t.Tokens += tokens
				t.Samples = append(t.Samples, tokens)
				errKind := ""
				if msg.ToolResult != nil {
					t.StructuredResults++
					if msg.ToolResult.Status == "error" {
						errKind = msg.ToolResult.ErrorKind
						if errKind == "" {
							errKind = "execution_error"
						}
					}
				} else if call.tool == "(malformed tool call)" {
					errKind = "unknown_tool"
				} else {
					errKind = classifyLegacyErrorContent(call.tool, msg.Content)
				}
				if errKind != "" {
					t.Errors++
					if len(t.ErrorSamples) < 3 {
						t.ErrorSamples = append(t.ErrorSamples, firstLine(msg.Content, 160))
					}
					if call.primary {
						key := call.tool + "\x00" + errKind
						b := errorBuckets[key]
						if b == nil {
							b = &errorBucket{Tool: call.tool, Kind: errKind}
							errorBuckets[key] = b
						}
						b.Count++
						if len(b.Samples) < 3 {
							b.Samples = append(b.Samples, firstLine(msg.Content, 140))
						}
					}
				}
				if call.primary && call.tool == "read" && call.readPath != "" {
					canonicalPath := filepath.Clean(call.readPath)
					use := readUses[canonicalPath]
					use.calls++
					use.tokens += tokens
					if use.fingerprints == nil {
						use.fingerprints = map[string]int{}
					}
					if use.lines == nil {
						use.lines = map[int]bool{}
					}
					if use.fingerprints[call.readFingerprint] > 0 {
						use.exactDuplicates++
					}
					use.fingerprints[call.readFingerprint]++
					if call.readLimit > 0 {
						start := max(call.readOffset, 1)
						for line := start; line < start+call.readLimit; line++ {
							use.lines[line] = true
						}
						use.requestedLines += call.readLimit
					}
					readUses[canonicalPath] = use
				}
				if call.primary {
					data.Outliers = append(data.Outliers, toolOutlier{
						SessionID: call.sessionID,
						File:      call.file,
						Tool:      call.tool,
						Tokens:    tokens,
						Args:      call.args,
						Result:    firstLine(msg.Content, 180),
						ErrorKind: errKind,
					})
				}
			}
			if call, ok := pending[msg.ToolCallID]; ok {
				matched = true
				record(call)
				delete(pending, msg.ToolCallID)
			}
			if call, ok := pending[msg.ToolCallID+"\x00shell"]; ok {
				matched = true
				record(call)
				delete(pending, msg.ToolCallID+"\x00shell")
			}
			if !matched {
				data.UnmatchedResults++
			}
		}
	}
	for key, call := range pending {
		if call.primary && !strings.Contains(key, "\x00shell") {
			data.UnmatchedCalls++
		}
	}
	for p, use := range readUses {
		if use.calls <= 1 {
			continue
		}
		data.RepeatedReads = append(data.RepeatedReads, repeatedRead{
			SessionID:       sessionID,
			File:            fileName,
			Path:            p,
			Calls:           use.calls,
			Tokens:          use.tokens,
			ExactDuplicates: use.exactDuplicates,
			UniqueLines:     len(use.lines),
			OverlapRatio:    overlapRatio(use.requestedLines, len(use.lines)),
		})
	}
	for fingerprint, maxStreak := range maxStreaks {
		if maxStreak < 2 {
			continue
		}
		call := fingerprintCalls[fingerprint]
		data.RepeatedCalls = append(data.RepeatedCalls, repeatedCall{
			SessionID: sessionID, Tool: call.tool, Args: call.args, MaxStreak: maxStreak,
		})
	}
	for _, b := range errorBuckets {
		data.ErrorBreakdown = append(data.ErrorBreakdown, *b)
	}
	return sc.Err()
}

// shellLabel classifies a shell command by its first meaningful word,
// skipping leading comment lines, environment assignments, and "cd X &&"
// prefixes so commands group by what they actually run.
func shellLabel(command string) string {
	s := strings.TrimSpace(command)

	// Drop leading comment lines.
	for strings.HasPrefix(s, "#") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = strings.TrimSpace(s[i+1:])
		} else {
			return "(comment only)"
		}
	}

	for range 8 { // bounded unwrapping of prefixes
		fields := strings.Fields(s)
		if len(fields) == 0 {
			return "(empty)"
		}
		first := fields[0]

		switch {
		case first == "env", first == "sudo", first == "exec", first == "command", first == "nohup", first == "time":
			s = strings.TrimSpace(strings.TrimPrefix(s, first))
			continue
		case strings.Contains(first, "=") && !strings.HasPrefix(first, "="):
			// Environment assignment prefix (PATH=... cmd).
			s = strings.TrimSpace(strings.TrimPrefix(s, first))
			continue
		case first == "cd":
			// "cd X && rest" → classify by rest; bare cd stays cd.
			if i := strings.Index(s, "&&"); i >= 0 {
				s = strings.TrimSpace(s[i+2:])
				continue
			}
			return "cd"
		}

		// Paths like ./node_modules/.bin/vitest or /usr/bin/pnpm → basename.
		if strings.ContainsAny(first, "/") {
			return filepath.Base(first)
		}
		return first
	}
	return "(complex)"
}

func normalizeToolName(name string) string {
	n := strings.TrimSpace(name)
	switch strings.ToLower(n) {
	case "skill":
		return "skill"
	case "bash", "shell":
		return "shell"
	}
	if strings.HasPrefix(n, "tau-plugin-mcp") {
		for strings.Contains(n, "__") {
			n = strings.ReplaceAll(n, "__", "_")
		}
	}
	return n
}

func summarizeArgs(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return firstLine(raw, 220)
	}
	blob, err := json.Marshal(v)
	if err != nil {
		return firstLine(raw, 220)
	}
	return firstLine(string(blob), 220)
}

func readArgs(raw string) (path string, offset, limit int) {
	var args struct {
		Path   string `json:"path"`
		File   string `json:"file"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
		Full   bool   `json:"full"`
		Symbol string `json:"symbol"`
	}
	if json.Unmarshal([]byte(raw), &args) != nil {
		return "", 0, 0
	}
	if args.Path != "" {
		path = args.Path
	} else {
		path = args.File
	}
	if args.Limit == 0 && !args.Full && args.Symbol == "" {
		args.Limit = tools.DefaultReadLines
	}
	return path, args.Offset, args.Limit
}

// classifyLegacyErrorContent is a conservative compatibility fallback for
// sessions written before structured tool_result metadata existed.
func classifyLegacyErrorContent(tool, content string) string {
	head := content
	if len(head) > 200 {
		head = head[:200]
	}
	lower := strings.ToLower(head)
	switch {
	case strings.HasPrefix(head, "[exit code:"):
		return "shell_exit"
	case strings.HasPrefix(head, "[timeout after"):
		return "timeout"
	case strings.Contains(lower, "old_text not found"):
		return "stale_edit"
	case strings.Contains(lower, "no such file or directory"):
		return "not_found"
	case strings.Contains(lower, "regex parse error"), strings.Contains(lower, "invalid regex"):
		return "bad_regex"
	case strings.HasPrefix(lower, "path is required"), strings.HasPrefix(lower, "pattern is required"), strings.HasPrefix(lower, "command is required"), strings.Contains(lower, "cannot be empty"):
		return "invalid_args"
	case strings.Contains(lower, "escapes working directory"), strings.Contains(lower, "escaping docs sandbox"):
		return "sandbox_escape"
	case strings.Contains(lower, "is a directory"), strings.Contains(lower, "must be a directory"):
		return "wrong_path_type"
	case strings.Contains(lower, "too large"), strings.Contains(lower, "appears to be binary"):
		return "unsupported_file"
	}
	if tool == "edit" && strings.HasPrefix(lower, "edit ") {
		return "stale_edit"
	}
	for _, re := range errorPatterns {
		if re.MatchString(head) {
			return "tool_error"
		}
	}
	return ""
}

func overlapRatio(requested, unique int) float64 {
	if requested <= 0 || unique >= requested {
		return 0
	}
	return float64(requested-unique) / float64(requested)
}

func percentile(sorted []int, percent int) int {
	if len(sorted) == 0 {
		return 0
	}
	index := (len(sorted)*percent+99)/100 - 1
	return sorted[min(max(index, 0), len(sorted)-1)]
}

func firstLine(s string, maxChars int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > maxChars {
		s = s[:maxChars] + "…"
	}
	return s
}

func histogram(samples []int) []int {
	h := make([]int, len(bucketUpper)+1)
	for _, v := range samples {
		idx := len(bucketUpper)
		for i, upper := range bucketUpper {
			if v < upper {
				idx = i
				break
			}
		}
		h[idx]++
	}
	return h
}

func printSummary(data *reportData) {
	fmt.Printf("tau tool stats — %d unique sessions (%d files), %d tool calls, ~%s result tokens\n",
		data.UniqueSessions, data.Files, data.TotalCalls, formatCount(data.TotalTokens))
	if data.UnmatchedCalls > 0 || data.UnmatchedResults > 0 {
		fmt.Printf("join gaps: %d calls without results, %d results without calls\n", data.UnmatchedCalls, data.UnmatchedResults)
	}
	if data.ParseErrors > 0 {
		fmt.Printf("(%d unparseable lines skipped)\n", data.ParseErrors)
	}
	withMetrics := data.MetricsPath != ""
	if withMetrics {
		fmt.Printf("metrics join: %s (%d events outside sessions dir skipped)\n", data.MetricsPath, data.MetricsSkipped)
	}

	fmt.Printf("\n%-24s %7s %6s %6s %10s %6s %8s %8s %9s",
		"tool", "calls", "call%", "err%", "tokens", "tok%", "median", "p90", "max")
	if withMetrics {
		fmt.Printf(" %8s %7s %7s %8s %8s", "m.calls", "m.cov", "m.err%", "dur p50", "dur p90")
	}
	fmt.Println()
	for _, t := range data.Tools {
		fmt.Printf("%-24s %7d %5.1f%% %5.1f%% %10s %5.1f%% %8d %8d %9d",
			t.Name, t.Calls,
			pct(t.Calls, data.TotalCalls), t.ErrRate*100,
			formatCount(t.Tokens), pct(t.Tokens, data.TotalTokens),
			t.Median, t.P90, t.Max)
		if withMetrics {
			if t.MetricCalls > 0 {
				fmt.Printf(" %8d %6.1f%% %6.1f%% %7dms %7dms",
					t.MetricCalls, pct(t.MetricCalls, t.Calls), t.MetricErrRate*100, t.DurationMedian, t.DurationP90)
			} else {
				fmt.Printf(" %8s %7s %7s %8s %8s", "-", "-", "-", "-", "-")
			}
		}
		fmt.Println()
	}

	if len(data.Outliers) > 0 {
		fmt.Printf("\ntop result outliers:\n")
		for i, o := range data.Outliers {
			if i >= 8 {
				break
			}
			fmt.Printf("%-16s %8s tokens  %-80s  %s\n", o.Tool, formatCount(o.Tokens), o.Args, o.SessionID)
		}
	}

	if len(data.RepeatedReads) > 0 {
		fmt.Printf("\nrepeated reads:\n")
		for i, r := range data.RepeatedReads {
			if i >= 8 {
				break
			}
			fmt.Printf("%3dx %8s tokens  %2d exact dup  %5.1f%% overlap  %-70s %s\n", r.Calls, formatCount(r.Tokens), r.ExactDuplicates, r.OverlapRatio*100, r.Path, r.SessionID)
		}
	}

	if len(data.RepeatedCalls) > 0 {
		fmt.Printf("\nexact repeated-call streaks:\n")
		for i, r := range data.RepeatedCalls {
			if i >= 8 {
				break
			}
			fmt.Printf("%3dx %-18s %-90s %s\n", r.MaxStreak, r.Tool, r.Args, r.SessionID)
		}
	}

	fmt.Printf("\ntop shell commands:\n")
	for i, s := range data.Shell {
		if i >= 15 {
			break
		}
		fmt.Printf("%-24s %7d calls %10s tokens\n", s.Name, s.Calls, formatCount(s.Tokens))
	}
}

func pct(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(part) / float64(total)
}

func formatCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

var htmlTemplate = template.Must(template.New("report").Parse(`<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>Tau Tool Stats</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.9/dist/chart.umd.min.js"></script>
<style>
  body { background: #0c0c0f; color: #e4e4e7; font: 14px/1.5 system-ui, sans-serif; margin: 0; padding: 24px; }
  main { max-width: 1200px; margin: 0 auto; }
  h1 { font-size: 24px; } h2 { font-size: 18px; margin-top: 32px; }
  .meta { color: #a1a1aa; font-size: 12px; }
  table { border-collapse: collapse; width: 100%; margin-top: 12px; font-variant-numeric: tabular-nums; }
  th, td { text-align: right; padding: 6px 10px; border-bottom: 1px solid #27272a; }
  th:first-child, td:first-child { text-align: left; }
  th { color: #a1a1aa; font-weight: 500; font-size: 12px; text-transform: uppercase; }
	.err { color: #f87171; } .warn { color: #fbbf24; }
	.errmsg { color: #a1a1aa; font-size: 12px; text-align: left; }
  .muted { color: #a1a1aa; font-size: 12px; }
  .path { max-width: 520px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.charts { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; margin-top: 16px; }
	.card { background: #18181b; border-radius: 8px; padding: 16px; }
  .cards { display: grid; grid-template-columns: repeat(5, 1fr); gap: 12px; margin-top: 16px; }
  .stat { background: #18181b; border-radius: 8px; padding: 12px; }
  .stat b { display: block; font-size: 20px; }
</style>
</head>
<body>
<main>
  <h1>Tau Tool Stats</h1>
  <p class="meta" id="meta">Generated {{.GeneratedAt}} · {{.SessionsDir}} · {{.UniqueSessions}} unique sessions ({{.Files}} files) · {{.ParseErrors}} parse errors</p>

  <div class="cards">
    <div class="stat"><span class="muted">Tool calls</span><b id="total-calls"></b></div>
    <div class="stat"><span class="muted">Result tokens</span><b id="total-tokens"></b></div>
    <div class="stat"><span class="muted">Read token share</span><b id="read-share"></b></div>
    <div class="stat"><span class="muted">Metric coverage</span><b id="metric-coverage"></b></div>
    <div class="stat"><span class="muted">Structured coverage</span><b id="structured-coverage"></b></div>
  </div>

  <div class="charts">
    <div class="card"><canvas id="calls"></canvas></div>
    <div class="card"><canvas id="tokens"></canvas></div>
  </div>

  <h2>Tools</h2>
  <table id="tools-table">
    <tr id="tools-head"><th>Tool</th><th>Aliases</th><th>Calls</th><th>Call %</th><th>Structured</th><th>Errors</th><th>Err %</th><th>Est. tokens</th><th>Tok %</th><th>Median</th><th>P90</th><th>P95</th><th>P99</th><th>Max</th></tr>
  </table>

  <h2>Outlier tool results</h2>
  <table id="outliers-table">
    <tr><th>Tool</th><th>Tokens</th><th class="path">Args</th><th class="path">Result/error preview</th><th>Session</th></tr>
  </table>

  <h2>Repeated reads</h2>
  <table id="repeated-reads-table">
    <tr><th class="path">Path</th><th>Calls</th><th>Exact duplicates</th><th>Unique lines</th><th>Overlap</th><th>Est. tokens</th><th>Session</th></tr>
  </table>

  <h2>Error breakdown</h2>
  <table id="error-breakdown-table">
    <tr><th>Tool</th><th>Kind</th><th>Count</th><th class="errmsg">Samples</th></tr>
  </table>

  <h2>Exact repeated-call streaks</h2>
  <table id="repeated-calls-table">
    <tr><th>Tool</th><th>Max streak</th><th class="path">Args</th><th>Session</th></tr>
  </table>

  <h2>Error samples</h2>
  <table id="errors-table">
    <tr><th>Tool</th><th class="errmsg">Sample error messages (up to 3, first line)</th></tr>
  </table>

  <h2>Shell commands</h2>
  <table id="shell-table">
    <tr><th>Command</th><th>Calls</th><th>Errors</th><th>Err %</th><th>Est. tokens</th><th>Median</th><th>P90</th><th>Max</th></tr>
  </table>
</main>
<script>
const data = {{.JSON}};
const fmtPct = (x) => (100 * x).toFixed(1) + "%";
const fmtN = (n) => n >= 1e6 ? (n / 1e6).toFixed(1) + "M" : n >= 1e3 ? (n / 1e3).toFixed(1) + "k" : String(n);
const text = (value) => value == null ? "" : String(value);
const esc = (value) => text(value).replace(/[&<>"']/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[ch]));
const totalCalls = data.totalCalls, totalTokens = data.totalTokens;
const metricCalls = data.tools.reduce((sum, t) => sum + (t.metricCalls || 0), 0);
const structuredResults = data.tools.reduce((sum, t) => sum + (t.structuredResults || 0), 0);
const readTool = data.tools.find((t) => t.name === "read");

document.getElementById("total-calls").textContent = fmtN(totalCalls);
document.getElementById("total-tokens").textContent = fmtN(totalTokens);
document.getElementById("read-share").textContent = readTool ? fmtPct(readTool.estimatedTokens / Math.max(totalTokens, 1)) : "0.0%";
document.getElementById("metric-coverage").textContent = fmtPct(metricCalls / Math.max(totalCalls, 1));
document.getElementById("structured-coverage").textContent = fmtPct(structuredResults / Math.max(totalCalls, 1));

const withMetrics = Boolean(data.metricsPath);
const meta = document.getElementById("meta");
meta.textContent += withMetrics
  ? " · metrics join: " + data.metricsPath + " (m.* columns are ground truth only where covered; " + (data.metricsSkipped || 0) + " events outside sessions dir skipped)"
  : " · error detection is heuristic (no metrics.jsonl found; set metrics.dir in the tau config for ground truth)";

const toolsTable = document.getElementById("tools-table");
if (withMetrics) {
  document.getElementById("tools-head").innerHTML +=
    "<th>M. calls</th><th>M. cov</th><th>M. err %</th><th>Dur p50</th><th>Dur p90</th><th>Dur max</th>";
}
for (const t of data.tools) {
  const tr = document.createElement("tr");
  const errClass = t.errorRate >= 0.15 ? "err" : t.errorRate >= 0.05 ? "warn" : "";
  const aliases = (t.aliases || []).join(", ");
  tr.innerHTML = "<td>" + esc(t.name) + '</td><td class="muted">' + esc(aliases) + "</td><td>" + t.calls + "</td><td>" + fmtPct(t.calls / totalCalls) +
    "</td><td>" + (t.structuredResults || 0) + "</td><td>" + t.errors + '</td><td class="' + errClass + '">' + fmtPct(t.errorRate) +
    "</td><td>" + fmtN(t.estimatedTokens) + "</td><td>" + fmtPct(t.estimatedTokens / Math.max(totalTokens, 1)) +
    "</td><td>" + t.median + "</td><td>" + t.p90 + "</td><td>" + t.p95 + "</td><td>" + t.p99 + "</td><td>" + t.max + "</td>";
  if (withMetrics) {
    if (t.metricCalls > 0) {
      const mErrClass = t.metricErrorRate >= 0.15 ? "err" : t.metricErrorRate >= 0.05 ? "warn" : "";
      tr.innerHTML += "<td>" + t.metricCalls + "</td><td>" + fmtPct(t.metricCalls / Math.max(t.calls, 1)) +
        '</td><td class="' + mErrClass + '">' + fmtPct(t.metricErrorRate || 0) +
        "</td><td>" + (t.durationMedianMs || 0) + "ms</td><td>" + (t.durationP90Ms || 0) + "ms</td><td>" + (t.durationMaxMs || 0) + "ms</td>";
    } else {
      tr.innerHTML += "<td>-</td><td>-</td><td>-</td><td>-</td><td>-</td><td>-</td>";
    }
  }
  toolsTable.appendChild(tr);
}

const outliersTable = document.getElementById("outliers-table");
for (const o of (data.outliers || []).slice(0, 30)) {
  const tr = document.createElement("tr");
  tr.innerHTML = "<td>" + esc(o.tool) + "</td><td>" + fmtN(o.tokens || 0) + '</td><td class="path" title="' + esc(o.args) + '">' + esc(o.args) +
    '</td><td class="path" title="' + esc(o.result) + '">' + esc(o.errorKind ? "[" + o.errorKind + "] " : "") + esc(o.result) +
    '</td><td class="muted">' + esc(text(o.sessionId).slice(0, 12)) + "</td>";
  outliersTable.appendChild(tr);
}

const repeatedReadsTable = document.getElementById("repeated-reads-table");
for (const r of (data.repeatedReads || []).slice(0, 30)) {
  const tr = document.createElement("tr");
  tr.innerHTML = '<td class="path" title="' + esc(r.path) + '">' + esc(r.path) + "</td><td>" + r.calls +
    "</td><td>" + (r.exactDuplicates || 0) + "</td><td>" + (r.uniqueLines || "-") + "</td><td>" + fmtPct(r.overlapRatio || 0) +
    "</td><td>" + fmtN(r.tokens || 0) + '</td><td class="muted">' + esc(text(r.sessionId).slice(0, 12)) + "</td>";
  repeatedReadsTable.appendChild(tr);
}

const errorBreakdownTable = document.getElementById("error-breakdown-table");
for (const b of (data.errorBreakdown || []).slice(0, 30)) {
  const tr = document.createElement("tr");
  tr.innerHTML = "<td>" + esc(b.tool) + "</td><td>" + esc(b.kind) + "</td><td>" + b.count +
    '</td><td class="errmsg">' + esc((b.samples || []).join("  ·  ")) + "</td>";
  errorBreakdownTable.appendChild(tr);
}

const repeatedCallsTable = document.getElementById("repeated-calls-table");
for (const r of (data.repeatedCalls || []).slice(0, 30)) {
  const tr = document.createElement("tr");
  tr.innerHTML = "<td>" + esc(r.tool) + "</td><td>" + r.maxStreak + '</td><td class="path" title="' + esc(r.args) + '">' + esc(r.args) +
    '</td><td class="muted">' + esc(text(r.sessionId).slice(0, 12)) + "</td>";
  repeatedCallsTable.appendChild(tr);
}

const errorsTable = document.getElementById("errors-table");
for (const t of data.tools.filter((t) => (t.errorSamples || []).length > 0)) {
  const tr = document.createElement("tr");
  const cell = document.createElement("td");
  cell.className = "errmsg";
  cell.textContent = t.errorSamples.join("  ·  ");
  tr.innerHTML = "<td>" + t.name + "</td>";
  tr.appendChild(cell);
  errorsTable.appendChild(tr);
}

const shellTable = document.getElementById("shell-table");
for (const s of data.shellCommands.slice(0, 40)) {
  const tr = document.createElement("tr");
  tr.innerHTML = "<td>" + s.name + "</td><td>" + s.calls + "</td><td>" + s.errors + "</td><td>" + fmtPct(s.errorRate) +
    "</td><td>" + fmtN(s.estimatedTokens) + "</td><td>" + s.median + "</td><td>" + s.p90 + "</td><td>" + s.max + "</td>";
  shellTable.appendChild(tr);
}

if (typeof Chart !== "undefined") {
  const top = data.tools.slice(0, 10);
  const opts = { plugins: { legend: { display: false } }, scales: { x: { ticks: { color: "#a1a1aa" } }, y: { ticks: { color: "#a1a1aa" } } } };
  new Chart(document.getElementById("calls"), {
    type: "bar",
    data: { labels: top.map((t) => t.name), datasets: [{ label: "calls", data: top.map((t) => t.calls), backgroundColor: "#60a5fa" }] },
    options: { ...opts, plugins: { ...opts.plugins, title: { display: true, text: "Calls by tool", color: "#e4e4e7" } } },
  });
  const byTokens = [...data.tools].sort((a, b) => b.estimatedTokens - a.estimatedTokens).slice(0, 10);
  new Chart(document.getElementById("tokens"), {
    type: "bar",
    data: { labels: byTokens.map((t) => t.name), datasets: [{ label: "tokens", data: byTokens.map((t) => t.estimatedTokens), backgroundColor: "#f472b6" }] },
    options: { ...opts, plugins: { ...opts.plugins, title: { display: true, text: "Estimated result tokens by tool", color: "#e4e4e7" } } },
  });
}
</script>
</body>
</html>
`))

func writeHTML(path string, data *reportData) error {
	blob, err := json.Marshal(data)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return htmlTemplate.Execute(f, struct {
		*reportData
		JSON template.JS
	}{data, template.JS(blob)})
}
