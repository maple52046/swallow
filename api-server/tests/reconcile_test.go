package tests

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

const (
	testSiteID        = "site-1"
	testIntegrationID = "integration-1"
)

type reconcileFixture struct {
	uc           *provisioningapp.ReconcileUseCase
	servers      *fakeServerRepo
	integrations *fakeIntegrationRepo
	provider     *fakeProvider
	factory      *fakeProviderFactory
	overlays     *fakeOSImageOverlayRepo
	tagOverlays  *fakeServerTagOverlayRepo
}

func setupReconcile(t *testing.T) *reconcileFixture {
	t.Helper()

	servers := newFakeServerRepo()
	integrations := newFakeIntegrationRepo()
	provider := newFakeProvider()
	factory := newFakeProviderFactory()
	factory.providers[testIntegrationID] = provider

	now := time.Now().UTC()
	_ = integrations.Create(context.Background(), &sitedomain.Integration{
		ID:           testIntegrationID,
		SiteID:       testSiteID,
		Kind:         sitedomain.IntegrationKindProvisioner,
		ProviderKind: sitedomain.ProviderKindMAAS,
		Name:         "maas-east",
		Endpoint:     "http://maas:5240/MAAS",
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, "ck:tk:ts")

	overlays := newFakeOSImageOverlayRepo()
	tagOverlays := newFakeServerTagOverlayRepo()
	return &reconcileFixture{
		uc:           provisioningapp.NewReconcileUseCase(integrations, servers, factory, overlays, tagOverlays),
		servers:      servers,
		integrations: integrations,
		provider:     provider,
		factory:      factory,
		overlays:     overlays,
		tagOverlays:  tagOverlays,
	}
}

func testMachine(id, hostname string) *provisioningdomain.Machine {
	return &provisioningdomain.Machine{
		ID:             id,
		Hostname:       hostname,
		FQDN:           hostname + ".maas",
		Status:         provisioningdomain.MachineStatusReady,
		ProviderStatus: "Ready",
		PowerState:     provisioningdomain.PowerStateOff,
		Architecture:   "amd64/generic",
		CPUCores:       32,
		MemoryMiB:      131072,
		StorageGB:      512,
		IPAddresses:    []string{"10.0.1.10"},
	}
}

// A provisioner that cannot own tags leaves the tags to swallow: reconcile unions the swallow-owned
// overlay into the Server's Observed.Tags at projection, so every consumer sees the owned tags
// (decision 031). This is inert for MAAS (tagging-capable); here the provider is made non-capable.
func TestReconcile_MergesSwallowOwnedTagsWhenProviderNotTaggingCapable(t *testing.T) {
	f := setupReconcile(t)
	f.provider.capabilities.Tagging = false

	machine := testMachine("m1", "host-1")
	machine.Tags = []string{"provider-tag"}
	f.provider.withMachine(machine)

	// An existing Server for this machine, plus a swallow-owned overlay keyed by its id.
	now := time.Now().UTC()
	f.servers.servers["s1"] = &serverdomain.Server{
		ID:         "s1",
		Source:     serverdomain.Source{SiteID: testSiteID, IntegrationID: testIntegrationID, ProviderMachineID: "m1"},
		LastSeenAt: now,
		CreatedAt:  now,
	}
	if err := f.tagOverlays.Upsert(context.Background(), &provisioningdomain.ServerTagOverlay{
		ServerID:      "s1",
		IntegrationID: testIntegrationID,
		Tags:          []string{"owned-tag"},
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("seed overlay: %v", err)
	}

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := f.servers.servers["s1"].Observed.Tags
	want := []string{"provider-tag", "owned-tag"}
	if len(got) != len(want) {
		t.Fatalf("merged tags: got %v, want %v", got, want)
	}
	seen := map[string]bool{}
	for _, tag := range got {
		seen[tag] = true
	}
	if !seen["provider-tag"] || !seen["owned-tag"] {
		t.Errorf("merged tags: got %v, want both provider-tag and owned-tag", got)
	}
}

func TestReconcile_CreatesServerFromMachine(t *testing.T) {
	f := setupReconcile(t)
	f.provider.withMachine(testMachine("abc123", "gpu-node-01"))

	report, err := f.uc.Execute(context.Background(), testIntegrationID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Created != 1 || report.Machines != 1 {
		t.Fatalf("expected 1 machine created, got %+v", report)
	}
	if len(f.servers.servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(f.servers.servers))
	}

	for _, server := range f.servers.servers {
		if server.Source.SiteID != testSiteID || server.Source.ProviderMachineID != "abc123" {
			t.Errorf("unexpected source: %+v", server.Source)
		}
		if server.ID == "abc123" {
			t.Error("server ID must be swallow-issued, not the provider's machine ID")
		}
		if server.Observed.Hostname != "gpu-node-01" {
			t.Errorf("hostname: got %q", server.Observed.Hostname)
		}
		if server.Provisioning == nil || server.Provisioning.State != "ready" {
			t.Errorf("provisioning axis: %+v", server.Provisioning)
		}
		// The other two axes have never been observed and must stay absent rather
		// than being defaulted to something.
		if server.Membership != nil {
			t.Error("membership axis must be nil until a platform reports it")
		}
		if server.Health != nil {
			t.Error("health axis must never be set by the reconciler")
		}
	}
}

// A deployed machine gets its OS image's effective name mirrored onto the provisioning axis,
// resolved once per pass from the catalog (matching a subarch machine arch to the image's
// primary arch), so the fleet list can show a real image name without a per-request catalog read.
func TestReconcile_MirrorsDeployedImageName(t *testing.T) {
	f := setupReconcile(t)
	machine := testMachine("abc123", "gpu-node-01")
	machine.Status = provisioningdomain.MachineStatusDeployed
	machine.ProviderStatus = "Deployed"
	machine.OSSystem = "ubuntu"
	machine.DistroSeries = "jammy"
	// MAAS reports the machine arch with a subarch; the catalog image is plain "amd64".
	machine.Architecture = "amd64/generic"
	f.provider.withMachine(machine)

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var server *serverdomain.Server
	for _, s := range f.servers.servers {
		server = s
	}
	if server == nil || server.Provisioning == nil {
		t.Fatal("expected a projected server with a provisioning axis")
	}
	if server.Provisioning.DeployedImageName != "Ubuntu 22.04 LTS" {
		t.Errorf("DeployedImageName = %q, want the catalog name %q", server.Provisioning.DeployedImageName, "Ubuntu 22.04 LTS")
	}
	// No overlay default user: the built-in for the provider OS family applies (decision 039).
	if server.Provisioning.DeployedImageDefaultUser != "ubuntu" {
		t.Errorf("DeployedImageDefaultUser = %q, want the built-in %q", server.Provisioning.DeployedImageDefaultUser, "ubuntu")
	}
}

// Renaming an OS Image re-mirrors the effective name onto that image's deployed servers at once,
// without a reconcile pass: RefreshDeployedImageNames reads the catalog + overlay and updates the
// projection so the fleet list shows the swallow name immediately.
func TestReconcile_RefreshDeployedImageNamesAfterRename(t *testing.T) {
	f := setupReconcile(t)
	machine := testMachine("abc123", "gpu-node-01")
	machine.Status = provisioningdomain.MachineStatusDeployed
	machine.ProviderStatus = "Deployed"
	machine.OSSystem = "ubuntu"
	machine.DistroSeries = "jammy"
	machine.Architecture = "amd64/generic"
	f.provider.withMachine(machine)

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The operator renames the image via the overlay; the provider catalog title is
	// "Ubuntu 22.04 LTS", and the overlay's architecture matches the catalog's primary arch.
	if err := f.overlays.Upsert(context.Background(), &provisioningdomain.OSImageOverlay{
		IntegrationID: testIntegrationID,
		ImageID:       "ubuntu/jammy",
		Architecture:  "amd64",
		DisplayName:   "Golden Ubuntu",
		DefaultUser:   "ops",
	}); err != nil {
		t.Fatalf("seed overlay: %v", err)
	}

	if err := f.uc.RefreshDeployedImageNames(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("RefreshDeployedImageNames: %v", err)
	}

	var server *serverdomain.Server
	for _, s := range f.servers.servers {
		server = s
	}
	if server == nil || server.Provisioning == nil {
		t.Fatal("expected a projected server with a provisioning axis")
	}
	if server.Provisioning.DeployedImageName != "Golden Ubuntu" {
		t.Errorf("DeployedImageName = %q, want the renamed %q", server.Provisioning.DeployedImageName, "Golden Ubuntu")
	}
	if server.Provisioning.DeployedImageDefaultUser != "ops" {
		t.Errorf("DeployedImageDefaultUser = %q, want the overlay default user %q", server.Provisioning.DeployedImageDefaultUser, "ops")
	}
}

// Right after a deploy completes, the single-machine RefreshServer fills the mirrored OS image
// name so the fleet list shows the friendly name immediately, instead of the raw OS/release id
// until the next reconcile pass.
func TestRefreshServer_FillsDeployedImageNameOnFreshDeploy(t *testing.T) {
	f := setupReconcile(t)
	machine := testMachine("abc123", "gpu-node-01")
	machine.Status = provisioningdomain.MachineStatusDeployed
	machine.ProviderStatus = "Deployed"
	machine.OSSystem = "ubuntu"
	machine.DistroSeries = "jammy"
	machine.Architecture = "amd64/generic"
	f.provider.withMachine(machine)

	// Seed the projection as a just-completed deploy: deployed, but the mirrored name is still
	// blank because reconcile has not run yet — the exact gap the fleet list was showing an id for.
	if err := f.servers.Upsert(context.Background(), &serverdomain.Server{
		ID:           "srv-1",
		Source:       serverdomain.Source{SiteID: testSiteID, IntegrationID: testIntegrationID, ProviderMachineID: "abc123"},
		Observed:     serverdomain.Observed{Architecture: "amd64/generic"},
		Provisioning: &serverdomain.ProvisioningStatus{State: "deployed", OSSystem: "ubuntu", DistroSeries: "jammy"},
	}); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	uc := provisioningapp.NewRefreshServerUseCase(f.servers, f.factory, f.overlays)
	if _, err := uc.Execute(context.Background(), "srv-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := f.servers.servers["srv-1"]
	if got == nil || got.Provisioning == nil {
		t.Fatal("expected the refreshed server projection")
	}
	if got.Provisioning.DeployedImageName != "Ubuntu 22.04 LTS" {
		t.Errorf("DeployedImageName = %q, want the catalog name filled on refresh", got.Provisioning.DeployedImageName)
	}
	if got.Provisioning.DeployedImageDefaultUser != "ubuntu" {
		t.Errorf("DeployedImageDefaultUser = %q, want it filled on refresh so wait-for-ssh can log in", got.Provisioning.DeployedImageDefaultUser)
	}
}

// A redeploy to a different image must not carry the previous image's default user forward:
// automation would otherwise log in to the new image as the old image's account (decision 039).
func TestRefreshServer_DropsStaleDefaultUserOnRedeploy(t *testing.T) {
	f := setupReconcile(t)
	machine := testMachine("abc123", "gpu-node-01")
	machine.Status = provisioningdomain.MachineStatusDeploying
	machine.ProviderStatus = "Deploying"
	machine.OSSystem = "custom"
	machine.DistroSeries = "rocky-10"
	f.provider.withMachine(machine)

	if err := f.servers.Upsert(context.Background(), &serverdomain.Server{
		ID:     "srv-1",
		Source: serverdomain.Source{SiteID: testSiteID, IntegrationID: testIntegrationID, ProviderMachineID: "abc123"},
		Provisioning: &serverdomain.ProvisioningStatus{
			State: "deployed", OSSystem: "ubuntu", DistroSeries: "jammy",
			DeployedImageName: "Ubuntu 22.04 LTS", DeployedImageDefaultUser: "ubuntu",
		},
	}); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	uc := provisioningapp.NewRefreshServerUseCase(f.servers, f.factory, f.overlays)
	if _, err := uc.Execute(context.Background(), "srv-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := f.servers.servers["srv-1"].Provisioning
	if got.DeployedImageDefaultUser != "" || got.DeployedImageName != "" {
		t.Errorf("after a redeploy to another image, mirrored image = %q/%q, want both cleared", got.DeployedImageName, got.DeployedImageDefaultUser)
	}
}

// A Server Default User set on the Server belongs to one OS installation (decision 045): reconcile
// keeps it while the machine runs that OS and clears it once the machine is seen without one.
func TestReconcile_ClearsServerDefaultUserOnlyWithoutOS(t *testing.T) {
	tests := []struct {
		name   string
		status provisioningdomain.MachineStatus
		want   string
	}{
		{name: "deployed keeps it", status: provisioningdomain.MachineStatusDeployed, want: "amd"},
		{name: "rescue keeps it", status: provisioningdomain.MachineStatusRescue, want: "amd"},
		{name: "ready clears it", status: provisioningdomain.MachineStatusReady, want: ""},
		{name: "allocated clears it", status: provisioningdomain.MachineStatusAllocated, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupReconcile(t)
			machine := testMachine("abc123", "tainan-ci")
			machine.Status = tc.status
			f.provider.withMachine(machine)
			f.servers.servers["srv-1"] = &serverdomain.Server{
				ID:          "srv-1",
				Source:      serverdomain.Source{SiteID: testSiteID, IntegrationID: testIntegrationID, ProviderMachineID: "abc123"},
				DefaultUser: "amd",
			}

			if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got := f.servers.servers["srv-1"].DefaultUser; got != tc.want {
				t.Errorf("DefaultUser after reconcile of a %s machine = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}

// A machine that is not deployed carries no deployed-image name, so a ready or inspecting
// machine is never mislabelled with whatever image the catalog happens to list.
func TestReconcile_NoDeployedImageNameWhenNotDeployed(t *testing.T) {
	f := setupReconcile(t)
	f.provider.withMachine(testMachine("abc123", "gpu-node-01")) // status ready

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, s := range f.servers.servers {
		if s.Provisioning != nil && s.Provisioning.DeployedImageName != "" {
			t.Errorf("DeployedImageName = %q, want empty for a non-deployed machine", s.Provisioning.DeployedImageName)
		}
	}
}

func TestReconcile_UpdatesInPlaceOnSecondPass(t *testing.T) {
	f := setupReconcile(t)
	machine := testMachine("abc123", "gpu-node-01")
	f.provider.withMachine(machine)

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	var firstID string
	for id := range f.servers.servers {
		firstID = id
	}

	machine.Status = provisioningdomain.MachineStatusDeployed
	machine.ProviderStatus = "Deployed"
	machine.OSSystem = "ubuntu"
	machine.DistroSeries = "jammy"

	report, err := f.uc.Execute(context.Background(), testIntegrationID)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if report.Updated != 1 || report.Created != 0 {
		t.Fatalf("expected an in-place update, got %+v", report)
	}
	if len(f.servers.servers) != 1 {
		t.Fatalf("expected still 1 server, got %d", len(f.servers.servers))
	}
	server := f.servers.servers[firstID]
	if server == nil {
		t.Fatal("server identity changed across passes")
	}
	if server.Provisioning.State != "deployed" {
		t.Errorf("provisioning state: got %q, want deployed", server.Provisioning.State)
	}
}

// Re-enrollment inside one provisioner: the machine reappears under a new ID and must
// keep its server identity, so that operations and allocations still point somewhere.
func TestReconcile_RelinksReEnrolledMachineWithinSameIntegration(t *testing.T) {
	f := setupReconcile(t)
	machine := testMachine("abc123", "gpu-node-01")
	machine.SystemUUID = "uuid-aaa"
	machine.SerialNumber = "SN-1"
	f.provider.withMachine(machine)

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	var originalID string
	for id := range f.servers.servers {
		originalID = id
	}

	// Same hardware, new provider identity.
	delete(f.provider.machines, "abc123")
	reEnrolled := testMachine("xyz789", "gpu-node-01")
	reEnrolled.SystemUUID = "uuid-aaa"
	reEnrolled.SerialNumber = "SN-1"
	f.provider.withMachine(reEnrolled)

	report, err := f.uc.Execute(context.Background(), testIntegrationID)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if report.Relinked != 1 {
		t.Fatalf("expected 1 relink, got %+v", report)
	}
	if len(f.servers.servers) != 1 {
		t.Fatalf("expected no duplicate server, got %d", len(f.servers.servers))
	}
	server := f.servers.servers[originalID]
	if server == nil {
		t.Fatal("relink must keep the original server ID")
	}
	if server.Source.ProviderMachineID != "xyz789" {
		t.Errorf("source not re-pointed: %+v", server.Source)
	}
}

func TestReconcile_RelinksMachineThatMovedFromAnAbsentIntegration(t *testing.T) {
	f := setupReconcile(t)

	// A server from a different provisioner that has stopped reporting it.
	f.servers.servers["srv-moved"] = &serverdomain.Server{
		ID: "srv-moved",
		Source: serverdomain.Source{
			SiteID: "site-old", IntegrationID: "integration-old", ProviderMachineID: "old-1",
		},
		Hardware:   serverdomain.Hardware{SystemUUID: "uuid-move"},
		Absent:     true,
		LastSeenAt: time.Now().Add(-24 * time.Hour),
		CreatedAt:  time.Now().Add(-48 * time.Hour),
	}

	machine := testMachine("new-1", "gpu-node-09")
	machine.SystemUUID = "uuid-move"
	f.provider.withMachine(machine)

	report, err := f.uc.Execute(context.Background(), testIntegrationID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Relinked != 1 {
		t.Fatalf("expected the moved machine to be relinked, got %+v", report)
	}
	server := f.servers.servers["srv-moved"]
	if server.Source.IntegrationID != testIntegrationID || server.Source.SiteID != testSiteID {
		t.Errorf("source not moved to the new provisioner: %+v", server.Source)
	}
	if server.Absent {
		t.Error("a machine that is being reported again must not stay absent")
	}
}

// If two provisioners both actively report the same hardware, relinking would make
// their reconcilers fight over one server on every pass. Refuse instead.
func TestReconcile_ConflictWhenHardwareStillReportedByAnotherIntegration(t *testing.T) {
	f := setupReconcile(t)

	f.servers.servers["srv-active"] = &serverdomain.Server{
		ID: "srv-active",
		Source: serverdomain.Source{
			SiteID: "site-other", IntegrationID: "integration-other", ProviderMachineID: "other-1",
		},
		Hardware:   serverdomain.Hardware{SystemUUID: "uuid-shared"},
		Absent:     false,
		LastSeenAt: time.Now(),
	}

	machine := testMachine("mine-1", "gpu-node-10")
	machine.SystemUUID = "uuid-shared"
	f.provider.withMachine(machine)

	report, err := f.uc.Execute(context.Background(), testIntegrationID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(report.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %+v", report)
	}
	if report.Created != 0 || report.Relinked != 0 {
		t.Fatalf("a conflict must not also create or relink: %+v", report)
	}
	if f.servers.servers["srv-active"].Source.IntegrationID != "integration-other" {
		t.Error("the other provisioner's server must be left alone")
	}
	if len(f.servers.servers) != 1 {
		t.Errorf("nothing new should be created for a conflicted machine, got %d servers", len(f.servers.servers))
	}
}

func TestReconcile_ConflictWhenHardwareMatchesMoreThanOneServer(t *testing.T) {
	f := setupReconcile(t)

	for _, id := range []string{"srv-a", "srv-b"} {
		f.servers.servers[id] = &serverdomain.Server{
			ID:     id,
			Source: serverdomain.Source{SiteID: testSiteID, IntegrationID: testIntegrationID, ProviderMachineID: id},
			// Cloned virtual machines really do share a system UUID, which is why
			// this is treated as bad data rather than a merge instruction.
			Hardware:   serverdomain.Hardware{SystemUUID: "uuid-cloned"},
			LastSeenAt: time.Now(),
		}
	}

	machine := testMachine("clone-3", "gpu-node-11")
	machine.SystemUUID = "uuid-cloned"
	f.provider.withMachine(machine)

	report, err := f.uc.Execute(context.Background(), testIntegrationID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(report.Conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %+v", report)
	}
	if len(report.Conflicts[0].CandidateServerIDs) != 2 {
		t.Errorf("the conflict must name both candidates, got %v", report.Conflicts[0].CandidateServerIDs)
	}
	if len(f.servers.servers) != 2 {
		t.Errorf("no server may be created or merged, got %d", len(f.servers.servers))
	}
}

// A machine with no hardware identifiers is normal before commissioning. It must be
// created, not matched against every other identifier-less server.
func TestReconcile_MachineWithoutHardwareIdentifiersIsCreated(t *testing.T) {
	f := setupReconcile(t)

	f.servers.servers["srv-existing"] = &serverdomain.Server{
		ID:         "srv-existing",
		Source:     serverdomain.Source{SiteID: testSiteID, IntegrationID: testIntegrationID, ProviderMachineID: "existing"},
		Hardware:   serverdomain.Hardware{},
		LastSeenAt: time.Now(),
	}

	f.provider.withMachine(testMachine("uncommissioned", "unknown-node"))

	report, err := f.uc.Execute(context.Background(), testIntegrationID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Created != 1 {
		t.Fatalf("expected a new server, got %+v", report)
	}
	if len(report.Conflicts) != 0 {
		t.Fatalf("empty identifiers must not match anything: %+v", report.Conflicts)
	}
}

// Firmware that reports a placeholder instead of leaving a field blank must not look
// like an identity. MAAS reports the literal string "Unknown" as the serial of every
// virtual machine, which made each machine in a batch match the previous one and
// collapsed a seven-machine lab into a single server, one relink at a time.
func TestReconcile_PlaceholderSerialsDoNotCollapseDistinctMachines(t *testing.T) {
	f := setupReconcile(t)

	names := []string{"lab-control-1", "lab-control-2", "lab-compute-1", "lab-compute-2"}
	for i, name := range names {
		machine := testMachine(fmt.Sprintf("machine-%d", i), name)
		machine.SystemUUID = fmt.Sprintf("uuid-%d", i)
		machine.SerialNumber = "Unknown"
		machine.MACAddresses = []string{fmt.Sprintf("52:54:00:00:00:%02d", i)}
		f.provider.withMachine(machine)
	}

	report, err := f.uc.Execute(context.Background(), testIntegrationID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Created != len(names) {
		t.Fatalf("each machine is distinct and needs its own server, got %+v", report)
	}
	if report.Relinked != 0 || len(report.Conflicts) != 0 {
		t.Fatalf("a placeholder serial must not look like a match: %+v", report)
	}
	if len(f.servers.servers) != len(names) {
		t.Fatalf("expected %d servers, got %d", len(names), len(f.servers.servers))
	}
	for _, server := range f.servers.servers {
		if server.Hardware.SerialNumber != "" {
			t.Errorf("a placeholder must not be stored as a serial, got %q", server.Hardware.SerialNumber)
		}
	}
}

func TestHardwareIdentifiers_IgnoresPlaceholderValues(t *testing.T) {
	for _, placeholder := range []string{
		"Unknown", "unknown", "  None  ", "N/A", "Not Specified", "Default string",
		"To Be Filled By O.E.M.", "System Serial Number", "0", "Invalid",
		"00000000-0000-0000-0000-000000000000",
	} {
		hardware := serverdomain.Hardware{
			SystemUUID:   placeholder,
			SerialNumber: placeholder,
			MACAddresses: []string{placeholder, "00:00:00:00:00:00"},
		}
		if !hardware.Empty() {
			systemUUID, serial, macs := hardware.Identifiers()
			t.Errorf("%q must not identify anything, got uuid=%q serial=%q macs=%v",
				placeholder, systemUUID, serial, macs)
		}
	}

	real := serverdomain.Hardware{SerialNumber: " SN-1 "}
	if _, serial, _ := real.Identifiers(); serial != "SN-1" {
		t.Errorf("a real serial must survive trimming, got %q", serial)
	}
}

// Two machines reporting the same identifier must not take turns claiming one server.
// The first relink leaves the server count at one, so without recording the claim the
// second match still looks unambiguous and silently steals the same record.
func TestReconcile_SecondMachineClaimingTheSameServerInOnePassConflicts(t *testing.T) {
	f := setupReconcile(t)

	f.servers.servers["srv-shared"] = &serverdomain.Server{
		ID: "srv-shared",
		Source: serverdomain.Source{
			SiteID: "site-old", IntegrationID: "integration-old", ProviderMachineID: "old-1",
		},
		Hardware:   serverdomain.Hardware{SystemUUID: "uuid-dup"},
		Absent:     true,
		LastSeenAt: time.Now().Add(-24 * time.Hour),
	}

	for _, id := range []string{"dup-a", "dup-b"} {
		machine := testMachine(id, id)
		machine.SystemUUID = "uuid-dup"
		f.provider.withMachine(machine)
	}

	report, err := f.uc.Execute(context.Background(), testIntegrationID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if report.Relinked != 1 {
		t.Fatalf("exactly one machine may claim the server, got %+v", report)
	}
	if len(report.Conflicts) != 1 {
		t.Fatalf("the other machine must be reported as a conflict, got %+v", report)
	}
	if report.Created != 0 {
		t.Errorf("a conflicted machine must not get a server of its own: %+v", report)
	}
	if len(f.servers.servers) != 1 {
		t.Errorf("expected the single existing server, got %d", len(f.servers.servers))
	}
	if ids := report.Conflicts[0].CandidateServerIDs; len(ids) != 1 || ids[0] != "srv-shared" {
		t.Errorf("the conflict must name the contested server, got %v", ids)
	}
}

// Ephemerality is the provisioner's fact, not a memory of what swallow once asked for: a
// machine can be redeployed the other way round without swallow being involved, so the
// reconciler has to carry whatever the provisioner currently reports.
func TestReconcile_ProjectsEphemeralityFromTheProvisioner(t *testing.T) {
	f := setupReconcile(t)
	machine := testMachine("abc123", "gpu-node-01")
	machine.Status = provisioningdomain.MachineStatusDeployed
	machine.ProviderStatus = "Deployed"
	machine.PowerState = provisioningdomain.PowerStateOn
	machine.Ephemeral = true
	machine.HWEKernel = "ga-24.04"
	f.provider.withMachine(machine)

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	for _, server := range f.servers.servers {
		if !server.Provisioning.Ephemeral {
			t.Error("an ephemerally deployed machine must be projected as such")
		}
		if server.Provisioning.HWEKernel != "ga-24.04" {
			t.Errorf("kernel label: got %q", server.Provisioning.HWEKernel)
		}
	}

	// Redeployed to disk outside swallow: the axis must follow the provisioner rather than
	// keeping the value it first saw.
	machine.Ephemeral = false
	f.provider.withMachine(machine)
	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	for _, server := range f.servers.servers {
		if server.Provisioning.Ephemeral {
			t.Error("the axis must stop claiming ephemerality once the provisioner does")
		}
	}
}

func TestReconcile_MarksVanishedMachinesAbsentWithoutDeleting(t *testing.T) {
	f := setupReconcile(t)
	f.provider.withMachine(testMachine("abc123", "gpu-node-01"))

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("first pass: %v", err)
	}

	delete(f.provider.machines, "abc123")

	report, err := f.uc.Execute(context.Background(), testIntegrationID)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if report.MarkedAbsent != 1 {
		t.Fatalf("expected 1 marked absent, got %+v", report)
	}
	if len(f.servers.servers) != 1 {
		t.Fatal("an absent server must not be deleted: allocation history depends on it")
	}
	for _, server := range f.servers.servers {
		if !server.Absent {
			t.Error("expected the server to be flagged absent")
		}
	}
}

func TestReconcile_ReturningMachineClearsAbsent(t *testing.T) {
	f := setupReconcile(t)
	machine := testMachine("abc123", "gpu-node-01")
	f.provider.withMachine(machine)

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	delete(f.provider.machines, "abc123")
	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	f.provider.withMachine(machine)

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("third pass: %v", err)
	}
	for _, server := range f.servers.servers {
		if server.Absent {
			t.Error("a machine that came back must no longer be absent")
		}
	}
}

func TestReconcile_PreservesMembershipAcrossPasses(t *testing.T) {
	f := setupReconcile(t)
	f.provider.withMachine(testMachine("abc123", "gpu-node-01"))

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("first pass: %v", err)
	}

	var serverID string
	for id := range f.servers.servers {
		serverID = id
	}
	membership := &serverdomain.MembershipStatus{
		PlatformID: "platform-1", NodeName: "gpu-node-01", Role: "worker",
		State: "ready", ObservedAt: time.Now().UTC(),
	}
	if err := f.servers.SetMembership(context.Background(), serverID, membership); err != nil {
		t.Fatalf("SetMembership: %v", err)
	}

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("second pass: %v", err)
	}

	if f.servers.servers[serverID].Membership == nil {
		t.Fatal("the reconciler must not clear the membership axis it does not own")
	}
}

func TestReconcile_RecordsSyncSuccess(t *testing.T) {
	f := setupReconcile(t)
	f.provider.withMachine(testMachine("abc123", "gpu-node-01"))

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	integration, err := f.integrations.FindByID(context.Background(), testIntegrationID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if integration.Sync.LastSucceededAt == nil {
		t.Fatal("a successful pass must record lastSucceededAt")
	}
	if integration.Sync.LastError != "" {
		t.Errorf("unexpected error recorded: %q", integration.Sync.LastError)
	}
}

// A provider failure must be reported without discarding the previous success time:
// a reader needs to know both that the last attempt failed and how old the data is.
func TestReconcile_ProviderFailureKeepsPreviousSuccessTimestamp(t *testing.T) {
	f := setupReconcile(t)
	f.provider.withMachine(testMachine("abc123", "gpu-node-01"))

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	before, _ := f.integrations.FindByID(context.Background(), testIntegrationID)
	firstSuccess := before.Sync.LastSucceededAt
	if firstSuccess == nil {
		t.Fatal("expected the first pass to record success")
	}

	f.provider.listErr = &provisioningdomain.ProviderError{
		Kind:   provisioningdomain.ProviderErrorUnavailable,
		Detail: "Could not reach MAAS.",
	}

	report, err := f.uc.Execute(context.Background(), testIntegrationID)
	if err != nil {
		t.Fatalf("Execute must report the failure in the report, not as an error: %v", err)
	}
	if report.Error == nil {
		t.Fatal("expected the report to carry the failure")
	}

	after, _ := f.integrations.FindByID(context.Background(), testIntegrationID)
	if after.Sync.LastSucceededAt == nil || !after.Sync.LastSucceededAt.Equal(*firstSuccess) {
		t.Error("a failed pass must keep the previous success timestamp")
	}
	if after.Sync.LastError == "" {
		t.Error("expected the failure to be recorded")
	}
}

// A machine the provider still reports must not be marked absent because a later
// integration failed. Each provisioner's projection is independent.
func TestReconcile_FailureDoesNotMarkServersAbsent(t *testing.T) {
	f := setupReconcile(t)
	f.provider.withMachine(testMachine("abc123", "gpu-node-01"))

	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("first pass: %v", err)
	}

	f.provider.listErr = errors.New("boom")
	if _, err := f.uc.Execute(context.Background(), testIntegrationID); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, server := range f.servers.servers {
		if server.Absent {
			t.Error("an unreachable provisioner must not make its servers look gone")
		}
	}
}

func TestReconcile_RejectsNonProvisionerIntegration(t *testing.T) {
	f := setupReconcile(t)

	now := time.Now().UTC()
	_ = f.integrations.Create(context.Background(), &sitedomain.Integration{
		ID:           "integration-metrics",
		SiteID:       testSiteID,
		Kind:         sitedomain.IntegrationKindMetrics,
		ProviderKind: sitedomain.ProviderKindPrometheus,
		Name:         "prometheus",
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, "token")

	_, err := f.uc.Execute(context.Background(), "integration-metrics")
	if !errors.Is(err, provisioningdomain.ErrIntegrationNotProvisioner) {
		t.Fatalf("expected ErrIntegrationNotProvisioner, got %v", err)
	}
}

func TestReconcile_ExecuteAllSkipsDisabledIntegrations(t *testing.T) {
	f := setupReconcile(t)
	f.provider.withMachine(testMachine("abc123", "gpu-node-01"))

	integration, _ := f.integrations.FindByID(context.Background(), testIntegrationID)
	integration.Enabled = false

	reports, err := f.uc.ExecuteAll(context.Background())
	if err != nil {
		t.Fatalf("ExecuteAll: %v", err)
	}
	if len(reports) != 0 {
		t.Fatalf("a paused integration must not be reconciled, got %d reports", len(reports))
	}
	if len(f.servers.servers) != 0 {
		t.Fatal("no servers should have been projected")
	}
}

// One provisioner failing must not stop the others: sites fail independently.
func TestReconcile_ExecuteAllContinuesAfterOneFailure(t *testing.T) {
	f := setupReconcile(t)
	f.provider.listErr = errors.New("site down")

	healthy := newFakeProvider()
	healthy.withMachine(testMachine("ok-1", "gpu-node-20"))
	f.factory.providers["integration-2"] = healthy

	now := time.Now().UTC()
	_ = f.integrations.Create(context.Background(), &sitedomain.Integration{
		ID:           "integration-2",
		SiteID:       "site-2",
		Kind:         sitedomain.IntegrationKindProvisioner,
		ProviderKind: sitedomain.ProviderKindMAAS,
		Name:         "maas-west",
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, "ck:tk:ts")

	reports, err := f.uc.ExecuteAll(context.Background())
	if err != nil {
		t.Fatalf("ExecuteAll: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("expected a report per integration, got %d", len(reports))
	}

	failures, successes := 0, 0
	for _, report := range reports {
		if report.Error != nil {
			failures++
		} else {
			successes++
		}
	}
	if failures != 1 || successes != 1 {
		t.Fatalf("expected one failure and one success, got %d/%d", failures, successes)
	}
	if len(f.servers.servers) != 1 {
		t.Fatal("the healthy site's machine should still have been projected")
	}
}
