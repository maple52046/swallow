package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// inspectionProvider is a scripted provisioner for the inspect-hardware Tasks. Inspect and Abort
// move the Machine through onInspect and onAbort; reads may advance it through onRead.
type inspectionProvider struct {
	cancellationProvider
	mu        sync.Mutex
	status    provisioningdomain.MachineStatus
	onInspect func(p *inspectionProvider)
	onAbort   func(p *inspectionProvider)
	onRead    func(p *inspectionProvider, reads int)
	reads     int
	inspects  int
	settled   []bool
	settles   int
}

func (p *inspectionProvider) GetMachine(context.Context, string) (*provisioningdomain.Machine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads++
	if p.onRead != nil {
		p.onRead(p, p.reads)
	}
	return &provisioningdomain.Machine{ID: "machine-id", Status: p.status, ProviderStatus: string(p.status)}, nil
}

func (p *inspectionProvider) Inspect(context.Context, string) (*provisioningdomain.Machine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inspects++
	if p.onInspect != nil {
		p.onInspect(p)
	}
	return nil, nil
}

func (p *inspectionProvider) Abort(context.Context, string) (*provisioningdomain.Machine, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.aborts++
	if p.onAbort != nil {
		p.onAbort(p)
	}
	return nil, nil
}

// ListMachineEvents reports no events, which is what an attempt that never booted looks like.
func (p *inspectionProvider) ListMachineEvents(context.Context, string, int) ([]provisioningdomain.MachineEvent, error) {
	return nil, nil
}

func (p *inspectionProvider) EnrollmentSettled(context.Context, string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.settles++
	if len(p.settled) == 0 {
		return false, nil
	}
	reading := p.settled[0]
	if len(p.settled) > 1 {
		p.settled = p.settled[1:]
	}
	return reading, nil
}

type inspectionProviderFactory struct {
	provider provisioningdomain.OSProvisioningProvider
}

func (f inspectionProviderFactory) For(context.Context, string) (provisioningdomain.OSProvisioningProvider, error) {
	return f.provider, nil
}

func inspectionExecutor(provider provisioningdomain.OSProvisioningProvider) providerStepExecutor {
	server := &serverdomain.Server{
		ID: "server-id", Observed: serverdomain.Observed{Hostname: "gpu-07"},
		Source: serverdomain.Source{IntegrationID: "integration-id", ProviderMachineID: "machine-id"},
	}
	repository := &deploymentProjectionTestRepo{server: server}
	factory := inspectionProviderFactory{provider: provider}
	return providerStepExecutor{
		servers: repository, providers: factory,
		refresh:              provisioningapp.NewRefreshServerUseCase(repository, factory, nil),
		poll:                 time.Millisecond,
		enrollmentSettlePoll: time.Millisecond, enrollmentSettleWait: 200 * time.Millisecond,
		inspectionStallWait: 5 * time.Millisecond, inspectionAttemptWait: time.Second,
		inspectionStartWait: 5 * time.Millisecond, inspectionAbortWait: 200 * time.Millisecond,
	}
}

func inspectionTask(kind string, parameters map[string]any) temporalworkflow.StepExecutionInput {
	return temporalworkflow.StepExecutionInput{OperationID: "workflow-id", Step: operationdomain.Task{
		ID: kind + "-server-id", Kind: kind, Attempt: 1, Parameters: parameters,
		Targets: []operationdomain.ResourceReference{{Kind: "server", ID: "server-id"}},
	}}
}

func TestWaitEnrollmentSettled(t *testing.T) {
	t.Run("requested inspection skips the wait", func(t *testing.T) {
		provider := &inspectionProvider{}
		result := inspectionExecutor(provider).Execute(context.Background(),
			inspectionTask(waitEnrollmentTaskKind, map[string]any{skipEnrollmentWaitParameter: true}))
		if result.Status != operationdomain.TaskSucceeded || provider.settles != 0 {
			t.Errorf("result = %+v after %d readings, want success without reading", result, provider.settles)
		}
	})
	t.Run("a retried run skips the wait", func(t *testing.T) {
		provider := &inspectionProvider{}
		input := inspectionTask(waitEnrollmentTaskKind, map[string]any{skipEnrollmentWaitParameter: false})
		input.Step.Attempt = 2
		if result := inspectionExecutor(provider).Execute(context.Background(), input); result.Status != operationdomain.TaskSucceeded || provider.settles != 0 {
			t.Errorf("result = %+v after %d readings, want success without reading", result, provider.settles)
		}
	})
	t.Run("two consecutive settled readings are needed", func(t *testing.T) {
		provider := &inspectionProvider{settled: []bool{false, true, false, true, true}}
		result := inspectionExecutor(provider).Execute(context.Background(),
			inspectionTask(waitEnrollmentTaskKind, map[string]any{skipEnrollmentWaitParameter: false}))
		if result.Status != operationdomain.TaskSucceeded || provider.settles != 5 {
			t.Errorf("result = %+v after %d readings, want success after 5", result, provider.settles)
		}
	})
	t.Run("an enrollment that never settles asks for attention", func(t *testing.T) {
		provider := &inspectionProvider{settled: []bool{false}}
		result := inspectionExecutor(provider).Execute(context.Background(),
			inspectionTask(waitEnrollmentTaskKind, map[string]any{skipEnrollmentWaitParameter: false}))
		if result.Status != operationdomain.TaskRequiresAttention || result.Error == nil ||
			result.Error.Code != "enrollment_not_settled" || !result.Error.Retryable {
			t.Errorf("result = %+v, want retryable enrollment_not_settled attention", result)
		}
	})
	t.Run("a provisioner without the capability has nothing to wait for", func(t *testing.T) {
		provider := &cancellationProvider{}
		result := inspectionExecutor(provider).Execute(context.Background(),
			inspectionTask(waitEnrollmentTaskKind, map[string]any{skipEnrollmentWaitParameter: false}))
		if result.Status != operationdomain.TaskSucceeded {
			t.Errorf("result = %+v, want success", result)
		}
	})
}

func TestInspectServer(t *testing.T) {
	t.Run("an inspection that reaches ready succeeds", func(t *testing.T) {
		provider := &inspectionProvider{status: provisioningdomain.MachineStatusNew,
			onInspect: func(p *inspectionProvider) { p.status = provisioningdomain.MachineStatusInspecting },
			onRead: func(p *inspectionProvider, reads int) {
				if p.status == provisioningdomain.MachineStatusInspecting && reads > 2 {
					p.status = provisioningdomain.MachineStatusReady
				}
			},
		}
		result := inspectionExecutor(provider).Execute(context.Background(), inspectionTask(inspectTaskKind, nil))
		if result.Status != operationdomain.TaskSucceeded || provider.inspects != 1 || provider.aborts != 0 {
			t.Errorf("result = %+v (%d inspects, %d aborts), want success after one inspect", result, provider.inspects, provider.aborts)
		}
	})
	t.Run("stalled attempts are aborted, retried, and end in attention naming Boot Media", func(t *testing.T) {
		provider := &inspectionProvider{status: provisioningdomain.MachineStatusNew,
			onInspect: func(p *inspectionProvider) { p.status = provisioningdomain.MachineStatusInspecting },
			onAbort:   func(p *inspectionProvider) { p.status = provisioningdomain.MachineStatusNew },
		}
		executor := inspectionExecutor(provider)
		executor.inspectionAttempts = 3
		result := executor.Execute(context.Background(), inspectionTask(inspectTaskKind, nil))
		if result.Status != operationdomain.TaskRequiresAttention || result.Error == nil ||
			result.Error.Code != "inspect_pxe_unreached" || !result.Error.Retryable {
			t.Fatalf("result = %+v, want retryable inspect_pxe_unreached attention", result)
		}
		if provider.inspects != 3 || provider.aborts != 3 {
			t.Errorf("inspects = %d, aborts = %d, want 3 each", provider.inspects, provider.aborts)
		}
		if !strings.Contains(result.Error.Message, "Boot Media") || provider.status != provisioningdomain.MachineStatusNew {
			t.Errorf("message = %q, status = %s; want Boot Media remediation and the Server back in new", result.Error.Message, provider.status)
		}
	})
	t.Run("provider-reported failures are retried and end in inspect_failed", func(t *testing.T) {
		provider := &inspectionProvider{status: provisioningdomain.MachineStatusNew,
			onInspect: func(p *inspectionProvider) { p.status = provisioningdomain.MachineStatusFailed },
		}
		executor := inspectionExecutor(provider)
		executor.inspectionAttempts = 2
		result := executor.Execute(context.Background(), inspectionTask(inspectTaskKind, nil))
		if result.Status != operationdomain.TaskRequiresAttention || result.Error == nil || result.Error.Code != "inspect_failed" {
			t.Fatalf("result = %+v, want inspect_failed attention", result)
		}
		if provider.inspects != 2 || provider.aborts != 0 {
			t.Errorf("inspects = %d, aborts = %d, want 2 inspects and no abort", provider.inspects, provider.aborts)
		}
	})
	t.Run("an inspection stopped outside swallow is not commissioned again", func(t *testing.T) {
		provider := &inspectionProvider{status: provisioningdomain.MachineStatusNew,
			onInspect: func(p *inspectionProvider) { p.status = provisioningdomain.MachineStatusInspecting },
			onRead: func(p *inspectionProvider, reads int) {
				if reads > 2 {
					p.status = provisioningdomain.MachineStatusNew
				}
			},
		}
		executor := inspectionExecutor(provider)
		executor.inspectionStallWait = time.Hour
		result := executor.Execute(context.Background(), inspectionTask(inspectTaskKind, nil))
		if result.Status != operationdomain.TaskRequiresAttention || result.Error == nil || result.Error.Code != "inspect_interrupted" {
			t.Fatalf("result = %+v, want inspect_interrupted attention", result)
		}
		if provider.inspects != 1 {
			t.Errorf("inspects = %d, want 1", provider.inspects)
		}
	})
	t.Run("an inspection already running is followed, not issued again", func(t *testing.T) {
		provider := &inspectionProvider{status: provisioningdomain.MachineStatusInspecting,
			onRead: func(p *inspectionProvider, reads int) {
				if reads > 3 {
					p.status = provisioningdomain.MachineStatusReady
				}
			},
		}
		result := inspectionExecutor(provider).Execute(context.Background(), inspectionTask(inspectTaskKind, nil))
		if result.Status != operationdomain.TaskSucceeded || provider.inspects != 0 {
			t.Errorf("result = %+v after %d inspects, want success without a second inspection", result, provider.inspects)
		}
	})
	t.Run("an automatic inspection of a ready Server has nothing to do", func(t *testing.T) {
		for origin, wantInspects := range map[provisioningdomain.InspectionOrigin]int{
			provisioningdomain.InspectionOriginAutomatic: 0,
			provisioningdomain.InspectionOriginRequested: 1,
		} {
			provider := &inspectionProvider{status: provisioningdomain.MachineStatusReady}
			result := inspectionExecutor(provider).Execute(context.Background(),
				inspectionTask(inspectTaskKind, map[string]any{inspectionOriginParameter: string(origin)}))
			if result.Status != operationdomain.TaskSucceeded || provider.inspects != wantInspects {
				t.Errorf("%s: result = %+v after %d inspects, want success after %d", origin, result, provider.inspects, wantInspects)
			}
		}
	})
	t.Run("a locked Server is not commissioned", func(t *testing.T) {
		provider := &inspectionProvider{status: provisioningdomain.MachineStatusNew}
		executor := inspectionExecutor(provider)
		executor.protection = stubMutationGuard{err: &serverdomain.ServerLockedError{Name: "gpu-07"}}
		result := executor.Execute(context.Background(), inspectionTask(inspectTaskKind, nil))
		if result.Status != operationdomain.TaskRequiresAttention || result.Error == nil || result.Error.Code != "target_locked" || provider.inspects != 0 {
			t.Errorf("result = %+v after %d inspects, want target_locked attention and no inspect", result, provider.inspects)
		}
	})
}

// The inspect-hardware ensure-boot-media Task reads Boot Media when it runs: disabled Boot Media
// succeeds without the BMC, enabled Boot Media is ensured with its Boot ISO's URL, and an enabled
// setting whose Boot ISO is not served fails retryably so the operator can fix it and retry.
func TestEnsureBootMediaResolvesLive(t *testing.T) {
	live := temporalworkflow.StepExecutionInput{Step: operationdomain.Task{
		ID: "ensure-boot-media-srv", Kind: ensureBootMediaTaskKind,
		Targets:    []operationdomain.ResourceReference{{Kind: "server", ID: "srv"}},
		Parameters: map[string]any{resolveBootMediaLiveParameter: true},
	}}
	cases := []struct {
		name      string
		setting   *serverdomain.BootMediaSetting
		wantCode  string
		wantISO   string
		wantCalls bool
	}{
		{name: "disabled", setting: &serverdomain.BootMediaSetting{Enabled: false, ISOID: "iso-a"}},
		{name: "never set"},
		{name: "enabled", setting: &serverdomain.BootMediaSetting{Enabled: true, ISOID: "iso-a"}, wantISO: (plannerISOs{}).URL("iso-a"), wantCalls: true},
		{name: "enabled without a served ISO", setting: &serverdomain.BootMediaSetting{Enabled: true, ISOID: "iso-missing"}, wantCode: "boot_media_not_configured"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ensurer := &fakeEnsurer{}
			executor := platformWorkflowStepExecutor{
				bootMedia: ensurer, bootISOs: plannerISOs{},
				servers: bootMediaServers{servers: map[string]*serverdomain.Server{"srv": {ID: "srv", BootMedia: tc.setting}}},
			}
			result := executor.Execute(context.Background(), live)
			if tc.wantCode != "" {
				if result.Status != operationdomain.TaskFailed || result.Error == nil || result.Error.Code != tc.wantCode || !result.Error.Retryable {
					t.Errorf("result = %+v, want retryable %s", result, tc.wantCode)
				}
				return
			}
			if result.Status != operationdomain.TaskSucceeded {
				t.Fatalf("result = %+v, want success", result)
			}
			if called := ensurer.serverID != ""; called != tc.wantCalls || ensurer.isoURL != tc.wantISO {
				t.Errorf("ensure(%q, %q), want called=%v with %q", ensurer.serverID, ensurer.isoURL, tc.wantCalls, tc.wantISO)
			}
		})
	}
}
