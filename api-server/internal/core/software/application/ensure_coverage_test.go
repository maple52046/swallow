package application

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// countingSiteAutomation is a fakeSiteAutomation that records the Site of each read and can fail
// every read with err.
type countingSiteAutomation struct {
	fakeSiteAutomation
	err   error
	reads []string
}

func (c *countingSiteAutomation) FindBySiteID(ctx context.Context, siteID string) (*workflowdomain.AutomationConfiguration, error) {
	c.reads = append(c.reads, siteID)
	if c.err != nil {
		return nil, c.err
	}
	return c.fakeSiteAutomation.FindBySiteID(ctx, siteID)
}

// coverageServer is a present, deployed, unlocked Server at siteID.
func coverageServer(id, siteID string, mutate func(*serverdomain.Server)) *serverdomain.Server {
	server := &serverdomain.Server{
		ID: id, Source: serverdomain.Source{SiteID: siteID},
		Provisioning: &serverdomain.ProvisioningStatus{State: "deployed"},
	}
	if mutate != nil {
		mutate(server)
	}
	return server
}

// The policy covers a present, deployed, unlocked Server at a Site whose automation is on; it reads
// each Site once for a batch and not at all for a Server the Server facts rule out.
func TestEnsureCoverageCoveredServers(t *testing.T) {
	automation := &countingSiteAutomation{fakeSiteAutomation: fakeSiteAutomation{enabled: map[string]bool{"on": true, "off": false}}}
	servers := []*serverdomain.Server{
		coverageServer("covered", "on", nil),
		coverageServer("also-covered", "on", nil),
		coverageServer("automation-off", "off", nil),
		coverageServer("no-configuration", "none", nil),
		coverageServer("absent", "on", func(s *serverdomain.Server) { s.Absent = true }),
		coverageServer("locked", "on", func(s *serverdomain.Server) { s.Provisioning.Locked = true }),
		coverageServer("ready", "on", func(s *serverdomain.Server) { s.Provisioning.State = "ready" }),
		coverageServer("unobserved", "skipped", func(s *serverdomain.Server) { s.Provisioning = nil }),
	}

	got, err := NewEnsureCoverage(automation).CoveredServers(context.Background(), servers)
	if err != nil {
		t.Fatalf("CoveredServers() error = %v", err)
	}
	if want := map[string]bool{"covered": true, "also-covered": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("CoveredServers() = %v, want %v", got, want)
	}
	if want := []string{"on", "off", "none"}; !slices.Equal(automation.reads, want) {
		t.Errorf("automation reads = %v, want one read of each Site that has a coverable Server, %v", automation.reads, want)
	}
}

// Covers is the same policy for one Server, and a failed read is an error for both forms.
func TestEnsureCoverageReadFailure(t *testing.T) {
	errStore := errors.New("store unreachable")
	coverage := NewEnsureCoverage(&countingSiteAutomation{err: errStore})
	server := coverageServer("s1", "site-1", nil)

	if _, err := coverage.Covers(context.Background(), server); !errors.Is(err, errStore) {
		t.Errorf("Covers() error = %v, want %v", err, errStore)
	}
	if _, err := coverage.CoveredServers(context.Background(), []*serverdomain.Server{server}); !errors.Is(err, errStore) {
		t.Errorf("CoveredServers() error = %v, want %v", err, errStore)
	}
}
