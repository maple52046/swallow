package domain

import (
	"errors"
	"regexp"
	"strings"
)

// MachineTag is one tag a provider knows about, in swallow's provider-neutral vocabulary.
//
// Editable separates a tag swallow may hand-assign from one the provider computes itself. A MAAS
// automatic tag carries an XPath definition and is applied by MAAS to matching hardware, so
// swallow must not try to assign or unassign it; the editor shows such a tag disabled and the API
// reports Editable=false. A manual tag (no definition) is Editable.
type MachineTag struct {
	Name     string
	Editable bool
}

// maxTagNameLen bounds a tag name so a single label cannot carry an unbounded string into storage
// or a provider request. It is generous for an operator label while staying well within a
// provider's own limits.
const maxTagNameLen = 100

// tagNamePattern is the character set swallow accepts for a tag name. It matches MAAS's own tag
// naming rule (letters, digits, and `_-.`), so a name swallow accepts is one the provider will
// accept too — a swallow-owned fallback and a MAAS-driven path therefore never disagree on which
// names are legal. Whitespace and other punctuation are rejected rather than silently rewritten.
var tagNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ErrInvalidTag means a requested tag name failed validation — it is empty, too long, or carries
// characters a tag name may not contain. It is a domain validation miss, mapped by delivery onto a
// 400, not an infrastructure failure.
var ErrInvalidTag = errors.New("invalid tag")

// NormalizeTagName trims and validates a tag name, returning the cleaned value callers must both
// store and send to a provider so surrounding whitespace never makes swallow and the provider
// disagree. An empty, over-long, or malformed name is ErrInvalidTag.
func NormalizeTagName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || len(trimmed) > maxTagNameLen || !tagNamePattern.MatchString(trimmed) {
		return "", ErrInvalidTag
	}
	return trimmed, nil
}

// NormalizeTagNames normalizes each name, dropping duplicates while preserving first-occurrence
// order. Any invalid name fails the whole set, because a partially applied tag edit is worse than
// a refused one. A nil or empty input returns a nil slice.
func NormalizeTagNames(names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(names))
	normalized := make([]string, 0, len(names))
	for _, name := range names {
		clean, err := NormalizeTagName(name)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[clean]; dup {
			continue
		}
		seen[clean] = struct{}{}
		normalized = append(normalized, clean)
	}
	return normalized, nil
}
