package detect

import (
	"fmt"
	"net"
	"os"
)

func collectNetwork() (NetworkInfo, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return NetworkInfo{}, fmt.Errorf("network detection: hostname: %w", err)
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return NetworkInfo{}, fmt.Errorf("network detection: interfaces: %w", err)
	}

	var allIPs []string
	var primaryIP string

	for _, iface := range ifaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			ipStr := ip.String()
			allIPs = append(allIPs, ipStr)
			if primaryIP == "" {
				primaryIP = ipStr
			}
		}
	}

	return NetworkInfo{
		Hostname:  hostname,
		PrimaryIP: primaryIP,
		AllIPs:    allIPs,
	}, nil
}
