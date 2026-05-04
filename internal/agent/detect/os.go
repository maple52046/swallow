package detect

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"

	"os/exec"
)

func collectOS() (OSInfo, error) {
	info := OSInfo{
		Type:         runtime.GOOS,
		Architecture: runtime.GOARCH,
	}

	// Read distribution info from /etc/os-release (Linux standard)
	f, err := os.Open("/etc/os-release")
	if err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			key, val, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			val = strings.Trim(val, `"`)
			switch key {
			case "ID":
				info.Distribution = val
			case "VERSION_ID":
				info.Version = val
			}
		}
	}

	// Kernel version via uname -r
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		info.KernelVersion = strings.TrimSpace(string(out))
	}

	if info.Type == "" {
		return OSInfo{}, fmt.Errorf("OS detection: could not determine OS type")
	}

	return info, nil
}
