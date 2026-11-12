package provisioning

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/adapters/store/mongodb/mongotest"
	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
)

// Adoption inserts only when no setting exists, a spec write upserts and increments the generation,
// and the status round trips. Skipped without SWALLOW_TEST_MONGO_URI.
func TestProvisionerSettingsRepos(t *testing.T) {
	_, db := mongotest.Database(t, "settings")
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	proxies := NewMongoHTTPProxyRepo(db)
	observed := provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeBuiltin}
	adopted := &provisioningdomain.HTTPProxySetting{IntegrationID: "maas-a", Generation: 1, Spec: observed,
		Status: provisioningdomain.HTTPProxyStatus{ObservedGeneration: 1, Observed: &observed, ObservedAt: &now}}
	if ok, err := proxies.Adopt(ctx, adopted); err != nil || !ok {
		t.Fatalf("Adopt = %v, %v", ok, err)
	}
	if ok, err := proxies.Adopt(ctx, adopted); err != nil || ok {
		t.Fatalf("second Adopt = %v, %v; want refused", ok, err)
	}
	spec := provisioningdomain.HTTPProxyConfiguration{Mode: provisioningdomain.HTTPProxyModeExternal, URL: "http://proxy:3128"}
	stored, err := proxies.PutSpec(ctx, "maas-a", spec)
	if err != nil || stored.Generation != 2 || stored.Spec != spec || stored.Status.Observed == nil || *stored.Status.Observed != observed {
		t.Fatalf("PutSpec = %+v, %v; want generation 2 with the status kept", stored, err)
	}
	created, err := proxies.PutSpec(ctx, "maas-b", spec)
	if err != nil || created.Generation != 1 || created.Status.Observed != nil {
		t.Fatalf("PutSpec of a new Integration = %+v, %v; want generation 1, not observed", created, err)
	}

	dns, err := NewMongoSubnetDNSRepo(db)
	if err != nil {
		t.Fatalf("NewMongoSubnetDNSRepo: %v", err)
	}
	if ok, err := dns.Adopt(ctx, &provisioningdomain.SubnetDNSSetting{IntegrationID: "maas-a", SubnetID: "7", Generation: 1}); err != nil || !ok {
		t.Fatalf("Adopt subnet = %v, %v", ok, err)
	}
	setting, err := dns.PutSpec(ctx, "maas-a", "7", []string{"10.0.0.53"})
	if err != nil || setting.Generation != 2 || !slices.Equal(setting.Servers, []string{"10.0.0.53"}) {
		t.Fatalf("PutSpec subnet = %+v, %v", setting, err)
	}
	if err := dns.SetStatus(ctx, "maas-a", "7", provisioningdomain.SubnetDNSStatus{ObservedGeneration: 2, Observed: []string{"10.0.0.53"}, ObservedAt: &now}); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	listed, err := dns.ListByIntegration(ctx, "maas-a")
	if err != nil || len(listed) != 1 || listed[0].SubnetID != "7" || listed[0].Status.ObservedGeneration != 2 || !listed[0].Status.ObservedAt.Equal(now) {
		t.Fatalf("ListByIntegration = %+v, %v", listed, err)
	}
	if err := dns.Delete(ctx, "maas-a", "7"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := dns.Get(ctx, "maas-a", "7"); err != provisioningdomain.ErrSettingNotFound {
		t.Fatalf("Get after Delete = %v, want ErrSettingNotFound", err)
	}
}
