package domain

import (
	"errors"
	"regexp"
	"strings"
)

// MachineTag is one tag a provider knows about, in swallow's provider-neutral vocabulary.
//
// Editable separates a manual tag, which swallow owns through the Server specs, from an automatic
// tag the provider computes or maintains itself (decision 071). An automatic tag is observed only:
// swallow never assigns it, never puts it in a spec, and never treats a change the provider makes to
// it as drift. Which provider tags are automatic is the adapter's judgment; core never names them.
//
// Removable says whether swallow may still take the tag off a machine, as a one-off action. It is
// always true for a manual tag. For an automatic tag it is true when the provider maintains the tag
// without a rule, so it may apply it again later and swallow accepts that, and false when the
// provider computes it from a rule, because the rule would re-apply it: such a tag can only be
// deleted.
type MachineTag struct {
	Name      string
	Editable  bool
	Removable bool
}

// ErrTagNotFound means a tag to delete or rename is unknown to the Site: its provisioner's catalog
// does not hold it and no Server of that provisioner names it in its spec. It is a domain miss,
// mapped by delivery onto a 404.
var ErrTagNotFound = errors.New("tag not found")

// maxTagNameLen bounds a tag name so a single label cannot carry an unbounded string into storage
// or a provider request. It is generous for a user label while staying well within a
// provider's own limits.
const maxTagNameLen = 100

// tagNamePattern is the character set swallow accepts for a tag name: letters, digits, and `_-.`,
// which provisioners accept as a tag name, so a spec tag swallow accepts is one the provisioner can
// realize. Whitespace and other punctuation are rejected rather than silently rewritten.
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
