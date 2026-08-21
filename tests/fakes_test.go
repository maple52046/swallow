package tests

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	clusterdomain "github.com/AFDEAPAC/swallow/internal/cluster/domain"
	monitoringdomain "github.com/AFDEAPAC/swallow/internal/monitoring/domain"
	operationdomain "github.com/AFDEAPAC/swallow/internal/operation/domain"
	"github.com/AFDEAPAC/swallow/internal/operation/infra/awx"
	provisioningdomain "github.com/AFDEAPAC/swallow/internal/provisioning/domain"
	serverdomain "github.com/AFDEAPAC/swallow/internal/server/domain"
	sitedomain "github.com/AFDEAPAC/swallow/internal/site/domain"
)

// --- server repository ---

type fakeServerRepo struct {
	servers map[string]*serverdomain.Server
}

func newFakeServerRepo() *fakeServerRepo {
	return &fakeServerRepo{servers: make(map[string]*serverdomain.Server)}
}

func (r *fakeServerRepo) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
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
		if filter.ClusterID != "" {
			if s.Membership == nil || s.Membership.ClusterID != filter.ClusterID {
				continue
			}
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
	existing, ok := r.servers[server.ID]
	if ok {
		// Membership is owned by the cluster context and must survive a reconcile,
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

func (r *fakeServerRepo) MarkAbsent(_ context.Context, integrationID string, seenBefore time.Time) (int, error) {
	marked := 0
	for _, s := range r.servers {
		if s.Source.IntegrationID != integrationID || s.Absent {
			continue
		}
		if s.LastSeenAt.Before(seenBefore) {
			s.Absent = true
			marked++
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
	machines  map[string]*provisioningdomain.Machine
	images    []*provisioningdomain.OSImage
	listErr   error
	deployErr error
	// capabilities defaults to the MAAS adapter's, so a test only sets it when the
	// point of the test is a provisioner that cannot do something.
	capabilities provisioningdomain.ProviderCapabilities

	// gpus answers ListGPUs by machine ID, so the inventory sweep can be exercised
	// without a real device inventory.
	gpus   map[string][]provisioningdomain.GPU
	gpuErr error
	// detail is what GetMachineDetail returns; detailErr forces a failure.
	detail    *provisioningdomain.MachineDetail
	detailErr error
	// actionErr forces every capability action to fail, for testing error propagation.
	actionErr error

	deployRequests []provisioningdomain.DeployRequest
	releaseCalls   []string
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
		gpus: make(map[string][]provisioningdomain.GPU),
		// The full set, matching the MAAS adapter, so capability assertions succeed by
		// default; a test that needs an incapable provisioner uses minimalProvider.
		capabilities: provisioningdomain.ProviderCapabilities{
			EphemeralDeploy:    true,
			Power:              true,
			HardwareValidation: true,
			OperatorState:      true,
			MachineDetail:      true,
			HardwareInventory:  true,
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
	return p.recordAction("lock", machineID)
}

func (p *fakeProvider) Unlock(_ context.Context, machineID string) (*provisioningdomain.Machine, error) {
	return p.recordAction("unlock", machineID)
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
	machine, ok := p.machines[machineID]
	if !ok {
		return nil, provisioningdomain.ErrMachineNotFound
	}
	return machine, nil
}

func (p *fakeProvider) ListOSImages(_ context.Context) ([]*provisioningdomain.OSImage, error) {
	return p.images, nil
}

func (p *fakeProvider) Deploy(_ context.Context, req provisioningdomain.DeployRequest) (*provisioningdomain.Machine, error) {
	if p.deployErr != nil {
		return nil, p.deployErr
	}
	machine, ok := p.machines[req.MachineID]
	if !ok {
		return nil, provisioningdomain.ErrMachineNotFound
	}
	p.deployRequests = append(p.deployRequests, req)

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

// --- clusters ---

type fakeClusterRepo struct {
	clusters map[string]*clusterdomain.Cluster
}

func newFakeClusterRepo() *fakeClusterRepo {
	return &fakeClusterRepo{clusters: make(map[string]*clusterdomain.Cluster)}
}

func (r *fakeClusterRepo) Create(_ context.Context, cluster *clusterdomain.Cluster) error {
	for _, existing := range r.clusters {
		if existing.SiteID == cluster.SiteID && existing.Name == cluster.Name {
			return clusterdomain.ErrClusterNameTaken
		}
	}
	r.clusters[cluster.ID] = cluster
	return nil
}

func (r *fakeClusterRepo) FindByID(_ context.Context, id string) (*clusterdomain.Cluster, error) {
	cluster, ok := r.clusters[id]
	if !ok {
		return nil, clusterdomain.ErrClusterNotFound
	}
	return cluster, nil
}

func (r *fakeClusterRepo) List(_ context.Context, siteID string) ([]*clusterdomain.Cluster, error) {
	var matches []*clusterdomain.Cluster
	for _, cluster := range r.clusters {
		if siteID != "" && cluster.SiteID != siteID {
			continue
		}
		matches = append(matches, cluster)
	}
	return matches, nil
}

func (r *fakeClusterRepo) Update(_ context.Context, cluster *clusterdomain.Cluster) error {
	if _, ok := r.clusters[cluster.ID]; !ok {
		return clusterdomain.ErrClusterNotFound
	}
	r.clusters[cluster.ID] = cluster
	return nil
}

func (r *fakeClusterRepo) UpdateSyncState(_ context.Context, id string, state clusterdomain.SyncState) error {
	cluster, ok := r.clusters[id]
	if !ok {
		return clusterdomain.ErrClusterNotFound
	}
	cluster.Sync = state
	return nil
}

func (r *fakeClusterRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.clusters[id]; !ok {
		return clusterdomain.ErrClusterNotFound
	}
	delete(r.clusters, id)
	return nil
}

type fakeClusterReader struct {
	members []clusterdomain.Member
	err     error
}

func (r *fakeClusterReader) ListMembers(_ context.Context) ([]clusterdomain.Member, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.members, nil
}

type fakeReaderFactory struct {
	readers map[string]*fakeClusterReader
	err     error
}

func newFakeReaderFactory() *fakeReaderFactory {
	return &fakeReaderFactory{readers: make(map[string]*fakeClusterReader)}
}

func (f *fakeReaderFactory) For(_ context.Context, cluster *clusterdomain.Cluster) (clusterdomain.ClusterReader, error) {
	if f.err != nil {
		return nil, f.err
	}
	reader, ok := f.readers[cluster.ID]
	if !ok {
		return nil, clusterdomain.ErrNoClusterIntegration
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

// --- operations ---

type fakeOperationRepo struct {
	operations map[string]*operationdomain.Operation
	createErr  error
}

func newFakeOperationRepo() *fakeOperationRepo {
	return &fakeOperationRepo{operations: make(map[string]*operationdomain.Operation)}
}

func (r *fakeOperationRepo) Create(_ context.Context, operation *operationdomain.Operation) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.operations[operation.ID] = operation
	return nil
}

func (r *fakeOperationRepo) FindByID(_ context.Context, id string) (*operationdomain.Operation, error) {
	operation, ok := r.operations[id]
	if !ok {
		return nil, operationdomain.ErrOperationNotFound
	}
	return operation, nil
}

func (r *fakeOperationRepo) FindByJobID(_ context.Context, integrationID, jobID string) (*operationdomain.Operation, error) {
	for _, operation := range r.operations {
		if operation.Automation.IntegrationID == integrationID && operation.Automation.JobID == jobID {
			return operation, nil
		}
	}
	return nil, operationdomain.ErrOperationNotFound
}

func (r *fakeOperationRepo) List(_ context.Context, filter operationdomain.ListFilter) (operationdomain.ListResult, error) {
	var matches []*operationdomain.Operation
	for _, operation := range r.operations {
		if filter.SiteID != "" && operation.SiteID != filter.SiteID {
			continue
		}
		if filter.ServerID != "" && !containsString(operation.TargetServerIDs, filter.ServerID) {
			continue
		}
		if filter.Kind != "" && operation.Kind != filter.Kind {
			continue
		}
		if filter.Status != "" && operation.Automation.Status != filter.Status {
			continue
		}
		if filter.ActiveOnly && operation.Automation.Status.Terminal() {
			continue
		}
		matches = append(matches, operation)
	}
	return operationdomain.ListResult{Operations: matches, Total: len(matches)}, nil
}

func (r *fakeOperationRepo) UpdateAutomation(_ context.Context, id string, ref operationdomain.AutomationRef) error {
	operation, ok := r.operations[id]
	if !ok {
		return operationdomain.ErrOperationNotFound
	}
	operation.Automation = ref
	return nil
}

func (r *fakeOperationRepo) FindActiveByServerIDs(_ context.Context, serverIDs []string) ([]*operationdomain.Operation, error) {
	var matches []*operationdomain.Operation
	for _, operation := range r.operations {
		if operation.Automation.Status.Terminal() {
			continue
		}
		for _, id := range serverIDs {
			if containsString(operation.TargetServerIDs, id) {
				matches = append(matches, operation)
				break
			}
		}
	}
	return matches, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// fakeController stands in for AWX.
type fakeController struct {
	templates map[string]string
	jobStates map[string]operationdomain.JobState
	logs      map[string]string

	launchErr error
	stateErr  error

	launches  []operationdomain.LaunchRequest
	nextJobID int
}

func newFakeController() *fakeController {
	return &fakeController{
		templates: map[string]string{
			"install-gpu-driver": "10",
			"deploy-kubernetes":  "11",
			"configure-slurm":    "12",
		},
		jobStates: make(map[string]operationdomain.JobState),
		logs:      make(map[string]string),
	}
}

func (c *fakeController) Name() string { return "awx" }

func (c *fakeController) FindJobTemplate(_ context.Context, name string) (string, error) {
	id, ok := c.templates[name]
	if !ok {
		return "", fmt.Errorf("%w: %q", operationdomain.ErrJobTemplateNotFound, name)
	}
	return id, nil
}

func (c *fakeController) Launch(_ context.Context, req operationdomain.LaunchRequest) (string, operationdomain.JobState, error) {
	if c.launchErr != nil {
		return "", operationdomain.JobState{}, c.launchErr
	}
	c.launches = append(c.launches, req)

	c.nextJobID++
	jobID := strconv.Itoa(100 + c.nextJobID)
	started := time.Now().UTC()
	state := operationdomain.JobState{Status: operationdomain.StatusRunning, StartedAt: &started}
	c.jobStates[jobID] = state
	return jobID, state, nil
}

func (c *fakeController) JobState(_ context.Context, jobID string) (operationdomain.JobState, error) {
	if c.stateErr != nil {
		return operationdomain.JobState{}, c.stateErr
	}
	state, ok := c.jobStates[jobID]
	if !ok {
		return operationdomain.JobState{}, awx.ErrJobNotFound
	}
	return state, nil
}

func (c *fakeController) JobLogs(_ context.Context, jobID string) (string, error) {
	return c.logs[jobID], nil
}

type fakeControllerFactory struct {
	controllers map[string]operationdomain.AutomationController
}

func newFakeControllerFactory() *fakeControllerFactory {
	return &fakeControllerFactory{controllers: make(map[string]operationdomain.AutomationController)}
}

func (f *fakeControllerFactory) For(_ context.Context, integrationID string) (operationdomain.AutomationController, error) {
	controller, ok := f.controllers[integrationID]
	if !ok {
		return nil, sitedomain.ErrIntegrationNotFound
	}
	return controller, nil
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
