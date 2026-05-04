package detect

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func collectMemory() (MemoryInfo, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemoryInfo{}, fmt.Errorf("memory detection: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return MemoryInfo{}, fmt.Errorf("memory detection: parse MemTotal: %w", err)
		}
		return MemoryInfo{TotalKB: kb}, nil
	}

	return MemoryInfo{}, fmt.Errorf("memory detection: MemTotal not found in /proc/meminfo")
}
