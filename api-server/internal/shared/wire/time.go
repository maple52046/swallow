// Package wire holds helpers for shaping values on the API boundary.
package wire

import "time"

// Time formats a timestamp for an API response. All timestamps are RFC 3339 in UTC.
func Time(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// TimePtr formats an optional timestamp, returning nil for the zero value so that an
// unobserved time is null rather than a misleading year-one date.
func TimePtr(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	formatted := Time(t)
	return &formatted
}

// String returns a pointer to s, or nil when s is empty.
//
// The API contract requires an optional-but-unset field to be null rather than an
// empty string, so that "no hostname yet" is distinguishable from "hostname is blank".
func String(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Strings returns a non-nil slice, so that a list-valued field serializes as [] rather
// than null.
func Strings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
