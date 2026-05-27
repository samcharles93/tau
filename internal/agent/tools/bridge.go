package tools

import (
	"context"
	"errors"
)

var (
	ErrInteractiveUnsupported = errors.New("interactive prompts are not supported in this mode")
	ErrInteractiveCanceled    = errors.New("interactive prompt was canceled")
)

type NonInteractiveBridge struct{}

func (NonInteractiveBridge) Confirm(context.Context, string, string) (bool, error) {
	return false, ErrInteractiveUnsupported
}

func (NonInteractiveBridge) Select(context.Context, string, []string) (string, error) {
	return "", ErrInteractiveUnsupported
}

func (NonInteractiveBridge) Input(context.Context, string, string) (string, error) {
	return "", ErrInteractiveUnsupported
}

func (NonInteractiveBridge) Notify(string, string) {}
