package app

import (
	"context"
	"fmt"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// integrationDependents implements siteapp.IntegrationDependents over the records of the
// provisioning and platform contexts that name an Integration, so deleting an Integration neither
// strands them nor leaves them behind.
type integrationDependents struct {
	bootISOs      provisioningdomain.BootISORepository
	platforms     platformdomain.PlatformRepository
	metadata      provisioningdomain.OSImageMetadataRepository
	verifications provisioningdomain.OSImageVerificationRepository
}

// CheckDeletable refuses while a Boot ISO chains to the Integration or a Platform reads through it.
// Platforms are few, so all of the Integration's Site's are read.
func (d integrationDependents) CheckDeletable(ctx context.Context, integrationID string) error {
	isos, err := d.bootISOs.List(ctx, provisioningdomain.BootISOFilter{IntegrationID: integrationID})
	if err != nil {
		return fmt.Errorf("list boot ISOs: %w", err)
	}
	if len(isos) > 0 {
		return sitedomain.ErrIntegrationHasBootISOs
	}
	platforms, err := d.platforms.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list platforms: %w", err)
	}
	for _, platform := range platforms {
		if platform.IntegrationID == integrationID || platform.OwnedIntegrationID == integrationID {
			return sitedomain.ErrIntegrationHasPlatforms
		}
	}
	return nil
}

// RemoveRecords deletes the Integration's OS Image metadata and verification records. Both
// describe images of that provider only, so nothing else reads them once it is gone.
func (d integrationDependents) RemoveRecords(ctx context.Context, integrationID string) error {
	if err := d.metadata.DeleteByIntegration(ctx, integrationID); err != nil {
		return fmt.Errorf("delete OS Image metadata: %w", err)
	}
	if err := d.verifications.DeleteByIntegration(ctx, integrationID); err != nil {
		return fmt.Errorf("delete OS Image verifications: %w", err)
	}
	return nil
}
