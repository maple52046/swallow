package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// platformDeploymentTemplates is the Platform context's port to Deployment Templates
// (platformdomain.DeploymentTemplates) over the OS Provisioning context's template store. It reads
// and creates templates the way the template service would, without the live catalog check: a
// generated template's image was validated with the Platform's machine preparation, and its creation
// must stay inside the store transaction that accepts the realization (decision 083).
type platformDeploymentTemplates struct {
	repo provisioningdomain.DeploymentTemplateRepository
}

// Find implements platformdomain.DeploymentTemplates.
func (t platformDeploymentTemplates) Find(ctx context.Context, id string) (*platformdomain.DeploymentTemplateIntent, error) {
	if id == "" {
		return nil, nil
	}
	template, err := t.repo.FindByID(ctx, id)
	if errors.Is(err, provisioningdomain.ErrDeploymentTemplateNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	intent := &platformdomain.DeploymentTemplateIntent{
		ID: template.ID, IntegrationID: template.IntegrationID, Name: template.Name,
		Ephemeral: template.Ephemeral, Digest: templateDigest(template),
	}
	if minimum := template.MinimumHardware; minimum != nil {
		intent.Minimum = &platformdomain.TemplateMinimum{
			CPUCores: minimum.CPUCores, MemoryMiB: minimum.MemoryMiB, StorageGB: minimum.StorageGB,
			GPUModel: minimum.GPUModel, GPUCount: minimum.GPUCount,
		}
	}
	return intent, nil
}

// templateDigest is what a template installs, hashed: its image, deploy mode, network intent, and
// whether it carries cloud-init. A rename or a new floor is not a different installation; replacing
// the sealed cloud-init is seen through updatedAt only when it is set at the same time as one of these.
func templateDigest(template *provisioningdomain.DeploymentTemplate) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%t|%s|%s|%t|%t",
		template.ImageID, template.Ephemeral, template.NetworkMode, template.SubnetID, template.DefaultGateway, template.HasUserData)))
	return hex.EncodeToString(sum[:])
}

// NameTaken implements platformdomain.DeploymentTemplates.
func (t platformDeploymentTemplates) NameTaken(ctx context.Context, integrationID, name, id string) (bool, error) {
	templates, err := t.repo.List(ctx, provisioningdomain.DeploymentTemplateFilter{IntegrationID: integrationID})
	if err != nil {
		return false, err
	}
	for _, template := range templates {
		if template.ID != id && strings.EqualFold(strings.TrimSpace(template.Name), strings.TrimSpace(name)) {
			return true, nil
		}
	}
	return false, nil
}

// EnsureGenerated implements platformdomain.DeploymentTemplates: each generated template that does
// not exist is created from preparation, its cloud-init sealed by the store as any template's is.
func (t platformDeploymentTemplates) EnsureGenerated(ctx context.Context, generated []platformdomain.GeneratedTemplate, preparation platformdomain.MachinePreparation) error {
	for _, g := range generated {
		if _, err := t.repo.FindByID(ctx, g.ID); err == nil {
			continue
		} else if !errors.Is(err, provisioningdomain.ErrDeploymentTemplateNotFound) {
			return err
		}
		mode, ok := provisioningdomain.NormalizeDeploymentNetworkMode(preparation.NetworkMode)
		if !ok {
			return fmt.Errorf("create Deployment Template %s: network mode %q is not automatic, dhcp, or static", g.Name, preparation.NetworkMode)
		}
		if strings.TrimSpace(preparation.ImageID) == "" {
			return fmt.Errorf("create Deployment Template %s: the machine preparation names no image", g.Name)
		}
		now := time.Now().UTC()
		template := &provisioningdomain.DeploymentTemplate{
			ID: g.ID, IntegrationID: g.IntegrationID, Name: g.Name,
			Description: "Created by its Platform for node group " + g.Group + ", from the Platform's machine preparation.",
			ImageID:     strings.TrimSpace(preparation.ImageID), Ephemeral: preparation.Ephemeral != nil && *preparation.Ephemeral,
			NetworkMode: mode, SubnetID: strings.TrimSpace(preparation.SubnetID),
			DefaultGateway: preparation.DefaultGateway != nil && *preparation.DefaultGateway,
			CreatedAt:      now, UpdatedAt: now,
		}
		userData := ""
		if preparation.UserDataMode == "replace" {
			userData = preparation.UserData
		}
		template.HasUserData = userData != ""
		if err := t.repo.Create(ctx, template, userData); err != nil {
			return fmt.Errorf("create Deployment Template %s: %w", g.Name, err)
		}
	}
	return nil
}

// templateReferenceReader is the OS Provisioning context's port to the Platforms that install from
// templates (provisioningdomain.DeploymentTemplateReferenceReader): every node group's template, and
// a machine preparation's template, of every Platform with a spec.
type templateReferenceReader struct {
	platforms platformdomain.PlatformRepository
	modules   map[platformdomain.PlatformType]platformdomain.PlatformModule
}

// TemplateReferences implements provisioningdomain.DeploymentTemplateReferenceReader.
func (r templateReferenceReader) TemplateReferences(ctx context.Context) (map[string][]provisioningdomain.DeploymentTemplateReference, error) {
	platforms, err := r.platforms.List(ctx, "")
	if err != nil {
		return nil, err
	}
	references := map[string][]provisioningdomain.DeploymentTemplateReference{}
	for _, platform := range platforms {
		if platform.Spec == nil {
			continue
		}
		seen := map[string]bool{}
		add := func(templateID, group string) {
			if templateID == "" || seen[templateID+"/"+group] {
				return
			}
			seen[templateID+"/"+group] = true
			references[templateID] = append(references[templateID], provisioningdomain.DeploymentTemplateReference{
				PlatformID: platform.ID, PlatformName: platform.Name, Group: group,
			})
		}
		if grouped, ok := r.modules[platform.Type].(platformdomain.NodeGroupModule); ok {
			for _, group := range grouped.NodeGroups(*platform.Spec) {
				add(group.TemplateID, group.Name)
			}
		}
		add(platform.Spec.MachinePreparation.TemplateID, "machine preparation")
		for _, assignment := range platform.Spec.MachinePreparation.Assignments {
			add(assignment.TemplateID, "member "+assignment.ServerID)
		}
	}
	return references, nil
}

var (
	_ platformdomain.DeploymentTemplates                   = platformDeploymentTemplates{}
	_ provisioningdomain.DeploymentTemplateReferenceReader = templateReferenceReader{}
)

// lateTemplateReferences lets the template service, built before the Platform store, read the
// references once the composition root has set reader; until then it reports none.
type lateTemplateReferences struct {
	reader provisioningdomain.DeploymentTemplateReferenceReader
}

// TemplateReferences implements provisioningdomain.DeploymentTemplateReferenceReader.
func (l *lateTemplateReferences) TemplateReferences(ctx context.Context) (map[string][]provisioningdomain.DeploymentTemplateReference, error) {
	if l.reader == nil {
		return map[string][]provisioningdomain.DeploymentTemplateReference{}, nil
	}
	return l.reader.TemplateReferences(ctx)
}
