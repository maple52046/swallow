package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// SpecSchema is the JSON Schema subset a module declares its resource spec with (the manifest's
// "spec"). It is deliberately small: one closed object whose properties are strings, whole numbers,
// booleans, or lists of strings, each with a title. The core validates a spec with it, and the
// dashboard renders a generic form and details from it for a module without its own screens, so
// every keyword it allows must mean the same to both.
type SpecSchema struct {
	Type       string                  `json:"type"`
	Properties map[string]SpecProperty `json:"properties"`
	Required   []string                `json:"required,omitempty"`
	// AdditionalProperties must be false: a key the schema does not list is refused.
	AdditionalProperties *bool `json:"additionalProperties"`
}

// The property types a SpecSchema allows.
const (
	SpecTypeString  = "string"
	SpecTypeInteger = "integer"
	SpecTypeBoolean = "boolean"
	SpecTypeArray   = "array"
)

// SpecProperty is one field of a SpecSchema. Examples are sample values (the dashboard shows the
// first as a placeholder). Enum applies to a string, Minimum and Maximum to an integer, and Items
// (always strings) to an array. Pattern applies to a non-blank string and must mean the same in Go
// (RE2) and in the dashboard (ECMAScript), so keep it to the syntax both share.
type SpecProperty struct {
	Type        string     `json:"type"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Examples    []any      `json:"examples,omitempty"`
	Enum        []string   `json:"enum,omitempty"`
	Pattern     string     `json:"pattern,omitempty"`
	Minimum     *int64     `json:"minimum,omitempty"`
	Maximum     *int64     `json:"maximum,omitempty"`
	Items       *SpecItems `json:"items,omitempty"`

	pattern *regexp.Regexp
}

// SpecItems is the item schema of an array property; the only item type is string.
type SpecItems struct {
	Type string `json:"type"`
}

var specPropertyNamePattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)

// ParseSpecSchema decodes a module's spec schema, refusing any keyword outside the subset so the
// core and the dashboard never disagree about what a schema means, and checks it is well formed.
func ParseSpecSchema(raw []byte) (*SpecSchema, error) {
	var schema SpecSchema
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&schema); err != nil {
		return nil, err
	}
	if schema.Type != "object" {
		return nil, errors.New(`type must be "object"`)
	}
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		return nil, errors.New("additionalProperties must be false")
	}
	if len(schema.Properties) == 0 {
		return nil, errors.New("properties must list at least one setting")
	}
	for name, property := range schema.Properties {
		if !specPropertyNamePattern.MatchString(name) {
			return nil, fmt.Errorf("property %q must be camelCase", name)
		}
		if err := property.prepare(); err != nil {
			return nil, fmt.Errorf("property %s: %w", name, err)
		}
		schema.Properties[name] = property
	}
	for i, name := range schema.Required {
		if _, ok := schema.Properties[name]; !ok {
			return nil, fmt.Errorf("required names %q, which is not a property", name)
		}
		if slices.Contains(schema.Required[:i], name) {
			return nil, fmt.Errorf("required lists %q twice", name)
		}
	}
	return &schema, nil
}

func (p *SpecProperty) prepare() error {
	if strings.TrimSpace(p.Title) == "" {
		return errors.New("title is required")
	}
	switch p.Type {
	case SpecTypeString, SpecTypeInteger, SpecTypeBoolean, SpecTypeArray:
	default:
		return fmt.Errorf("type %q is not string, integer, boolean, or array", p.Type)
	}
	if (p.Type == SpecTypeArray) != (p.Items != nil) {
		return errors.New("items is required for an array and allowed only there")
	}
	if p.Items != nil && p.Items.Type != SpecTypeString {
		return errors.New(`items type must be "string"`)
	}
	if len(p.Enum) > 0 && p.Type != SpecTypeString {
		return errors.New("enum applies only to a string")
	}
	for i, value := range p.Enum {
		if strings.TrimSpace(value) == "" || slices.Contains(p.Enum[:i], value) {
			return errors.New("enum values must be distinct and not blank")
		}
	}
	if p.Pattern != "" {
		if p.Type != SpecTypeString {
			return errors.New("pattern applies only to a string")
		}
		compiled, err := regexp.Compile(p.Pattern)
		if err != nil {
			return fmt.Errorf("pattern: %w", err)
		}
		p.pattern = compiled
	}
	if (p.Minimum != nil || p.Maximum != nil) && p.Type != SpecTypeInteger {
		return errors.New("minimum and maximum apply only to an integer")
	}
	if p.Minimum != nil && p.Maximum != nil && *p.Minimum > *p.Maximum {
		return errors.New("minimum is greater than maximum")
	}
	for _, example := range p.Examples {
		if err := p.check(example); err != nil {
			return fmt.Errorf("example %v %w", example, err)
		}
	}
	return nil
}

// Validate checks a spec against the schema: every key is a property, every value has the
// property's type and meets its enum, pattern, and range, and every required property is set. A
// null value, a blank string, or an empty list counts as unset, as the core's spec fields treat it.
// The error names the first offending field, for example "spec.exportPath must be a string".
func (s *SpecSchema) Validate(spec map[string]any) error {
	names := make([]string, 0, len(spec))
	for name := range spec {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		property, ok := s.Properties[name]
		if !ok {
			return fmt.Errorf("spec.%s is not a setting of this module", name)
		}
		if err := property.check(spec[name]); err != nil {
			return fmt.Errorf("spec.%s %w", name, err)
		}
	}
	for _, name := range s.Required {
		if unset(spec[name]) {
			return fmt.Errorf("spec.%s is required", name)
		}
	}
	return nil
}

// Names returns the schema's property names, sorted.
func (s *SpecSchema) Names() []string {
	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (p SpecProperty) check(value any) error {
	if value == nil {
		return nil
	}
	switch p.Type {
	case SpecTypeString:
		text, ok := value.(string)
		if !ok {
			return errors.New("must be a string")
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return nil
		}
		if len(p.Enum) > 0 && !slices.Contains(p.Enum, trimmed) {
			return fmt.Errorf("must be one of %s", strings.Join(p.Enum, ", "))
		}
		if p.pattern != nil && !p.pattern.MatchString(trimmed) {
			return fmt.Errorf("must match %s", p.Pattern)
		}
	case SpecTypeInteger:
		number, ok := wholeNumber(value)
		if !ok {
			return errors.New("must be a whole number")
		}
		if p.Minimum != nil && number < *p.Minimum {
			return fmt.Errorf("must be at least %d", *p.Minimum)
		}
		if p.Maximum != nil && number > *p.Maximum {
			return fmt.Errorf("must be at most %d", *p.Maximum)
		}
	case SpecTypeBoolean:
		if _, ok := value.(bool); !ok {
			return errors.New("must be true or false")
		}
	case SpecTypeArray:
		if !stringList(value) {
			return errors.New("must be a list of strings")
		}
	}
	return nil
}

func wholeNumber(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if number != math.Trunc(number) || math.Abs(number) > 1<<53 {
			return 0, false
		}
		return int64(number), true
	case json.Number:
		parsed, err := number.Int64()
		return parsed, err == nil
	}
	return 0, false
}

func stringList(value any) bool {
	switch items := value.(type) {
	case []string:
		return true
	case []any:
		for _, item := range items {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	}
	return false
}

func unset(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []string:
		return len(typed) == 0
	case []any:
		return len(typed) == 0
	}
	return false
}
