package application

import (
	"context"
	"strings"
	"time"

	clusterdomain "github.com/AFDEAPAC/swallow/internal/cluster/domain"
	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
)

// MembershipReport summarises one membership read.
type MembershipReport struct {
	ClusterID   string   `json:"clusterId"`
	ClusterName string   `json:"clusterName"`
	Members     int      `json:"members"`
	Matched     int      `json:"matched"`
	Cleared     int      `json:"cleared"`
	Unmatched   []string `json:"unmatched"`
	Error       *string  `json:"error"`
}

// MembershipSyncUseCase reads a cluster's membership and writes it onto the servers'
// membership axis.
//
// The cluster is authoritative: gdcm records what the cluster says, and never the other
// way around. An operation may have asked a server to join, but only the cluster can say
// whether it did.
type MembershipSyncUseCase struct {
	clusters clusterdomain.ClusterRepository
	servers  serverdomain.ServerRepository
	readers  clusterdomain.ReaderFactory
}

func NewMembershipSyncUseCase(
	clusters clusterdomain.ClusterRepository,
	servers serverdomain.ServerRepository,
	readers clusterdomain.ReaderFactory,
) *MembershipSyncUseCase {
	return &MembershipSyncUseCase{clusters: clusters, servers: servers, readers: readers}
}

// ExecuteAll syncs every registered cluster. One failing cluster does not stop the rest.
func (uc *MembershipSyncUseCase) ExecuteAll(ctx context.Context) ([]MembershipReport, error) {
	clusters, err := uc.clusters.List(ctx, "")
	if err != nil {
		return nil, err
	}

	reports := make([]MembershipReport, 0, len(clusters))
	for _, cluster := range clusters {
		if cluster.IntegrationID == "" {
			// Registered but not yet reachable, which is the normal state between
			// deciding to build a cluster and having built it.
			continue
		}
		reports = append(reports, uc.sync(ctx, cluster))
	}
	return reports, nil
}

func (uc *MembershipSyncUseCase) Execute(ctx context.Context, clusterID string) (*MembershipReport, error) {
	cluster, err := uc.clusters.FindByID(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	report := uc.sync(ctx, cluster)
	return &report, nil
}

func (uc *MembershipSyncUseCase) sync(ctx context.Context, cluster *clusterdomain.Cluster) MembershipReport {
	report := MembershipReport{
		ClusterID:   cluster.ID,
		ClusterName: cluster.Name,
		Unmatched:   []string{},
	}

	startedAt := time.Now().UTC()
	state := cluster.Sync
	state.LastStartedAt = &startedAt
	_ = uc.clusters.UpdateSyncState(ctx, cluster.ID, state)

	reader, err := uc.readers.For(ctx, cluster)
	if err != nil {
		return uc.fail(ctx, cluster, report, startedAt, err)
	}

	members, err := reader.ListMembers(ctx)
	if err != nil {
		return uc.fail(ctx, cluster, report, startedAt, err)
	}
	report.Members = len(members)

	candidates, err := uc.servers.List(ctx, serverdomain.ListFilter{
		SiteID:        cluster.SiteID,
		IncludeAbsent: true,
	})
	if err != nil {
		return uc.fail(ctx, cluster, report, startedAt, err)
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
			ClusterID:  cluster.ID,
			NodeName:   member.Name,
			Role:       member.Role,
			State:      member.State,
			ObservedAt: now,
		})
		if err != nil {
			return uc.fail(ctx, cluster, report, startedAt, err)
		}
		matchedServerIDs[server.ID] = true
		report.Matched++
	}

	// Clear membership from servers the cluster no longer lists. Leaving it would show
	// a server as part of a cluster it has been removed from.
	existing, err := uc.servers.List(ctx, serverdomain.ListFilter{
		ClusterID:     cluster.ID,
		IncludeAbsent: true,
	})
	if err != nil {
		return uc.fail(ctx, cluster, report, startedAt, err)
	}
	for _, server := range existing.Servers {
		if matchedServerIDs[server.ID] {
			continue
		}
		if err := uc.servers.SetMembership(ctx, server.ID, nil); err != nil {
			return uc.fail(ctx, cluster, report, startedAt, err)
		}
		report.Cleared++
	}

	succeededAt := time.Now().UTC()
	_ = uc.clusters.UpdateSyncState(ctx, cluster.ID, clusterdomain.SyncState{
		LastStartedAt:   &startedAt,
		LastSucceededAt: &succeededAt,
		MemberCount:     report.Members,
		MatchedCount:    report.Matched,
	})

	return report
}

// fail records the failure while keeping the previous success timestamp, so a reader can
// see both that the last attempt failed and how old the membership view is.
func (uc *MembershipSyncUseCase) fail(
	ctx context.Context,
	cluster *clusterdomain.Cluster,
	report MembershipReport,
	startedAt time.Time,
	cause error,
) MembershipReport {
	message := cause.Error()
	report.Error = &message

	_ = uc.clusters.UpdateSyncState(ctx, cluster.ID, clusterdomain.SyncState{
		LastStartedAt:   &startedAt,
		LastSucceededAt: cluster.Sync.LastSucceededAt,
		LastError:       message,
		MemberCount:     cluster.Sync.MemberCount,
		MatchedCount:    cluster.Sync.MatchedCount,
	})
	return report
}

// serverIndex matches cluster members to servers.
//
// A cluster's node name is usually the machine's hostname, so that is tried first, with
// addresses as the fallback. Ambiguous matches are treated as no match: attributing a
// cluster membership to the wrong server would misdirect every operation aimed at it.
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
		// A cluster may know a node by its fully qualified name.
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

func (i *serverIndex) match(member clusterdomain.Member) *serverdomain.Server {
	name := strings.ToLower(member.Name)
	if server := unique(i.byHostname[name]); server != nil {
		return server
	}
	// A short cluster node name against a stored FQDN, or the reverse.
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
