package platformapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

// DefaultSlurmAPIVersion is slurmrestd's path version. It is configurable because
// slurmrestd's endpoint version tracks the Slurm release, so a fleet will run several.
const DefaultSlurmAPIVersion = "v0.0.40"

// SlurmReader reads nodes from slurmrestd.
type SlurmReader struct {
	baseURL    string
	token      string
	apiVersion string
	httpClient *http.Client
}

func NewSlurmReader(rawURL, token, apiVersion string, timeout time.Duration, insecureSkipVerify bool) (*SlurmReader, error) {
	baseURL, err := normalizeBaseURL(rawURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("Slurm API token is empty")
	}
	if apiVersion == "" {
		apiVersion = DefaultSlurmAPIVersion
	}

	return &SlurmReader{
		baseURL:    baseURL,
		token:      token,
		apiVersion: apiVersion,
		httpClient: &http.Client{Timeout: timeout, Transport: newReaderTransport(insecureSkipVerify)},
	}, nil
}

type slurmNodeListJSON struct {
	Nodes []struct {
		Name       string   `json:"name"`
		Address    string   `json:"address"`
		State      []string `json:"state"`
		Partitions []string `json:"partitions"`
	} `json:"nodes"`
}

func (r *SlurmReader) ListMembers(ctx context.Context) ([]platformdomain.Member, error) {
	var out slurmNodeListJSON
	if err := r.getJSON(ctx, "nodes", &out); err != nil {
		return nil, err
	}

	members := make([]platformdomain.Member, 0, len(out.Nodes))
	for _, node := range out.Nodes {
		member := platformdomain.Member{
			Name:  node.Name,
			Role:  strings.Join(node.Partitions, ","),
			State: slurmState(node.State),
		}
		if node.Address != "" {
			member.Addresses = []string{node.Address}
		}
		members = append(members, member)
	}
	return members, nil
}

var _ platformdomain.SlurmClusterReader = (*SlurmReader)(nil)

// getJSON performs an authenticated GET against a slurmrestd sub-path relative to the
// versioned base (for example "nodes", "partitions", "ping") and decodes the JSON body.
// slurmrestd authenticates with its own X-SLURM-USER-TOKEN header rather than Authorization.
// Transport and API failures are translated into a platformdomain.ReaderError so callers map
// them to a stable status.
func (r *SlurmReader) getJSON(ctx context.Context, subpath string, out any) error {
	endpoint := fmt.Sprintf("%s/slurm/%s/%s", r.baseURL, r.apiVersion, subpath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build slurm request: %w", err)
	}
	req.Header.Set("X-SLURM-USER-TOKEN", r.token)
	req.Header.Set("Accept", "application/json")
	if err := doJSON(r.httpClient, req, out); err != nil {
		return translateError(err, "Slurm")
	}
	return nil
}

// GetClusterState reads a Slurm platform's live cluster state: controllers (slurmctld ping),
// partitions, and compute node states. It is the read behind the Slurm-specific view and is
// independent of ListMembers (membership sync). Any endpoint failure aborts with a
// ReaderError; a healthy cluster with no partitions or nodes yields empty slices, not an error.
func (r *SlurmReader) GetClusterState(ctx context.Context) (*platformdomain.SlurmClusterState, error) {
	controllers, err := r.pingControllers(ctx)
	if err != nil {
		return nil, err
	}
	partitions, err := r.listPartitions(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := r.listNodes(ctx)
	if err != nil {
		return nil, err
	}
	return &platformdomain.SlurmClusterState{
		Controllers: controllers, Partitions: partitions, Nodes: nodes,
	}, nil
}

// slurmPingJSON is slurmrestd's /ping response. It lists controllers in SlurmctldHost order
// with their reachability and role; field presence varies by slurmrestd version, so an absent
// field degrades to an unknown status rather than an error.
type slurmPingJSON struct {
	Pings []struct {
		Hostname string `json:"hostname"`
		Pinged   string `json:"pinged"`
		Mode     string `json:"mode"`
	} `json:"pings"`
}

// pingControllers reports each slurmctld in failover order. slurmrestd reports mode as
// "primary"/"backup"; when it is absent, the first host in SlurmctldHost order is the primary,
// matching Slurm's ordered active/standby failover.
func (r *SlurmReader) pingControllers(ctx context.Context) ([]platformdomain.SlurmController, error) {
	var out slurmPingJSON
	if err := r.getJSON(ctx, "ping", &out); err != nil {
		return nil, err
	}
	controllers := make([]platformdomain.SlurmController, 0, len(out.Pings))
	for index, ping := range out.Pings {
		primary := strings.EqualFold(ping.Mode, "primary")
		if ping.Mode == "" {
			primary = index == 0
		}
		controllers = append(controllers, platformdomain.SlurmController{
			Hostname: ping.Hostname, Primary: primary, Status: slurmPingStatus(ping.Pinged),
		})
	}
	return controllers, nil
}

// slurmPingStatus normalizes slurmrestd's ping word to up/down/unknown. This is the
// controller RPC liveness, never the host's monitoring health.
func slurmPingStatus(pinged string) string {
	switch strings.ToLower(strings.TrimSpace(pinged)) {
	case "up":
		return "up"
	case "down":
		return "down"
	default:
		return "unknown"
	}
}

// slurmPartitionsJSON is a tolerant subset of slurmrestd's /partitions response. Nested
// shapes differ between slurmrestd versions, so unread fields simply stay zero-valued.
type slurmPartitionsJSON struct {
	Partitions []struct {
		Name  string `json:"name"`
		Nodes struct {
			Configured string `json:"configured"`
			Total      int    `json:"total"`
		} `json:"nodes"`
		Partition struct {
			State []string `json:"state"`
		} `json:"partition"`
	} `json:"partitions"`
}

func (r *SlurmReader) listPartitions(ctx context.Context) ([]platformdomain.SlurmPartition, error) {
	var out slurmPartitionsJSON
	if err := r.getJSON(ctx, "partitions", &out); err != nil {
		return nil, err
	}
	partitions := make([]platformdomain.SlurmPartition, 0, len(out.Partitions))
	for _, partition := range out.Partitions {
		partitions = append(partitions, platformdomain.SlurmPartition{
			Name:       partition.Name,
			State:      strings.ToLower(strings.Join(partition.Partition.State, ",")),
			NodeSpec:   partition.Nodes.Configured,
			TotalNodes: partition.Nodes.Total,
		})
	}
	return partitions, nil
}

// slurmNodesFullJSON reads the node fields the Slurm view needs beyond membership: CPU,
// memory and GRES, alongside the state and partitions ListMembers already consumes.
type slurmNodesFullJSON struct {
	Nodes []struct {
		Name       string   `json:"name"`
		Address    string   `json:"address"`
		State      []string `json:"state"`
		Partitions []string `json:"partitions"`
		CPUs       int      `json:"cpus"`
		RealMemory int64    `json:"real_memory"`
		Gres       string   `json:"gres"`
	} `json:"nodes"`
}

func (r *SlurmReader) listNodes(ctx context.Context) ([]platformdomain.SlurmNode, error) {
	var out slurmNodesFullJSON
	if err := r.getJSON(ctx, "nodes", &out); err != nil {
		return nil, err
	}
	nodes := make([]platformdomain.SlurmNode, 0, len(out.Nodes))
	for _, node := range out.Nodes {
		nodes = append(nodes, platformdomain.SlurmNode{
			Name: node.Name, State: slurmState(node.State),
			CPUs: node.CPUs, RealMemoryMiB: node.RealMemory,
			Gres: node.Gres, Partitions: append([]string(nil), node.Partitions...),
			Address: node.Address,
		})
	}
	return nodes, nil
}

// slurmState collapses Slurm's state flag list into one word.
//
// Slurm reports a set of flags — IDLE plus DRAIN, for instance — and the flags matter
// more than the base state: a node that is draining is not available regardless of what
// else it says.
func slurmState(states []string) string {
	lowered := make([]string, 0, len(states))
	for _, state := range states {
		lowered = append(lowered, strings.ToLower(state))
	}

	for _, state := range lowered {
		switch state {
		case "down", "drain", "draining", "drained", "fail", "failing":
			return state
		}
	}
	if len(lowered) > 0 {
		return lowered[0]
	}
	return "unknown"
}
