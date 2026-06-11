package app

import (
	"fmt"

	"github.com/google/uuid"
)

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generating id: %w", err)
	}
	return id.String(), nil
}
