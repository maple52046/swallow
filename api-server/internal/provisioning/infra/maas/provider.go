package maas

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
)

const (
	// providerName is persisted in server.ProvisioningSource, so it must stay stable.
	providerName        = "maas"
	providerDisplayName = "Ubuntu MAAS"
)

// Provider implements provisioningdomain.OSProvisioningProvider against MAAS.
type Provider struct {
	client *Client
}

func NewProvider(client *Client) *Provider {
	return &Provider{client: client}
}

func (p *Provider) Name() string { return providerName }

func (p *Provider) Probe(ctx context.Context) (provisioningdomain.ProviderInfo, error) {
	var out versionJSON
	if err := p.client.get(ctx, "/version/", nil, &out); err != nil {
		return provisioningdomain.ProviderInfo{}, translateError(err, "")
	}

	version := out.Version
	if out.Subversion != "" {
		version += " " + out.Subversion
	}

	return provisioningdomain.ProviderInfo{
		Name:        providerName,
		DisplayName: providerDisplayName,
		Version:     version,
	}, nil
}

func (p *Provider) ListMachines(
	ctx context.Context,
	filter provisioningdomain.MachineFilter,
) ([]*provisioningdomain.Machine, error) {
	var out []machineJSON
	if err := p.client.get(ctx, "/machines/", nil, &out); err != nil {
		return nil, translateError(err, "")
	}

	machines := make([]*provisioningdomain.Machine, 0, len(out))
	for i := range out {
		machine := toDomainMachine(&out[i])
		if matchesFilter(machine, filter) {
			machines = append(machines, machine)
		}
	}
	return machines, nil
}

func (p *Provider) GetMachine(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	var out machineJSON
	if err := p.client.get(ctx, machinePath(machineID), nil, &out); err != nil {
		return nil, translateError(err, machineID)
	}
	return toDomainMachine(&out), nil
}

func (p *Provider) ListOSImages(ctx context.Context) ([]*provisioningdomain.OSImage, error) {
	var out []bootResourceJSON
	if err := p.client.get(ctx, "/boot-resources/", nil, &out); err != nil {
		return nil, translateError(err, "")
	}
	return toDomainOSImages(out), nil
}

func (p *Provider) Deploy(
	ctx context.Context,
	req provisioningdomain.DeployRequest,
) (*provisioningdomain.Machine, error) {
	fields := map[string]string{
		"osystem":       req.OSSystem,
		"distro_series": req.DistroSeries,
		"comment":       req.Comment,
	}
	if req.UserData != "" {
		// MAAS serves user data to the machine through its metadata service and
		// expects it base64-encoded on the way in.
		fields["user_data"] = base64.StdEncoding.EncodeToString([]byte(req.UserData))
	}
	if req.Ephemeral {
		// Sent only when asked, so that MAAS keeps deciding what a plain deployment
		// means rather than swallow asserting a default it does not own.
		fields["ephemeral_deploy"] = "true"
	}

	var out machineJSON
	if err := p.client.postOperation(ctx, machinePath(req.MachineID), "deploy", fields, &out); err != nil {
		return nil, translateError(err, req.MachineID)
	}

	// MAAS ignores form fields it does not recognise, so an older version would accept
	// this request and quietly install to disk. Only an explicit contradiction is
	// treated as one: silence means the version predates the field and cannot be read
	// either way.
	if req.Ephemeral && out.EphemeralDeploy != nil && !*out.EphemeralDeploy {
		return nil, &provisioningdomain.ProviderError{
			Kind: provisioningdomain.ProviderErrorRejected,
			Detail: "MAAS accepted the deployment but reports it as non-ephemeral, " +
				"so it is installing to disk. The deployment is already running: " +
				"release the machine if that is not wanted.",
		}
	}
	return toDomainMachine(&out), nil
}

// ValidateDeploymentTarget checks the provider-owned network prerequisite without
// mutating the machine. MAAS refuses deployment when no interface is linked to a
// subnet; Swallow surfaces that before dispatch but never guesses which subnet to use.
func (p *Provider) ValidateDeploymentTarget(ctx context.Context, machineID string) error {
	var machine machineJSON
	if err := p.client.get(ctx, machinePath(machineID), nil, &machine); err != nil {
		return translateError(err, machineID)
	}
	for _, iface := range machine.InterfaceSet {
		for _, link := range iface.Links {
			if link.Subnet != nil {
				return nil
			}
		}
	}
	return &provisioningdomain.ProviderError{
		Kind: provisioningdomain.ProviderErrorRejected,
		Detail: "No MAAS interface is linked to a subnet. Configure the machine's " +
			"Network in MAAS, then check deployment readiness again.",
	}
}

// Capabilities reports what this adapter honours. All of these are present on the MAAS
// versions this adapter targets, so every flag is set; the flags exist so a client can
// hide an action a future provider lacks, and so an unsupported request is refused
// rather than silently dropped.
func (p *Provider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{
		EphemeralDeploy:     true,
		DeploymentReadiness: true,
		Power:               true,
		HardwareValidation:  true,
		OperatorState:       true,
		MachineDetail:       true,
		HardwareInventory:   true,
	}
}

func (p *Provider) Release(ctx context.Context, machineID string) (*provisioningdomain.Machine, error) {
	var out machineJSON
	if err := p.client.postOperation(ctx, machinePath(machineID), "release", nil, &out); err != nil {
		return nil, translateError(err, machineID)
	}
	return toDomainMachine(&out), nil
}

func machinePath(machineID string) string {
	return "/machines/" + url.PathEscape(machineID) + "/"
}

// translateError converts transport and MAAS HTTP failures into the errors the
// provisioning port defines.
//
// machineID is non-empty for machine-scoped calls, where a 404 means that machine
// is gone rather than that the API endpoint is wrong.
func translateError(err error, machineID string) error {
	var transportErr *transportError
	if errors.As(err, &transportErr) {
		return &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorUnavailable,
			Detail: "Could not reach MAAS.",
			Err:    err,
		}
	}

	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		// A decoding or programming failure; not something to blame MAAS for.
		return err
	}

	switch {
	case apiErr.StatusCode == http.StatusNotFound && machineID != "":
		return provisioningdomain.ErrMachineNotFound

	case apiErr.StatusCode == http.StatusUnauthorized, apiErr.StatusCode == http.StatusForbidden:
		// The caller's own credentials were already accepted by swallow, so this can
		// only be the API key swallow is configured with.
		return &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorAuth,
			Detail: "MAAS rejected the API key swallow is configured with.",
			Err:    err,
		}

	case apiErr.StatusCode >= http.StatusInternalServerError:
		return &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorUnavailable,
			Detail: fmt.Sprintf("MAAS reported an internal error (HTTP %d).", apiErr.StatusCode),
			Err:    err,
		}

	default:
		return &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorRejected,
			Detail: rejectionDetail(apiErr),
			Err:    err,
		}
	}
}

// rejectionDetail renders MAAS's own explanation for a refused request.
//
// MAAS answers either with a plain sentence or with a JSON object mapping a field
// name to its complaints; both are flattened to one line so that whatever MAAS
// objected to reaches the operator intact.
func rejectionDetail(e *apiError) string {
	if e.Body == "" {
		return fmt.Sprintf("MAAS refused the request (HTTP %d).", e.StatusCode)
	}

	var byField map[string][]string
	if err := json.Unmarshal([]byte(e.Body), &byField); err == nil && len(byField) > 0 {
		fields := make([]string, 0, len(byField))
		for field := range byField {
			fields = append(fields, field)
		}
		sort.Strings(fields)

		parts := make([]string, 0, len(fields))
		for _, field := range fields {
			parts = append(parts, field+": "+strings.Join(byField[field], " "))
		}
		return "MAAS refused the request: " + strings.Join(parts, "; ")
	}

	return "MAAS refused the request: " + e.Body
}
