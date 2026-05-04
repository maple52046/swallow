package detect

import (
	"fmt"
	"os/exec"
	"strings"
)

// collectIPMI checks whether ipmitool is available and attempts to detect the BMC IP.
// The agent never reads or reports IPMI passwords; only connectivity metadata is collected.
func collectIPMI() (IPMIInfo, error) {
	// Check if ipmitool is present in PATH
	path, err := exec.LookPath("ipmitool")
	if err != nil {
		return IPMIInfo{Available: false, Status: "unavailable", Source: "path-lookup"}, nil
	}
	_ = path

	// Try to get BMC LAN info; failure means IPMI is present but may be unconfigured.
	out, err := exec.Command("ipmitool", "lan", "print").Output()
	if err != nil {
		return IPMIInfo{Available: true, Status: "inaccessible", Source: "ipmitool"}, nil
	}

	bmcIP := parseBMCIP(string(out))
	return IPMIInfo{
		Available: true,
		BMCIP:     bmcIP,
		Source:    "ipmitool",
		Status:    "detected",
	}, nil
}

// parseBMCIP extracts the IP Address field from ipmitool lan print output.
func parseBMCIP(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "IP Address") && !strings.Contains(line, "Source") &&
			!strings.Contains(line, "MAC") {
			_, val, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			ip := strings.TrimSpace(val)
			if ip != "" && ip != "0.0.0.0" {
				return ip
			}
		}
	}
	return fmt.Sprintf("")
}
