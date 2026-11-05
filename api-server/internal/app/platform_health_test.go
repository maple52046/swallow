package app

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/adapters/executor/temporalworkflow"
	platformapp "github.com/maple52046/swallow/internal/core/platform/application"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// recordedWaits is a WaitingRecorder that keeps every observation a Runner publishes and answers
// each with err.
type recordedWaits struct {
	mu   sync.Mutex
	seen []workflowdomain.TaskWaiting
	err  error
}

func (r *recordedWaits) UpdateWaiting(_ context.Context, _ workflowapp.TaskRef, waiting workflowdomain.TaskWaiting) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, waiting)
	return r.err
}

// publisher binds the recorder to an attempt, as the activity that runs it does.
func (r *recordedWaits) publisher() workflowapp.WaitingPublisher {
	return workflowapp.NewWaitingPublisher(r, workflowapp.TaskRef{WorkflowID: "op-1", Generation: 1, TaskID: "task", Attempt: 1})
}

// observations returns the published observation texts in order.
func (r *recordedWaits) observations() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	texts := make([]string, len(r.seen))
	for i, waiting := range r.seen {
		texts[i] = waiting.Observation
	}
	return texts
}

// healthServers is the Server inventory a Platform health check reads: the membership sync lists
// and marks it, and the check reads each target back.
type healthServers struct {
	serverdomain.ServerRepository
	mu      sync.Mutex
	servers map[string]*serverdomain.Server
}

func newHealthServers(servers ...*serverdomain.Server) *healthServers {
	repo := &healthServers{servers: map[string]*serverdomain.Server{}}
	for _, server := range servers {
		repo.servers[server.ID] = server
	}
	return repo
}

func (r *healthServers) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	server, ok := r.servers[id]
	if !ok {
		return nil, serverdomain.ErrServerNotFound
	}
	copied := *server
	return &copied, nil
}

func (r *healthServers) List(_ context.Context, filter serverdomain.ListFilter) (serverdomain.ListResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.servers))
	for id := range r.servers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var servers []*serverdomain.Server
	for _, id := range ids {
		server := r.servers[id]
		if filter.PlatformID != "" && (server.Membership == nil || server.Membership.PlatformID != filter.PlatformID) {
			continue
		}
		copied := *server
		servers = append(servers, &copied)
	}
	return serverdomain.ListResult{Servers: servers, Total: len(servers)}, nil
}

func (r *healthServers) SetMembership(_ context.Context, id string, membership *serverdomain.MembershipStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.servers[id].Membership = membership
	return nil
}

// healthPlatforms holds the Platform under validation, or none.
type healthPlatforms struct {
	platformdomain.PlatformRepository
	platform *platformdomain.Platform
}

func (r healthPlatforms) FindByID(_ context.Context, id string) (*platformdomain.Platform, error) {
	if r.platform == nil || r.platform.ID != id {
		return nil, platformdomain.ErrPlatformNotFound
	}
	copied := *r.platform
	return &copied, nil
}

func (healthPlatforms) UpdateSyncState(context.Context, string, string, platformdomain.SyncState) error {
	return nil
}

// healthReading is one scripted membership read: building the reader fails with forErr, or the
// read returns members or err.
type healthReading struct {
	members []platformdomain.Member
	err     error
	forErr  error
}

// healthReaders answers each membership read with the next scripted reading; the last repeats.
type healthReaders struct {
	mu       sync.Mutex
	readings []healthReading
	reads    int
}

func (f *healthReaders) For(context.Context, *platformdomain.Platform) (platformdomain.PlatformReader, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reading := f.readings[min(f.reads, len(f.readings)-1)]
	f.reads++
	if reading.forErr != nil {
		return nil, reading.forErr
	}
	return healthReader(reading), nil
}

func (f *healthReaders) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

type healthReader healthReading

func (r healthReader) ListMembers(context.Context) ([]platformdomain.Member, error) {
	return r.members, r.err
}

// The validated Platform has a control plane c1 and a worker w1; its API endpoint was recorded as
// c1's address 10.170.169.126.
const recordedAPIEndpoint = "https://10.170.169.126:6443"

var bothMembers = []platformdomain.Member{{Name: "lab-c1"}, {Name: "lab-w1"}}

func healthTargets() *healthServers {
	return newHealthServers(
		&serverdomain.Server{ID: "c1", Observed: serverdomain.Observed{Hostname: "lab-c1", Addresses: []string{"10.170.169.126"}}},
		&serverdomain.Server{ID: "w1", Observed: serverdomain.Observed{Hostname: "lab-w1", Addresses: []string{"10.170.169.127"}}},
	)
}

func healthExecutor(readers *healthReaders, servers *healthServers) platformWorkflowStepExecutor {
	platforms := healthPlatforms{platform: &platformdomain.Platform{ID: "plat-1", SiteID: "site-1", Name: "lab", IntegrationID: "int-1"}}
	return platformWorkflowStepExecutor{
		servers: servers, membership: platformapp.NewMembershipSyncUseCase(platforms, servers, readers),
		poll: time.Millisecond, platformHealthWait: 20 * time.Millisecond,
	}
}

// healthTask is the validate-platform-health Task over c1 and w1, whose API Server is c1.
func healthTask(waits *recordedWaits) temporalworkflow.StepExecutionInput {
	return temporalworkflow.StepExecutionInput{
		OperationID: "op-1", PlatformID: "plat-1", Waiting: waits.publisher(),
		Step: workflowdomain.Task{
			ID: "validate-platform", Kind: "validate-platform-health", Attempt: 1, Targets: serverRefs("c1", "w1"),
			Parameters: map[string]any{platformAPIServerParameter: "c1"},
		},
	}
}

func refused() error {
	return &platformdomain.ReaderError{
		Kind: platformdomain.ReaderErrorUnavailable, Detail: "Could not reach the Kubernetes API.",
		Endpoint: recordedAPIEndpoint + "/api/v1/nodes", Operation: "list nodes", Transport: platformdomain.TransportRefused,
		Err: errors.New("dial tcp 10.170.169.126:6443: connect: connection refused"),
	}
}

// The health check succeeds once every target is a member, and publishes what it waited for
// meanwhile.
func TestValidatePlatformWaitsForMembership(t *testing.T) {
	waits := &recordedWaits{}
	readers := &healthReaders{readings: []healthReading{
		{members: bothMembers[:1]}, {err: refused()}, {members: bothMembers},
	}}
	executor := healthExecutor(readers, healthTargets())
	executor.platformHealthWait = time.Minute

	result := executor.Execute(context.Background(), healthTask(waits))

	if result.Status != workflowdomain.TaskSucceeded {
		t.Fatalf("result = %+v (error %+v), want succeeded", result, result.Error)
	}
	want := []string{
		"Platform membership is missing 1 of 2 target Servers: lab-w1.",
		"The Platform API at https://10.170.169.126:6443 refused the connection.",
	}
	if got := waits.observations(); !slices.Equal(got, want) {
		t.Errorf("published observations = %q, want %q", got, want)
	}
}

// A transient condition is waited on within the budget, for at least two observations, and the
// failure then carries the wait's evidence: the last observation, the endpoint and operation, the
// counts, and the Platform as target.
func TestValidatePlatformFailsATransientConditionAfterItsBudget(t *testing.T) {
	tests := []struct {
		name            string
		reading         healthReading
		wantCode        string
		wantObservation string
		wantEndpoint    string
	}{
		{
			name: "the API refuses the connection", reading: healthReading{err: refused()},
			wantCode:        "platform_api_unreachable",
			wantObservation: "The Platform API at https://10.170.169.126:6443 refused the connection.",
			wantEndpoint:    recordedAPIEndpoint + "/api/v1/nodes",
		},
		{
			name: "the API does not answer", reading: healthReading{err: &platformdomain.ReaderError{
				Kind: platformdomain.ReaderErrorUnavailable, Endpoint: recordedAPIEndpoint, Transport: platformdomain.TransportTimeout,
			}},
			wantCode:        "platform_api_unreachable",
			wantObservation: "The Platform API at https://10.170.169.126:6443 did not answer in time.",
			wantEndpoint:    recordedAPIEndpoint,
		},
		{
			name: "a target never joins", reading: healthReading{members: bothMembers[:1]},
			wantCode:        "platform_membership_incomplete",
			wantObservation: "Platform membership is missing 1 of 2 target Servers: lab-w1.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			readers := &healthReaders{readings: []healthReading{tc.reading}}
			executor := healthExecutor(readers, healthTargets())
			executor.platformHealthWait = time.Nanosecond

			result := executor.Execute(context.Background(), healthTask(&recordedWaits{}))

			if result.Status != workflowdomain.TaskFailed || result.Error == nil || result.Error.Code != tc.wantCode || !result.Error.Retryable {
				t.Fatalf("result = %+v (error %+v), want a retryable %s", result, result.Error, tc.wantCode)
			}
			if !strings.HasPrefix(result.Error.Message, tc.wantObservation) || strings.Contains(result.Error.Message, "stale") {
				t.Errorf("message = %q, want the last observation and no stale endpoint", result.Error.Message)
			}
			d := result.Error.Diagnostic
			if d == nil {
				t.Fatal("diagnostic = nil, want the wait's evidence")
			}
			if d.FailureClass != workflowdomain.FailureTransient || d.Attempts != platformHealthMinObservations || d.Observation != tc.wantObservation {
				t.Errorf("diagnostic = class %s, %d attempts, observation %q; want transient after %d with the last observation",
					d.FailureClass, d.Attempts, d.Observation, platformHealthMinObservations)
			}
			if d.Endpoint != tc.wantEndpoint || d.Target == nil || *d.Target != (workflowdomain.ResourceReference{Kind: "platform", ID: "plat-1"}) {
				t.Errorf("diagnostic endpoint %q target %+v, want %q and the Platform", d.Endpoint, d.Target, tc.wantEndpoint)
			}
			if !strings.Contains(d.Cause, workflowdomain.ErrWaitBudgetExhausted.Error()) || d.Remediation == "" {
				t.Errorf("diagnostic cause %q remediation %q, want the exhausted budget and a next step", d.Cause, d.Remediation)
			}
		})
	}
}

// A permanent condition fails at once, on the first observation, instead of spending the budget.
func TestValidatePlatformFailsAPermanentConditionAtOnce(t *testing.T) {
	tests := []struct {
		name         string
		reading      healthReading
		wantCode     string
		wantEndpoint string
	}{
		{
			name: "the API rejects the credential", wantCode: "platform_api_auth", wantEndpoint: recordedAPIEndpoint,
			reading: healthReading{err: &platformdomain.ReaderError{Kind: platformdomain.ReaderErrorAuth, Endpoint: recordedAPIEndpoint, Operation: "list nodes"}},
		},
		{
			name: "the API's certificate is not trusted", wantCode: "platform_api_tls", wantEndpoint: recordedAPIEndpoint,
			reading: healthReading{err: &platformdomain.ReaderError{
				Kind: platformdomain.ReaderErrorUnavailable, Endpoint: recordedAPIEndpoint, Transport: platformdomain.TransportTLS,
			}},
		},
		{
			name: "the API refuses the request", wantCode: "platform_api_rejected", wantEndpoint: recordedAPIEndpoint,
			reading: healthReading{err: &platformdomain.ReaderError{Kind: platformdomain.ReaderErrorNotFound, Endpoint: recordedAPIEndpoint}},
		},
		{
			name: "the Platform has no Integration", wantCode: "platform_integration_missing",
			reading: healthReading{forErr: platformdomain.ErrNoPlatformIntegration},
		},
		{
			name: "the Integration is gone", wantCode: "platform_integration_missing",
			reading: healthReading{forErr: sitedomain.ErrIntegrationNotFound},
		},
		{
			name: "the Integration has no credential", wantCode: "platform_credential_missing",
			reading: healthReading{forErr: sitedomain.ErrCredentialNotSet},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			waits := &recordedWaits{}
			readers := &healthReaders{readings: []healthReading{tc.reading}}
			executor := healthExecutor(readers, healthTargets())
			executor.platformHealthWait = time.Hour

			result := executor.Execute(context.Background(), healthTask(waits))

			if result.Status != workflowdomain.TaskFailed || result.Error == nil || result.Error.Code != tc.wantCode || !result.Error.Retryable {
				t.Fatalf("result = %+v (error %+v), want a retryable %s", result, result.Error, tc.wantCode)
			}
			if readers.count() != 1 || len(waits.observations()) != 0 {
				t.Errorf("reads = %d, published %q; want one read and nothing published", readers.count(), waits.observations())
			}
			d := result.Error.Diagnostic
			if d == nil || d.FailureClass != workflowdomain.FailurePermanent || d.Endpoint != tc.wantEndpoint || d.Remediation == "" || d.Cause == "" {
				t.Errorf("diagnostic = %+v, want permanent with endpoint %q, a cause, and a next step", d, tc.wantEndpoint)
			}
		})
	}
}

// The DF-90 incident: the Integration kept the API Server's old DHCP address. The failure names
// both addresses, says the endpoint is stale, and asks for a redeploy; an endpoint that still names
// the Server, a Task without its API Server, or an endpoint that is a host name says nothing about
// staleness.
func TestValidatePlatformNamesAStaleEndpoint(t *testing.T) {
	moved := func() *healthServers {
		servers := healthTargets()
		servers.servers["c1"].Observed.Addresses = []string{"10.170.169.140"}
		return servers
	}
	tests := []struct {
		name      string
		servers   *healthServers
		apiServer any
		endpoint  string
		wantStale bool
	}{
		{name: "the Server's address changed", servers: moved(), apiServer: "c1", endpoint: recordedAPIEndpoint, wantStale: true},
		{name: "the Server still has the address", servers: healthTargets(), apiServer: "c1", endpoint: recordedAPIEndpoint},
		{name: "the Task names no API Server", servers: moved(), endpoint: recordedAPIEndpoint},
		{name: "the endpoint is a host name", servers: moved(), apiServer: "c1", endpoint: "https://api.lab.example:6443"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			readers := &healthReaders{readings: []healthReading{{err: &platformdomain.ReaderError{
				Kind: platformdomain.ReaderErrorUnavailable, Endpoint: tc.endpoint, Transport: platformdomain.TransportRefused,
			}}}}
			executor := healthExecutor(readers, tc.servers)
			input := healthTask(&recordedWaits{})
			input.Step.Parameters = map[string]any{}
			if tc.apiServer != nil {
				input.Step.Parameters[platformAPIServerParameter] = tc.apiServer
			}

			result := executor.Execute(context.Background(), input)

			if result.Error == nil || result.Error.Diagnostic == nil {
				t.Fatalf("result = %+v, want a failure with a diagnostic", result)
			}
			message, remediation := result.Error.Message, result.Error.Diagnostic.Remediation
			stale := strings.Contains(message, "stale")
			if stale != tc.wantStale {
				t.Fatalf("message = %q, want stale named: %v", message, tc.wantStale)
			}
			if tc.wantStale && (!strings.Contains(message, "10.170.169.126") || !strings.Contains(message, "10.170.169.140") ||
				!strings.Contains(message, "lab-c1") || !strings.Contains(remediation, "Redeploy the Platform")) {
				t.Errorf("message = %q, remediation = %q; want both addresses, the Server, and a redeploy", message, remediation)
			}
		})
	}
}

// An unclassified read failure is waited on like a transient one; cancel and a superseded run end
// the wait without a failure.
func TestValidatePlatformKeepsWaitingAndStops(t *testing.T) {
	t.Run("an unclassified failure is waited on", func(t *testing.T) {
		readers := &healthReaders{readings: []healthReading{{forErr: errors.New("store unavailable")}, {members: bothMembers}}}
		executor := healthExecutor(readers, healthTargets())
		executor.platformHealthWait = time.Minute
		if result := executor.Execute(context.Background(), healthTask(&recordedWaits{})); result.Status != workflowdomain.TaskSucceeded {
			t.Errorf("result = %+v (error %+v), want succeeded after the failure cleared", result, result.Error)
		}
	})
	t.Run("an exhausted unclassified failure is unknown", func(t *testing.T) {
		readers := &healthReaders{readings: []healthReading{{forErr: errors.New("store unavailable")}}}
		executor := healthExecutor(readers, healthTargets())
		result := executor.Execute(context.Background(), healthTask(&recordedWaits{}))
		if result.Error == nil || result.Error.Code != "platform_health_timeout" || result.Error.Diagnostic == nil ||
			result.Error.Diagnostic.FailureClass != workflowdomain.FailureUnknown {
			t.Errorf("result = %+v (error %+v), want an unknown platform_health_timeout", result, result.Error)
		}
	})
	t.Run("a cancel ends the wait", func(t *testing.T) {
		readers := &healthReaders{readings: []healthReading{{err: refused()}}}
		executor := healthExecutor(readers, healthTargets())
		executor.platformHealthWait = time.Hour
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(20*time.Millisecond, cancel)
		if result := executor.Execute(ctx, healthTask(&recordedWaits{})); result.Status != workflowdomain.TaskCanceled {
			t.Errorf("result = %+v, want canceled", result)
		}
	})
	t.Run("a superseded run stops at its next publication", func(t *testing.T) {
		readers := &healthReaders{readings: []healthReading{{err: refused()}}}
		executor := healthExecutor(readers, healthTargets())
		executor.platformHealthWait = time.Hour
		waits := &recordedWaits{err: workflowdomain.ErrDispatchFenced}
		result := executor.Execute(context.Background(), healthTask(waits))
		if !isSuperseded(result) || readers.count() != 1 {
			t.Errorf("result = %+v after %d reads, want it to stop after the first", result, readers.count())
		}
	})
}

// The k0s deployment names the initial controller as the health check's API Server only when the
// cluster API is that Server's address; an HA cluster's API is its VIP.
func TestKubernetesHealthCheckNamesItsAPIServerWithoutHA(t *testing.T) {
	launcher := kubernetesGrafanaLauncher(t)

	single := findTask(t, preparedKubernetes(t, launcher, kubernetesLaunch(nil, nil)).workflow.Steps, "validate-platform")
	if got := single.Parameters[platformAPIServerParameter]; got != "c1" {
		t.Errorf("without HA %s = %v, want the initial controller c1", platformAPIServerParameter, got)
	}

	launch := kubernetesLaunch(nil, nil)
	launch.TrustedVars["swallow_k0s_high_availability"] = true
	ha := findTask(t, preparedKubernetes(t, launcher, launch).workflow.Steps, "validate-platform")
	if _, ok := ha.Parameters[platformAPIServerParameter]; ok {
		t.Errorf("with HA parameters = %v, want no API Server", ha.Parameters)
	}
}
