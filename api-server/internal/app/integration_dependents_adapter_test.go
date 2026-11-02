package app

import (
	"context"
	"errors"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

type dependentISOs struct {
	provisioningdomain.BootISORepository
	byIntegration map[string][]*provisioningdomain.BootISO
}

func (r dependentISOs) List(_ context.Context, filter provisioningdomain.BootISOFilter) ([]*provisioningdomain.BootISO, error) {
	return r.byIntegration[filter.IntegrationID], nil
}

type dependentPlatforms struct {
	platformdomain.PlatformRepository
	platforms []*platformdomain.Platform
}

func (r dependentPlatforms) List(context.Context, string) ([]*platformdomain.Platform, error) {
	return r.platforms, nil
}

type deletedMetadata struct {
	provisioningdomain.OSImageMetadataRepository
	deleted []string
}

func (r *deletedMetadata) DeleteByIntegration(_ context.Context, integrationID string) error {
	r.deleted = append(r.deleted, integrationID)
	return nil
}

type deletedVerifications struct {
	provisioningdomain.OSImageVerificationRepository
	deleted []string
}

func (r *deletedVerifications) DeleteByIntegration(_ context.Context, integrationID string) error {
	r.deleted = append(r.deleted, integrationID)
	return nil
}

// A Boot ISO built for the Integration, or a Platform that reads through it (its reader or its
// owned credential Integration), keeps it; another Integration's dependents do not.
func TestIntegrationDependentsCheckDeletable(t *testing.T) {
	dependents := integrationDependents{
		bootISOs: dependentISOs{byIntegration: map[string][]*provisioningdomain.BootISO{"maas-1": {{ID: "iso-1"}}}},
		platforms: dependentPlatforms{platforms: []*platformdomain.Platform{
			{ID: "p1", IntegrationID: "k8s-reader"}, {ID: "p2", OwnedIntegrationID: "slurm-owned"},
		}},
	}
	for integrationID, want := range map[string]error{
		"maas-1":      sitedomain.ErrIntegrationHasBootISOs,
		"k8s-reader":  sitedomain.ErrIntegrationHasPlatforms,
		"slurm-owned": sitedomain.ErrIntegrationHasPlatforms,
		"maas-2":      nil,
	} {
		if err := dependents.CheckDeletable(context.Background(), integrationID); !errors.Is(err, want) {
			t.Errorf("CheckDeletable(%s) = %v, want %v", integrationID, err, want)
		}
	}
}

// Removing the records deletes the Integration's OS Image metadata and verifications.
func TestIntegrationDependentsRemoveRecords(t *testing.T) {
	metadata, verifications := &deletedMetadata{}, &deletedVerifications{}
	dependents := integrationDependents{metadata: metadata, verifications: verifications}
	if err := dependents.RemoveRecords(context.Background(), "maas-1"); err != nil {
		t.Fatalf("RemoveRecords() error = %v", err)
	}
	if len(metadata.deleted) != 1 || metadata.deleted[0] != "maas-1" || len(verifications.deleted) != 1 || verifications.deleted[0] != "maas-1" {
		t.Errorf("deleted metadata %v, verifications %v; want maas-1 in both", metadata.deleted, verifications.deleted)
	}
}
