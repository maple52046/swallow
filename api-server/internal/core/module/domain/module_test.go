package domain

import (
	"errors"
	"testing"
)

func validManifest(kind Kind, id string) Manifest {
	return Manifest{
		ContractVersion: ContractVersion1, Kind: kind, ID: id, Version: "0.1.0",
		Playbooks: []string{"deploy-" + id}, WorkflowKinds: []string{"configure-" + id},
		Completions: []string{id + "-state"}, Capabilities: []string{PortHostAutomation},
		Routes: []string{"/api/v1/" + id},
	}
}

func TestParseManifestRefusesUnknownFieldsAndUnsupportedContracts(t *testing.T) {
	if _, err := ParseManifest([]byte(`{"contractVersion":1,"kind":"software","id":"nfs","version":"0.1.0","playbook":["x"]}`)); !errors.Is(err, ErrInvalidManifest) {
		t.Errorf("misspelled field: error = %v, want ErrInvalidManifest", err)
	}
	if _, err := ParseManifest([]byte(`{"contractVersion":2,"kind":"software","id":"nfs","version":"0.1.0"}`)); !errors.Is(err, ErrUnsupportedContract) {
		t.Errorf("contract version 2: error = %v, want ErrUnsupportedContract", err)
	}
	manifest, err := ParseManifest([]byte(`{"contractVersion":1,"kind":"software","id":"nfs","version":"0.1.0","playbooks":["deploy-nfs"]}`))
	if err != nil {
		t.Fatalf("valid manifest: %v", err)
	}
	if manifest.Definition() != "software/nfs@0.1.0" {
		t.Errorf("Definition() = %q, want software/nfs@0.1.0", manifest.Definition())
	}
}

func TestManifestValidateRefusesMalformedDeclarations(t *testing.T) {
	cases := map[string]func(*Manifest){
		"unknown kind":      func(m *Manifest) { m.Kind = "driver" },
		"id not kebab-case": func(m *Manifest) { m.ID = "Docker_CE" },
		"version not SemVer": func(m *Manifest) {
			m.Version = "v1"
		},
		"duplicate playbook":    func(m *Manifest) { m.Playbooks = []string{"a", "a"} },
		"hook without reason":   func(m *Manifest) { m.Hooks = []Hook{{Name: "validate"}} },
		"unknown port":          func(m *Manifest) { m.Capabilities = []string{"maas"} },
		"route outside the API": func(m *Manifest) { m.Routes = []string{"/metrics"} },
		"spec is not JSON":      func(m *Manifest) { m.Spec = []byte("{") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			manifest := validManifest(KindSoftware, "nfs")
			mutate(&manifest)
			if err := manifest.Validate(); !errors.Is(err, ErrInvalidManifest) {
				t.Errorf("Validate() = %v, want ErrInvalidManifest", err)
			}
		})
	}
}

func TestNewRegistryRefusesSharedClaims(t *testing.T) {
	cases := map[string]func(a, b *Manifest){
		"same module":        func(a, b *Manifest) { b.ID = a.ID },
		"same Workflow kind": func(a, b *Manifest) { b.WorkflowKinds = a.WorkflowKinds },
		"same completion":    func(a, b *Manifest) { b.Completions = a.Completions },
		"same route":         func(a, b *Manifest) { b.Routes = a.Routes },
		"same playbook":      func(a, b *Manifest) { b.Playbooks = a.Playbooks },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			a, b := validManifest(KindSoftware, "nfs"), validManifest(KindSoftware, "podman")
			mutate(&a, &b)
			if _, err := NewRegistry(Module{Manifest: a}, Module{Manifest: b}); !errors.Is(err, ErrModuleConflict) {
				t.Errorf("NewRegistry() = %v, want ErrModuleConflict", err)
			}
		})
	}
}

// A module's Go package implements exactly the hooks its manifest declares: an undeclared hook is
// module-specific Go code outside the contract, and a declared one without code would fail on use.
func TestNewRegistryRequiresHooksToMatchTheManifest(t *testing.T) {
	manifest := validManifest(KindSoftware, "nfs")
	manifest.Hooks = []Hook{{Name: "validate-spec", Reason: "role assignment needs cross-field checks"}}
	if _, err := NewRegistry(Module{Manifest: manifest}); !errors.Is(err, ErrInvalidManifest) {
		t.Errorf("declared hook without code: error = %v, want ErrInvalidManifest", err)
	}
	if _, err := NewRegistry(Module{Manifest: validManifest(KindSoftware, "nfs"), Hooks: map[string]any{"extra": 1}}); !errors.Is(err, ErrInvalidManifest) {
		t.Errorf("undeclared hook: error = %v, want ErrInvalidManifest", err)
	}
	registry, err := NewRegistry(Module{Manifest: manifest, Hooks: map[string]any{"validate-spec": 1}})
	if err != nil {
		t.Fatalf("matching hooks: %v", err)
	}
	if module, ok := registry.ForWorkflowKind("configure-nfs"); !ok || module.Manifest.ID != "nfs" {
		t.Errorf("ForWorkflowKind(configure-nfs) = %v, %v", module.Manifest.Ref(), ok)
	}
	if _, ok := registry.ForCompletion("nfs-state"); !ok {
		t.Error("ForCompletion(nfs-state) found no module")
	}
}

func TestHostAccessDeclaration(t *testing.T) {
	declaration := HostAccessDeclaration{
		Default: HostAccessRule{},
		Roles:   map[string]HostAccessRule{"login": {MemberLogin: true}, "workstation": {MemberLogin: true, MemberSudo: true}},
	}
	for _, tc := range []struct {
		roles []string
		want  HostAccessRule
	}{
		{roles: []string{"compute"}, want: HostAccessRule{}},
		{roles: []string{"compute", "login"}, want: HostAccessRule{MemberLogin: true}},
		{roles: []string{"login", "workstation"}, want: HostAccessRule{MemberLogin: true, MemberSudo: true}},
		{roles: nil, want: HostAccessRule{}},
	} {
		if got := declaration.RuleFor(tc.roles); got != tc.want {
			t.Errorf("RuleFor(%v) = %+v, want %+v", tc.roles, got, tc.want)
		}
	}

	software := validManifest(KindSoftware, "podman")
	software.HostAccess = &HostAccessDeclaration{}
	if err := software.Validate(); !errors.Is(err, ErrInvalidManifest) {
		t.Errorf("software module with hostAccess: err = %v, want ErrInvalidManifest", err)
	}
	platform := validManifest(KindPlatform, "slurm")
	platform.HostAccess = &HostAccessDeclaration{Roles: map[string]HostAccessRule{"compute": {MemberSudo: true}}}
	if err := platform.Validate(); !errors.Is(err, ErrInvalidManifest) {
		t.Errorf("sudo without login: err = %v, want ErrInvalidManifest", err)
	}
	platform.HostAccess = &declaration
	if err := platform.Validate(); err != nil {
		t.Errorf("valid platform hostAccess: %v", err)
	}
}
