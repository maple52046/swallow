// Package hostenroll enrolls the host the CLI runs on into a provisioner while the host keeps its
// operating system (Server Enrollment, decision 053; contract server-enrollment.md).
//
// Component boundary: this is the one part of the CLI that talks to a provisioner instead of
// api-server, because hardware facts can only be read on the host itself. It wraps the
// provisioner's own enrollment tooling (for MAAS: maas-run-scripts register-machine, then
// report-results) and never calls api-server; the Server appears there through reconciliation.
// The endpoint and credential come from api-server's enrollment bundle, which an admin copies to
// the host. Nothing here may persist the credential or print it.
package hostenroll

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ProviderMAAS is the only provisioner kind with existing-host enrollment today.
const ProviderMAAS = "maas"

// Options is one enrollment request.
type Options struct {
	// Provisioner is the provisioner kind, e.g. "maas".
	Provisioner string
	// Endpoint is the provisioner address the host reaches, for MAAS its region URL.
	Endpoint string
	// Token is the provisioner credential from the enrollment bundle. It is a secret.
	Token string
	// Hostname is the name to register; empty uses the host's short name.
	Hostname string
	// Stdout and Stderr receive progress and the provider tool's own output.
	Stdout, Stderr io.Writer
}

// enroller holds the host-facing effects so tests can replace them.
type enroller struct {
	httpClient *http.Client
	geteuid    func() int
	lookPath   func(string) (string, error)
	hostname   func() (string, error)
	// run executes name with args in dir, writing to stdout and stderr.
	run func(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) error
}

// Enroll registers this host with the provisioner and records its hardware, without a reboot.
//
// It must run as root on the host being enrolled: the provider's hardware report reads devices
// only root can. Errors name the step that failed; the provider tool's own output is passed
// through on Stderr. ctx cancellation stops the running step. Temporary files, including the
// Machine credentials the provider returns, are removed before Enroll returns.
func Enroll(ctx context.Context, opts Options) error {
	return defaultEnroller().enroll(ctx, opts)
}

func defaultEnroller() enroller {
	return enroller{
		httpClient: &http.Client{Timeout: 2 * time.Minute},
		geteuid:    os.Geteuid,
		lookPath:   exec.LookPath,
		hostname:   os.Hostname,
		run: func(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) error {
			command := exec.CommandContext(ctx, name, args...)
			command.Dir, command.Stdout, command.Stderr = dir, stdout, stderr
			return command.Run()
		},
	}
}

func (e enroller) enroll(ctx context.Context, opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	switch strings.ToLower(strings.TrimSpace(opts.Provisioner)) {
	case ProviderMAAS:
		return e.enrollMAAS(ctx, opts)
	case "":
		return errors.New("--provisioner is required (supported: maas)")
	default:
		return fmt.Errorf("provisioner %q has no existing-host enrollment (supported: maas)", opts.Provisioner)
	}
}

// enrollMAAS mirrors the enrollment script api-server serves: download maas-run-scripts from the
// region, register-machine (the Machine is created as Deployed), then report-results.
func (e enroller) enrollMAAS(ctx context.Context, opts Options) error {
	endpoint := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(opts.Endpoint), "/"), "/api/2.0")
	if endpoint == "" {
		return errors.New("--endpoint is required: the MAAS region URL, e.g. http://10.0.0.5:5240/MAAS")
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return fmt.Errorf("--endpoint %q must be an http or https URL", endpoint)
	}
	token := strings.TrimSpace(opts.Token)
	if token == "" {
		return errors.New("--token is required: the MAAS API key from the enrollment bundle (or set SWALLOW_ENROLL_TOKEN)")
	}
	if e.geteuid() != 0 {
		return errors.New("run as root (sudo): recording the hardware needs root")
	}
	if _, err := e.lookPath("python3"); err != nil {
		return errors.New("python3 is required by maas-run-scripts; install it and retry")
	}
	name := strings.TrimSpace(opts.Hostname)
	if name == "" {
		full, err := e.hostname()
		if err != nil {
			return fmt.Errorf("read the host name: %w; pass --hostname", err)
		}
		name, _, _ = strings.Cut(full, ".")
	}

	workdir, err := os.MkdirTemp("", "swallow-enroll-")
	if err != nil {
		return fmt.Errorf("create a working directory: %w", err)
	}
	defer os.RemoveAll(workdir)

	script := filepath.Join(workdir, "maas-run-scripts")
	if err := e.download(ctx, endpoint+"/maas-run-scripts", script); err != nil {
		return err
	}

	fmt.Fprintf(opts.Stdout, "Registering %s with MAAS at %s as a deployed machine\n", name, endpoint)
	var registered bytes.Buffer
	// The token is an argument because that is how register-machine takes it.
	if err := e.run(ctx, workdir, &registered, opts.Stderr, script, "register-machine", "--hostname", name, endpoint, token); err != nil {
		return fmt.Errorf("maas-run-scripts register-machine failed: %w", err)
	}
	creds, err := machineCredentials(workdir, registered.Bytes())
	if err != nil {
		return err
	}

	fmt.Fprintf(opts.Stdout, "Recording the hardware of %s\n", name)
	if err := e.run(ctx, workdir, opts.Stdout, opts.Stderr, script, "report-results", "--config", creds); err != nil {
		return fmt.Errorf("maas-run-scripts report-results failed: %w", err)
	}
	fmt.Fprintf(opts.Stdout, "Enrolled %s. It appears in swallow as Deployed after the next inventory sync.\n", name)
	return nil
}

// download saves url to path as an owner-only executable.
func (e enroller) download(ctx context.Context, url, path string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("download maas-run-scripts: %w", err)
	}
	response, err := e.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("download maas-run-scripts from %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download maas-run-scripts from %s: HTTP %d", url, response.StatusCode)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return fmt.Errorf("save maas-run-scripts: %w", err)
	}
	if _, err := io.Copy(file, response.Body); err != nil {
		_ = file.Close()
		return fmt.Errorf("save maas-run-scripts: %w", err)
	}
	return file.Close()
}

// machineCredentials returns the path of the Machine credentials register-machine produced. It
// prints them as YAML; a version that writes a *-creds.yaml file instead is accepted too.
func machineCredentials(workdir string, printed []byte) (string, error) {
	if bytes.Contains(printed, []byte("reporting:")) {
		path := filepath.Join(workdir, "creds.yaml")
		if err := os.WriteFile(path, printed, 0o600); err != nil {
			return "", fmt.Errorf("save the machine credentials: %w", err)
		}
		return path, nil
	}
	matches, err := filepath.Glob(filepath.Join(workdir, "*-creds.yaml"))
	if err == nil && len(matches) > 0 {
		return matches[0], nil
	}
	return "", errors.New("maas-run-scripts register-machine returned no machine credentials")
}
