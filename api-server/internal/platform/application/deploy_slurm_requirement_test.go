package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

type fixedRequirementReader struct {
	requirement *platformdomain.DeploymentRequirement
	err         error
}

func (r fixedRequirementReader) FindByPlatformType(
	context.Context,
	platformdomain.PlatformType,
) (*platformdomain.DeploymentRequirement, error) {
	return r.requirement, r.err
}

func resourceSlurmHarness() (*DeployService, *recordingLauncher, *deployFakePlatformRepo) {
	servers := slurmServers()
	for _, server := range servers {
		server.Observed.CPUCores = 4
		server.Observed.MemoryMiB = 24576
		server.Observed.StorageGB = 80
	}
	return newDeployHarness(servers...)
}

func enabledSlurmRequirement() *platformdomain.DeploymentRequirement {
	return &platformdomain.DeploymentRequirement{
		PlatformType: platformdomain.PlatformTypeSlurm,
		MinimumResources: &platformdomain.MinimumResources{
			CPUCores: 4, MemoryMiB: 24576, StorageGB: 80,
		},
	}
}

func TestDeploySlurmAcceptsResourcesEqualToRequirement(t *testing.T) {
	service, launcher, _ := resourceSlurmHarness()
	service.AttachDeploymentRequirementReader(fixedRequirementReader{requirement: enabledSlurmRequirement()})

	if _, err := service.Deploy(context.Background(), validSlurmInput()); err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if launcher.launched == nil {
		t.Fatal("eligible deployment was not launched")
	}
}

func TestDeploySlurmRejectsEveryResourceShortfallWithoutSideEffects(t *testing.T) {
	tests := []struct {
		name string
		edit func(*platformdomain.MinimumResources)
		want string
	}{
		{name: "cpu", edit: func(r *platformdomain.MinimumResources) { r.CPUCores = 5 }, want: "CPU 4 cores (minimum 5)"},
		{name: "memory", edit: func(r *platformdomain.MinimumResources) { r.MemoryMiB = 24577 }, want: "memory 24576 MiB (minimum 24577 MiB)"},
		{name: "storage", edit: func(r *platformdomain.MinimumResources) { r.StorageGB = 81 }, want: "storage 80.00 GB (minimum 81.00 GB)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, launcher, platforms := resourceSlurmHarness()
			requirement := enabledSlurmRequirement()
			test.edit(requirement.MinimumResources)
			service.AttachDeploymentRequirementReader(fixedRequirementReader{requirement: requirement})

			_, err := service.Deploy(context.Background(), validSlurmInput())
			if !errors.Is(err, platformdomain.ErrInvalidDeployment) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Deploy() error = %v, want %q", err, test.want)
			}
			if launcher.launched != nil || len(platforms.platforms) != 0 {
				t.Fatal("ineligible deployment created a Platform or Workflow")
			}
		})
	}
}

func TestDeploySlurmRequirementAppliesToLoginOnlyNode(t *testing.T) {
	service, launcher, platforms := resourceSlurmHarness()
	input := validSlurmInput()
	input.SlurmSpec.NodeAssignments[2] = platformdomain.SlurmNodeAssignment{ServerID: "s3", Login: true}
	service.servers.(*deployFakeServerRepo).servers["s3"].Observed.MemoryMiB = 0
	service.AttachDeploymentRequirementReader(fixedRequirementReader{requirement: enabledSlurmRequirement()})

	_, err := service.Deploy(context.Background(), input)
	if !errors.Is(err, platformdomain.ErrInvalidDeployment) || !strings.Contains(err.Error(), "lab-slurm-3") {
		t.Fatalf("Deploy() error = %v, want login-node resource rejection", err)
	}
	if launcher.launched != nil || len(platforms.platforms) != 0 {
		t.Fatal("ineligible login deployment created side effects")
	}
}

func TestDeploySlurmRequirementReadFailureFailsClosed(t *testing.T) {
	service, launcher, platforms := resourceSlurmHarness()
	service.AttachDeploymentRequirementReader(fixedRequirementReader{err: errors.New("mongo unavailable")})

	_, err := service.Deploy(context.Background(), validSlurmInput())
	if err == nil || !strings.Contains(err.Error(), "read Slurm deployment requirement") {
		t.Fatalf("Deploy() error = %v, want repository failure", err)
	}
	if launcher.launched != nil || len(platforms.platforms) != 0 {
		t.Fatal("repository failure must not create side effects")
	}
}

func TestKubernetesIgnoresSlurmRequirementReader(t *testing.T) {
	service, launcher, _ := newDeployHarness(haServers()...)
	service.AttachDeploymentRequirementReader(fixedRequirementReader{err: errors.New("mongo unavailable")})

	if _, err := service.Deploy(context.Background(), validDeployInput()); err != nil {
		t.Fatalf("Kubernetes Deploy() error = %v", err)
	}
	if launcher.launched == nil {
		t.Fatal("Kubernetes deployment was not launched")
	}
}
