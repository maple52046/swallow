package domain

import "regexp"

// DeclaredFailure is how a module classifies one failure code of its host automation (decision 077):
// the Failure Class, the next step for the user, and — when set — whether a Task retry can help.
type DeclaredFailure struct {
	Class       FailureClass
	Remediation string
	// Retryable, when set, overrides whether the failed Task may be retried.
	Retryable *bool
}

// FailureCatalog is every failure code the build's modules declare, by code. The zero value
// classifies nothing.
type FailureCatalog map[string]DeclaredFailure

// failureCodeToken finds the bracketed codes a failure message carries ("[gpu_probe_failed] ...").
var failureCodeToken = regexp.MustCompile(`\[([a-z0-9]+(?:_[a-z0-9]+)*)\]`)

// Classify returns the first code in message that the catalog declares, with its declaration. A
// failed run's message summarizes every failing host, so the first declared code is the one its
// earliest failure reported.
func (c FailureCatalog) Classify(message string) (string, DeclaredFailure, bool) {
	if len(c) == 0 {
		return "", DeclaredFailure{}, false
	}
	for _, match := range failureCodeToken.FindAllStringSubmatch(message, -1) {
		if declared, ok := c[match[1]]; ok {
			return match[1], declared, true
		}
	}
	return "", DeclaredFailure{}, false
}
