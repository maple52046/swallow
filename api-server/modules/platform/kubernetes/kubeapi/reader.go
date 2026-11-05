// Package kubeapi is the Kubernetes module's client of a deployed cluster's API: the membership
// reader and the read/write explorer client, both built from the Platform's Connection. They speak
// plain REST rather than pulling in client-go, which would be a large dependency for a handful of
// calls.
package kubeapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	"github.com/maple52046/swallow/internal/shared/httpjson"
)

// ProviderKind is the provider kind of the Integration swallow records for a deployed Kubernetes
// Platform: the Platform type. It is persisted, so it stays stable.
const ProviderKind = "kubernetes"

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

// Reader reads nodes from the Kubernetes API.
//
// When discoverControllerLeases is set it also reports dedicated k0s controllers, which are
// not registered as Kubernetes nodes and would otherwise be invisible to a membership read.
type Reader struct {
	baseURL                  string
	token                    string
	httpClient               *http.Client
	discoverControllerLeases bool
}

// NewReader builds a reader for the Connection's API base URL and bearer token. The Connection's
// controllerLeaseDiscovery setting turns on reporting dedicated k0s controllers.
func NewReader(conn platformdomain.Connection) (*Reader, error) {
	baseURL, err := httpjson.NormalizeBaseURL(conn.Endpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(conn.Token) == "" {
		return nil, errors.New("Kubernetes API token is empty")
	}
	transport, err := httpjson.Transport(conn.InsecureSkipVerify, conn.CACertificate)
	if err != nil {
		return nil, err
	}
	return &Reader{
		baseURL:                  baseURL,
		token:                    conn.Token,
		httpClient:               &http.Client{Timeout: conn.Timeout, Transport: transport},
		discoverControllerLeases: conn.SettingBool(SettingControllerLeaseDiscovery),
	}, nil
}

// ReaderBuilder is the Kubernetes module's platform-reader hook.
type ReaderBuilder struct{}

// PlatformType is kubernetes.
func (ReaderBuilder) PlatformType() platformdomain.PlatformType {
	return platformdomain.PlatformTypeKubernetes
}

// NewReader builds the Kubernetes reader of conn.
func (ReaderBuilder) NewReader(conn platformdomain.Connection) (platformdomain.PlatformReader, error) {
	reader, err := NewReader(conn)
	if err != nil {
		return nil, err
	}
	return reader, nil
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

func (r *Reader) ListMembers(ctx context.Context) ([]platformdomain.Member, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/api/v1/nodes", nil)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/json")

	var out nodeListJSON
	if err := httpjson.Do(r.httpClient, req, &out); err != nil {
		return nil, translateError(err, r.baseURL, "list nodes")
	}

	members := make([]platformdomain.Member, 0, len(out.Items))
	seen := map[string]bool{}
	for _, item := range out.Items {
		member := platformdomain.Member{
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
func (r *Reader) controllerMembers(ctx context.Context) ([]platformdomain.Member, error) {
	endpoint := r.baseURL + "/apis/coordination.k8s.io/v1/namespaces/kube-node-lease/leases"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build kubernetes lease request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/json")

	var out leaseListJSON
	if err := httpjson.Do(r.httpClient, req, &out); err != nil {
		return nil, translateError(err, r.baseURL, "list controller leases")
	}

	now := time.Now()
	members := make([]platformdomain.Member, 0)
	for _, item := range out.Items {
		name, ok := strings.CutPrefix(item.Metadata.Name, controllerLeasePrefix)
		if !ok || name == "" {
			continue
		}
		members = append(members, platformdomain.Member{
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
