package clusterapi

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	clusterdomain "github.com/maple52046/swallow/internal/cluster/domain"
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

	transport := http.DefaultTransport
	if insecureSkipVerify {
		transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}

	return &SlurmReader{
		baseURL:    baseURL,
		token:      token,
		apiVersion: apiVersion,
		httpClient: &http.Client{Timeout: timeout, Transport: transport},
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

func (r *SlurmReader) ListMembers(ctx context.Context) ([]clusterdomain.Member, error) {
	endpoint := fmt.Sprintf("%s/slurm/%s/nodes", r.baseURL, r.apiVersion)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build slurm request: %w", err)
	}
	// slurmrestd uses its own header rather than Authorization.
	req.Header.Set("X-SLURM-USER-TOKEN", r.token)
	req.Header.Set("Accept", "application/json")

	var out slurmNodeListJSON
	if err := doJSON(r.httpClient, req, &out); err != nil {
		return nil, translateError(err, "Slurm")
	}

	members := make([]clusterdomain.Member, 0, len(out.Nodes))
	for _, node := range out.Nodes {
		member := clusterdomain.Member{
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
