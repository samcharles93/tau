// Command tool-stats analyses tau session files and reports how the agent's
// tools are actually used: call counts, result sizes, estimated token cost,
// and error rates per tool, plus a breakdown of what shell commands run.
//
// Usage:
//
//	go run ./scripts/tool-stats [--sessions-dir <dir>] [--output <file.html>] [--json]
//
// It reads every *.jsonl and *.jsonl.tmp session file, prints a summary table
// to stdout, and writes a self-contained HTML report.
//
// Error detection is heuristic: sessions persist tool result text but not the
// is_error flag, so results are matched against known tau error shapes.
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
	"sort"
	"strings"
	"time"
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
	Name      string  `json:"name"`
	Calls     int     `json:"calls"`
	Results   int     `json:"results"`
	Errors    int     `json:"errors"`
	Tokens    int     `json:"estimatedTokens"`
	Samples   []int   `json:"-"`
	Histogram []int   `json:"histogram"`
	ErrRate   float64 `json:"errorRate"`
	Median    int     `json:"median"`
	P90       int     `json:"p90"`
	Max       int     `json:"max"`
	// ErrorSamples holds up to three distinct error messages for the report.
	ErrorSamples []string `json:"errorSamples,omitempty"`
}

type reportData struct {
	GeneratedAt string      `json:"generatedAt"`
	SessionsDir string      `json:"sessionsDir"`
	Files       int         `json:"files"`
	ParseErrors int         `json:"parseErrors"`
	TotalCalls  int         `json:"totalCalls"`
	TotalTokens int         `json:"totalTokens"`
	Buckets     []string    `json:"bucketLabels"`
	Tools       []*toolStat `json:"tools"`
	Shell       []*toolStat `json:"shellCommands"`
}

// sessionMessage is the subset of a persisted chat message the script needs.
type sessionMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id"`
	ToolCalls  []struct {
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
	output := flag.String("output", filepath.Join(os.TempDir(), "tau-tool-stats.html"), "path for the HTML report")
	asJSON := flag.Bool("json", false, "print the aggregated data as JSON to stdout instead of a table")
	flag.Parse()

	data, err := analyse(*sessionsDir)
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

func analyse(dir string) (*reportData, error) {
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

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !(strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".jsonl.tmp")) {
			continue
		}
		data.Files++
		if err := analyseFile(filepath.Join(dir, name), tools, shell, data); err != nil {
			data.ParseErrors++
		}
	}

	finalize := func(m map[string]*toolStat) []*toolStat {
		out := make([]*toolStat, 0, len(m))
		for _, t := range m {
			sort.Ints(t.Samples)
			t.Histogram = histogram(t.Samples)
			if n := len(t.Samples); n > 0 {
				t.Median = t.Samples[n/2]
				t.P90 = t.Samples[min(n*9/10, n-1)]
				t.Max = t.Samples[n-1]
			}
			if t.Calls > 0 {
				t.ErrRate = float64(t.Errors) / float64(t.Calls)
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
	return data, nil
}

func analyseFile(path string, tools, shell map[string]*toolStat, data *reportData) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// callID → tool stat entry the result should be attributed to.
	pending := map[string]*toolStat{}

	get := func(m map[string]*toolStat, name string) *toolStat {
		t, ok := m[name]
		if !ok {
			t = &toolStat{Name: name}
			m[name] = t
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
				t := get(tools, tc.Function.Name)
				t.Calls++
				pending[tc.ID] = t

				if tc.Function.Name == "shell" {
					var args struct {
						Command string `json:"command"`
					}
					if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil && args.Command != "" {
						s := get(shell, shellLabel(args.Command))
						s.Calls++
						pending[tc.ID+"\x00shell"] = s
					}
				}
			}
		case "tool":
			record := func(t *toolStat) {
				t.Results++
				tokens := (len(msg.Content) + 3) / 4
				t.Tokens += tokens
				t.Samples = append(t.Samples, tokens)
				if isErrorContent(msg.Content) {
					t.Errors++
					if len(t.ErrorSamples) < 3 {
						t.ErrorSamples = append(t.ErrorSamples, firstLine(msg.Content, 160))
					}
				}
			}
			if t, ok := pending[msg.ToolCallID]; ok {
				record(t)
				delete(pending, msg.ToolCallID)
			}
			if s, ok := pending[msg.ToolCallID+"\x00shell"]; ok {
				record(s)
				delete(pending, msg.ToolCallID+"\x00shell")
			}
		}
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

func isErrorContent(content string) bool {
	head := content
	if len(head) > 200 {
		head = head[:200]
	}
	for _, re := range errorPatterns {
		if re.MatchString(head) {
			return true
		}
	}
	return false
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
	fmt.Printf("tau tool stats — %d session files, %d tool calls, ~%s result tokens\n",
		data.Files, data.TotalCalls, formatCount(data.TotalTokens))
	if data.ParseErrors > 0 {
		fmt.Printf("(%d unparseable lines skipped)\n", data.ParseErrors)
	}

	fmt.Printf("\n%-24s %7s %6s %6s %10s %6s %8s %8s %9s\n",
		"tool", "calls", "call%", "err%", "tokens", "tok%", "median", "p90", "max")
	for _, t := range data.Tools {
		fmt.Printf("%-24s %7d %5.1f%% %5.1f%% %10s %5.1f%% %8d %8d %9d\n",
			t.Name, t.Calls,
			pct(t.Calls, data.TotalCalls), t.ErrRate*100,
			formatCount(t.Tokens), pct(t.Tokens, data.TotalTokens),
			t.Median, t.P90, t.Max)
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
  .charts { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; margin-top: 16px; }
  .card { background: #18181b; border-radius: 8px; padding: 16px; }
</style>
</head>
<body>
<main>
  <h1>Tau Tool Stats</h1>
  <p class="meta">Generated {{.GeneratedAt}} · {{.SessionsDir}} · {{.Files}} session files · {{.ParseErrors}} parse errors · error detection is heuristic (is_error is not persisted in sessions)</p>

  <div class="charts">
    <div class="card"><canvas id="calls"></canvas></div>
    <div class="card"><canvas id="tokens"></canvas></div>
  </div>

  <h2>Tools</h2>
  <table id="tools-table">
    <tr><th>Tool</th><th>Calls</th><th>Call %</th><th>Errors</th><th>Err %</th><th>Est. tokens</th><th>Tok %</th><th>Median</th><th>P90</th><th>Max</th></tr>
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
const totalCalls = data.totalCalls, totalTokens = data.totalTokens;

const toolsTable = document.getElementById("tools-table");
for (const t of data.tools) {
  const tr = document.createElement("tr");
  const errClass = t.errorRate >= 0.15 ? "err" : t.errorRate >= 0.05 ? "warn" : "";
  tr.innerHTML = "<td>" + t.name + "</td><td>" + t.calls + "</td><td>" + fmtPct(t.calls / totalCalls) +
    "</td><td>" + t.errors + '</td><td class="' + errClass + '">' + fmtPct(t.errorRate) +
    "</td><td>" + fmtN(t.estimatedTokens) + "</td><td>" + fmtPct(t.estimatedTokens / Math.max(totalTokens, 1)) +
    "</td><td>" + t.median + "</td><td>" + t.p90 + "</td><td>" + t.max + "</td>";
  toolsTable.appendChild(tr);
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
