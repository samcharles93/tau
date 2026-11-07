package agent

import (
	"errors"
	"net/http"
	"strings"

	aisdkchat "github.com/samcharles93/ai-sdk/chat"
	errx "github.com/samcharles93/ai-sdk/error"
)

// isUnsupportedMediaError reports whether err is the provider rejecting the
// request's multimodal content (e.g. image parts sent to a text-only model).
// Two shapes are recognised: the SDK's typed ErrUnsupportedContent, and an
// HTTP 400/422 whose body blames the content (image/vision/modality wording).
func isUnsupportedMediaError(err error) bool {
	if errors.Is(err, aisdkchat.ErrUnsupportedContent) {
		return true
	}
	var perr *errx.ProviderError
	if errors.As(err, &perr) && (perr.StatusCode == http.StatusBadRequest || perr.StatusCode == http.StatusUnprocessableEntity) {
		msg := strings.ToLower(perr.Message + " " + perr.Base.Error())
		for _, kw := range []string{"image", "vision", "multimodal", "modality", "content part", "unsupported content"} {
			if strings.Contains(msg, kw) {
				return true
			}
		}
	}
	return false
}
