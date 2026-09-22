package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func decode(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return v
}

func render(t *testing.T, format Format, raw string) string {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := New(buf, format).Print(decode(t, raw)); err != nil {
		t.Fatalf("Print: %v", err)
	}
	return buf.String()
}

// TestTablePaginationEnvelope renders the shared {items,total,page,pageSize}
// envelope as a table with a trailing summary, flattening a nested field into a
// dotted column so it stays visible.
func TestTablePaginationEnvelope(t *testing.T) {
	out := render(t, FormatTable, `{
		"items":[
			{"id":"s1","hostname":"gpu-1","provisioning":{"state":"deployed"}},
			{"id":"s2","hostname":"gpu-2","provisioning":{"state":"ready"}}
		],
		"total":2,"page":1,"pageSize":20
	}`)
	for _, want := range []string{"ID", "HOSTNAME", "PROVISIONING.STATE", "gpu-1", "deployed", "total 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}

// TestTablePlainArray renders a plain array (no envelope) as a column table.
func TestTablePlainArray(t *testing.T) {
	out := render(t, FormatTable, `[{"id":"z1","name":"rack-a"},{"id":"z2","name":"rack-b"}]`)
	if !strings.Contains(out, "rack-a") || !strings.Contains(out, "rack-b") {
		t.Errorf("array table missing rows:\n%s", out)
	}
}

// TestTableSingleObject renders one object as a key/value table.
func TestTableSingleObject(t *testing.T) {
	out := render(t, FormatTable, `{"id":"u1","username":"admin","role":"admin"}`)
	if !strings.Contains(out, "FIELD") || !strings.Contains(out, "username") || !strings.Contains(out, "admin") {
		t.Errorf("key/value table unexpected:\n%s", out)
	}
}

// TestEmptyItems renders an explicit placeholder rather than an empty table.
func TestEmptyItems(t *testing.T) {
	out := render(t, FormatTable, `{"items":[],"total":0,"page":1,"pageSize":20}`)
	if !strings.Contains(out, "(no items)") {
		t.Errorf("expected empty placeholder:\n%s", out)
	}
}

// TestJSONAndYAML confirm the lossless formats emit parseable output.
func TestJSONAndYAML(t *testing.T) {
	j := render(t, FormatJSON, `{"a":1,"b":"x"}`)
	if !strings.Contains(j, "\"a\": 1") {
		t.Errorf("json output unexpected:\n%s", j)
	}
	y := render(t, FormatYAML, `{"a":1,"b":"x"}`)
	if !strings.Contains(y, "a: 1") || !strings.Contains(y, "b: x") {
		t.Errorf("yaml output unexpected:\n%s", y)
	}
}

// TestParseFormat validates accepted values and rejects an unknown one.
func TestParseFormat(t *testing.T) {
	for _, ok := range []string{"table", "json", "yaml", "TABLE"} {
		if _, err := ParseFormat(ok); err != nil {
			t.Errorf("ParseFormat(%q) unexpected error: %v", ok, err)
		}
	}
	if _, err := ParseFormat("xml"); err == nil {
		t.Error("ParseFormat(xml) should error")
	}
}
