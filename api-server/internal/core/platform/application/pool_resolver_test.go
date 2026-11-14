package application

import (
	"context"
	"slices"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// A Platform's Servers are the ones it claims and its spec's members, including a member removed
// from the spec that it still claims; an unknown Platform is reported as not found, not as an error.
func TestPoolResolverServersOf(t *testing.T) {
	platforms := &deployFakePlatformRepo{platforms: map[string]*platformdomain.Platform{
		"plat-1": {ID: "plat-1", Spec: &platformdomain.PlatformSpec{Members: []platformdomain.PlatformMember{
			{ServerID: "srv-a"}, {ServerID: "srv-b"},
		}}},
	}}
	servers := &deployFakeServerRepo{servers: map[string]*serverdomain.Server{
		"srv-a":   {ID: "srv-a", PlatformClaim: &serverdomain.PlatformClaim{PlatformID: "plat-1"}},
		"srv-old": {ID: "srv-old", PlatformClaim: &serverdomain.PlatformClaim{PlatformID: "plat-1"}},
		"srv-b":   {ID: "srv-b"},
	}}
	resolver := NewPoolResolver(platforms, servers, servers)

	ids, found, err := resolver.ServersOf(context.Background(), "plat-1")
	if err != nil || !found {
		t.Fatalf("ServersOf(plat-1) = found %v, error %v; want found", found, err)
	}
	slices.Sort(ids)
	if want := []string{"srv-a", "srv-a", "srv-b", "srv-old"}; !slices.Equal(ids, want) {
		t.Errorf("ServersOf(plat-1) = %v, want %v", ids, want)
	}

	if _, found, err := resolver.ServersOf(context.Background(), "missing"); err != nil || found {
		t.Errorf("ServersOf(missing) = found %v, error %v; want not found without an error", found, err)
	}
}
