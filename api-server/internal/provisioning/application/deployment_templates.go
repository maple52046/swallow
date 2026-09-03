package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// DeploymentTemplateItem is safe template metadata returned by the API.
type DeploymentTemplateItem struct {
	ID            string                        `json:"id"`
	SiteID        string                        `json:"siteId"`
	IntegrationID string                        `json:"integrationId"`
	Name          string                        `json:"name"`
	Description   string                        `json:"description"`
	ImageID       string                        `json:"imageId"`
	Ephemeral     bool                          `json:"ephemeral"`
	Network       DeploymentNetworkSettingsItem `json:"network"`
	HasUserData   bool                          `json:"hasUserData"`
	CreatedAt     string                        `json:"createdAt"`
	UpdatedAt     string                        `json:"updatedAt"`
}

// DeploymentNetworkSettingsItem is reusable intent without target NIC or IP data.
type DeploymentNetworkSettingsItem struct {
	Mode           string `json:"mode"`
	SubnetID       string `json:"subnetId"`
	DefaultGateway bool   `json:"defaultGateway"`
}

// DeploymentNetworkSettingsInput accepts Swallow DHCP/static template intent.
// A nil input applies the backward-compatible DHCP default.
type DeploymentNetworkSettingsInput struct {
	Mode           string
	SubnetID       string
	DefaultGateway bool
}

// CreateDeploymentTemplateInput carries template intent and optional write-only user data.
type CreateDeploymentTemplateInput struct {
	IntegrationID string
	Name          string
	Description   string
	ImageID       string
	Ephemeral     bool
	UserData      string
	Network       *DeploymentNetworkSettingsInput
}

// UpdateDeploymentTemplateInput changes non-secret template intent.
type UpdateDeploymentTemplateInput struct {
	Name        *string
	Description *string
	ImageID     *string
	Ephemeral   *bool
	Network     *DeploymentNetworkSettingsInput
}

// DeploymentTemplateService owns template validation and lifecycle.
type DeploymentTemplateService struct {
	templates    provisioningdomain.DeploymentTemplateRepository
	integrations provisioningdomain.ProvisionerIntegrationReader
	providers    provisioningdomain.ProviderFactory
}

// NewDeploymentTemplateService creates the template application service.
func NewDeploymentTemplateService(
	templates provisioningdomain.DeploymentTemplateRepository,
	integrations provisioningdomain.ProvisionerIntegrationReader,
	providers provisioningdomain.ProviderFactory,
) *DeploymentTemplateService {
	return &DeploymentTemplateService{
		templates: templates, integrations: integrations, providers: providers,
	}
}

// Create validates the live image before storing reusable intent.
func (s *DeploymentTemplateService) Create(
	ctx context.Context,
	input CreateDeploymentTemplateInput,
) (*DeploymentTemplateItem, error) {
	name := strings.TrimSpace(input.Name)
	imageID := strings.TrimSpace(input.ImageID)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", provisioningdomain.ErrInvalidDeploymentTemplate)
	}
	if imageID == "" {
		return nil, fmt.Errorf("%w: imageId is required", provisioningdomain.ErrInvalidDeploymentTemplate)
	}
	integration, err := s.integrations.Find(ctx, input.IntegrationID)
	if err != nil {
		return nil, err
	}
	provider, _, err := validateDeployImage(ctx, s.providers, integration.ID, imageID)
	if err != nil {
		return nil, err
	}
	mode, subnetID, defaultGateway, err := validateTemplateNetwork(ctx, provider, input.Network)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	template := &provisioningdomain.DeploymentTemplate{
		ID:             uuid.NewString(),
		IntegrationID:  integration.ID,
		Name:           name,
		Description:    strings.TrimSpace(input.Description),
		ImageID:        imageID,
		Ephemeral:      input.Ephemeral,
		NetworkMode:    mode,
		SubnetID:       subnetID,
		DefaultGateway: defaultGateway,
		HasUserData:    input.UserData != "",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.templates.Create(ctx, template, input.UserData); err != nil {
		return nil, err
	}
	return deploymentTemplateItem(template, integration.SiteID), nil
}

// List applies site and integration filters conjunctively.
func (s *DeploymentTemplateService) List(
	ctx context.Context,
	siteID, integrationID string,
) ([]DeploymentTemplateItem, error) {
	filter := provisioningdomain.DeploymentTemplateFilter{}
	siteByIntegration := map[string]string{}

	if siteID != "" {
		integrations, err := s.integrations.ListBySite(ctx, siteID)
		if err != nil {
			return nil, err
		}
		if len(integrations) == 0 {
			return []DeploymentTemplateItem{}, nil
		}
		filter.IntegrationIDs = make([]string, 0, len(integrations))
		for _, integration := range integrations {
			filter.IntegrationIDs = append(filter.IntegrationIDs, integration.ID)
			siteByIntegration[integration.ID] = integration.SiteID
		}
	}
	if integrationID != "" {
		integration, err := s.integrations.Find(ctx, integrationID)
		if err != nil {
			return nil, err
		}
		if siteID != "" && integration.SiteID != siteID {
			return []DeploymentTemplateItem{}, nil
		}
		filter.IntegrationID = integrationID
		filter.IntegrationIDs = nil
		siteByIntegration[integration.ID] = integration.SiteID
	}

	templates, err := s.templates.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	items := make([]DeploymentTemplateItem, 0, len(templates))
	for _, template := range templates {
		resolvedSiteID := siteByIntegration[template.IntegrationID]
		if resolvedSiteID == "" {
			integration, findErr := s.integrations.Find(ctx, template.IntegrationID)
			if findErr != nil {
				return nil, findErr
			}
			resolvedSiteID = integration.SiteID
		}
		items = append(items, *deploymentTemplateItem(template, resolvedSiteID))
	}
	return items, nil
}

// Get returns metadata without reading cloud-init.
func (s *DeploymentTemplateService) Get(
	ctx context.Context,
	id string,
) (*DeploymentTemplateItem, error) {
	template, err := s.templates.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	integration, err := s.integrations.Find(ctx, template.IntegrationID)
	if err != nil {
		return nil, err
	}
	return deploymentTemplateItem(template, integration.SiteID), nil
}

// Update validates a changed image before persisting non-secret intent.
func (s *DeploymentTemplateService) Update(
	ctx context.Context,
	id string,
	input UpdateDeploymentTemplateInput,
) (*DeploymentTemplateItem, error) {
	template, err := s.templates.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	integration, err := s.integrations.Find(ctx, template.IntegrationID)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: name cannot be empty", provisioningdomain.ErrInvalidDeploymentTemplate)
		}
		template.Name = name
	}
	if input.Description != nil {
		template.Description = strings.TrimSpace(*input.Description)
	}
	if input.ImageID != nil {
		imageID := strings.TrimSpace(*input.ImageID)
		if imageID == "" {
			return nil, fmt.Errorf("%w: imageId cannot be empty", provisioningdomain.ErrInvalidDeploymentTemplate)
		}
		if imageID != template.ImageID {
			if _, _, err := validateDeployImage(ctx, s.providers, template.IntegrationID, imageID); err != nil {
				return nil, err
			}
			template.ImageID = imageID
		}
	}
	if input.Ephemeral != nil {
		template.Ephemeral = *input.Ephemeral
	}
	if input.Network != nil {
		provider, err := s.providers.For(ctx, template.IntegrationID)
		if err != nil {
			return nil, err
		}
		mode, subnetID, defaultGateway, err := validateTemplateNetwork(ctx, provider, input.Network)
		if err != nil {
			return nil, err
		}
		template.NetworkMode = mode
		template.SubnetID = subnetID
		template.DefaultGateway = defaultGateway
	}
	template.UpdatedAt = time.Now().UTC()
	if err := s.templates.Update(ctx, template); err != nil {
		return nil, err
	}
	return deploymentTemplateItem(template, integration.SiteID), nil
}

// Delete removes one template.
func (s *DeploymentTemplateService) Delete(ctx context.Context, id string) error {
	return s.templates.Delete(ctx, id)
}

// ReplaceUserData stores new cloud-init without making it readable through the API.
func (s *DeploymentTemplateService) ReplaceUserData(ctx context.Context, id, userData string) error {
	if userData == "" {
		return fmt.Errorf("%w: userData cannot be empty", provisioningdomain.ErrInvalidDeploymentTemplate)
	}
	return s.templates.ReplaceUserData(ctx, id, userData)
}

// ClearUserData removes a template's cloud-init.
func (s *DeploymentTemplateService) ClearUserData(ctx context.Context, id string) error {
	return s.templates.ClearUserData(ctx, id)
}

func deploymentTemplateItem(
	template *provisioningdomain.DeploymentTemplate,
	siteID string,
) *DeploymentTemplateItem {
	return &DeploymentTemplateItem{
		ID:            template.ID,
		SiteID:        siteID,
		IntegrationID: template.IntegrationID,
		Name:          template.Name,
		Description:   template.Description,
		ImageID:       template.ImageID,
		Ephemeral:     template.Ephemeral,
		HasUserData:   template.HasUserData,
		Network: DeploymentNetworkSettingsItem{
			Mode:           string(template.NetworkMode),
			SubnetID:       template.SubnetID,
			DefaultGateway: template.DefaultGateway,
		},
		CreatedAt: wire.Time(template.CreatedAt),
		UpdatedAt: wire.Time(template.UpdatedAt),
	}
}

func validateDeployImage(
	ctx context.Context,
	providers provisioningdomain.ProviderFactory,
	integrationID, imageID string,
) (provisioningdomain.OSProvisioningProvider, *provisioningdomain.OSImage, error) {
	provider, err := providers.For(ctx, integrationID)
	if err != nil {
		return nil, nil, err
	}
	images, err := provider.ListOSImages(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, image := range images {
		if image.ID == imageID {
			return provider, image, nil
		}
	}
	return nil, nil, fmt.Errorf(
		"%w: imageId %q is not available from the provisioner",
		provisioningdomain.ErrInvalidDeploymentTemplate,
		imageID,
	)
}

// validateTemplateNetwork resolves the DHCP default and verifies provider-owned
// subnet references without storing provider interface or target IP data.
func validateTemplateNetwork(
	ctx context.Context,
	provider provisioningdomain.OSProvisioningProvider,
	input *DeploymentNetworkSettingsInput,
) (provisioningdomain.DeploymentNetworkMode, string, bool, error) {
	mode := provisioningdomain.DeploymentNetworkDHCP
	subnetID := ""
	defaultGateway := false
	if input != nil {
		mode = provisioningdomain.DeploymentNetworkMode(strings.ToLower(strings.TrimSpace(input.Mode)))
		if mode == "" {
			mode = provisioningdomain.DeploymentNetworkDHCP
		}
		subnetID = strings.TrimSpace(input.SubnetID)
		defaultGateway = input.DefaultGateway
	}
	if mode != provisioningdomain.DeploymentNetworkDHCP &&
		mode != provisioningdomain.DeploymentNetworkStatic {
		return "", "", false, fmt.Errorf(
			"%w: network.mode must be dhcp or static",
			provisioningdomain.ErrInvalidDeploymentTemplate,
		)
	}
	if mode == provisioningdomain.DeploymentNetworkDHCP && defaultGateway {
		return "", "", false, fmt.Errorf(
			"%w: network.defaultGateway is supported only for static mode",
			provisioningdomain.ErrInvalidDeploymentTemplate,
		)
	}
	networkProvider, err := requireNetworkProvider(provider)
	if err != nil {
		return "", "", false, err
	}
	if mode == provisioningdomain.DeploymentNetworkStatic && subnetID == "" {
		return "", "", false, fmt.Errorf(
			"%w: network.subnetId is required for static mode",
			provisioningdomain.ErrInvalidDeploymentTemplate,
		)
	}
	if subnetID == "" {
		return mode, "", defaultGateway, nil
	}
	subnets, err := networkProvider.ListNetworkSubnets(ctx)
	if err != nil {
		return "", "", false, err
	}
	for _, subnet := range subnets {
		if subnet.ID == subnetID {
			return mode, subnetID, defaultGateway, nil
		}
	}
	return "", "", false, fmt.Errorf(
		"%w: network.subnetId %q is not available from the provisioner",
		provisioningdomain.ErrInvalidDeploymentTemplate,
		subnetID,
	)
}
