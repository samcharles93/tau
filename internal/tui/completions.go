package tui

import (
	"slices"
	"strings"

	gt "github.com/grindlemire/go-tui"

	tauchat "github.com/samcharles93/tau/internal/chat"
	"github.com/samcharles93/tau/internal/theme"
	"github.com/samcharles93/tau/internal/tui/components"
)

type completionItem struct {
	Value       string
	Label       string
	Description string
	AcceptsArgs bool
}

type completionTextArea struct {
	textarea    *gt.TextArea
	completions *gt.State[[]completionItem]
	onUp        func()
	onDown      func()
}

func newCompletionTextArea(
	value *gt.State[string],
	completions *gt.State[[]completionItem],
	onSubmit func(string),
	onUp func(),
	onDown func(),
) *completionTextArea {
	return &completionTextArea{
		textarea: gt.NewTextArea(
			gt.WithTextAreaValue(value),
			gt.WithTextAreaWidth(120),
			gt.WithTextAreaMaxHeight(inlineTextAreaMaxRows),
			gt.WithTextAreaPlaceholder("Send a message…"),
			gt.WithTextAreaPlaceholderStyle(theme.DimStyle()),
			gt.WithTextAreaTextStyle(theme.BodyStyle()),
			gt.WithTextAreaFocusColor(theme.ColorPurple),
			gt.WithTextAreaAutoFocus(true),
			gt.WithTextAreaOnSubmit(onSubmit),
		),
		completions: completions,
		onUp:        onUp,
		onDown:      onDown,
	}
}

func (i *completionTextArea) BindApp(app *gt.App) { i.textarea.BindApp(app) }

func (i *completionTextArea) Render(app *gt.App) *gt.Element { return i.textarea.Render(app) }

func (i *completionTextArea) Watchers() []gt.Watcher { return i.textarea.Watchers() }

func (i *completionTextArea) IsFocused() bool { return i.textarea.IsFocused() }

func (i *completionTextArea) Focus() { i.textarea.Focus() }

func (i *completionTextArea) Clear() { i.textarea.Clear() }

func (i *completionTextArea) SetText(value string) { i.textarea.SetText(value) }

func (i *completionTextArea) Text() string { return i.textarea.Text() }

func (i *completionTextArea) Height() int { return i.textarea.Height() }

func (i *completionTextArea) KeyMap() gt.KeyMap {
	if len(i.completions.Get()) == 0 {
		return i.textarea.KeyMap()
	}
	km := make(gt.KeyMap, 0, len(i.textarea.KeyMap()))
	for _, binding := range i.textarea.KeyMap() {
		if binding.Pattern.Key == gt.KeyUp || binding.Pattern.Key == gt.KeyDown {
			continue
		}
		km = append(km, binding)
	}
	km = append(km,
		gt.OnFocused(gt.KeyUp, func(gt.KeyEvent) { i.onUp() }),
		gt.OnFocused(gt.KeyDown, func(gt.KeyEvent) { i.onDown() }),
	)
	return km
}

func (c *ChatPanel) renderCompletions() *gt.Element {
	items := c.completions.Get()
	if len(items) == 0 {
		return nil
	}
	selected := clamp(c.completionIndex.Get(), 0, len(items)-1)
	container := gt.New(
		gt.WithDisplay(gt.DisplayFlex),
		gt.WithDirection(gt.Column),
		gt.WithFlexShrink(0),
		gt.WithGap(0),
	)
	// Compute visible window around the selected index.
	maxRows := min(inlineCompletionMaxRows, len(items))
	startRow := max(selected-maxRows/2, 0)
	if end := startRow + maxRows; end > len(items) {
		startRow = len(items) - maxRows
	}
	endRow := startRow + maxRows
	for i := startRow; i < endRow; i++ {
		item := items[i]
		prefix := "  "
		style := theme.DimStyle()
		if i == selected {
			prefix = "› "
			style = theme.BrandStyle()
		}
		line := prefix + item.Label
		if item.Description != "" {
			line += " — " + item.Description
		}
		container.AddChild(gt.New(
			gt.WithText(line),
			gt.WithTextStyle(style),
			gt.WithTruncate(true),
		))
	}
	return container
}

func (c *ChatPanel) syncCompletions(value string) {
	items := c.completionItems(value)
	c.completions.Set(items)
	if len(items) == 0 {
		c.completionIndex.Set(0)
		return
	}
	c.completionIndex.Set(clamp(c.completionIndex.Get(), 0, len(items)-1))
}

func (c *ChatPanel) completionItems(value string) []completionItem {
	if !strings.HasPrefix(value, "/") {
		return nil
	}
	parts := strings.SplitN(value, " ", 2)
	commandPrefix := strings.TrimPrefix(strings.ToLower(parts[0]), "/")
	if len(parts) == 1 {
		return c.commandCompletions(commandPrefix)
	}

	argPrefix := parts[1]
	switch commandPrefix {
	case "model":
		return c.modelCompletions(strings.TrimSpace(argPrefix))
	case "reasoning":
		return c.reasoningCompletions(strings.TrimSpace(argPrefix))
	case "session":
		return c.sessionCompletions(strings.TrimSpace(argPrefix))
	case "debug":
		return c.debugCompletions(argPrefix)
	default:
		return nil
	}
}

func (c *ChatPanel) commandCompletions(prefix string) []completionItem {
	commands := builtinCompletionItems(c.cfg.Debug)
	for _, ext := range c.sortedExtensionCommands() {
		commands = append(commands, completionItem{
			Value:       "/" + ext.Name,
			Label:       "/" + ext.Name,
			Description: ext.Description,
		})
	}

	matches := make([]completionItem, 0, len(commands))
	for _, item := range commands {
		name := strings.TrimPrefix(strings.ToLower(item.Label), "/")
		if prefix == "" || strings.HasPrefix(name, prefix) {
			matches = append(matches, item)
		}
	}
	return matches
}

func (c *ChatPanel) modelCompletions(prefix string) []completionItem {
	prefix = strings.ToLower(prefix)
	models := slices.Clone(c.availableModels.Get())
	slices.SortFunc(models, func(a, b tauchat.ChatModelRef) int {
		return strings.Compare(a.ID, b.ID)
	})
	items := make([]completionItem, 0, len(models))
	for _, model := range models {
		if !model.Ready {
			continue
		}
		if prefix != "" && !strings.Contains(strings.ToLower(model.ID), prefix) {
			continue
		}
		items = append(items, completionItem{
			Value:       "/model " + model.ID,
			Label:       model.ID,
			Description: model.URL,
		})
	}
	return items
}

func (c *ChatPanel) reasoningCompletions(prefix string) []completionItem {
	options := []completionItem{
		{Value: "/reasoning on", Label: "on", Description: "show reasoning before Tau responses"},
		{Value: "/reasoning off", Label: "off", Description: "hide reasoning"},
		{Value: "/reasoning toggle", Label: "toggle", Description: "toggle reasoning visibility"},
	}
	prefix = strings.ToLower(prefix)
	matches := make([]completionItem, 0, len(options))
	for _, item := range options {
		if prefix == "" || strings.HasPrefix(item.Label, prefix) {
			matches = append(matches, item)
		}
	}
	return matches
}

func (c *ChatPanel) sessionCompletions(prefix string) []completionItem {
	options := []completionItem{
		{Value: "/session list", Label: "list", Description: "show saved sessions"},
		{Value: "/session tree", Label: "tree", Description: "show session tree dashboard"},
		{Value: "/session info ", Label: "info", Description: "show session details", AcceptsArgs: true},
		{Value: "/session export ", Label: "export", Description: "export a session", AcceptsArgs: true},
		{Value: "/session delete ", Label: "delete", Description: "delete a session", AcceptsArgs: true},
	}
	prefix = strings.ToLower(prefix)
	matches := make([]completionItem, 0, len(options))
	for _, item := range options {
		if prefix == "" || strings.HasPrefix(item.Label, prefix) {
			matches = append(matches, item)
		}
	}
	return matches
}

func (c *ChatPanel) debugCompletions(arg string) []completionItem {
	parts := strings.Fields(arg)

	// Case 1: user typed "/debug" or "/debug " or "/debug sub" -> suggest subcommands
	if len(parts) == 0 || (len(parts) == 1 && !strings.HasSuffix(arg, " ")) {
		prefix := ""
		if len(parts) == 1 {
			prefix = strings.ToLower(parts[0])
		}
		options := []completionItem{
			{Value: "/debug components", Label: "components", Description: "list/preview TUI components"},
		}
		matches := make([]completionItem, 0, len(options))
		for _, item := range options {
			if prefix == "" || strings.HasPrefix(item.Label, prefix) {
				matches = append(matches, item)
			}
		}
		return matches
	}

	// Case 2: user typed "/debug components " or "/debug components name"
	sub := strings.ToLower(parts[0])
	if sub == "components" {
		prefix := ""
		if len(parts) > 1 {
			prefix = strings.ToLower(parts[1])
		}
		options := []completionItem{}
		for _, name := range components.ListNames() {
			options = append(options, completionItem{
				Value:       "/debug components " + name,
				Label:       name,
				Description: "Debug component: " + name,
			})
		}
		matches := make([]completionItem, 0, len(options))
		for _, item := range options {
			if prefix == "" || strings.HasPrefix(item.Label, prefix) {
				matches = append(matches, item)
			}
		}
		return matches
	}

	return nil
}

func (c *ChatPanel) sortedExtensionCommands() []tauchat.ExtensionCommand {
	commands := c.extensionCommands.Get()
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]tauchat.ExtensionCommand, 0, len(names))
	for _, name := range names {
		out = append(out, commands[name])
	}
	return out
}

func (c *ChatPanel) shouldApplyCompletion(value string) bool {
	items := c.completions.Get()
	if len(items) == 0 {
		return false
	}
	item := items[clamp(c.completionIndex.Get(), 0, len(items)-1)]
	return item.Value != value
}

func (c *ChatPanel) applySelectedCompletion() (completed string, acceptsArgs bool) {
	items := c.completions.Get()
	if len(items) == 0 {
		return "", false
	}
	item := items[clamp(c.completionIndex.Get(), 0, len(items)-1)]
	c.setInputText(item.Value)
	return item.Value, item.AcceptsArgs
}

func (c *ChatPanel) selectCompletion(delta int) {
	items := c.completions.Get()
	if len(items) == 0 {
		return
	}
	idx := c.completionIndex.Get() + delta
	if idx < 0 {
		idx = len(items) - 1
	} else if idx >= len(items) {
		idx = 0
	}
	c.completionIndex.Set(idx)
}

func (c *ChatPanel) closeCompletions() {
	c.completions.Set(nil)
	c.completionIndex.Set(0)
}

func builtinCompletionItems(debug bool) []completionItem {
	items := []completionItem{
		{Value: "/new", Label: "/new", Description: "start a new conversation"},
		{Value: "/system ", Label: "/system", Description: "set system prompt", AcceptsArgs: true},
		{Value: "/model ", Label: "/model", Description: "switch model", AcceptsArgs: true},
		{Value: "/refresh", Label: "/refresh", Description: "refresh models"},
		{Value: "/reload", Label: "/reload", Description: "reload extensions while idle"},
		{Value: "/reasoning ", Label: "/reasoning", Description: "show or hide reasoning", AcceptsArgs: true},
		{Value: "/settings", Label: "/settings", Description: "show settings help"},
		{Value: "/session ", Label: "/session", Description: "manage saved sessions", AcceptsArgs: true},
		{Value: "/resume", Label: "/resume", Description: "resume a saved session"},
	}
	if debug {
		items = append(items, completionItem{Value: "/debug ", Label: "/debug", Description: "preview TUI components (developer)", AcceptsArgs: true})
	}
	items = append(items, completionItem{Value: "/exit", Label: "/exit", Description: "quit"})
	return items
}
