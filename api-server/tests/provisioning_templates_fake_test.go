package tests

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// fakeDeploymentTemplateRepo mirrors uniqueness and secret-presence behavior for HTTP tests.
type fakeDeploymentTemplateRepo struct {
	mu        sync.Mutex
	templates map[string]*provisioningdomain.DeploymentTemplate
	userData  map[string]string
}

func newFakeDeploymentTemplateRepo() *fakeDeploymentTemplateRepo {
	return &fakeDeploymentTemplateRepo{
		templates: make(map[string]*provisioningdomain.DeploymentTemplate),
		userData:  make(map[string]string),
	}
}

func (r *fakeDeploymentTemplateRepo) Create(
	_ context.Context,
	template *provisioningdomain.DeploymentTemplate,
	userData string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.nameTaken(template.IntegrationID, template.PoolID, template.Name, "") {
		return provisioningdomain.ErrDeploymentTemplateNameTaken
	}
	copy := *template
	copy.HasUserData = userData != ""
	r.templates[copy.ID] = &copy
	if userData != "" {
		r.userData[copy.ID] = userData
	}
	return nil
}

func (r *fakeDeploymentTemplateRepo) FindByID(
	_ context.Context,
	id string,
) (*provisioningdomain.DeploymentTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	template, ok := r.templates[id]
	if !ok {
		return nil, provisioningdomain.ErrDeploymentTemplateNotFound
	}
	copy := *template
	copy.HasUserData = r.userData[id] != ""
	return &copy, nil
}

func (r *fakeDeploymentTemplateRepo) List(
	_ context.Context,
	filter provisioningdomain.DeploymentTemplateFilter,
) ([]*provisioningdomain.DeploymentTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	allowed := make(map[string]bool, len(filter.IntegrationIDs))
	for _, id := range filter.IntegrationIDs {
		allowed[id] = true
	}
	items := make([]*provisioningdomain.DeploymentTemplate, 0)
	for _, template := range r.templates {
		if filter.IntegrationID != "" && template.IntegrationID != filter.IntegrationID {
			continue
		}
		if len(allowed) > 0 && !allowed[template.IntegrationID] {
			continue
		}
		if filter.PoolIDs != nil && !slices.Contains(filter.PoolIDs, template.PoolID) {
			continue
		}
		copy := *template
		copy.HasUserData = r.userData[template.ID] != ""
		items = append(items, &copy)
	}
	return items, nil
}

func (r *fakeDeploymentTemplateRepo) Update(
	_ context.Context,
	template *provisioningdomain.DeploymentTemplate,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.templates[template.ID]; !ok {
		return provisioningdomain.ErrDeploymentTemplateNotFound
	}
	if r.nameTaken(template.IntegrationID, template.PoolID, template.Name, template.ID) {
		return provisioningdomain.ErrDeploymentTemplateNameTaken
	}
	copy := *template
	copy.HasUserData = r.userData[template.ID] != ""
	r.templates[template.ID] = &copy
	return nil
}

func (r *fakeDeploymentTemplateRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.templates[id]; !ok {
		return provisioningdomain.ErrDeploymentTemplateNotFound
	}
	delete(r.templates, id)
	delete(r.userData, id)
	return nil
}

func (r *fakeDeploymentTemplateRepo) ReplaceUserData(_ context.Context, id, userData string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	template, ok := r.templates[id]
	if !ok {
		return provisioningdomain.ErrDeploymentTemplateNotFound
	}
	r.userData[id] = userData
	template.HasUserData = true
	template.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *fakeDeploymentTemplateRepo) ClearUserData(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	template, ok := r.templates[id]
	if !ok {
		return provisioningdomain.ErrDeploymentTemplateNotFound
	}
	delete(r.userData, id)
	template.HasUserData = false
	template.UpdatedAt = time.Now().UTC()
	return nil
}

func (r *fakeDeploymentTemplateRepo) UserData(_ context.Context, id string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.templates[id]; !ok {
		return "", provisioningdomain.ErrDeploymentTemplateNotFound
	}
	value, ok := r.userData[id]
	if !ok {
		return "", provisioningdomain.ErrDeploymentTemplateUserDataMissing
	}
	return value, nil
}

func (r *fakeDeploymentTemplateRepo) CountByIntegration(_ context.Context, integrationID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, template := range r.templates {
		if template.IntegrationID == integrationID {
			count++
		}
	}
	return count, nil
}

func (r *fakeDeploymentTemplateRepo) CountByPool(_ context.Context, poolID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, template := range r.templates {
		if template.PoolID == poolID {
			count++
		}
	}
	return count, nil
}

func (r *fakeDeploymentTemplateRepo) AssignPool(_ context.Context, ids []string, poolID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := 0
	for _, id := range ids {
		if template, ok := r.templates[id]; ok && template.PoolID != poolID {
			template.PoolID = poolID
			changed++
		}
	}
	return changed, nil
}

func (r *fakeDeploymentTemplateRepo) nameTaken(integrationID, poolID, name, exceptID string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, template := range r.templates {
		if template.ID != exceptID &&
			template.IntegrationID == integrationID && template.PoolID == poolID &&
			strings.ToLower(strings.TrimSpace(template.Name)) == normalized {
			return true
		}
	}
	return false
}

// fakePlatformTemplates is the Platform context's port to Deployment Templates over the fake
// template store, the way the composition root's adapter is over the Mongo one.
type fakePlatformTemplates struct {
	repo *fakeDeploymentTemplateRepo
}

func (t fakePlatformTemplates) Find(ctx context.Context, id string) (*platformdomain.DeploymentTemplateIntent, error) {
	template, err := t.repo.FindByID(ctx, id)
	if errors.Is(err, provisioningdomain.ErrDeploymentTemplateNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	intent := &platformdomain.DeploymentTemplateIntent{
		ID: template.ID, IntegrationID: template.IntegrationID, PoolID: template.PoolID, Name: template.Name, Ephemeral: template.Ephemeral,
		Digest: template.ImageID + "|" + string(template.NetworkMode),
	}
	if minimum := template.MinimumHardware; minimum != nil {
		intent.Minimum = &platformdomain.TemplateMinimum{CPUCores: minimum.CPUCores, MemoryMiB: minimum.MemoryMiB,
			StorageGB: minimum.StorageGB, GPUModel: minimum.GPUModel, GPUCount: minimum.GPUCount}
	}
	return intent, nil
}

func (t fakePlatformTemplates) NameTaken(ctx context.Context, integrationID, poolID, name, id string) (bool, error) {
	templates, err := t.repo.List(ctx, provisioningdomain.DeploymentTemplateFilter{IntegrationID: integrationID})
	if err != nil {
		return false, err
	}
	for _, template := range templates {
		if template.ID != id && template.PoolID == poolID && template.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (t fakePlatformTemplates) AdoptGenerated(ctx context.Context, ids []string, poolID string) error {
	_, err := t.repo.AssignPool(ctx, ids, poolID)
	return err
}

func (t fakePlatformTemplates) EnsureGenerated(ctx context.Context, generated []platformdomain.GeneratedTemplate, preparation platformdomain.MachinePreparation,
	owner platformdomain.TemplateOwner,
) error {
	for _, g := range generated {
		if _, err := t.repo.FindByID(ctx, g.ID); err == nil {
			continue
		}
		template := &provisioningdomain.DeploymentTemplate{
			ID: g.ID, IntegrationID: g.IntegrationID, PoolID: owner.PoolID, CreatedBy: owner.CreatedBy, Name: g.Name, ImageID: preparation.ImageID,
			Ephemeral: preparation.Ephemeral != nil && *preparation.Ephemeral, NetworkMode: provisioningdomain.DeploymentNetworkMode(preparation.NetworkMode),
		}
		if err := t.repo.Create(ctx, template, ""); err != nil {
			return err
		}
	}
	return nil
}

// fakeTemplateReferences lists the Platform node groups a test says install from each template.
type fakeTemplateReferences struct {
	refs map[string][]provisioningdomain.DeploymentTemplateReference
}

func (f *fakeTemplateReferences) TemplateReferences(context.Context) (map[string][]provisioningdomain.DeploymentTemplateReference, error) {
	return f.refs, nil
}
