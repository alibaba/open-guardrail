// Package resource defines exact resource references used by policy bindings.
package resource

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxResources = 100

var kind = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

type Reference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (r Reference) Valid() bool {
	return kind.MatchString(r.Kind) && len(r.ID) > 0 && len(r.ID) <= 128 && utf8.ValidString(r.ID) &&
		strings.TrimSpace(r.ID) == r.ID && !strings.ContainsFunc(r.ID, unicode.IsControl)
}

// ValidReferences permits omission and rejects invalid or duplicate references.
func ValidReferences(refs []Reference) bool {
	if len(refs) > MaxResources {
		return false
	}
	seen := make(map[Reference]bool, len(refs))
	for _, ref := range refs {
		if !ref.Valid() || seen[ref] {
			return false
		}
		seen[ref] = true
	}
	return true
}
