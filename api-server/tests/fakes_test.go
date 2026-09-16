package tests

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	monitoringdomain "github.com/maple52046/swallow/internal/monitoring/domain"
	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// --- server repository ---

type fakeServerRepo struct {
	mu        sync.Mutex
	servers   map[string]*serverdomain.Server
	deleteErr error
}

func newFakeServerRepo() *fakeServerRepo {
	return &fakeServerRepo{servers: make(map[string]*serverdomain.Server)}
}

func (r *fakeServerRepo) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.servers[id]
	if !ok {
		return nil, serverdomain.ErrServerNotFound
	}
	return s, nil
}

func (r *fakeServerRepo) FindBySource(_ context.Context, source serverdomain.Source) (*serverdomain.Server, error) {
	for _, s := range r.servers {
		if s.Source == source {
			return s, nil
		}
	}
	return nil, serverdomain.ErrServerNotFound
}

// FindByHardware mirrors the production rule: only non-empty identifiers match, so a
// server with no serial number is not a candidate for every machine that also has none.
func (r *fakeServerRepo) FindByHardware(_ context.Context, hardware serverdomain.Hardware) ([]*serverdomain.Server, error) {
	systemUUID, serial, macs := hardware.Identifiers()
	if systemUUID == "" && serial == "" && len(macs) == 0 {
		return nil, nil
	}

	var matches []*serverdomain.Server
	for _, s := range r.servers {
		if systemUUID != "" && s.Hardware.SystemUUID == systemUUID {
			matches = append(matches, s)
			continue
		}
		if serial != "" && s.Hardware.SerialNumber == serial {
			matches = append(matches, s)
			continue
		}
		if macsIntersect(s.Hardware.MACAddresses, macs) {
			matches = append(matches, s)
		}
	}
	return matches, nil
}

func macsIntersect(a, b []string) bool {
	for _, left := range a {
		if left == "" {
			continue
		}
		for _, right := range b {
			if left == right {
				return true
			}
		}
	}
	return false
}

func (r *fakeServerRepo) List(_ context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error) {
	var all []*serverdomain.Server
	for _, s := range r.servers {
		if filter.SiteID != "" && s.Source.SiteID != filter.SiteID {
			continue
		}
		if filter.IntegrationID != "" && s.Source.IntegrationID != filter.IntegrationID {
			continue
		}
		if filter.ProvisioningState != "" {
			if s.Provisioning == nil || s.Provisioning.State != filter.ProvisioningState {
				continue
			}
		}
		if filter.PlatformID != "" {
			if s.Membership == nil || s.Membership.PlatformID != filter.PlatformID {
				continue
			}
		}
		if filter.Tag != "" && !containsString(s.Observed.Tags, filter.Tag) {
			continue
		}
		if !filter.IncludeAbsent && s.Absent {
			continue
		}
		if filter.Keyword != "" {
			keyword := strings.ToLower(filter.Keyword)
			if !strings.Contains(strings.ToLower(s.Observed.Hostname), keyword) &&
				!strings.Contains(strings.ToLower(s.Observed.FQDN), keyword) {
				continue
			}
		}
		all = append(all, s)
	}

	// Sorted so that pagination assertions are stable, matching the production sort.
	sortServersByHostname(all)

	total := len(all)
	start := filter.Offset
	if start > total {
		start = total
	}
	end := total
	if filter.Limit > 0 && start+filter.Limit < total {
		end = start + filter.Limit
	}

	return serverdomain.ListResult{Servers: all[start:end], Total: total}, nil
}

// containsString reports whether value is present in list, used by the fake repo to
// mirror MongoDB's scalar-against-array tag match.
func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func sortServersByHostname(servers []*serverdomain.Server) {
	for i := 1; i < len(servers); i++ {
		for j := i; j > 0; j-- {
			left, right := servers[j-1], servers[j]
			if left.Observed.Hostname < right.Observed.Hostname ||
				(left.Observed.Hostname == right.Observed.Hostname && left.ID <= right.ID) {
				break
			}
			servers[j-1], servers[j] = right, left
		}
	}
}

func (r *fakeServerRepo) Upsert(_ context.Context, server *serverdomain.Server) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.servers[server.ID]
	if ok {
		// Membership is owned by the platform context and must survive a reconcile,
		// exactly as the Mongo implementation leaves it untouched.
		server.Membership = existing.Membership
		// GPUs are written by the inventory sweep on their own cadence, so a reconcile
		// Upsert must preserve them just as the Mongo implementation does by keeping them
		// out of its $set.
		server.Observed.GPUs = existing.Observed.GPUs
		if !existing.CreatedAt.IsZero() {
			server.CreatedAt = existing.CreatedAt
		}
	}
	r.servers[server.ID] = server
	return nil
}

func (r *fakeServerRepo) SetGPUs(_ context.Context, id string, gpus []serverdomain.GPU) error {
	s, ok := r.servers[id]
	if !ok {
		return serverdomain.ErrServerNotFound
	}
	s.Observed.GPUs = gpus
	return nil
}

func (r *fakeServerRepo) MarkAbsent(_ context.Context, integrationID string, seenBefore time.Time) ([]string, error) {
	var marked []string
	for _, s := range r.servers {
		if s.Source.IntegrationID != integrationID || s.Absent {
			continue
		}
		if s.LastSeenAt.Before(seenBefore) {
			s.Absent = true
			marked = append(marked, s.ID)
		}
	}
	return marked, nil
}

func (r *fakeServerRepo) SetMembership(_ context.Context, id string, membership *serverdomain.MembershipStatus) error {
	s, ok := r.servers[id]
	if !ok {
		return serverdomain.ErrServerNotFound
	}
	s.Membership = membership
	return nil
}

func (r *fakeServerRepo) SetDeployment(_ context.Context, id string, deployment *serverdomain.DeploymentStatus) error {
	s, ok := r.servers[id]
	if !ok {
		return serverdomain.ErrServerNotFound
	}
	s.Deployment = deployment
	return nil
}

func (r *fakeServerRepo) CountByIntegration(_ context.Context, integrationID string) (int, error) {
	count := 0
	for _, s := range r.servers {
		if s.Source.IntegrationID == integrationID {
			count++
		}
	}
	return count, nil
}

func (r *fakeServerRepo) Delete(_ context.Context, id string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	if _, ok := r.servers[id]; !ok {
		return serverdomain.ErrServerNotFound
	}
	delete(r.servers, id)
	return nil
}

// --- site and integration repositories ---

type fakeSiteRepo struct {
	sites map[string]*sitedomain.Site
}

func newFakeSiteRepo() *fakeSiteRepo {
	return &fakeSiteRepo{sites: make(map[string]*sitedomain.Site)}
}

func (r *fakeSiteRepo) Create(_ context.Context, site *sitedomain.Site) error {
	for _, existing := range r.sites {
		if existing.Name == site.Name {
			return sitedomain.ErrSiteNameTaken
		}
	}
	r.sites[site.ID] = site
	return nil
}

func (r *fakeSiteRepo) FindByID(_ context.Context, id string) (*sitedomain.Site, error) {
	site, ok := r.sites[id]
	if !ok {
		return nil, sitedomain.ErrSiteNotFound
	}
	return site, nil
}

func (r *fakeSiteRepo) List(_ context.Context) ([]*sitedomain.Site, error) {
	sites := make([]*sitedomain.Site, 0, len(r.sites))
	for _, site := range r.sites {
		sites = append(sites, site)
	}
	return sites, nil
}

func (r *fakeSiteRepo) Update(_ context.Context, site *sitedomain.Site) error {
	if _, ok := r.sites[site.ID]; !ok {
		return sitedomain.ErrSiteNotFound
	}
	for id, existing := range r.sites {
		if id != site.ID && existing.Name == site.Name {
			return sitedomain.ErrSiteNameTaken
		}
	}
	r.sites[site.ID] = site
	return nil
}

func (r *fakeSiteRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.sites[id]; !ok {
		return sitedomain.ErrSiteNotFound
	}
	delete(r.sites, id)
	return nil
}

// fakeIntegrationRepo keeps credentials in a separate map, mirroring the production
// property that a credential is never part of the integration struct.
type fakeIntegrationRepo struct {
	integrations map[string]*sitedomain.Integration
	credentials  map[string]string
}

func newFakeIntegrationRepo() *fakeIntegrationRepo {
	return &fakeIntegrationRepo{
		integrations: make(map[string]*sitedomain.Integration),
		credentials:  make(map[string]string),
	}
}

func (r *fakeIntegrationRepo) Create(_ context.Context, integration *sitedomain.Integration, credential string) error {
	r.integrations[integration.ID] = integration
	if credential != "" {
		r.credentials[integration.ID] = credential
	}
	return nil
}

func (r *fakeIntegrationRepo) FindByID(_ context.Context, id string) (*sitedomain.Integration, error) {
	integration, ok := r.integrations[id]
	if !ok {
		return nil, sitedomain.ErrIntegrationNotFound
	}
	return integration, nil
}

func (r *fakeIntegrationRepo) List(_ context.Context, filter sitedomain.IntegrationFilter) ([]*sitedomain.Integration, error) {
	var matches []*sitedomain.Integration
	for _, integration := range r.integrations {
		if filter.SiteID != "" && integration.SiteID != filter.SiteID {
			continue
		}
		if filter.Kind != "" && integration.Kind != filter.Kind {
			continue
		}
		if filter.EnabledOnly && !integration.Enabled {
			continue
		}
		matches = append(matches, integration)
	}
	return matches, nil
}

func (r *fakeIntegrationRepo) Update(_ context.Context, integration *sitedomain.Integration) error {
	if _, ok := r.integrations[integration.ID]; !ok {
		return sitedomain.ErrIntegrationNotFound
	}
	r.integrations[integration.ID] = integration
	return nil
}

func (r *fakeIntegrationRepo) ReplaceCredential(_ context.Context, id, credential string) error {
	if _, ok := r.integrations[id]; !ok {
		return sitedomain.ErrIntegrationNotFound
	}
	r.credentials[id] = credential
	return nil
}

func (r *fakeIntegrationRepo) Credential(_ context.Context, id string) (string, error) {
	if _, ok := r.integrations[id]; !ok {
		return "", sitedomain.ErrIntegrationNotFound
	}
	credential, ok := r.credentials[id]
	if !ok {
		return "", sitedomain.ErrCredentialNotSet
	}
	return credential, nil
}

func (r *fakeIntegrationRepo) UpdateSyncState(_ context.Context, id string, state sitedomain.SyncState) error {
	integration, ok := r.integrations[id]
	if !ok {
		return sitedomain.ErrIntegrationNotFound
	}
	integration.Sync = state
	return nil
}

func (r *fakeIntegrationRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.integrations[id]; !ok {
		return sitedomain.ErrIntegrationNotFound
	}
	delete(r.integrations, id)
	delete(r.credentials, id)
	return nil
}

// --- provisioning provider ---

type fakeProvider struct {
	mu                    sync.Mutex
	machines              map[string]*provisioningdomain.Machine
	machineErr            error
	images                []*provisioningdomain.OSImage
	listErr               error
	imageErr              error
	deployErr             error
	deployErrByMachine    map[string]error
	readinessErrByMachine map[string]error
	deleteErr             error
	deployDelay           time.Duration
	activeDeploys         int
	maxActiveDeploys      int
	// capabilities defaults to the MAAS adapter's, so a test only sets it when the
	// point of the test is a provisioner that cannot do something.
	capabilities provisioningdomain.ProviderCapabilities

	// gpus answers ListGPUs by machine ID, so the inventory sweep can be exercised
	// without a real device inventory.
	gpus   map[string][]provisioningdomain.GPU
	gpuErr error
	// detail is what GetMachineDetail returns; detailErr forces a failure.
	detail         *provisioningdomain.MachineDetail
	detailErr      error
	events         []provisioningdomain.MachineEvent
	eventsErr      error
	eventMachineID string
	eventLimit     int
	// actionErr forces every capability action to fail, for testing error propagation.
	actionErr error

	deployRequests        []provisioningdomain.DeployRequest
	networkConfigureCalls []string
	releaseRequests       []provisioningdomain.ReleaseRequest
	releaseCalls          []string
	deleteCalls           []string
	deleteImageErr        error
	deletedImages         [][2]string
	uploadImageErr        error
	uploadedImages        []provisioningdomain.UploadOSImageRequest
	// autoTags marks tag names reported by ListTags as non-editable (a MAAS automatic tag) and
	// refused by AddTag, so the auto-tag path can be exercised. ensuredTags records EnsureTag calls
	// and tagErr forces every tag call to fail.
	autoTags    map[string]bool
	ensuredTags []string
	tagErr      error
	// actions records every capability action taken, as "op machineID", so a test can
	// assert the provider was driven correctly.
	actions []string
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		machines: make(map[string]*provisioningdomain.Machine),
		images: []*provisioningdomain.OSImage{
			{ID: "ubuntu/jammy", Name: "Ubuntu 22.04 LTS", OSSystem: "ubuntu", Release: "jammy", Architecture: "amd64"},
		},
		gpus:                  make(map[string][]provisioningdomain.GPU),
		deployErrByMachine:    make(map[string]error),
		readinessErrByMachine: make(map[string]error),
		autoTags:              make(map[string]bool),
		// The full set, matching the MAAS adapter, so capability assertions succeed by
		// default; a test that needs an incapable provisioner uses minimalProvider.
		capabilities: provisioningdomain.ProviderCapabilities{
			EphemeralDeploy:      true,
			DeploymentReadiness:  true,
			Power:                true,
			HardwareValidation:   true,
			OperatorState:        true,
			MachineDetail:        true,
			HardwareInventory:    true,
			MachineRemoval:       true,
			ReleaseOptions:       true,
			NetworkConfiguration: true,
			ImageRemoval:         true,
			ImageUpload:          true,
			Tagging:              true,
		},
	}
}

func (p *fakeProvider) withMachine(machine *provisioningdomain.Machine) *fakeProvider {
	p.machines[machine.ID] = machine
	return p
}

func (p *fakeProvider) Name() string { return "maas" }

func (p *fakeProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return p.capabilities
}

// recordAction stands in for a state-changing MAAS operation: it notes the call and
// returns the machine as it would look afterwards.
func (p *fakeProvider) recordAction(op, machineID string) (*provisioningdomain.Machine, error) {
	if p.actionErr != nil {
		return nil, p.actionErr
	}
	machine, ok := p.machines[machineID]
	if !ok {
		return nil, provisioningdomain.ErrMachineNotFound
	}
	p.actions = append(p.actions, op+" "+machineID)
	updated := *machine
	return &updated, nil
}

func (p *fakeProvider) ListGPUs(_ context.Context, machineID string) ([]provisioningdomain.GPU, error) {
	if p.gpuErr != nil {
		return nil, p.gpuErr
	}
	return p.gpus[machineID], nil
}

func (p *fakeProvider) GetMachineDetail(_ context.Context, machineID string) (*provisioningdomain.MachineDetail, error) {
	if p.detailErr != nil {
		return nil, p.detailErr
	}
	if _, ok := p.machines[machineID]; !ok {
		return nil, provisioningdomain.ErrMachineNotFound
	}
	if p.detail != nil {
		return p.detail, nil
	}
	return &provisioningdomain.MachineDetail{}, nil
}

func (p *fakeProvider) ListMachineEvents(_ context.Context, machineID string, limit int) ([]provisioningdomain.MachineEvent, error) {
	p.eventMachineID = machineID
	p.eventLimit = limit
	if p.eventsErr != nil {
		return nil, p.eventsErr
	}
	if _, ok := p.machines[machineID]; !ok {
		return nil, provisioningdomain.ErrMachineNotFound
	}
	return p.events, nil
}

func (p *fakeProvider) PowerOn(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	m, err := p.recordAction("power_on", machineID)
	if err == nil {
		m.PowerState = provisioningdomain.PowerStateOn
	}
	return m, err
}

func (p *fakeProvider) PowerOff(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	m, err := p.recordAction("power_off", machineID)
	if err == nil {
		m.PowerState = provisioningdomain.PowerStateOff
	}
	return m, err
}

func (p *fakeProvider) QueryPowerState(_ context.Context, machineID string) (provisioningdomain.PowerState, error) {
	if p.actionErr != nil {
		return provisioningdomain.PowerStateUnknown, p.actionErr
	}
	if m, ok := p.machines[machineID]; ok {
		return m.PowerState, nil
	}
	return provisioningdomain.PowerStateUnknown, provisioningdomain.ErrMachineNotFound
}

func (p *fakeProvider) Commission(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.recordAction("commission", machineID)
}

func (p *fakeProvider) Test(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.recordAction("test", machineID)
}

func (p *fakeProvider) Abort(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.recordAction("abort", machineID)
}

func (p *fakeProvider) OverrideFailedTesting(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.recordAction("override_failed_testing", machineID)
}

func (p *fakeProvider) Lock(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	machine, err := p.recordAction("lock", machineID)
	if err != nil {
		return nil, err
	}
	machine.Locked = true
	p.machines[machineID].Locked = true
	return machine, nil
}

func (p *fakeProvider) Unlock(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	machine, err := p.recordAction("unlock", machineID)
	if err != nil {
		return nil, err
	}
	machine.Locked = false
	p.machines[machineID].Locked = false
	return machine, nil
}

func (p *fakeProvider) MarkBroken(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.recordAction("mark_broken", machineID)
}

func (p *fakeProvider) MarkFixed(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.recordAction("mark_fixed", machineID)
}

func (p *fakeProvider) EnterRescueMode(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.recordAction("rescue_mode", machineID)
}

func (p *fakeProvider) ExitRescueMode(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.recordAction("exit_rescue_mode", machineID)
}

func (p *fakeProvider) DeleteMachine(_ context.Context, machineID string) error {
	if p.deleteErr != nil {
		return p.deleteErr
	}
	if _, ok := p.machines[machineID]; !ok {
		return provisioningdomain.ErrMachineNotFound
	}
	p.deleteCalls = append(p.deleteCalls, machineID)
	delete(p.machines, machineID)
	return nil
}

// ListTags reports the union of every machine's tags plus any ensured tags, flagging a tag as not
// editable when it is registered as an automatic tag.
func (p *fakeProvider) ListTags(_ context.Context) ([]provisioningdomain.MachineTag, error) {
	if p.tagErr != nil {
		return nil, p.tagErr
	}
	seen := map[string]bool{}
	var tags []provisioningdomain.MachineTag
	add := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		tags = append(tags, provisioningdomain.MachineTag{Name: name, Editable: !p.autoTags[name]})
	}
	for _, machine := range p.machines {
		for _, tag := range machine.Tags {
			add(tag)
		}
	}
	for _, tag := range p.ensuredTags {
		add(tag)
	}
	for tag := range p.autoTags {
		add(tag)
	}
	return tags, nil
}

// EnsureTag records the ensured manual tag; a repeated ensure is harmless.
func (p *fakeProvider) EnsureTag(_ context.Context, name string) error {
	if p.tagErr != nil {
		return p.tagErr
	}
	p.ensuredTags = append(p.ensuredTags, name)
	return nil
}

// AddTag assigns the tag to each machine (idempotently), refusing an automatic tag the way MAAS
// refuses update_nodes on a tag with a definition.
func (p *fakeProvider) AddTag(_ context.Context, name string, machineIDs []string) error {
	if p.tagErr != nil {
		return p.tagErr
	}
	if p.autoTags[name] {
		return &provisioningdomain.ProviderError{
			Kind:   provisioningdomain.ProviderErrorRejected,
			Detail: "Cannot add nodes to a tag that has a definition.",
		}
	}
	p.actions = append(p.actions, "addTag "+name)
	for _, id := range machineIDs {
		machine, ok := p.machines[id]
		if !ok {
			return provisioningdomain.ErrMachineNotFound
		}
		if !containsTag(machine.Tags, name) {
			machine.Tags = append(machine.Tags, name)
		}
	}
	return nil
}

// RemoveTag unassigns the tag from each machine.
func (p *fakeProvider) RemoveTag(_ context.Context, name string, machineIDs []string) error {
	if p.tagErr != nil {
		return p.tagErr
	}
	p.actions = append(p.actions, "removeTag "+name)
	for _, id := range machineIDs {
		machine, ok := p.machines[id]
		if !ok {
			return provisioningdomain.ErrMachineNotFound
		}
		machine.Tags = withoutTag(machine.Tags, name)
	}
	return nil
}

func containsTag(tags []string, name string) bool {
	for _, tag := range tags {
		if tag == name {
			return true
		}
	}
	return false
}

func withoutTag(tags []string, name string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag != name {
			out = append(out, tag)
		}
	}
	return out
}

func (p *fakeProvider) DeleteOSImage(_ context.Context, imageID, architecture string) error {
	if p.deleteImageErr != nil {
		return p.deleteImageErr
	}
	p.deletedImages = append(p.deletedImages, [2]string{imageID, architecture})
	return nil
}

// UploadOSImage records the upload and returns the created image as MAAS would classify it: an
// uploaded resource surfaces with osystem "custom", so the delivery test can assert the provider
// — not the caller — decides the classification. The content is drained like a real adapter
// streams it once.
func (p *fakeProvider) UploadOSImage(_ context.Context, req provisioningdomain.UploadOSImageRequest) (*provisioningdomain.OSImage, error) {
	if req.Content != nil {
		_, _ = io.Copy(io.Discard, req.Content)
	}
	if p.uploadImageErr != nil {
		return nil, p.uploadImageErr
	}
	p.uploadedImages = append(p.uploadedImages, req)
	name := req.Title
	if name == "" {
		name = req.Name
	}
	return &provisioningdomain.OSImage{
		ID:           req.Name,
		Name:         name,
		OSSystem:     "custom",
		Release:      req.Name,
		Architecture: req.Architecture,
		SizeBytes:    req.Size,
	}, nil
}

// minimalProvider implements only the base OSProvisioningProvider, standing in for a
// provisioner that offers none of the optional capabilities. It is how the "refused, not
// silently dropped" path is tested.
type minimalProvider struct {
	machines map[string]*provisioningdomain.Machine
}

func (p *minimalProvider) Name() string { return "minimal" }
func (p *minimalProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{}
}
func (p *minimalProvider) Probe(_ context.Context) (provisioningdomain.ProviderInfo, error) {
	return provisioningdomain.ProviderInfo{Name: "minimal"}, nil
}
func (p *minimalProvider) ListMachines(_ context.Context, _ provisioningdomain.MachineFilter) ([]*provisioningdomain.Machine, error) {
	return nil, nil
}
func (p *minimalProvider) GetMachine(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	if m, ok := p.machines[machineID]; ok {
		return m, nil
	}
	return nil, provisioningdomain.ErrMachineNotFound
}
func (p *minimalProvider) ListOSImages(_ context.Context) ([]*provisioningdomain.OSImage, error) {
	return nil, nil
}
func (p *minimalProvider) Deploy(_ context.Context, _ provisioningdomain.DeployRequest) (*provisioningdomain.Machine, error) {
	return nil, nil
}
func (p *minimalProvider) Release(_ context.Context, _ string) (*provisioningdomain.Machine, error) {
	return nil, nil
}

func (p *fakeProvider) Probe(_ context.Context) (provisioningdomain.ProviderInfo, error) {
	return provisioningdomain.ProviderInfo{Name: "maas", DisplayName: "Ubuntu MAAS", Version: "3.6.1"}, nil
}

func (p *fakeProvider) ListMachines(_ context.Context, _ provisioningdomain.MachineFilter) ([]*provisioningdomain.Machine, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	machines := make([]*provisioningdomain.Machine, 0, len(p.machines))
	for _, machine := range p.machines {
		machines = append(machines, machine)
	}
	return machines, nil
}

func (p *fakeProvider) GetMachine(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	if p.machineErr != nil {
		return nil, p.machineErr
	}
	machine, ok := p.machines[machineID]
	if !ok {
		return nil, provisioningdomain.ErrMachineNotFound
	}
	return machine, nil
}

func (p *fakeProvider) ListNetworkSubnets(_ context.Context) ([]provisioningdomain.NetworkSubnet, error) {
	return []provisioningdomain.NetworkSubnet{{
		ID:             "subnet-1",
		Name:           "management",
		CIDR:           "192.0.2.0/24",
		GatewayAddress: "192.0.2.1",
		Managed:        true,
	}}, nil
}

func (p *fakeProvider) InspectNetwork(_ context.Context, machineID string) (*provisioningdomain.MachineNetwork, error) {
	if _, ok := p.machines[machineID]; !ok {
		return nil, provisioningdomain.ErrMachineNotFound
	}
	return &provisioningdomain.MachineNetwork{
		MachineID: machineID,
		Interfaces: []provisioningdomain.NetworkInterface{{
			ID:            "interface-1",
			Name:          "eth0",
			MACAddress:    "52:54:00:00:00:01",
			Boot:          true,
			PhysicalState: provisioningdomain.PhysicalLinkUp,
			State:         provisioningdomain.NetworkStateProviderManaged,
			ProviderMode:  "AUTO",
			Links: []provisioningdomain.NetworkLink{{
				ID:           "link-1",
				State:        provisioningdomain.NetworkStateProviderManaged,
				ProviderMode: "AUTO",
				SubnetID:     "subnet-1",
				SubnetName:   "management",
				CIDR:         "192.0.2.0/24",
			}},
			AvailableSubnets: []provisioningdomain.NetworkSubnet{{
				ID:             "subnet-1",
				Name:           "management",
				CIDR:           "192.0.2.0/24",
				GatewayAddress: "192.0.2.1",
				Managed:        true,
			}},
		}},
	}, nil
}

func (p *fakeProvider) ConfigureNetworkLink(
	_ context.Context,
	machineID string,
	req provisioningdomain.NetworkLinkRequest,
) (*provisioningdomain.MachineNetwork, error) {
	p.mu.Lock()
	p.networkConfigureCalls = append(p.networkConfigureCalls, machineID)
	p.mu.Unlock()
	network, err := p.InspectNetwork(context.Background(), machineID)
	if err != nil {
		return nil, err
	}
	state := provisioningdomain.NetworkStateDHCP
	providerMode := "DHCP"
	if req.Mode == provisioningdomain.NetworkLinkStatic {
		state = provisioningdomain.NetworkStateStatic
		providerMode = "STATIC"
	} else if req.Mode == provisioningdomain.NetworkLinkLinkOnly {
		state = provisioningdomain.NetworkStateLinkOnly
		providerMode = "LINK_UP"
	}
	network.Interfaces[0].State = state
	network.Interfaces[0].ProviderMode = providerMode
	network.Interfaces[0].Links = []provisioningdomain.NetworkLink{{
		ID:             "link-1",
		State:          state,
		ProviderMode:   providerMode,
		SubnetID:       req.SubnetID,
		SubnetName:     "management",
		CIDR:           "192.0.2.0/24",
		IPAddress:      strings.TrimSpace(req.IPAddress),
		DefaultGateway: req.DefaultGateway,
	}}
	return network, nil
}

func (p *fakeProvider) UnlinkNetwork(
	_ context.Context,
	machineID, _, _ string,
) (*provisioningdomain.MachineNetwork, error) {
	network, err := p.InspectNetwork(context.Background(), machineID)
	if err != nil {
		return nil, err
	}
	network.Interfaces[0].State = provisioningdomain.NetworkStateUnconfigured
	network.Interfaces[0].ProviderMode = ""
	network.Interfaces[0].Links = nil
	return network, nil
}

func (p *fakeProvider) ValidateDeploymentTarget(_ context.Context, machineID string) error {
	if err := p.readinessErrByMachine[machineID]; err != nil {
		return err
	}
	return nil
}

func (p *fakeProvider) ListOSImages(_ context.Context) ([]*provisioningdomain.OSImage, error) {
	if p.imageErr != nil {
		return nil, p.imageErr
	}
	return p.images, nil
}

func (p *fakeProvider) Deploy(_ context.Context, req provisioningdomain.DeployRequest) (*provisioningdomain.Machine, error) {
	p.mu.Lock()
	p.activeDeploys++
	if p.activeDeploys > p.maxActiveDeploys {
		p.maxActiveDeploys = p.activeDeploys
	}
	delay := p.deployDelay
	deployErr := p.deployErr
	machineErr := p.deployErrByMachine[req.MachineID]
	machine, ok := p.machines[req.MachineID]
	if ok && deployErr == nil && machineErr == nil {
		p.deployRequests = append(p.deployRequests, req)
	}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.activeDeploys--
		p.mu.Unlock()
	}()
	if delay > 0 {
		time.Sleep(delay)
	}
	if deployErr != nil {
		return nil, deployErr
	}
	if machineErr != nil {
		return nil, machineErr
	}
	if !ok {
		return nil, provisioningdomain.ErrMachineNotFound
	}

	deploying := *machine
	deploying.Status = provisioningdomain.MachineStatusDeploying
	deploying.ProviderStatus = "Deploying"
	deploying.OSSystem = req.OSSystem
	deploying.DistroSeries = req.DistroSeries
	// Mirrored the way a provisioner does: what was asked for becomes an observable
	// property of the machine, which is what the axis then projects.
	deploying.Ephemeral = req.Ephemeral
	return &deploying, nil
}

func (p *fakeProvider) Release(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	machine, ok := p.machines[machineID]
	if !ok {
		return nil, provisioningdomain.ErrMachineNotFound
	}
	p.releaseCalls = append(p.releaseCalls, machineID)

	released := *machine
	released.Status = provisioningdomain.MachineStatusReady
	released.ProviderStatus = "Ready"
	return &released, nil
}

func (p *fakeProvider) ReleaseWithOptions(ctx context.Context, req provisioningdomain.ReleaseRequest) (*provisioningdomain.Machine, error) {
	p.releaseRequests = append(p.releaseRequests, req)
	return p.Release(ctx, req.MachineID)
}

// --- platforms ---

type fakePlatformRepo struct {
	platforms map[string]*platformdomain.Platform
}

func newFakePlatformRepo() *fakePlatformRepo {
	return &fakePlatformRepo{platforms: make(map[string]*platformdomain.Platform)}
}

func (r *fakePlatformRepo) Create(_ context.Context, platform *platformdomain.Platform) error {
	for _, existing := range r.platforms {
		if existing.SiteID == platform.SiteID && existing.Name == platform.Name {
			return platformdomain.ErrPlatformNameTaken
		}
	}
	r.platforms[platform.ID] = platform
	return nil
}

func (r *fakePlatformRepo) FindByID(_ context.Context, id string) (*platformdomain.Platform, error) {
	platform, ok := r.platforms[id]
	if !ok {
		return nil, platformdomain.ErrPlatformNotFound
	}
	return platform, nil
}

func (r *fakePlatformRepo) List(_ context.Context, siteID string) ([]*platformdomain.Platform, error) {
	var matches []*platformdomain.Platform
	for _, platform := range r.platforms {
		if siteID != "" && platform.SiteID != siteID {
			continue
		}
		matches = append(matches, platform)
	}
	return matches, nil
}

func (r *fakePlatformRepo) Update(_ context.Context, platform *platformdomain.Platform) error {
	if _, ok := r.platforms[platform.ID]; !ok {
		return platformdomain.ErrPlatformNotFound
	}
	r.platforms[platform.ID] = platform
	return nil
}

func (r *fakePlatformRepo) UpdateSyncState(
	_ context.Context, id, integrationID string, state platformdomain.SyncState,
) error {
	platform, ok := r.platforms[id]
	if !ok {
		return platformdomain.ErrPlatformNotFound
	}
	if platform.IntegrationID != integrationID {
		return platformdomain.ErrPlatformNotFound
	}
	platform.Sync = state
	return nil
}

func (r *fakePlatformRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.platforms[id]; !ok {
		return platformdomain.ErrPlatformNotFound
	}
	delete(r.platforms, id)
	return nil
}

// fakeDeploymentLauncher records the last launch and returns a fixed operation id, so
// platform deployment HTTP tests can assert acceptance without an operation backend.
type fakeDeploymentLauncher struct {
	lastLaunch  *platformdomain.DeploymentLaunch
	operationID string
	err         error
}

func (l *fakeDeploymentLauncher) Launch(_ context.Context, launch platformdomain.DeploymentLaunch) (string, error) {
	if l.err != nil {
		return "", l.err
	}
	launchCopy := launch
	l.lastLaunch = &launchCopy
	if l.operationID == "" {
		return "operation-deploy-1", nil
	}
	return l.operationID, nil
}

type fakePlatformReader struct {
	members []platformdomain.Member
	err     error
}

func (r *fakePlatformReader) ListMembers(_ context.Context) ([]platformdomain.Member, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.members, nil
}

type fakeReaderFactory struct {
	readers map[string]*fakePlatformReader
	err     error
}

func newFakeReaderFactory() *fakeReaderFactory {
	return &fakeReaderFactory{readers: make(map[string]*fakePlatformReader)}
}

func (f *fakeReaderFactory) For(_ context.Context, platform *platformdomain.Platform) (platformdomain.PlatformReader, error) {
	if f.err != nil {
		return nil, f.err
	}
	reader, ok := f.readers[platform.ID]
	if !ok {
		return nil, platformdomain.ErrNoPlatformIntegration
	}
	return reader, nil
}

// --- monitoring ---

type fakeQuerier struct {
	// samples answers by expression substring, so a test can describe what a query
	// returns without reproducing the exact PromQL.
	samples map[string][]monitoringdomain.Sample
	err     error
	queries []string
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{samples: make(map[string][]monitoringdomain.Sample)}
}

func (q *fakeQuerier) Name() string { return "prometheus" }

func (q *fakeQuerier) Query(_ context.Context, expr string) ([]monitoringdomain.Sample, error) {
	q.queries = append(q.queries, expr)
	if q.err != nil {
		return nil, q.err
	}
	for match, samples := range q.samples {
		if strings.Contains(expr, match) {
			return samples, nil
		}
	}
	return nil, nil
}

type fakeAlertSource struct {
	alerts   []*monitoringdomain.Alert
	err      error
	silences []monitoringdomain.SilenceRequest
}

func (s *fakeAlertSource) ListAlerts(_ context.Context) ([]*monitoringdomain.Alert, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.alerts, nil
}

func (s *fakeAlertSource) Silence(_ context.Context, req monitoringdomain.SilenceRequest) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	s.silences = append(s.silences, req)
	return "silence-1", nil
}

type fakeMonitoringFactory struct {
	querier    *fakeQuerier
	alerts     *fakeAlertSource
	querierErr error
	alertsErr  error
	grafanaURL string
}

func newFakeMonitoringFactory() *fakeMonitoringFactory {
	return &fakeMonitoringFactory{
		querier: newFakeQuerier(),
		alerts:  &fakeAlertSource{},
	}
}

func (f *fakeMonitoringFactory) Querier(_ context.Context, _ string) (monitoringdomain.MetricsQuerier, error) {
	if f.querierErr != nil {
		return nil, f.querierErr
	}
	return f.querier, nil
}

func (f *fakeMonitoringFactory) Alerts(_ context.Context, _ string) (monitoringdomain.AlertSource, error) {
	if f.alertsErr != nil {
		return nil, f.alertsErr
	}
	return f.alerts, nil
}

func (f *fakeMonitoringFactory) GrafanaURL(_ context.Context, _ string) string {
	return f.grafanaURL
}

// fakeProviderFactory resolves integration IDs to providers, standing in for the real
// factory's credential lookup and adapter selection.
type fakeProviderFactory struct {
	providers map[string]provisioningdomain.OSProvisioningProvider
	err       error
}

func newFakeProviderFactory() *fakeProviderFactory {
	return &fakeProviderFactory{providers: make(map[string]provisioningdomain.OSProvisioningProvider)}
}

func (f *fakeProviderFactory) For(_ context.Context, integrationID string) (provisioningdomain.OSProvisioningProvider, error) {
	if f.err != nil {
		return nil, f.err
	}
	provider, ok := f.providers[integrationID]
	if !ok {
		return nil, sitedomain.ErrIntegrationNotFound
	}
	return provider, nil
}

// fakeLifecycleReader supplies the operation-derived platform projection to HTTP tests.
type fakeLifecycleReader struct {
	snapshots map[string]platformdomain.LifecycleSnapshot
}

func newFakeLifecycleReader() *fakeLifecycleReader {
	return &fakeLifecycleReader{snapshots: make(map[string]platformdomain.LifecycleSnapshot)}
}

func (r *fakeLifecycleReader) Read(
	_ context.Context,
	platformIDs []string,
) (map[string]platformdomain.LifecycleSnapshot, error) {
	result := make(map[string]platformdomain.LifecycleSnapshot, len(platformIDs))
	for _, id := range platformIDs {
		if snapshot, ok := r.snapshots[id]; ok {
			result[id] = snapshot
		} else {
			result[id] = platformdomain.LifecycleSnapshot{
				Origin: platformdomain.PlatformOriginRegistered,
				State:  platformdomain.PlatformLifecycleRegistered,
			}
		}
	}
	return result, nil
}

type fakeUninstallLauncher struct {
	lastLaunch  *platformdomain.UninstallLaunch
	operationID string
	err         error
}

func (l *fakeUninstallLauncher) LaunchUninstall(
	_ context.Context,
	launch platformdomain.UninstallLaunch,
) (string, error) {
	if l.err != nil {
		return "", l.err
	}
	copy := launch
	l.lastLaunch = &copy
	if l.operationID == "" {
		return "operation-uninstall-1", nil
	}
	return l.operationID, nil
}

type fakeActiveServerWorkReader struct {
	work map[string]provisioningapp.ActiveServerWork
	err  error
}

func (r *fakeActiveServerWorkReader) ActiveWork(
	_ context.Context,
	serverID string,
) (provisioningapp.ActiveServerWork, error) {
	if r.err != nil {
		return provisioningapp.ActiveServerWork{}, r.err
	}
	return r.work[serverID], nil
}
