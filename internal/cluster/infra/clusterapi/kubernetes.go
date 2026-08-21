// Package clusterapi reads live state from cluster APIs.
//
// Both readers speak plain REST rather than pulling in a vendored client library: gdcm
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

	clusterdomain "github.com/AFDEAPAC/swallow/internal/cluster/domain"
)

const maxErrorBodyBytes = 512

// Kubernetes role labels. A node carrying either is a control-plane node; the label was
// renamed upstream, so both are checked.
const (
	labelControlPlane = "node-role.kubernetes.io/control-plane"
	labelMaster       = "node-role.kubernetes.io/master"
)

// KubernetesReader reads nodes from the Kubernetes API.
type KubernetesReader struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewKubernetesReader(rawURL, token string, timeout time.Duration, insecureSkipVerify bool) (*KubernetesReader, error) {
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
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{Timeout: timeout, Transport: transport},
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

		members = append(members, member)
	}
	return members, nil
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
			Detail: fmt.Sprintf("The %s API rejected the credential gdcm is configured with.", system),
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
