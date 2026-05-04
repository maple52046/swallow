// Package detect implements modular local environment detection for the agent.
// Each collector is independent; a partial failure (e.g. GPU unavailable) does
// not abort the whole detection run unless the failed collector is marked fatal.
package detect

import "time"

// OSInfo describes the operating system detected on the local machine.
type OSInfo struct {
	Type          string
	Distribution  string
	Version       string
	KernelVersion string
	Architecture  string
}

// CPUInfo describes the CPU detected on the local machine.
type CPUInfo struct {
	Model   string
	Cores   int32
	Threads int32
}

// MemoryInfo describes total RAM detected on the local machine.
type MemoryInfo struct {
	TotalKB int64
}

// GPUInfo describes a single GPU detected on the local machine.
type GPUInfo struct {
	Vendor string
	Model  string
	Index  int32
}

// NetworkInfo describes the network identity of the local machine.
type NetworkInfo struct {
	Hostname  string
	PrimaryIP string
	AllIPs    []string
}

// IPMIInfo describes the IPMI/BMC availability on the local machine.
// The agent never attempts to read or report IPMI passwords.
type IPMIInfo struct {
	Available bool
	BMCIP     string
	Source    string
	Status    string
}

// Inventory aggregates all detected environment data.
type Inventory struct {
	OS        OSInfo
	CPU       CPUInfo
	Memory    MemoryInfo
	GPUs      []GPUInfo
	Network   NetworkInfo
	IPMI      IPMIInfo
	CollectedAt time.Time
}

// Collect runs all registered collectors and returns the combined Inventory.
// Partial failures are collected and returned alongside the inventory so the
// caller can log warnings without aborting startup. OS detection is the only
// fatal case; all other collectors degrade gracefully.
func Collect() (Inventory, []error) {
	inv := Inventory{CollectedAt: time.Now().UTC()}
	var errs []error

	// OS detection is required; all other detections are best-effort.
	os, err := collectOS()
	if err != nil {
		return inv, []error{err}
	}
	inv.OS = os

	if cpu, err := collectCPU(); err != nil {
		errs = append(errs, err)
	} else {
		inv.CPU = cpu
	}

	if mem, err := collectMemory(); err != nil {
		errs = append(errs, err)
	} else {
		inv.Memory = mem
	}

	if gpus, err := collectGPUs(); err != nil {
		errs = append(errs, err)
		inv.GPUs = nil
	} else {
		inv.GPUs = gpus
	}

	if net, err := collectNetwork(); err != nil {
		errs = append(errs, err)
	} else {
		inv.Network = net
	}

	if ipmi, err := collectIPMI(); err != nil {
		errs = append(errs, err)
		inv.IPMI = IPMIInfo{Available: false, Status: "detection-failed", Source: "unknown"}
	} else {
		inv.IPMI = ipmi
	}

	return inv, errs
}
