package tests

import (
	"context"
	"strings"
	"sync"
	"time"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
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
	if r.nameTaken(template.IntegrationID, template.Name, "") {
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
	if r.nameTaken(template.IntegrationID, template.Name, template.ID) {
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

func (r *fakeDeploymentTemplateRepo) nameTaken(integrationID, name, exceptID string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, template := range r.templates {
		if template.ID != exceptID &&
			template.IntegrationID == integrationID &&
			strings.ToLower(strings.TrimSpace(template.Name)) == normalized {
			return true
		}
	}
	return false
}
