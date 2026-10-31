package temporalworkflow

import (
	"bytes"
	"context"
	"os/exec"
	"strconv"
)

// sshKeyscanHostKeyScanner is the production HostKeyScanner. It shells out to ssh-keyscan,
// which returns every host's advertised key types in known_hosts format — matching what the
// ssh client used by the run presents and verifies. ssh-keyscan must be present in the image
// that runs this executor (the worker image ships it).
type sshKeyscanHostKeyScanner struct {
	command     string
	timeoutSecs int
}

// NewSSHKeyscanHostKeyScanner returns the production scanner.
func NewSSHKeyscanHostKeyScanner() HostKeyScanner {
	return sshKeyscanHostKeyScanner{command: "ssh-keyscan", timeoutSecs: 8}
}

// Scan returns known_hosts lines for the given addresses on the given SSH port. ssh-keyscan
// writes keys to stdout and diagnostics to stderr; the captured stdout is returned even on a
// non-zero exit so a partial result (some hosts reachable) is still usable, and the caller
// decides whether an empty result is fatal. A per-host connect timeout bounds the total time.
func (s sshKeyscanHostKeyScanner) Scan(ctx context.Context, addresses []string, port int) (string, error) {
	if len(addresses) == 0 {
		return "", nil
	}
	args := []string{"-T", strconv.Itoa(s.timeoutSecs)}
	if port > 0 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	args = append(args, addresses...)
	cmd := exec.CommandContext(ctx, s.command, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	return stdout.String(), err
}
