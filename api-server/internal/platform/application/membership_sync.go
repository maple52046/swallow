package application

import (
	"context"
	"log/slog"
	"strings"
	"time"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// MembershipReport summarises one membership read.
type MembershipReport struct {
	PlatformID   string   `json:"platformId"`
	PlatformName string   `json:"platformName"`
	Members      int      `json:"members"`
	Matched      int      `json:"matched"`
	Cleared      int      `json:"cleared"`
	Unmatched    []string `json:"unmatched"`
	Error        *string  `json:"error"`
}

// MembershipSyncUseCase reads a platform's membership and writes it onto the servers'
// membership axis.
//
// The platform is authoritative: swallow records what the platform says, and never the other
// way around. An operation may have asked a server to join, but only the platform can say
// whether it did.
type MembershipSyncUseCase struct {
	platforms platformdomain.PlatformRepository
	servers   serverdomain.ServerRepository
	readers   platformdomain.ReaderFactory
}

func NewMembershipSyncUseCase(
	platforms platformdomain.PlatformRepository,
	servers serverdomain.ServerRepository,
	readers platformdomain.ReaderFactory,
) *MembershipSyncUseCase {
	return &MembershipSyncUseCase{platforms: platforms, servers: servers, readers: readers}
}

// ExecuteAll syncs every registered platform. One failing platform does not stop the rest.
func (uc *MembershipSyncUseCase) ExecuteAll(ctx context.Context) ([]MembershipReport, error) {
	platforms, err := uc.platforms.List(ctx, "")
	if err != nil {
		return nil, err
	}

	reports := make([]MembershipReport, 0, len(platforms))
	for _, platform := range platforms {
		if platform.IntegrationID == "" {
			// Registered but not yet reachable, which is the normal state between
			// deciding to build a platform and having built it.
			continue
		}
		reports = append(reports, uc.sync(ctx, platform))
	}
	return reports, nil
}

func (uc *MembershipSyncUseCase) Execute(ctx context.Context, platformID string) (*MembershipReport, error) {
	platform, err := uc.platforms.FindByID(ctx, platformID)
	if err != nil {
		return nil, err
	}
	report := uc.sync(ctx, platform)
	return &report, nil
}

func (uc *MembershipSyncUseCase) sync(ctx context.Context, platform *platformdomain.Platform) MembershipReport {
	report := MembershipReport{
		PlatformID:   platform.ID,
		PlatformName: platform.Name,
		Unmatched:    []string{},
	}

	startedAt := time.Now().UTC()
	state := platform.Sync
	state.LastStartedAt = &startedAt
	uc.recordSyncState(ctx, platform, state)

	reader, err := uc.readers.For(ctx, platform)
	if err != nil {
		return uc.fail(ctx, platform, report, startedAt, err)
	}

	members, err := reader.ListMembers(ctx)
	if err != nil {
		return uc.fail(ctx, platform, report, startedAt, err)
	}
	report.Members = len(members)

	candidates, err := uc.servers.List(ctx, serverdomain.ListFilter{
		SiteID:        platform.SiteID,
		IncludeAbsent: true,
	})
	if err != nil {
		return uc.fail(ctx, platform, report, startedAt, err)
	}
	index := buildServerIndex(candidates.Servers)

	matchedServerIDs := map[string]bool{}
	now := time.Now().UTC()

	for _, member := range members {
		server := index.match(member)
		if server == nil {
			report.Unmatched = append(report.Unmatched, member.Name)
			continue
		}

		err := uc.servers.SetMembership(ctx, server.ID, &serverdomain.MembershipStatus{
			PlatformID: platform.ID,
			NodeName:   member.Name,
			Role:       member.Role,
			State:      member.State,
			ObservedAt: now,
		})
		if err != nil {
			return uc.fail(ctx, platform, report, startedAt, err)
		}
		matchedServerIDs[server.ID] = true
		report.Matched++
	}

	// Clear membership from servers the platform no longer lists. Leaving it would show
	// a server as part of a platform it has been removed from.
	existing, err := uc.servers.List(ctx, serverdomain.ListFilter{
		PlatformID:    platform.ID,
		IncludeAbsent: true,
	})
	if err != nil {
		return uc.fail(ctx, platform, report, startedAt, err)
	}
	for _, server := range existing.Servers {
		if matchedServerIDs[server.ID] {
			continue
		}
		if err := uc.servers.SetMembership(ctx, server.ID, nil); err != nil {
			return uc.fail(ctx, platform, report, startedAt, err)
		}
		report.Cleared++
	}

	succeededAt := time.Now().UTC()
	uc.recordSyncState(ctx, platform, platformdomain.SyncState{
		LastStartedAt:   &startedAt,
		LastSucceededAt: &succeededAt,
		MemberCount:     report.Members,
		MatchedCount:    report.Matched,
	})

	return report
}

// recordSyncState persists the platform sync projection. A failed projection write must
// not abort membership reconciliation: the authoritative platform read has already
// happened, and its outcome is carried by MembershipReport. But the failure must be
// observable rather than silently dropped, otherwise the persisted sync view can drift
// stale with no signal. ErrPlatformNotFound here typically means the platform integration
// was reconfigured concurrently, which is expected and safe to skip.
func (uc *MembershipSyncUseCase) recordSyncState(ctx context.Context, platform *platformdomain.Platform, state platformdomain.SyncState) {
	if err := uc.platforms.UpdateSyncState(ctx, platform.ID, platform.IntegrationID, state); err != nil {
		slog.Warn("platform membership sync projection write failed",
			"platformId", platform.ID, "integrationId", platform.IntegrationID, "error", err)
	}
}

// fail records the failure while keeping the previous success timestamp, so a reader can
// see both that the last attempt failed and how old the membership view is.
func (uc *MembershipSyncUseCase) fail(
	ctx context.Context,
	platform *platformdomain.Platform,
	report MembershipReport,
	startedAt time.Time,
	cause error,
) MembershipReport {
	message := cause.Error()
	report.Error = &message

	uc.recordSyncState(ctx, platform, platformdomain.SyncState{
		LastStartedAt:   &startedAt,
		LastSucceededAt: platform.Sync.LastSucceededAt,
		LastError:       message,
		MemberCount:     platform.Sync.MemberCount,
		MatchedCount:    platform.Sync.MatchedCount,
	})
	return report
}

// serverIndex matches platform members to servers.
//
// A platform's node name is usually the machine's hostname, so that is tried first, with
// addresses as the fallback. Ambiguous matches are treated as no match: attributing a
// platform membership to the wrong server would misdirect every operation aimed at it.
type serverIndex struct {
	byHostname map[string][]*serverdomain.Server
	byAddress  map[string][]*serverdomain.Server
}

func buildServerIndex(servers []*serverdomain.Server) *serverIndex {
	index := &serverIndex{
		byHostname: map[string][]*serverdomain.Server{},
		byAddress:  map[string][]*serverdomain.Server{},
	}

	for _, server := range servers {
		if hostname := strings.ToLower(server.Observed.Hostname); hostname != "" {
			index.byHostname[hostname] = append(index.byHostname[hostname], server)
		}
		// A platform may know a node by its fully qualified name.
		if fqdn := strings.ToLower(server.Observed.FQDN); fqdn != "" {
			index.byHostname[fqdn] = append(index.byHostname[fqdn], server)
		}
		for _, address := range server.Observed.Addresses {
			if address != "" {
				index.byAddress[address] = append(index.byAddress[address], server)
			}
		}
	}
	return index
}

func (i *serverIndex) match(member platformdomain.Member) *serverdomain.Server {
	name := strings.ToLower(member.Name)
	if server := unique(i.byHostname[name]); server != nil {
		return server
	}
	// A short platform node name against a stored FQDN, or the reverse.
	if host, _, found := strings.Cut(name, "."); found {
		if server := unique(i.byHostname[host]); server != nil {
			return server
		}
	}
	for _, address := range member.Addresses {
		if server := unique(i.byAddress[address]); server != nil {
			return server
		}
	}
	return nil
}

func unique(servers []*serverdomain.Server) *serverdomain.Server {
	if len(servers) == 1 {
		return servers[0]
	}
	return nil
}
