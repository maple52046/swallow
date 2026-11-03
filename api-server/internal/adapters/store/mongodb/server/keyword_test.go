package server

import (
	"context"
	"slices"
	"testing"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// The servers-list keyword matches hostname, FQDN, address, serial number, and system UUID as a
// case-insensitive literal substring: a regex metacharacter matches only itself, and an unbalanced
// one is not a query error.
func TestListKeywordIsALiteralSubstringOfIdentityFields(t *testing.T) {
	_, repo, _ := claimTestDatabase(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, server := range []*serverdomain.Server{
		{
			ID: "srv-a", Source: serverdomain.Source{SiteID: "site-1", IntegrationID: "maas-1", ProviderMachineID: "a"},
			Hardware:  serverdomain.Hardware{SerialNumber: "SN-12345", SystemUUID: "4C4C4544-0042"},
			Observed:  serverdomain.Observed{Hostname: "gpu-01", FQDN: "gpu-01.lab", Addresses: []string{"10.0.0.11"}},
			CreatedAt: now, UpdatedAt: now, LastSeenAt: now,
		},
		{
			ID: "srv-b", Source: serverdomain.Source{SiteID: "site-1", IntegrationID: "maas-1", ProviderMachineID: "b"},
			Hardware:  serverdomain.Hardware{SerialNumber: "XYZ(1)"},
			Observed:  serverdomain.Observed{Hostname: "cpu-02", FQDN: "cpu-02.lab", Addresses: []string{"10.0.0.12"}},
			CreatedAt: now, UpdatedAt: now, LastSeenAt: now,
		},
	} {
		if err := repo.Upsert(ctx, server); err != nil {
			t.Fatalf("Upsert(%s) error = %v", server.ID, err)
		}
	}

	for _, tc := range []struct {
		keyword string
		want    []string
	}{
		{keyword: "GPU-01", want: []string{"srv-a"}},
		{keyword: "10.0.0.12", want: []string{"srv-b"}},
		{keyword: "sn-123", want: []string{"srv-a"}},
		{keyword: "4c4c4544", want: []string{"srv-a"}},
		{keyword: "XYZ(1", want: []string{"srv-b"}},
		{keyword: "(", want: []string{"srv-b"}},
		{keyword: ".*", want: nil},
	} {
		result, err := repo.List(ctx, serverdomain.ListFilter{Keyword: tc.keyword})
		if err != nil {
			t.Errorf("List(keyword %q) error = %v", tc.keyword, err)
			continue
		}
		var got []string
		for _, server := range result.Servers {
			got = append(got, server.ID)
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Errorf("List(keyword %q) = %v, want %v", tc.keyword, got, tc.want)
		}
	}
}

// IDs restricts the listing to the given Servers; an empty, non-nil list matches none.
func TestListIDsRestrictsTheResult(t *testing.T) {
	_, repo, _ := claimTestDatabase(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, id := range []string{"srv-a", "srv-b", "srv-c"} {
		server := &serverdomain.Server{
			ID: id, Source: serverdomain.Source{SiteID: "site-1", IntegrationID: "maas-1", ProviderMachineID: id},
			Observed:  serverdomain.Observed{Hostname: id},
			CreatedAt: now, UpdatedAt: now, LastSeenAt: now,
		}
		if err := repo.Upsert(ctx, server); err != nil {
			t.Fatalf("Upsert(%s) error = %v", id, err)
		}
	}
	for _, tc := range []struct {
		ids  []string
		want []string
	}{
		{ids: []string{"srv-c", "srv-a", "srv-missing"}, want: []string{"srv-a", "srv-c"}},
		{ids: []string{}, want: nil},
		{ids: nil, want: []string{"srv-a", "srv-b", "srv-c"}},
	} {
		result, err := repo.List(ctx, serverdomain.ListFilter{IDs: tc.ids})
		if err != nil {
			t.Fatalf("List(ids %v) error = %v", tc.ids, err)
		}
		var got []string
		for _, server := range result.Servers {
			got = append(got, server.ID)
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Errorf("List(ids %v) = %v, want %v", tc.ids, got, tc.want)
		}
	}
}
