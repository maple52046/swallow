package domain

import (
	"errors"
	"fmt"
	"strings"
)

// ContainerRuntime is the container runtime feature of a Slurm Platform (decision 063). Slurm runs
// no containers itself, but its workloads (AI ones especially) usually do, so a Slurm deploy may
// install one runtime on every compute and login node. The runtime is Managed Software, not a
// Platform component: each Server's install state is its Software Assignment, and the Platform
// records only the choice. Empty means the feature is off.
type ContainerRuntime string

const (
	ContainerRuntimeDockerCE ContainerRuntime = "docker-ce"
	ContainerRuntimePodman   ContainerRuntime = "podman"
)

// ParseContainerRuntime trims value and accepts "" (feature off), docker-ce, or podman.
func ParseContainerRuntime(value string) (ContainerRuntime, error) {
	runtime := ContainerRuntime(strings.TrimSpace(value))
	switch runtime {
	case "", ContainerRuntimeDockerCE, ContainerRuntimePodman:
		return runtime, nil
	default:
		return "", fmt.Errorf("%w: kind must be %s or %s, got %q", ErrInvalidContainerRuntime,
			ContainerRuntimeDockerCE, ContainerRuntimePodman, value)
	}
}

// ContainerRuntimeLaunch adds the container runtime Job to a Slurm deployment Workflow: Kind
// installed on ServerIDs (the compute and login nodes).
type ContainerRuntimeLaunch struct {
	Kind      ContainerRuntime
	ServerIDs []string
}

var (
	// ErrInvalidContainerRuntime means a container runtime request cannot apply: an unknown kind,
	// or a Platform type without the feature.
	ErrInvalidContainerRuntime = errors.New("invalid container runtime request")
	// ErrComposedSoftwareConflict means a deployment's targets cannot take the Managed Software it
	// composes, for example Podman requested where Docker CE (mutually exclusive) is installed.
	ErrComposedSoftwareConflict = errors.New("the deployment's targets cannot take the composed software")
)
