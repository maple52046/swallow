package apicontract

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The contract assembled from the fragments must be valid OpenAPI and follow the authoring rules.
func TestContractIsValid(t *testing.T) {
	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

// The committed openapi.yaml is what the dashboard generates its types from, so it must equal
// the bundle of the current fragments. UPDATE_OPENAPI=1 rewrites it.
func TestBundledFileIsCurrent(t *testing.T) {
	want, err := Bundle(Dir())
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}
	path := filepath.Join(Dir(), BundledFile)
	if os.Getenv("UPDATE_OPENAPI") == "1" {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatalf("write %s: %v", BundledFile, err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run UPDATE_OPENAPI=1 go test ./internal/shared/apicontract)", BundledFile, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is out of date with common.yaml and areas/*.yaml; run UPDATE_OPENAPI=1 go test ./internal/shared/apicontract", BundledFile)
	}
}

func TestBundleRefusesDuplicateDefinitions(t *testing.T) {
	common := "openapi: 3.0.3\ninfo: {title: t, version: v1}\npaths: {}\n"
	tests := []struct {
		name    string
		areas   map[string]string
		wantErr string
	}{
		{
			name: "same operation in two areas",
			areas: map[string]string{
				"a.yaml": "paths:\n  /x:\n    get: {responses: {'200': {description: ok}}}\n",
				"b.yaml": "paths:\n  /x:\n    get: {responses: {'200': {description: ok}}}\n",
			},
			wantErr: "operation get /x is already defined",
		},
		{
			name: "same component name in two areas",
			areas: map[string]string{
				"a.yaml": "components:\n  schemas:\n    Thing: {type: string}\n",
				"b.yaml": "components:\n  schemas:\n    Thing: {type: string}\n",
			},
			wantErr: "components.schemas.Thing is already defined",
		},
		{
			name: "same tag in two areas",
			areas: map[string]string{
				"a.yaml": "tags:\n  - name: servers\n",
				"b.yaml": "tags:\n  - name: servers\n",
			},
			wantErr: `tag "servers" is already defined`,
		},
		{
			name: "fragment with a document-level key",
			areas: map[string]string{
				"a.yaml": "info: {title: other, version: v2}\n",
			},
			wantErr: `unsupported top-level key "info"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "common.yaml"), common)
			for name, content := range test.areas {
				writeFile(t, filepath.Join(dir, "areas", name), content)
			}
			_, err := Bundle(dir)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("Bundle() error = %v, want it to contain %q", err, test.wantErr)
			}
		})
	}
}

// Different methods of one path may come from different areas, and unquoted status codes are
// emitted as the string keys OpenAPI requires.
func TestBundleMergesMethodsOfOnePath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "common.yaml"), "openapi: 3.0.3\ninfo: {title: t, version: v1}\npaths: {}\n")
	writeFile(t, filepath.Join(dir, "areas", "a.yaml"), "paths:\n  /x:\n    get:\n      responses:\n        200: {description: ok}\n")
	writeFile(t, filepath.Join(dir, "areas", "b.yaml"), "paths:\n  /x:\n    delete:\n      responses:\n        204: {description: gone}\n")
	got, err := Bundle(dir)
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}
	for _, want := range []string{"get:", "delete:", `"200":`, `"204":`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("Bundle() output lacks %q:\n%s", want, got)
		}
	}
}

func TestRouteTemplate(t *testing.T) {
	tests := []struct {
		fiberPath string
		want      string
	}{
		{fiberPath: "/api/v1/servers/", want: "/api/v1/servers"},
		{fiberPath: "/api/v1/servers/:id", want: "/api/v1/servers/{id}"},
		{fiberPath: "/api/v1/platforms/:id/kubernetes/pods/:namespace/:name/logs", want: "/api/v1/platforms/{id}/kubernetes/pods/{namespace}/{name}/logs"},
		{fiberPath: "/boot-media/ipxe/:id/swallow-ipxe.iso", want: "/boot-media/ipxe/{id}/swallow-ipxe.iso"},
		{fiberPath: "/", want: "/"},
	}
	for _, test := range tests {
		if got := RouteTemplate(test.fiberPath); got != test.want {
			t.Errorf("RouteTemplate(%q) = %q, want %q", test.fiberPath, got, test.want)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
