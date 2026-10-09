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
	// observations are the enrollment readings in order; the last one repeats. Empty means a
	// Machine that is always still enrolling.
	observations []provisioningdomain.EnrollmentObservation
	settles      int
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

func (p *inspectionProvider) ObserveEnrollment(context.Context, string) (provisioningdomain.EnrollmentObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.settles++
	if len(p.observations) == 0 {
		return enrolling, nil
	}
	reading := p.observations[0]
	if len(p.observations) > 1 {
		p.observations = p.observations[1:]
	}
	return reading, nil
}

// Enrollment readings of a Machine with an automatic driver, and of one without any driver.
var (
	enrolling = provisioningdomain.EnrollmentObservation{
		State: provisioningdomain.EnrollmentEnrolling, Driver: "ipmi", Control: provisioningdomain.PowerControlAutomatic,
	}
	settled = provisioningdomain.EnrollmentObservation{
		State: provisioningdomain.EnrollmentSettled, Driver: "ipmi", Control: provisioningdomain.PowerControlAutomatic,
	}
	unobservable = provisioningdomain.EnrollmentObservation{
		State: provisioningdomain.EnrollmentPowerUnobservable, Control: provisioningdomain.PowerControlNone,
	}
	noDriverEnrolling = provisioningdomain.EnrollmentObservation{
		State: provisioningdomain.EnrollmentEnrolling, Control: provisioningdomain.PowerControlNone,
	}
)

// poweredInspectionProvider adds a Power Configuration to the scripted provisioner, so the inspect
// Task can tell a virtual machine from a Server with a BMC.
type poweredInspectionProvider struct {
	*inspectionProvider
	config provisioningdomain.PowerConfiguration
}

func (p *poweredInspectionProvider) Capabilities() provisioningdomain.ProviderCapabilities {
	return provisioningdomain.ProviderCapabilities{HardwareValidation: true, PowerConfiguration: true}
}

func (p *poweredInspectionProvider) PowerConfiguration(context.Context, string) (*provisioningdomain.PowerConfiguration, error) {
	config := p.config
	return &config, nil
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
	waitTask := func() temporalworkflow.StepExecutionInput {
		return inspectionTask(waitEnrollmentTaskKind, map[string]any{skipEnrollmentWaitParameter: false})
	}
	t.Run("requested inspection does not wait", func(t *testing.T) {
		provider := &inspectionProvider{observations: []provisioningdomain.EnrollmentObservation{enrolling}}
		result := inspectionExecutor(provider).Execute(context.Background(),
			inspectionTask(waitEnrollmentTaskKind, map[string]any{skipEnrollmentWaitParameter: true}))
		if result.Status != operationdomain.TaskSucceeded || provider.settles != 1 {
			t.Errorf("result = %+v after %d readings, want success after one reading", result, provider.settles)
		}
	})
	t.Run("requested inspection of a Machine without a power driver asks for a Power Configuration", func(t *testing.T) {
		provider := &inspectionProvider{observations: []provisioningdomain.EnrollmentObservation{noDriverEnrolling}}
		result := inspectionExecutor(provider).Execute(context.Background(),
			inspectionTask(waitEnrollmentTaskKind, map[string]any{skipEnrollmentWaitParameter: true}))
		if result.Status != operationdomain.TaskRequiresAttention || result.Error == nil ||
			result.Error.Code != "power_configuration_required" || !result.Error.Retryable {
			t.Errorf("result = %+v, want retryable power_configuration_required attention", result)
		}
	})
	t.Run("requested inspection of a manual driver proceeds", func(t *testing.T) {
		manual := provisioningdomain.EnrollmentObservation{
			State: provisioningdomain.EnrollmentPowerUnobservable, Driver: "manual", Control: provisioningdomain.PowerControlManual,
		}
		provider := &inspectionProvider{observations: []provisioningdomain.EnrollmentObservation{manual}}
		result := inspectionExecutor(provider).Execute(context.Background(),
			inspectionTask(waitEnrollmentTaskKind, map[string]any{skipEnrollmentWaitParameter: true}))
		if result.Status != operationdomain.TaskSucceeded {
			t.Errorf("result = %+v, want success: an operator can switch a manual driver", result)
		}
	})
	t.Run("a retried run waits again", func(t *testing.T) {
		provider := &inspectionProvider{observations: []provisioningdomain.EnrollmentObservation{enrolling, settled, settled}}
		input := waitTask()
		input.Step.Attempt = 2
		if result := inspectionExecutor(provider).Execute(context.Background(), input); result.Status != operationdomain.TaskSucceeded || provider.settles != 3 {
			t.Errorf("result = %+v after %d readings, want success after 3 readings", result, provider.settles)
		}
	})
	t.Run("two consecutive settled readings are needed", func(t *testing.T) {
		provider := &inspectionProvider{observations: []provisioningdomain.EnrollmentObservation{enrolling, settled, enrolling, settled, settled}}
		result := inspectionExecutor(provider).Execute(context.Background(), waitTask())
		if result.Status != operationdomain.TaskSucceeded || provider.settles != 5 {
			t.Errorf("result = %+v after %d readings, want success after 5", result, provider.settles)
		}
	})
	t.Run("an unobservable power-off asks for a Power Configuration without waiting for the timeout", func(t *testing.T) {
		provider := &inspectionProvider{observations: []provisioningdomain.EnrollmentObservation{noDriverEnrolling, unobservable, unobservable}}
		result := inspectionExecutor(provider).Execute(context.Background(), waitTask())
		if result.Status != operationdomain.TaskRequiresAttention || result.Error == nil ||
			result.Error.Code != "power_configuration_required" || !result.Error.Retryable || provider.settles != 3 {
			t.Fatalf("result = %+v after %d readings, want retryable power_configuration_required after 3", result, provider.settles)
		}
		if !strings.Contains(result.Error.Message, "Power Configuration") || !strings.Contains(result.Error.Message, "gpu-07") {
			t.Errorf("message = %q, want the Server and the Power Configuration named", result.Error.Message)
		}
	})
	t.Run("a single unobservable reading is not enough", func(t *testing.T) {
		provider := &inspectionProvider{observations: []provisioningdomain.EnrollmentObservation{unobservable, settled, settled}}
		result := inspectionExecutor(provider).Execute(context.Background(), waitTask())
		if result.Status != operationdomain.TaskSucceeded {
			t.Errorf("result = %+v, want success once the power-off was observed", result)
		}
	})
	t.Run("an enrollment that never settles asks for attention", func(t *testing.T) {
		provider := &inspectionProvider{observations: []provisioningdomain.EnrollmentObservation{enrolling}}
		result := inspectionExecutor(provider).Execute(context.Background(), waitTask())
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
	t.Run("a virtual machine that never booted is pointed at its boot order, not Boot Media", func(t *testing.T) {
		provider := &poweredInspectionProvider{
			inspectionProvider: &inspectionProvider{status: provisioningdomain.MachineStatusNew,
				onInspect: func(p *inspectionProvider) { p.status = provisioningdomain.MachineStatusInspecting },
				onAbort:   func(p *inspectionProvider) { p.status = provisioningdomain.MachineStatusNew },
			},
			config: provisioningdomain.PowerConfiguration{Driver: "virsh", Address: "qemu+ssh://h/system", PowerID: "vm"},
		}
		executor := inspectionExecutor(provider)
		executor.inspectionAttempts = 1
		result := executor.Execute(context.Background(), inspectionTask(inspectTaskKind, nil))
		if result.Error == nil || result.Error.Code != "inspect_pxe_unreached" {
			t.Fatalf("result = %+v, want inspect_pxe_unreached attention", result)
		}
		if !strings.Contains(result.Error.Message, "boot order") || strings.Contains(result.Error.Message, "enable Boot Media") {
			t.Errorf("message = %q, want the boot-order remediation instead of enabling Boot Media", result.Error.Message)
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
