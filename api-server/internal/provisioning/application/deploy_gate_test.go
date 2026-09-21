package application

import (
	"context"
	"errors"
	"testing"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

// TestRequireCustomImageVerified pins the deploy gate: an unverified custom image is blocked for the
// requested deploy target, a verified one passes, and synced provider images are never gated.
func TestRequireCustomImageVerified(t *testing.T) {
	customImage := &provisioningdomain.OSImage{ID: "custom/rocky", OSSystem: "custom", Architecture: "amd64"}
	syncedImage := &provisioningdomain.OSImage{ID: "ubuntu/jammy", OSSystem: "ubuntu", Architecture: "amd64"}

	t.Run("unverified custom image is blocked", func(t *testing.T) {
		uc := &DeployServersUseCase{verifications: newOSImageVerificationRepoFake()}
		err := uc.requireCustomImageVerified(context.Background(), "int-1", customImage, provisioningdomain.DeployTargetDisk)
		if !errors.Is(err, provisioningdomain.ErrDeploymentBatchConflict) {
			t.Fatalf("err = %v, want a deployment batch conflict", err)
		}
	})

	t.Run("custom image verified for the target passes", func(t *testing.T) {
		verifications := newOSImageVerificationRepoFake()
		verifications.seed(&provisioningdomain.OSImageVerification{
			IntegrationID: "int-1", ImageID: "custom/rocky", Architecture: "amd64",
			Targets: map[provisioningdomain.DeployTarget]provisioningdomain.OSImageVerificationEvidence{
				provisioningdomain.DeployTargetDisk: {},
			},
		})
		uc := &DeployServersUseCase{verifications: verifications}
		if err := uc.requireCustomImageVerified(context.Background(), "int-1", customImage, provisioningdomain.DeployTargetDisk); err != nil {
			t.Fatalf("verified disk image blocked: %v", err)
		}
		// Verified for disk must not imply ram.
		if err := uc.requireCustomImageVerified(context.Background(), "int-1", customImage, provisioningdomain.DeployTargetRAM); err == nil {
			t.Error("ram deploy of a disk-only-verified image must still be blocked")
		}
	})

	t.Run("synced provider image is never gated", func(t *testing.T) {
		uc := &DeployServersUseCase{verifications: newOSImageVerificationRepoFake()}
		if err := uc.requireCustomImageVerified(context.Background(), "int-1", syncedImage, provisioningdomain.DeployTargetDisk); err != nil {
			t.Fatalf("synced image blocked: %v", err)
		}
	})

	t.Run("nil repository disables the gate", func(t *testing.T) {
		uc := &DeployServersUseCase{}
		if err := uc.requireCustomImageVerified(context.Background(), "int-1", customImage, provisioningdomain.DeployTargetDisk); err != nil {
			t.Fatalf("nil repo must disable the gate, got %v", err)
		}
	})
}
