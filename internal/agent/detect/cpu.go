package detect

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func collectCPU() (CPUInfo, error) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return CPUInfo{}, fmt.Errorf("CPU detection: %w", err)
	}
	defer f.Close()

	info := CPUInfo{}
	physicalIDs := map[string]struct{}{}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		switch key {
		case "model name":
			if info.Model == "" {
				info.Model = val
			}
		case "processor":
			info.Threads++
		case "physical id":
			physicalIDs[val] = struct{}{}
		}
	}

	cores := int32(len(physicalIDs))
	if cores == 0 && info.Threads > 0 {
		// Single-socket systems may omit physical id; treat thread count as core count.
		cores = info.Threads
	}
	info.Cores = cores

	return info, nil
}
