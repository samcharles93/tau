package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/samcharles93/tau/internal/agent/tools"
	lua "github.com/yuin/gopher-lua"
)

type luaExtension struct {
	manifest Extension
	state    *lua.LState
	mu       sync.Mutex
	hooks    map[Event][]*lua.LFunction
	tools    []string
}

func newLuaExtension(
	ext Extension,
	registry *tools.Registry,
	recordDiagnostic func(Diagnostic),
) (*luaExtension, error) {
	if registry == nil {
		return nil, errors.New("tool registry is required")
	}

	host := &luaExtension{
		manifest: ext,
		state:    lua.NewState(),
		hooks:    make(map[Event][]*lua.LFunction),
	}
	host.injectTauAPI(registry, recordDiagnostic)
	if err := host.state.DoFile(ext.Entry); err != nil {
		for _, toolName := range host.tools {
			registry.Unregister(toolName)
		}
		host.state.Close()
		return nil, fmt.Errorf("load lua entry: %w", err)
	}
	return host, nil
}

func (h *luaExtension) injectTauAPI(
	registry *tools.Registry,
	recordDiagnostic func(Diagnostic),
) {
	tauTable := h.state.NewTable()
	h.state.SetFuncs(tauTable, map[string]lua.LGFunction{
		"log":           h.luaLog(recordDiagnostic),
		"on":            h.luaRegisterHook(),
		"register_hook": h.luaRegisterHook(),
		"register_tool": h.luaRegisterTool(registry),
	})
	h.state.SetGlobal("tau", tauTable)
}

func (h *luaExtension) luaLog(recordDiagnostic func(Diagnostic)) lua.LGFunction {
	return func(l *lua.LState) int {
		level := strings.ToLower(strings.TrimSpace(l.CheckString(1)))
		message := l.CheckString(2)
		severity := SeverityInfo
		switch level {
		case "warn", "warning":
			severity = SeverityWarning
		case "error":
			severity = SeverityError
		}
		slog.Default().Log(context.Background(), slogLevel(level), message, "extension", h.manifest.Name)
		if recordDiagnostic != nil {
			recordDiagnostic(Diagnostic{
				Path:          h.manifest.Entry,
				ExtensionName: h.manifest.Name,
				Severity:      severity,
				Message:       message,
			})
		}
		return 0
	}
}

func (h *luaExtension) luaRegisterHook() lua.LGFunction {
	return func(l *lua.LState) int {
		event := Event(l.CheckString(1))
		fn := l.CheckFunction(2)
		if _, ok := safeEvents[event]; !ok {
			l.RaiseError("unsupported event %q", event)
			return 0
		}
		h.hooks[event] = append(h.hooks[event], fn)
		return 0
	}
}

func (h *luaExtension) luaRegisterTool(registry *tools.Registry) lua.LGFunction {
	return func(l *lua.LState) int {
		schemaTable := l.CheckTable(1)
		fn := l.CheckFunction(2)
		schema, err := schemaFromLua(schemaTable)
		if err != nil {
			l.RaiseError("%s", err.Error())
			return 0
		}
		toolName := schema.Name
		executor := func(ctx context.Context, params json.RawMessage, _ tools.UIBridge) (tools.Result, error) {
			return h.executeLuaTool(ctx, fn, params)
		}
		if err := registry.Register(tools.Tool{
			Schema:  schema,
			Execute: executor,
			Source:  "extension:" + h.manifest.Name,
		}); err != nil {
			l.RaiseError("%s", err.Error())
			return 0
		}
		h.tools = append(h.tools, toolName)
		return 0
	}
}

func (h *luaExtension) executeLuaTool(
	ctx context.Context,
	fn *lua.LFunction,
	params json.RawMessage,
) (tools.Result, error) {
	select {
	case <-ctx.Done():
		return tools.Result{}, ctx.Err()
	default:
	}

	var decoded any
	if len(params) == 0 {
		decoded = map[string]any{}
	} else if err := json.Unmarshal(params, &decoded); err != nil {
		return tools.Result{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	args := goToLua(h.state, decoded)
	if err := h.state.CallByParam(lua.P{
		Fn:      fn,
		NRet:    1,
		Protect: true,
	}, args); err != nil {
		return tools.Result{}, err
	}
	result := h.state.Get(-1)
	h.state.Pop(1)
	return toolResultFromLua(result), nil
}

func (h *luaExtension) dispatch(event Event, ctx map[string]any) []Diagnostic {
	if _, ok := safeEvents[event]; !ok {
		return []Diagnostic{{
			Path:          h.manifest.Entry,
			ExtensionName: h.manifest.Name,
			Severity:      SeverityError,
			Message:       fmt.Sprintf("unsupported event %q", event),
		}}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	var diagnostics []Diagnostic
	arg := goToLua(h.state, ctx)
	for _, hook := range h.hooks[event] {
		if err := h.state.CallByParam(lua.P{Fn: hook, NRet: 0, Protect: true}, arg); err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Path:          h.manifest.Entry,
				ExtensionName: h.manifest.Name,
				Severity:      SeverityError,
				Message:       fmt.Sprintf("%s hook: %v", event, err),
			})
		}
	}
	return diagnostics
}

func (h *luaExtension) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state.Close()
}

func schemaFromLua(table *lua.LTable) (tools.Schema, error) {
	name := strings.TrimSpace(lua.LVAsString(table.RawGetString("name")))
	if name == "" {
		return tools.Schema{}, errors.New("tool schema name is required")
	}
	description := lua.LVAsString(table.RawGetString("description"))
	parameters := json.RawMessage(`{"type":"object"}`)
	if value := table.RawGetString("parameters"); value != lua.LNil {
		raw, err := json.Marshal(luaToGo(value))
		if err != nil {
			return tools.Schema{}, fmt.Errorf("encode tool parameters: %w", err)
		}
		parameters = raw
	}
	return tools.Schema{Name: name, Description: description, Parameters: parameters}, nil
}

func luaToGo(value lua.LValue) any {
	switch value := value.(type) {
	case lua.LBool:
		return bool(value)
	case lua.LNumber:
		return float64(value)
	case lua.LString:
		return string(value)
	case *lua.LTable:
		if isLuaArray(value) {
			items := make([]any, 0, value.Len())
			for i := 1; i <= value.Len(); i++ {
				items = append(items, luaToGo(value.RawGetInt(i)))
			}
			return items
		}
		obj := make(map[string]any)
		value.ForEach(func(key, val lua.LValue) {
			obj[lua.LVAsString(key)] = luaToGo(val)
		})
		return obj
	case *lua.LNilType:
		return nil
	default:
		return lua.LVAsString(value)
	}
}

func goToLua(l *lua.LState, value any) lua.LValue {
	switch value := value.(type) {
	case nil:
		return lua.LNil
	case bool:
		return lua.LBool(value)
	case float64:
		return lua.LNumber(value)
	case string:
		return lua.LString(value)
	case []any:
		table := l.NewTable()
		for i, item := range value {
			table.RawSetInt(i+1, goToLua(l, item))
		}
		return table
	case map[string]any:
		table := l.NewTable()
		for key, item := range value {
			table.RawSetString(key, goToLua(l, item))
		}
		return table
	default:
		return lua.LString(fmt.Sprint(value))
	}
}

func toolResultFromLua(value lua.LValue) tools.Result {
	switch value := value.(type) {
	case lua.LString:
		return tools.Result{Content: string(value)}
	case *lua.LTable:
		content := lua.LVAsString(value.RawGetString("content"))
		if content == "" {
			content = lua.LVAsString(value.RawGetString("text"))
		}
		return tools.Result{
			Content: content,
			Details: luaToGo(value.RawGetString("details")),
			IsError: lua.LVAsBool(value.RawGetString("is_error")),
		}
	default:
		return tools.Result{Content: lua.LVAsString(value)}
	}
}

func isLuaArray(table *lua.LTable) bool {
	if table.Len() == 0 {
		return false
	}
	isArray := true
	table.ForEach(func(key, _ lua.LValue) {
		if _, ok := key.(lua.LNumber); !ok {
			isArray = false
		}
	})
	return isArray
}

func slogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
