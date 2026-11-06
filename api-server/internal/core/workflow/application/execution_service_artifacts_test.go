package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// artifactTestServers finds every id as a deployed Server of site-1.
type artifactTestServers struct {
	serverdomain.ServerRepository
}

func (artifactTestServers) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	return &serverdomain.Server{
		ID: id, Source: serverdomain.Source{SiteID: "site-1", IntegrationID: "maas-1"},
		Observed: serverdomain.Observed{Hostname: id}, Provisioning: &serverdomain.ProvisioningStatus{State: "deployed"},
	}, nil
}

// artifactTestAutomation answers every Site with a runnable configuration.
type artifactTestAutomation struct {
	workflowdomain.AutomationConfigurationRepository
}

func (artifactTestAutomation) FindBySiteID(_ context.Context, siteID string) (*workflowdomain.AutomationConfiguration, error) {
	return &workflowdomain.AutomationConfiguration{SiteID: siteID, Enabled: true, CredentialSource: workflowdomain.CredentialSourceDeploymentKey}, nil
}

// artifactTestCatalog registers every name; the names in fetches fetch artifacts.
type artifactTestCatalog struct {
	fetches map[string]bool
}

func (artifactTestCatalog) Resolve(name string) (string, error) {
	return "shared/playbooks/" + name + ".yml", nil
}
func (c artifactTestCatalog) FetchesArtifacts(name string) bool {
	return c.fetches[name]
}

type artifactTestUnlocked struct{}

func (artifactTestUnlocked) RequireUnlocked(context.Context, []string) error { return nil }

// fakeArtifactVars answers with vars, or err, and records the Sites it was asked for.
type fakeArtifactVars struct {
	vars  map[string]any
	err   error
	sites []string
}

func (f *fakeArtifactVars) ArtifactVars(_ context.Context, siteID string) (map[string]any, error) {
	f.sites = append(f.sites, siteID)
	return f.vars, f.err
}

func newArtifactTestExecutionService(resolver ArtifactVars) *ExecutionService {
	catalog := artifactTestCatalog{fetches: map[string]bool{"deploy-kubernetes": true}}
	return NewExecutionService(artifactTestServers{}, artifactTestAutomation{}, catalog, workflowdomain.WorkflowKinds{},
		nil, nil, nil, artifactTestUnlocked{}, nil, resolver)
}

// A playbook the catalog marks gets the resolver's vars, from the Local Repository or from the
// upstreams alike, as trusted vars no caller can override; an unmarked playbook never asks the
// resolver.
func TestPrepareAnsibleStepInjectsTheArtifactVars(t *testing.T) {
	for source, value := range map[string]map[string]any{
		"local_repository": {"source": "local_repository", "registry": "10.0.0.5:8080"},
		"upstream":         {"source": "upstream", "registry": "", "files": map[string]any{"tools/tool-1": "https://files.example/tool-1"}},
	} {
		t.Run(source, func(t *testing.T) {
			resolver := &fakeArtifactVars{vars: map[string]any{"swallow_artifacts": value}}
			service := newArtifactTestExecutionService(resolver)

			prepared, err := service.PrepareAnsibleStep(context.Background(), CreateExecutionInput{
				Kind: string(workflowdomain.WorkflowKindCustom), TargetServerIDs: []string{"server-1"}, PlaybookName: "deploy-kubernetes",
				ExtraVars:   map[string]any{"swallow_artifacts": "forged by a user"},
				TrustedVars: map[string]any{"swallow_artifacts": "forged by a caller"},
			}, "op-1", true)
			if err != nil {
				t.Fatalf("PrepareAnsibleStep: %v", err)
			}
			if got := prepared.ExtraVars["swallow_artifacts"]; !reflect.DeepEqual(got, value) {
				t.Errorf("swallow_artifacts = %v, want the resolver's %v", got, value)
			}
			if !reflect.DeepEqual(resolver.sites, []string{"site-1"}) {
				t.Errorf("resolver asked for %v, want the targets' Site", resolver.sites)
			}

			resolver.sites = nil
			prepared, err = service.PrepareAnsibleStep(context.Background(), CreateExecutionInput{
				Kind: string(workflowdomain.WorkflowKindCustom), TargetServerIDs: []string{"server-1"}, PlaybookName: "configure-ntp",
			}, "op-2", true)
			if err != nil {
				t.Fatalf("PrepareAnsibleStep of an unmarked playbook: %v", err)
			}
			if _, ok := prepared.ExtraVars["swallow_artifacts"]; ok || len(resolver.sites) > 0 {
				t.Errorf("unmarked playbook: vars %v, resolver asked for %v; want neither", prepared.ExtraVars, resolver.sites)
			}
		})
	}
}

// A marked playbook is refused, by PrepareAnsibleStep and by Create, while the resolver reports the
// Site's Local Repository serving deployments and not ready, with the resolver's account of what is
// not ready, and refused rather than run without the vars when there is no resolver at all.
func TestAnsibleAcceptanceRefusesAMarkedPlaybookWhileTheLocalRepositoryIsNotReady(t *testing.T) {
	const notReady = "source ubuntu is syncing; image registry.k8s.io/nfd/node-feature-discovery:v0.18.3 is pending"
	unavailable := fmt.Errorf("%w: %s", localrepodomain.ErrLocalRepositoryUnavailable, notReady)
	input := CreateExecutionInput{
		Kind: string(workflowdomain.WorkflowKindCustom), TargetServerIDs: []string{"server-1"}, PlaybookName: "deploy-kubernetes",
	}
	for name, resolver := range map[string]ArtifactVars{
		"not ready":   &fakeArtifactVars{err: unavailable},
		"no resolver": nil,
	} {
		t.Run(name, func(t *testing.T) {
			service := newArtifactTestExecutionService(resolver)
			_, err := service.PrepareAnsibleStep(context.Background(), input, "op-1", true)
			if !errors.Is(err, localrepodomain.ErrLocalRepositoryUnavailable) {
				t.Errorf("PrepareAnsibleStep error = %v, want ErrLocalRepositoryUnavailable", err)
			}
			if resolver != nil && (err == nil || !strings.Contains(err.Error(), notReady)) {
				t.Errorf("PrepareAnsibleStep error = %v, want it to name what is not ready: %s", err, notReady)
			}
			if _, err := service.Create(context.Background(), input); !errors.Is(err, localrepodomain.ErrLocalRepositoryUnavailable) {
				t.Errorf("Create error = %v, want ErrLocalRepositoryUnavailable", err)
			}
		})
	}
}
