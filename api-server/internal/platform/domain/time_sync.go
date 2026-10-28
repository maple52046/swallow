package domain

import "fmt"

// TimeSyncCheck is what a Platform deployment does when its time-sync check finds a node whose
// clock is not synchronized or is too far from the installation's NTP server (decision 065,
// glossary Time Sync Check). A Platform deployment never configures time itself; OS provisioning
// does. The check only verifies it before the platform is installed, because runtimes such as
// Slurm's MUNGE or etcd fail in confusing ways on disagreeing clocks.
type TimeSyncCheck string

const (
	// TimeSyncCheckEnforce fails the deployment, naming the nodes at fault. It is the default.
	TimeSyncCheckEnforce TimeSyncCheck = "enforce"
	// TimeSyncCheckWarn reports the nodes at fault and lets the deployment continue.
	TimeSyncCheckWarn TimeSyncCheck = "warn"
)

// ParseTimeSyncCheck reads the deploy request's timeSyncCheck. Empty means TimeSyncCheckEnforce,
// so a client that predates the field keeps the strict check; any other unknown value is
// ErrInvalidDeployment rather than a silent downgrade to warn.
func ParseTimeSyncCheck(raw string) (TimeSyncCheck, error) {
	switch check := TimeSyncCheck(raw); check {
	case "":
		return TimeSyncCheckEnforce, nil
	case TimeSyncCheckEnforce, TimeSyncCheckWarn:
		return check, nil
	default:
		return "", fmt.Errorf("%w: timeSyncCheck must be %q or %q", ErrInvalidDeployment, TimeSyncCheckEnforce, TimeSyncCheckWarn)
	}
}
