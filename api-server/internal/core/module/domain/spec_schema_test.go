package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const testSpecSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["mountPath"],
  "properties": {
    "mountPath": {"type": "string", "title": "Mount path", "pattern": "^/", "examples": ["/shared"]},
    "mode": {"type": "string", "title": "Mode", "enum": ["rw", "ro"]},
    "port": {"type": "integer", "title": "Port", "minimum": 1, "maximum": 65535, "examples": [8001]},
    "enabled": {"type": "boolean", "title": "Enabled"},
    "targets": {"type": "array", "title": "Targets", "items": {"type": "string"}}
  }
}`

func mustSpecSchema(t *testing.T) *SpecSchema {
	t.Helper()
	schema, err := ParseSpecSchema([]byte(testSpecSchema))
	if err != nil {
		t.Fatalf("ParseSpecSchema() = %v", err)
	}
	return schema
}

func TestParseSpecSchemaReadsTheSubset(t *testing.T) {
	schema := mustSpecSchema(t)
	if got := strings.Join(schema.Names(), ","); got != "enabled,mode,mountPath,port,targets" {
		t.Errorf("Names() = %s", got)
	}
}

// Every keyword the schema may use must mean the same to the core and the dashboard, so anything
// outside the subset, and any schema the core could not apply, is refused at load.
func TestParseSpecSchemaRefusesWhatTheSubsetCannotMean(t *testing.T) {
	cases := map[string]string{
		"unknown keyword":           `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string","title":"A","format":"uri"}}}`,
		"root not an object":        `{"type":"array","additionalProperties":false,"properties":{"a":{"type":"string","title":"A"}}}`,
		"open object":               `{"type":"object","properties":{"a":{"type":"string","title":"A"}}}`,
		"additional properties":     `{"type":"object","additionalProperties":true,"properties":{"a":{"type":"string","title":"A"}}}`,
		"no properties":             `{"type":"object","additionalProperties":false,"properties":{}}`,
		"name not camelCase":        `{"type":"object","additionalProperties":false,"properties":{"mount_path":{"type":"string","title":"A"}}}`,
		"no title":                  `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string"}}}`,
		"unknown type":              `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"number","title":"A"}}}`,
		"array without items":       `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"array","title":"A"}}}`,
		"items on a string":         `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string","title":"A","items":{"type":"string"}}}}`,
		"items not strings":         `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"array","title":"A","items":{"type":"integer"}}}}`,
		"enum on an integer":        `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"integer","title":"A","enum":["1"]}}}`,
		"repeated enum value":       `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string","title":"A","enum":["x","x"]}}}`,
		"pattern on an integer":     `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"integer","title":"A","pattern":"^1"}}}`,
		"invalid pattern":           `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string","title":"A","pattern":"("}}}`,
		"minimum on a string":       `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string","title":"A","minimum":1}}}`,
		"minimum above maximum":     `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"integer","title":"A","minimum":5,"maximum":1}}}`,
		"example of the wrong type": `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"integer","title":"A","examples":["x"]}}}`,
		"required unknown property": `{"type":"object","additionalProperties":false,"required":["b"],"properties":{"a":{"type":"string","title":"A"}}}`,
		"required twice":            `{"type":"object","additionalProperties":false,"required":["a","a"],"properties":{"a":{"type":"string","title":"A"}}}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseSpecSchema([]byte(raw)); err == nil {
				t.Error("ParseSpecSchema() accepted the schema")
			}
		})
	}
}

func TestManifestValidateRefusesAMalformedSpecSchema(t *testing.T) {
	manifest := validManifest(KindSoftware, "nfs")
	manifest.Spec = json.RawMessage(`{"type":"object","properties":{"a":{"type":"string","title":"A"}}}`)
	if err := manifest.Validate(); !errors.Is(err, ErrInvalidManifest) {
		t.Errorf("Validate() = %v, want ErrInvalidManifest", err)
	}
	manifest.Spec = json.RawMessage(testSpecSchema)
	if err := manifest.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestSpecSchemaValidate(t *testing.T) {
	schema := mustSpecSchema(t)
	valid := map[string]map[string]any{
		"every type": {
			"mountPath": "/shared", "mode": "ro", "port": float64(8001), "enabled": true,
			"targets": []any{"10.0.0.1:9100"},
		},
		"null and blank values are unset": {"mountPath": "/shared", "mode": " ", "port": nil},
		"an int from Go code":             {"mountPath": "/shared", "port": 22},
	}
	for name, spec := range valid {
		t.Run(name, func(t *testing.T) {
			if err := schema.Validate(spec); err != nil {
				t.Errorf("Validate() = %v", err)
			}
		})
	}
	refused := map[string]struct {
		spec map[string]any
		want string
	}{
		"unknown key":       {map[string]any{"mountPath": "/shared", "exportPath": "/x"}, "spec.exportPath is not a setting"},
		"string type":       {map[string]any{"mountPath": 7}, "spec.mountPath must be a string"},
		"pattern":           {map[string]any{"mountPath": "shared"}, "spec.mountPath must match ^/"},
		"enum":              {map[string]any{"mountPath": "/shared", "mode": "rx"}, "spec.mode must be one of rw, ro"},
		"fractional number": {map[string]any{"mountPath": "/shared", "port": 1.5}, "spec.port must be a whole number"},
		"below minimum":     {map[string]any{"mountPath": "/shared", "port": float64(0)}, "spec.port must be at least 1"},
		"above maximum":     {map[string]any{"mountPath": "/shared", "port": float64(70000)}, "spec.port must be at most 65535"},
		"boolean as text":   {map[string]any{"mountPath": "/shared", "enabled": "false"}, "spec.enabled must be true or false"},
		"list of numbers":   {map[string]any{"mountPath": "/shared", "targets": []any{1}}, "spec.targets must be a list of strings"},
		"required missing":  {map[string]any{}, "spec.mountPath is required"},
		"required blank":    {map[string]any{"mountPath": "  "}, "spec.mountPath is required"},
	}
	for name, tc := range refused {
		t.Run(name, func(t *testing.T) {
			err := schema.Validate(tc.spec)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate() = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}
