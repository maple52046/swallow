// Package clusterapi reads live state from cluster APIs.
//
// Both readers speak plain REST rather than pulling in a vendored client library: swallow
// reads one collection from each cluster and never writes, so a full client would be a
// large dependency for a single GET.
package clusterapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
)

const maxErrorBodyBytes = 512

// Kubernetes role labels. A node carrying either is a control-plane node; the label was
// renamed upstream, so both are checked.
const (
	labelControlPlane = "node-role.kubernetes.io/control-plane"
	labelMaster       = "node-role.kubernetes.io/master"
)

// controllerLeasePrefix is the name prefix k0s gives the Lease each controller renews in
// kube-node-lease. It is a k0s implementation detail, not a Kubernetes standard, which is
// why reading it is opt-in per integration rather than always on.
const controllerLeasePrefix = "k0s-ctrl-"

// controllerLeaseFreshFor bounds how recently a controller lease must have been renewed to
// be reported as ready. k0s renews the controller lease on the order of every ten seconds,
// so a minute without a renewal means the controller is not currently heartbeating. A lease
// that advertises its own duration overrides this with three times that duration, since a
// slower renewal cadence should not read as stale.
const controllerLeaseFreshFor = 60 * time.Second

// KubernetesReader reads nodes from the Kubernetes API.
//
// When discoverControllerLeases is set it also reports dedicated k0s controllers, which are
// not registered as Kubernetes nodes and would otherwise be invisible to a membership read.
type KubernetesReader struct {
	baseURL                  string
	token                    string
	httpClient               *http.Client
	discoverControllerLeases bool
}

func NewKubernetesReader(rawURL, token string, timeout time.Duration, insecureSkipVerify, discoverControllerLeases bool) (*KubernetesReader, error) {
	baseURL, err := normalizeBaseURL(rawURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("Kubernetes API token is empty")
	}

	transport := http.DefaultTransport
	if insecureSkipVerify {
		transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}

	return &KubernetesReader{
		baseURL:                  baseURL,
		token:                    token,
		httpClient:               &http.Client{Timeout: timeout, Transport: transport},
		discoverControllerLeases: discoverControllerLeases,
	}, nil
}

type nodeListJSON struct {
	Items []struct {
		Metadata struct {
			Name   string            `json:"name"`
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
		Status struct {
			Addresses []struct {
				Type    string `json:"type"`
				Address string `json:"address"`
			} `json:"addresses"`
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"conditions"`
		} `json:"status"`
	} `json:"items"`
}

func (r *KubernetesReader) ListMembers(ctx context.Context) ([]clusterdomain.Member, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/api/v1/nodes", nil)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/json")

	var out nodeListJSON
	if err := doJSON(r.httpClient, req, &out); err != nil {
		return nil, translateError(err, "Kubernetes")
	}

	members := make([]clusterdomain.Member, 0, len(out.Items))
	seen := map[string]bool{}
	for _, item := range out.Items {
		member := clusterdomain.Member{
			Name: item.Metadata.Name,
			Role: "worker",
		}
		if _, ok := item.Metadata.Labels[labelControlPlane]; ok {
			member.Role = "control-plane"
		} else if _, ok := item.Metadata.Labels[labelMaster]; ok {
			member.Role = "control-plane"
		}

		member.State = "notready"
		for _, condition := range item.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				member.State = "ready"
				break
			}
		}

		for _, address := range item.Status.Addresses {
			if address.Type == "InternalIP" && address.Address != "" {
				member.Addresses = append(member.Addresses, address.Address)
			}
		}

		seen[strings.ToLower(member.Name)] = true
		members = append(members, member)
	}

	if r.discoverControllerLeases {
		controllers, err := r.controllerMembers(ctx)
		if err != nil {
			return nil, err
		}
		for _, controller := range controllers {
			// A controller that is also a node (an all-in-one node with the lease
			// naming enabled) is already reported through the node list; the node
			// carries readiness and addresses, so it wins.
			if seen[strings.ToLower(controller.Name)] {
				continue
			}
			members = append(members, controller)
		}
	}

	return members, nil
}

// leaseListJSON is the subset of a Lease list swallow reads. renewTime is the heartbeat.
type leaseListJSON struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec struct {
			RenewTime            string `json:"renewTime"`
			LeaseDurationSeconds *int   `json:"leaseDurationSeconds"`
		} `json:"spec"`
	} `json:"items"`
}

// controllerMembers reports dedicated k0s controllers from their kube-node-lease Leases.
//
// Dedicated controllers are installed without --enable-worker and so never register as
// Kubernetes nodes; the lease each one renews is the only place the API exposes them.
// Leases carry no addresses, so a controller can only be matched to a server by name.
func (r *KubernetesReader) controllerMembers(ctx context.Context) ([]clusterdomain.Member, error) {
	endpoint := r.baseURL + "/apis/coordination.k8s.io/v1/namespaces/kube-node-lease/leases"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes lease request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/json")

	var out leaseListJSON
	if err := doJSON(r.httpClient, req, &out); err != nil {
		return nil, translateError(err, "Kubernetes")
	}

	now := time.Now()
	members := make([]clusterdomain.Member, 0)
	for _, item := range out.Items {
		name, ok := strings.CutPrefix(item.Metadata.Name, controllerLeasePrefix)
		if !ok || name == "" {
			continue
		}
		members = append(members, clusterdomain.Member{
			Name:  name,
			Role:  "control-plane",
			State: leaseState(item.Spec.RenewTime, item.Spec.LeaseDurationSeconds, now),
		})
	}
	return members, nil
}

// leaseState reports "ready" when the lease was renewed recently enough, mirroring how a
// node's Ready condition is read. A missing or unparseable renewTime is "notready" rather
// than an error: a controller whose heartbeat swallow cannot read is not one it can call
// healthy.
func leaseState(renewTime string, leaseDurationSeconds *int, now time.Time) string {
	if renewTime == "" {
		return "notready"
	}
	renewed, err := time.Parse(time.RFC3339Nano, renewTime)
	if err != nil {
		return "notready"
	}
	freshFor := controllerLeaseFreshFor
	if leaseDurationSeconds != nil && *leaseDurationSeconds > 0 {
		if scaled := time.Duration(*leaseDurationSeconds) * 3 * time.Second; scaled > freshFor {
			freshFor = scaled
		}
	}
	if now.Sub(renewed) <= freshFor {
		return "ready"
	}
	return "notready"
}

func normalizeBaseURL(rawURL string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if trimmed == "" {
		return "", errors.New("cluster api url is empty")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid cluster api url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("invalid cluster api url: scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("invalid cluster api url: missing host")
	}
	return trimmed, nil
}

func doJSON(client *http.Client, req *http.Request, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return &transportError{err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return &apiError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode cluster api response: %w", err)
	}
	return nil
}

type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("cluster api returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("cluster api returned HTTP %d: %s", e.StatusCode, e.Body)
}

type transportError struct {
	err error
}

func (e *transportError) Error() string { return e.err.Error() }
func (e *transportError) Unwrap() error { return e.err }

func translateError(err error, system string) error {
	var transportErr *transportError
	if errors.As(err, &transportErr) {
		return &clusterdomain.ReaderError{
			Kind:   clusterdomain.ReaderErrorUnavailable,
			Detail: fmt.Sprintf("Could not reach the %s API.", system),
			Err:    err,
		}
	}

	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		return err
	}

	switch {
	case apiErr.StatusCode == http.StatusUnauthorized, apiErr.StatusCode == http.StatusForbidden:
		return &clusterdomain.ReaderError{
			Kind:   clusterdomain.ReaderErrorAuth,
			Detail: fmt.Sprintf("The %s API rejected the credential swallow is configured with.", system),
			Err:    err,
		}
	case apiErr.StatusCode >= http.StatusInternalServerError:
		return &clusterdomain.ReaderError{
			Kind:   clusterdomain.ReaderErrorUnavailable,
			Detail: fmt.Sprintf("The %s API reported an internal error (HTTP %d).", system, apiErr.StatusCode),
			Err:    err,
		}
	default:
		return &clusterdomain.ReaderError{
			Kind:   clusterdomain.ReaderErrorRejected,
			Detail: apiErr.Error(),
			Err:    err,
		}
	}
}
