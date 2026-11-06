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

// repositoryTestServers finds every id as a deployed Server of site-1.
type repositoryTestServers struct {
	serverdomain.ServerRepository
}

func (repositoryTestServers) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	return &serverdomain.Server{
		ID: id, Source: serverdomain.Source{SiteID: "site-1", IntegrationID: "maas-1"},
		Observed: serverdomain.Observed{Hostname: id}, Provisioning: &serverdomain.ProvisioningStatus{State: "deployed"},
	}, nil
}

// repositoryTestAutomation answers every Site with a runnable configuration.
type repositoryTestAutomation struct {
	workflowdomain.AutomationConfigurationRepository
}

func (repositoryTestAutomation) FindBySiteID(_ context.Context, siteID string) (*workflowdomain.AutomationConfiguration, error) {
	return &workflowdomain.AutomationConfiguration{SiteID: siteID, Enabled: true, CredentialSource: workflowdomain.CredentialSourceDeploymentKey}, nil
}

// repositoryTestCatalog registers every name; the names in usesRepository use the Local Repository.
type repositoryTestCatalog struct {
	usesRepository map[string]bool
}

func (repositoryTestCatalog) Resolve(name string) (string, error) {
	return "shared/playbooks/" + name + ".yml", nil
}
func (c repositoryTestCatalog) UsesLocalRepository(name string) bool {
	return c.usesRepository[name]
}

type repositoryTestUnlocked struct{}

func (repositoryTestUnlocked) RequireUnlocked(context.Context, []string) error { return nil }

// fakeLocalRepositoryVars answers with vars, or err, and records the Sites it was asked for.
type fakeLocalRepositoryVars struct {
	vars  map[string]any
	err   error
	sites []string
}

func (f *fakeLocalRepositoryVars) HostVars(_ context.Context, siteID string) (map[string]any, error) {
	f.sites = append(f.sites, siteID)
	return f.vars, f.err
}

func newRepositoryTestExecutionService(resolver LocalRepositoryVars) *ExecutionService {
	catalog := repositoryTestCatalog{usesRepository: map[string]bool{"deploy-kubernetes": true}}
	return NewExecutionService(repositoryTestServers{}, repositoryTestAutomation{}, catalog, workflowdomain.WorkflowKinds{},
		nil, nil, nil, repositoryTestUnlocked{}, nil, resolver)
}

// A playbook the catalog marks gets the resolver's vars as trusted vars no caller can override; an
// unmarked playbook never asks the resolver.
func TestPrepareAnsibleStepInjectsTheLocalRepositoryVars(t *testing.T) {
	value := map[string]any{"registry": "10.0.0.5:8080"}
	resolver := &fakeLocalRepositoryVars{vars: map[string]any{"swallow_local_repository": value}}
	service := newRepositoryTestExecutionService(resolver)

	prepared, err := service.PrepareAnsibleStep(context.Background(), CreateExecutionInput{
		Kind: string(workflowdomain.WorkflowKindCustom), TargetServerIDs: []string{"server-1"}, PlaybookName: "deploy-kubernetes",
		ExtraVars:   map[string]any{"swallow_local_repository": "forged by a user"},
		TrustedVars: map[string]any{"swallow_local_repository": "forged by a caller"},
	}, "op-1", true)
	if err != nil {
		t.Fatalf("PrepareAnsibleStep: %v", err)
	}
	if got := prepared.ExtraVars["swallow_local_repository"]; !reflect.DeepEqual(got, value) {
		t.Errorf("swallow_local_repository = %v, want the resolver's %v", got, value)
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
	if _, ok := prepared.ExtraVars["swallow_local_repository"]; ok || len(resolver.sites) > 0 {
		t.Errorf("unmarked playbook: vars %v, resolver asked for %v; want neither", prepared.ExtraVars, resolver.sites)
	}
}

// A marked playbook is refused, by PrepareAnsibleStep and by Create, while the resolver reports the
// Local Repository unavailable, with the resolver's account of what is not ready, and refused rather
// than run without the vars when there is no resolver at all.
func TestAnsibleAcceptanceRefusesAMarkedPlaybookWithoutALocalRepository(t *testing.T) {
	const notReady = "source ubuntu is syncing; image registry.k8s.io/nfd/node-feature-discovery:v0.18.3 is pending"
	unavailable := fmt.Errorf("%w: %s", localrepodomain.ErrLocalRepositoryUnavailable, notReady)
	input := CreateExecutionInput{
		Kind: string(workflowdomain.WorkflowKindCustom), TargetServerIDs: []string{"server-1"}, PlaybookName: "deploy-kubernetes",
	}
	for name, resolver := range map[string]LocalRepositoryVars{
		"not ready":   &fakeLocalRepositoryVars{err: unavailable},
		"no resolver": nil,
	} {
		t.Run(name, func(t *testing.T) {
			service := newRepositoryTestExecutionService(resolver)
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
