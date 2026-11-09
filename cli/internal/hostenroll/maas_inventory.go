package hostenroll

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"slices"
)

// This file keeps a MAAS enrollment's hardware report honest about the host's address. MAAS 3.7
// can process a deploy-info report without errors yet leave the interface that carries the host's
// address without its IP link, so the Machine has no ip_addresses and the Server has no address
// in swallow; sending the same report once more makes MAAS link it (observed on MAAS 3.7.2, the
// trigger not yet traced in its source). The check reads the Machine back with the enrollment
// bundle's API key; MAAS processes a report before report-results returns, so no wait is needed.

// reportAgain reports this host's hardware under the Machine systemID, which it is already
// registered as: maas-run-scripts get-machine-token issues that Machine's credentials (with the
// admin API key, as register-machine does), then report-results sends the report. It returns the
// credentials' path in workdir so the report can be sent again. The caller has already checked
// that the Machine is this host.
func (e enroller) reportAgain(ctx context.Context, workdir, script, endpoint, token, name, systemID string, opts Options) (string, error) {
	creds := filepath.Join(workdir, "machine-creds.yaml")
	// The token is an argument because that is how get-machine-token takes it.
	if err := e.run(ctx, workdir, io.Discard, opts.Stderr, script, "get-machine-token", endpoint, token, systemID, creds); err != nil {
		return "", fmt.Errorf("maas-run-scripts get-machine-token for Machine %s failed: %w", systemID, err)
	}
	fmt.Fprintf(opts.Stdout, "Recording the hardware of %s again\n", name)
	if err := e.reportResults(ctx, workdir, script, creds, opts); err != nil {
		return "", err
	}
	return creds, nil
}

// ensureAddressRecorded checks that MAAS recorded the address this host reaches MAAS from on the
// host's Machine (the one registered under name, or systemID), and sends the report once more
// through reportAgain when it did not. It is best effort and never fails the enrollment, which
// has already happened: an address it cannot determine (or a loopback one, when the host is the
// MAAS host itself) is not checked, and an address still missing after the second report, or a
// Machine it cannot read, is reported as a warning naming what to look at.
func (e enroller) ensureAddressRecorded(ctx context.Context, api *maasAPI, name, systemID, endpoint string, reportAgain func() error, stdout io.Writer) {
	address, err := e.routeAddress(endpoint)
	if err != nil || address == "" {
		return
	}
	if ip := net.ParseIP(address); ip == nil || ip.IsLoopback() {
		return
	}
	recorded := func() (*maasMachine, bool, error) {
		machine, err := e.findMachine(ctx, api, name, systemID)
		if err != nil {
			return nil, false, err
		}
		return machine, slices.Contains(machine.IPAddresses, address), nil
	}

	machine, ok, err := recorded()
	if err != nil {
		fmt.Fprintf(stdout, "Warning: could not read the Machine back to check its address %s: %v\n", address, err)
		return
	}
	if ok {
		return
	}
	fmt.Fprintf(stdout, "MAAS did not record %s, the address %s reaches MAAS from; reporting the hardware once more\n", address, name)
	if err := reportAgain(); err != nil {
		fmt.Fprintf(stdout, "Warning: reporting the hardware again failed: %v\n", err)
		return
	}
	if machine, ok, err = recorded(); err == nil && ok {
		fmt.Fprintf(stdout, "MAAS now records %s for %s.\n", address, name)
		return
	}
	id := systemID
	if machine != nil {
		id = machine.SystemID
	}
	fmt.Fprintf(stdout, "Warning: MAAS still has no address %s for %s, so swallow will not show one. "+
		"Check the events of MAAS Machine %s for a refused network report (for example an overlapping subnet).\n", address, name, id)
}

// sourceAddress returns the local address the host's routing table picks to reach endpoint's host,
// by connecting a UDP socket, which sends nothing. An endpoint whose host does not resolve is an
// error; the caller then skips the check.
func sourceAddress(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("endpoint %q has no host", endpoint)
	}
	port := parsed.Port()
	if port == "" {
		port = "80"
		if parsed.Scheme == "https" {
			port = "443"
		}
	}
	conn, err := net.Dial("udp", net.JoinHostPort(parsed.Hostname(), port))
	if err != nil {
		return "", err
	}
	defer conn.Close()
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", fmt.Errorf("unexpected local address %v", conn.LocalAddr())
	}
	return local.IP.String(), nil
}
