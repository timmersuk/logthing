package storage

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/timmersuk/logthing/internal/model"
)

var ErrInvalidFilter = errors.New("invalid regex filter")

// CompileTextFilter shares identical search semantics between history and SSE.
// Slash-delimited expressions use Go's regexp syntax; other input is literal.
func CompileTextFilter(text string) (func(model.Message) bool, error) {
	text = strings.TrimSpace(text)
	if len(text) >= 2 && strings.HasPrefix(text, "/") && strings.HasSuffix(text, "/") {
		expression, err := regexp.Compile("(?i)" + text[1:len(text)-1])
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidFilter, err)
		}
		return func(msg model.Message) bool { return expression.MatchString(messageSearchText(msg)) }, nil
	}
	text = strings.ToLower(text)
	return func(msg model.Message) bool { return text == "" || matchesText(msg, text) }, nil
}
