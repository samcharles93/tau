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
	manifest   Extension
	state      *lua.LState
	mu         sync.Mutex
	hooks      map[Event][]*lua.LFunction
	tools      []string
	toolDefs   []tools.Tool
	commands   map[string]luaCommand
	currentCtx context.Context
	currentUI  tools.UIBridge
}

type luaCommand struct {
	command Command
	fn      *lua.LFunction
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
		commands: make(map[string]luaCommand),
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
		"log":              h.luaLog(recordDiagnostic),
		"on":               h.luaRegisterHook(),
		"register_hook":    h.luaRegisterHook(),
		"register_tool":    h.luaRegisterTool(registry),
		"register_command": h.luaRegisterCommand(),
		"confirm":          h.luaConfirm(),
		"ask":              h.luaAsk(),
	})
	h.state.SetGlobal("tau", tauTable)
}

func (h *luaExtension) luaRegisterCommand() lua.LGFunction {
	return func(l *lua.LState) int {
		schemaTable := l.CheckTable(1)
		fn := l.CheckFunction(2)
		name := strings.TrimPrefix(strings.TrimSpace(lua.LVAsString(schemaTable.RawGetString("name"))), "/")
		if name == "" {
			l.RaiseError("command name is required")
			return 0
		}
		if strings.ContainsAny(name, " \t\r\n") {
			l.RaiseError("command name must not contain whitespace")
			return 0
		}
		normalized := normalizeName(name)
		if _, exists := h.commands[normalized]; exists {
			l.RaiseError("command %q is already registered", name)
			return 0
		}
		h.commands[normalized] = luaCommand{
			command: Command{
				Name:          normalized,
				Description:   lua.LVAsString(schemaTable.RawGetString("description")),
				ExtensionName: h.manifest.Name,
			},
			fn: fn,
		}
		return 0
	}
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
		executor := func(ctx context.Context, params json.RawMessage, ui tools.UIBridge) (tools.Result, error) {
			return h.executeLuaTool(ctx, fn, params, ui)
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
		h.toolDefs = append(h.toolDefs, tools.Tool{
			Schema:  schema,
			Execute: executor,
			Source:  "extension:" + h.manifest.Name,
		})
		return 0
	}
}

func (h *luaExtension) executeLuaTool(
	ctx context.Context,
	fn *lua.LFunction,
	params json.RawMessage,
	ui tools.UIBridge,
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
	h.currentCtx = ctx
	h.currentUI = ui
	defer func() {
		h.currentCtx = nil
		h.currentUI = nil
	}()

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

func (h *luaExtension) luaConfirm() lua.LGFunction {
	return func(l *lua.LState) int {
		message := l.CheckString(1)
		title := "Confirm"
		if l.GetTop() >= 2 {
			title = l.CheckString(2)
		}
		if h.currentUI == nil {
			l.RaiseError("%s", tools.ErrInteractiveUnsupported.Error())
			return 0
		}
		ctx := h.currentCtx
		if ctx == nil {
			ctx = context.Background()
		}
		confirmed, err := h.currentUI.Confirm(ctx, title, message)
		if err != nil {
			l.RaiseError("%s", err.Error())
			return 0
		}
		l.Push(lua.LBool(confirmed))
		return 1
	}
}

func (h *luaExtension) luaAsk() lua.LGFunction {
	return func(l *lua.LState) int {
		message := l.CheckString(1)
		title := "Question"
		if l.GetTop() >= 2 {
			title = l.CheckString(2)
		}
		if h.currentUI == nil {
			l.RaiseError("%s", tools.ErrInteractiveUnsupported.Error())
			return 0
		}
		ctx := h.currentCtx
		if ctx == nil {
			ctx = context.Background()
		}
		answer, err := h.currentUI.Input(ctx, title, message)
		if err != nil {
			l.RaiseError("%s", err.Error())
			return 0
		}
		l.Push(lua.LString(answer))
		return 1
	}
}

func (h *luaExtension) hasCommand(normalized string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.commands[normalized]
	return ok
}

func (h *luaExtension) commandsSnapshot() []Command {
	h.mu.Lock()
	defer h.mu.Unlock()
	commands := make([]Command, 0, len(h.commands))
	for _, command := range h.commands {
		commands = append(commands, command.command)
	}
	return commands
}

func (h *luaExtension) executeCommand(ctx context.Context, normalized, args string, ui tools.UIBridge) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	command, ok := h.commands[normalized]
	if !ok {
		return "", fmt.Errorf("extension command %q not found", normalized)
	}
	h.currentCtx = ctx
	h.currentUI = ui
	defer func() {
		h.currentCtx = nil
		h.currentUI = nil
	}()

	argTable := h.state.NewTable()
	argTable.RawSetString("raw", lua.LString(args))
	argv := h.state.NewTable()
	if strings.TrimSpace(args) != "" {
		for i, field := range strings.Fields(args) {
			argv.RawSetInt(i+1, lua.LString(field))
		}
	}
	argTable.RawSetString("argv", argv)
	if err := h.state.CallByParam(lua.P{
		Fn:      command.fn,
		NRet:    1,
		Protect: true,
	}, argTable); err != nil {
		return "", err
	}
	result := h.state.Get(-1)
	h.state.Pop(1)
	switch result := result.(type) {
	case lua.LString:
		return string(result), nil
	case *lua.LNilType:
		return "", nil
	default:
		return lua.LVAsString(result), nil
	}
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
