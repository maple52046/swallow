// Package output renders api-server response payloads for the terminal in the
// operator-selected format: pretty JSON, YAML, or a human-readable table.
//
// Component boundary: this package presents opaque decoded JSON (Go values from
// encoding/json: maps, slices, scalars). It intentionally knows nothing about
// api-server domain types, so it renders any endpoint's payload without a
// hand-copied struct drifting from the contract. Table rendering is a
// best-effort convenience for list and object payloads; JSON and YAML are the
// lossless formats and are always available.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

// Format is the rendering mode chosen by the global --output flag.
type Format string

const (
	// FormatTable renders a column table for arrays and a key/value table for
	// objects; it falls back to JSON for payloads it cannot tabulate.
	FormatTable Format = "table"
	// FormatJSON renders indented JSON, the lossless default for scripting.
	FormatJSON Format = "json"
	// FormatYAML renders YAML, lossless and convenient for hand editing.
	FormatYAML Format = "yaml"
)

// ParseFormat validates a --output value and returns the corresponding Format.
// An unknown value is an error so a typo fails loudly instead of silently
// defaulting.
func ParseFormat(s string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(s))) {
	case FormatTable:
		return FormatTable, nil
	case FormatJSON:
		return FormatJSON, nil
	case FormatYAML:
		return FormatYAML, nil
	default:
		return "", fmt.Errorf("invalid output format %q: expected table, json, or yaml", s)
	}
}

// Printer writes rendered payloads to a destination writer in a fixed Format.
// It is stateless beyond those two fields and is safe to reuse across a command.
type Printer struct {
	Out    io.Writer
	Format Format
}

// New builds a Printer for the given writer and format.
func New(w io.Writer, format Format) *Printer {
	return &Printer{Out: w, Format: format}
}

// Print renders v according to the printer's format. For FormatTable it unwraps
// the shared pagination envelope ({items,total,page,pageSize}) and prints the
// items as a table with a trailing summary line; a plain array becomes a table;
// a single object becomes a key/value table. JSON and YAML render v as-is.
func (p *Printer) Print(v any) error {
	switch p.Format {
	case FormatJSON:
		return p.printJSON(v)
	case FormatYAML:
		return p.printYAML(v)
	default:
		return p.printTable(v)
	}
}

func (p *Printer) printJSON(v any) error {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}

func (p *Printer) printYAML(v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode yaml: %w", err)
	}
	_, err = p.Out.Write(data)
	return err
}

// printTable renders v as a table when its shape allows, otherwise falls back to
// JSON so no payload is ever unprintable. The pagination envelope is detected by
// the presence of an "items" array plus a "total" field.
func (p *Printer) printTable(v any) error {
	switch t := v.(type) {
	case map[string]any:
		if items, ok := t["items"].([]any); ok {
			if err := p.printRows(items); err != nil {
				return err
			}
			p.printPageSummary(t)
			return nil
		}
		return p.printKeyValue(t)
	case []any:
		return p.printRows(t)
	default:
		// Scalars and anything unexpected: JSON keeps the value visible.
		return p.printJSON(v)
	}
}

// printPageSummary prints the pagination counters when present so a table reader
// knows there may be more pages behind the shown rows.
func (p *Printer) printPageSummary(env map[string]any) {
	total, hasTotal := env["total"]
	if !hasTotal {
		return
	}
	page := env["page"]
	pageSize := env["pageSize"]
	fmt.Fprintf(p.Out, "\ntotal %v", total)
	if page != nil && pageSize != nil {
		fmt.Fprintf(p.Out, "  (page %v, pageSize %v)", page, pageSize)
	}
	fmt.Fprintln(p.Out)
}

// printRows renders a slice of objects as a column table. Columns are the union
// of scalar (and one-level-flattened) leaf keys across the rows, ordered with a
// curated set of common identity/status keys first and the rest alphabetically,
// then capped so a wide payload stays readable. A slice of non-objects prints
// one value per line.
func (p *Printer) printRows(items []any) error {
	if len(items) == 0 {
		fmt.Fprintln(p.Out, "(no items)")
		return nil
	}

	rows := make([]map[string]string, 0, len(items))
	keySet := map[string]struct{}{}
	scalarList := false
	for _, it := range items {
		if obj, ok := it.(map[string]any); ok {
			flat := flatten(obj)
			rows = append(rows, flat)
			for k := range flat {
				keySet[k] = struct{}{}
			}
			continue
		}
		scalarList = true
		break
	}

	if scalarList {
		for _, it := range items {
			fmt.Fprintln(p.Out, scalarString(it))
		}
		return nil
	}

	columns := orderColumns(keySet)
	tw := tabwriter.NewWriter(p.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(upperAll(columns), "\t"))
	for _, row := range rows {
		cells := make([]string, len(columns))
		for i, c := range columns {
			cells[i] = valueOrDash(row[c])
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

// printKeyValue renders a single object as a two-column key/value table using
// the same flattening as row rendering, so nested scalars (for example
// provisioning.state) each appear on their own line.
func (p *Printer) printKeyValue(obj map[string]any) error {
	flat := flatten(obj)
	keys := orderColumns(keysOf(flat))
	tw := tabwriter.NewWriter(p.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tVALUE")
	for _, k := range keys {
		fmt.Fprintf(tw, "%s\t%s\n", k, valueOrDash(flat[k]))
	}
	return tw.Flush()
}

// preferredColumns lists identity and status keys that operators scan first.
// Any present column named here is shown ahead of the alphabetical remainder.
var preferredColumns = []string{
	"id", "name", "hostname", "siteId", "kind", "type", "providerKind",
	"role", "status", "state", "provisioning.state", "provisioning.powerState",
	"health.state", "membership.platformId", "enabled", "severity", "phase",
	"fingerprint", "lifecycleState", "requestedAt", "updatedAt", "createdAt",
}

// maxColumns bounds how many columns a table shows so a wide payload stays
// readable in a terminal; JSON/YAML remain available for the full record.
const maxColumns = 9

// orderColumns sorts a key set into display order: preferred keys first in their
// listed order, then the remaining keys alphabetically, then truncated to
// maxColumns.
func orderColumns(keySet map[string]struct{}) []string {
	ordered := make([]string, 0, len(keySet))
	seen := map[string]struct{}{}
	for _, k := range preferredColumns {
		if _, ok := keySet[k]; ok {
			ordered = append(ordered, k)
			seen[k] = struct{}{}
		}
	}
	rest := make([]string, 0, len(keySet))
	for k := range keySet {
		if _, ok := seen[k]; !ok {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	ordered = append(ordered, rest...)
	if len(ordered) > maxColumns {
		ordered = ordered[:maxColumns]
	}
	return ordered
}

// flatten reduces an object to string leaf values keyed by dotted path, one
// level deep. Nested objects contribute "parent.child" scalar columns; arrays of
// scalars are joined; arrays of objects collapse to a "[n]" count; deeper
// nesting is rendered as compact JSON so a column always has a printable value.
func flatten(obj map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range obj {
		switch val := v.(type) {
		case map[string]any:
			for ck, cv := range val {
				if isScalar(cv) {
					out[k+"."+ck] = scalarString(cv)
				}
			}
			if len(val) == 0 {
				out[k] = "{}"
			}
		case []any:
			out[k] = sliceString(val)
		default:
			out[k] = scalarString(v)
		}
	}
	return out
}

// sliceString renders an array cell: scalars are comma-joined; a list that
// contains any object is shown as a bracketed count to keep the table narrow.
func sliceString(items []any) string {
	if len(items) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		if !isScalar(it) {
			return fmt.Sprintf("[%d]", len(items))
		}
		parts = append(parts, scalarString(it))
	}
	return strings.Join(parts, ",")
}

// isScalar reports whether v is a JSON scalar (string, number, bool, or null),
// which flatten treats as directly printable.
func isScalar(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	default:
		return true
	}
}

// scalarString renders a scalar JSON value compactly. Floats that are whole
// numbers print without a trailing ".0" so counts read naturally; nil prints
// empty so the caller can substitute a dash.
func scalarString(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// valueOrDash substitutes a dash for an empty cell so an absent value is visibly
// distinct from an empty string in a table.
func valueOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func keysOf(m map[string]string) map[string]struct{} {
	out := make(map[string]struct{}, len(m))
	for k := range m {
		out[k] = struct{}{}
	}
	return out
}

func upperAll(cols []string) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = strings.ToUpper(c)
	}
	return out
}
