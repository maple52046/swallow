package application

import (
	"context"
	"errors"
	"fmt"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/core/software/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// EnsureCoverage is the policy that decides which Servers an EnsureController keeps its kind
// installed on (software-deployment.md, Node Exporter): a Server is covered when it is present, in
// provisioning state `deployed` (disk or RAM), not locked as projected, and its Site's automation
// is enabled. It is derived desired state with no spec field, so it is computed from the Server and
// its Site's Automation Configuration on every read and never stored.
//
// The controller and the Server health classification (servers-list.md, Health) both read it, so
// "swallow expects this Server to have the Node Exporter" means one thing everywhere. The lock is
// the projected one; an install re-checks the live lock, and a health read needs no live check.
// EnsureCoverage holds no state and is safe for concurrent use.
type EnsureCoverage struct {
	automation SiteAutomation
}

// NewEnsureCoverage builds the policy over the Sites' Automation Configurations.
func NewEnsureCoverage(automation SiteAutomation) EnsureCoverage {
	return EnsureCoverage{automation: automation}
}

// Covers applies the policy to one Server. Only a failed Automation Configuration read is an
// error; a Site with no configuration is not covered.
func (p EnsureCoverage) Covers(ctx context.Context, server *serverdomain.Server) (bool, error) {
	if !coverable(server) {
		return false, nil
	}
	return p.automationEnabled(ctx, server.Source.SiteID)
}

// CoveredServers applies the policy to every Server in servers and returns the IDs of the covered
// ones. It reads each distinct Site's Automation Configuration once, and none for a Server the
// Server facts alone rule out, so a long list costs one read per Site. A failed read is an error.
func (p EnsureCoverage) CoveredServers(ctx context.Context, servers []*serverdomain.Server) (map[string]bool, error) {
	covered := map[string]bool{}
	enabled := map[string]bool{}
	for _, server := range servers {
		if !coverable(server) {
			continue
		}
		siteID := server.Source.SiteID
		on, read := enabled[siteID]
		if !read {
			var err error
			on, err = p.automationEnabled(ctx, siteID)
			if err != nil {
				return nil, fmt.Errorf("read the automation of site %q: %w", siteID, err)
			}
			enabled[siteID] = on
		}
		if on {
			covered[server.ID] = true
		}
	}
	return covered, nil
}

// coverable is the part of the policy the Server alone decides: present, `deployed`, not locked.
func coverable(server *serverdomain.Server) bool {
	return !server.Absent && server.Provisioning != nil && !server.Provisioning.Locked &&
		server.Provisioning.State == softwaredomain.RequiredProvisioningState
}

// automationEnabled reports whether the Site's automation is enabled; a Site with no Automation
// Configuration has none.
func (p EnsureCoverage) automationEnabled(ctx context.Context, siteID string) (bool, error) {
	configuration, err := p.automation.FindBySiteID(ctx, siteID)
	if errors.Is(err, workflowdomain.ErrAutomationConfigNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return configuration.Enabled, nil
}
