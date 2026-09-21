package domain

import "strings"

// ArchitecturesCompatible reports whether a Server architecture can run an OS Image architecture.
// It tolerates the MAAS "amd64/generic" sub-architecture suffix and case, comparing only the base
// architecture. An empty value on either side is not a match, so an unknown architecture never
// silently passes an architecture gate.
func ArchitecturesCompatible(serverArch, imageArch string) bool {
	base := func(arch string) string {
		arch = strings.ToLower(strings.TrimSpace(arch))
		if slash := strings.IndexByte(arch, '/'); slash >= 0 {
			return arch[:slash]
		}
		return arch
	}
	server, image := base(serverArch), base(imageArch)
	return server != "" && image != "" && server == image
}

// DeployTarget is where an OS Deployment runs: on the machine's disk, or from memory.
//
// It is the canonical operator-facing vocabulary. `ram` is the same fact the provider-neutral
// deploy request and the MAAS adapter still call "ephemeral" (the OS runs from memory and leaves
// the disks untouched); the two map one-to-one at the boundary so existing wire, storage, and
// durable-snapshot fields keep working. The dashboard label for `ram` is "RAM deploy (ephemeral)".
type DeployTarget string

const (
	// DeployTargetDisk installs the OS onto the machine's disk (ephemeral=false).
	DeployTargetDisk DeployTarget = "disk"
	// DeployTargetRAM boots the OS from memory, leaving the disks untouched (ephemeral=true).
	DeployTargetRAM DeployTarget = "ram"
)

// Valid reports whether the value is a known deploy target.
func (t DeployTarget) Valid() bool {
	return t == DeployTargetDisk || t == DeployTargetRAM
}

// Ephemeral maps the deploy target onto the provider-neutral ephemeral flag: `ram` is ephemeral,
// `disk` is not. This is the single translation point between the deploy-target vocabulary and
// the older ephemeral boolean that the deploy request, MAAS adapter, and durable snapshots use.
func (t DeployTarget) Ephemeral() bool {
	return t == DeployTargetRAM
}

// DeployTargetForEphemeral is the inverse of Ephemeral: it names the deploy target an ephemeral
// flag corresponds to, so a stored ephemeral fact can be reported in the deploy-target vocabulary.
func DeployTargetForEphemeral(ephemeral bool) DeployTarget {
	if ephemeral {
		return DeployTargetRAM
	}
	return DeployTargetDisk
}

// ParseDeployTarget normalizes an operator-supplied deploy-target string. It accepts the canonical
// "disk"/"ram" case-insensitively and returns ok=false for anything else, so a delivery adapter can
// map an unknown value onto a 400 rather than silently defaulting a deploy mode.
func ParseDeployTarget(value string) (DeployTarget, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(DeployTargetDisk):
		return DeployTargetDisk, true
	case string(DeployTargetRAM):
		return DeployTargetRAM, true
	default:
		return "", false
	}
}
