package detect

import (
	"fmt"
	"os/exec"
	"strings"
)

// collectGPUs attempts generic PCI detection first, then enriches with
// vendor-specific tooling if available. Absence of any GPU tooling is not
// an error; the function returns an empty slice in that case.
func collectGPUs() ([]GPUInfo, error) {
	gpus, err := detectGPUsFromLspci()
	if err != nil {
		// lspci unavailable; try vendor tools directly
		gpus = nil
	}

	// Enrich with nvidia-smi if present
	if nvidiaGPUs, err := enrichNvidiaGPUs(); err == nil && len(nvidiaGPUs) > 0 {
		gpus = mergeGPUs(gpus, nvidiaGPUs, "NVIDIA")
	}

	// Enrich with rocm-smi if present
	if amdGPUs, err := enrichAMDGPUs(); err == nil && len(amdGPUs) > 0 {
		gpus = mergeGPUs(gpus, amdGPUs, "AMD")
	}

	return gpus, nil
}

func detectGPUsFromLspci() ([]GPUInfo, error) {
	out, err := exec.Command("lspci").Output()
	if err != nil {
		return nil, fmt.Errorf("GPU detection: lspci: %w", err)
	}

	var gpus []GPUInfo
	idx := int32(0)
	for _, line := range strings.Split(string(out), "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "vga") || strings.Contains(lower, "display") ||
			strings.Contains(lower, "3d controller") || strings.Contains(lower, "gpu") {
			vendor, model := parseLspciLine(line)
			gpus = append(gpus, GPUInfo{Vendor: vendor, Model: model, Index: idx})
			idx++
		}
	}
	return gpus, nil
}

func parseLspciLine(line string) (vendor, model string) {
	// lspci format: "<addr> <class>: <vendor> <model> [<revision>]"
	_, rest, ok := strings.Cut(line, ": ")
	if !ok {
		return "", strings.TrimSpace(line)
	}
	rest = strings.TrimSpace(rest)
	for _, v := range []string{"NVIDIA", "AMD", "ATI", "Intel", "Matrox", "VIA"} {
		if strings.Contains(rest, v) {
			return v, rest
		}
	}
	return "", rest
}

func enrichNvidiaGPUs() ([]GPUInfo, error) {
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=index,name",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return nil, err
	}
	var gpus []GPUInfo
	for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ", ", 2)
		model := ""
		if len(parts) == 2 {
			model = strings.TrimSpace(parts[1])
		}
		gpus = append(gpus, GPUInfo{Vendor: "NVIDIA", Model: model, Index: int32(i)})
	}
	return gpus, nil
}

func enrichAMDGPUs() ([]GPUInfo, error) {
	out, err := exec.Command("rocm-smi", "--showproductname", "--csv").Output()
	if err != nil {
		return nil, err
	}
	var gpus []GPUInfo
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i, line := range lines {
		if i == 0 || line == "" {
			continue // skip header
		}
		parts := strings.SplitN(line, ",", 2)
		model := ""
		if len(parts) == 2 {
			model = strings.TrimSpace(parts[1])
		}
		gpus = append(gpus, GPUInfo{Vendor: "AMD", Model: model, Index: int32(i - 1)})
	}
	return gpus, nil
}

// mergeGPUs replaces any entries in base that match vendor with enriched,
// or appends enriched entries if base has no matching vendor.
func mergeGPUs(base, enriched []GPUInfo, vendor string) []GPUInfo {
	// Remove matching vendor entries from base
	filtered := base[:0]
	for _, g := range base {
		if g.Vendor != vendor {
			filtered = append(filtered, g)
		}
	}
	return append(filtered, enriched...)
}
