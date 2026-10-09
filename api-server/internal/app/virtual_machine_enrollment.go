package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	operationapp "github.com/maple52046/swallow/internal/operation/application"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
	"github.com/maple52046/swallow/internal/operation/infra/temporalworkflow"
	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	sitedomain "github.com/maple52046/swallow/internal/site/domain"
)

// The enroll-virtual-machines Workflow vocabulary (contract server-enrollment.md, decision 055).
// The definition, Job, Task kind, and attention codes are published; renaming them breaks
// Workflows already persisted.
const (
	virtualMachineEnrollmentDefinition = "virtual-machine-enrollment"
	virtualMachineEnrollmentJob        = "enroll-virtual-machines"
	enrollVirtualMachineTaskKind       = "enroll-virtual-machine"
)

// Timings of the enroll-virtual-machine Task. Projection is the API process's provisioner
// reconciliation, every 30 seconds by default.
const (
	virtualMachineProjectionWait = 5 * time.Minute
	virtualMachineProjectionPoll = 5 * time.Second
)

// enrollVirtualMachineParameters is the intent frozen into one enroll-virtual-machine Task.
type enrollVirtualMachineParameters struct {
	IntegrationID      string `json:"integrationId"`
	HypervisorServerID string `json:"hypervisorServerId"`
	Domain             string `json:"domain"`
	Account            string `json:"account"`
	BootISOID          string `json:"bootIsoId,omitempty"`
	PowerOffRunning    bool   `json:"powerOffRunning"`
}

func (p enrollVirtualMachineParameters) toMap() map[string]any {
	parameters := map[string]any{
		"integrationId": p.IntegrationID, "hypervisorServerId": p.HypervisorServerID,
		"domain": p.Domain, "account": p.Account, "powerOffRunning": p.PowerOffRunning,
	}
	if p.BootISOID != "" {
		parameters["bootIsoId"] = p.BootISOID
	}
	return parameters
}

// virtualMachineEnrollmentLauncher implements provisioningapp.VirtualMachineEnrollmentLauncher: it
// checks the provisioner, Hypervisor, and Boot ISO, then persists one enroll-virtual-machines
// Workflow targeting the Hypervisor. It lives in the composition root because it spans the
// provisioning and server contexts and Workflow persistence.
type virtualMachineEnrollmentLauncher struct {
	workflows    inspectionWorkflows
	integrations sitedomain.IntegrationRepository
	providers    provisioningdomain.ProviderFactory
	servers      serverdomain.ServerRepository
	protection   serverdomain.MutationGuard
	isos         serverdomain.BootISOResolver
	keys         interface {
		HasDeploymentKey(ctx context.Context) (bool, error)
	}
}

// LaunchVirtualMachineEnrollment implements provisioningapp.VirtualMachineEnrollmentLauncher.
func (l virtualMachineEnrollmentLauncher) LaunchVirtualMachineEnrollment(ctx context.Context, request provisioningapp.VirtualMachineEnrollmentRequest) (*provisioningapp.VirtualMachineEnrollmentAccepted, error) {
	request, err := request.Normalize()
	if err != nil {
		return nil, err
	}
	integration, err := l.integrations.FindByID(ctx, request.IntegrationID)
	if err != nil {
		return nil, err
	}
	if integration.Kind != sitedomain.IntegrationKindProvisioner {
		return nil, fmt.Errorf("%w: %q is registered as %q", provisioningdomain.ErrIntegrationNotProvisioner, integration.Name, integration.Kind)
	}
	provider, err := l.providers.For(ctx, integration.ID)
	if err != nil {
		return nil, err
	}
	capabilities := provider.Capabilities()
	if _, ok := provider.(provisioningdomain.MachineRegistrar); !ok || !capabilities.MachineRegistration || !capabilities.PowerConfiguration {
		return nil, fmt.Errorf("%w: provisioner %q cannot register machines with their power settings", provisioningdomain.ErrInvalidVirtualMachineEnrollment, integration.Name)
	}
	hypervisor, err := l.servers.FindByID(ctx, request.HypervisorServerID)
	if err != nil {
		return nil, err
	}
	login, err := serverdomain.HypervisorOf(hypervisor, request.Account)
	if err != nil {
		return nil, err
	}
	if hypervisor.Source.SiteID != integration.SiteID {
		return nil, fmt.Errorf("%w: hypervisor %s is not in the Site of provisioner %q", provisioningdomain.ErrVirtualMachineEnrollmentConflict, login.Name, integration.Name)
	}
	if request.BootISOID != "" {
		file, err := l.isos.File(ctx, request.BootISOID)
		if err != nil {
			return nil, err
		}
		if file.IntegrationID != integration.ID {
			return nil, fmt.Errorf("%w: Boot ISO %q chains to another provisioner than %q", serverdomain.ErrBootISOWrongIntegration, file.Name, integration.Name)
		}
	}
	if l.keys != nil {
		exists, err := l.keys.HasDeploymentKey(ctx)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, provisioningdomain.ErrDeploymentKeyMissing
		}
	}
	if l.protection != nil {
		if err := l.protection.RequireUnlocked(ctx, []string{hypervisor.ID}); err != nil {
			return nil, err
		}
	}
	target := []operationdomain.ResourceReference{{Kind: "server", ID: hypervisor.ID}}
	steps := make([]operationdomain.Task, 0, len(request.Domains))
	ids := map[string]bool{}
	for i, domain := range request.Domains {
		id := enrollVirtualMachineTaskKind + "-" + provisioningdomain.MachineHostnameOf(domain)
		if strings.HasSuffix(id, "-") || ids[id] {
			id = enrollVirtualMachineTaskKind + "-" + strconv.Itoa(i+1)
		}
		ids[id] = true
		steps = append(steps, operationdomain.Task{
			ID: id, Kind: enrollVirtualMachineTaskKind, Name: "Enroll virtual machine " + domain,
			Job: virtualMachineEnrollmentJob, Executor: operationdomain.RunnerKindProvisioner, Targets: target,
			Parameters: enrollVirtualMachineParameters{
				IntegrationID: integration.ID, HypervisorServerID: hypervisor.ID, Domain: domain,
				Account: login.Account, BootISOID: request.BootISOID, PowerOffRunning: request.PowerOffRunning,
			}.toMap(),
		})
	}
	summary := fmt.Sprintf("Enroll %d virtual machines of %s", len(request.Domains), login.Name)
	if len(request.Domains) == 1 {
		summary = fmt.Sprintf("Enroll virtual machine %s of %s", request.Domains[0], login.Name)
	}
	created, err := l.workflows.Create(ctx, operationapp.CreateWorkflowInput{
		Kind:          operationdomain.WorkflowKindEnrollVirtualMachines,
		IntentSummary: summary,
		IntentSnapshot: map[string]any{
			"integrationId": integration.ID, "hypervisorServerId": hypervisor.ID, "domains": request.Domains,
			"bootIsoId": request.BootISOID, "account": login.Account, "powerOffRunning": request.PowerOffRunning,
		},
		Definition: virtualMachineEnrollmentDefinition, DefinitionVersion: 1,
		SiteID: integration.SiteID, TargetServerIDs: []string{hypervisor.ID},
		Steps:       steps,
		RequestedBy: request.RequestedBy, RequestCorrelation: request.RequestID,
	})
	if err != nil {
		return nil, err
	}
	return &provisioningapp.VirtualMachineEnrollmentAccepted{WorkflowID: created.ID}, nil
}

// virtualMachineBootMedia is the slice of the Boot Media use case the enrollment Task uses.
type virtualMachineBootMedia interface {
	EnableApplied(ctx context.Context, serverID, isoID string) error
}

// virtualMachineEnroller runs enroll-virtual-machine Tasks for the provisioner executor.
type virtualMachineEnroller struct {
	libvirt      serverdomain.LibvirtHost
	isos         serverdomain.BootISOResolver
	bootMedia    virtualMachineBootMedia
	integrations sitedomain.IntegrationRepository
	// projectionWait and projectionPoll override the production timings in tests.
	projectionWait, projectionPoll time.Duration
}

// enrollVirtualMachine runs one enroll-virtual-machine Task (contract server-enrollment.md): every
// step is an ensure, so a retry resumes where the last attempt stopped and reuses the Machine it
// registered. Nothing here commissions the Machine; automatic inspection does.
func (e providerStepExecutor) enrollVirtualMachine(ctx context.Context, input temporalworkflow.StepExecutionInput) temporalworkflow.StepExecutionResult {
	enroller := e.virtualMachines
	if enroller == nil {
		return providerFailed("virtual_machine_enrollment_unavailable", "Virtual machine enrollment is unavailable in this worker.", false).StepExecutionResult
	}
	var p enrollVirtualMachineParameters
	raw, err := json.Marshal(input.Step.Parameters)
	if err == nil {
		err = json.Unmarshal(raw, &p)
	}
	if err != nil || p.Domain == "" || p.HypervisorServerID == "" || p.IntegrationID == "" {
		return providerFailed("virtual_machine_enrollment_invalid", "The enroll-virtual-machine Task parameters are incomplete.", false).StepExecutionResult
	}
	hypervisor, err := e.servers.FindByID(ctx, p.HypervisorServerID)
	if err != nil {
		return providerAttention("hypervisor_unreachable", "The hypervisor Server could not be read: "+err.Error(), "hypervisor")
	}
	login, err := serverdomain.HypervisorOf(hypervisor, p.Account)
	if err != nil {
		return providerAttention("hypervisor_unreachable", err.Error(), "hypervisor")
	}
	if blocked := e.requireUnlocked(ctx, hypervisor.ID); blocked != nil {
		return *blocked
	}

	// 1–2. The domain exists and is shut off.
	machine, err := enroller.libvirt.Domain(ctx, login, p.Domain)
	if err != nil {
		return hypervisorFailure(err, "domain")
	}
	if !machine.ShutOff() {
		if !p.PowerOffRunning {
			return providerAttention("domain_running", fmt.Sprintf(
				"Virtual machine %q on %s is %s. Shut it down, or enroll it again with powerOffRunning to stop it, then retry this Task.",
				p.Domain, login.Name, machine.State), "domain")
		}
		if err := enroller.libvirt.DestroyDomain(ctx, login, p.Domain); err != nil {
			return hypervisorFailure(err, "domain")
		}
	}

	// 3. The provisioner's SSH identity may log in to the Hypervisor.
	integration, err := enroller.integrations.FindByID(ctx, p.IntegrationID)
	if err != nil {
		return providerAttention("provider_unavailable", "The provisioner Integration could not be read: "+err.Error(), "integration")
	}
	if key := provisioningapp.VirshSSHPublicKey(integration); key != "" {
		if err := enroller.libvirt.AuthorizePublicKey(ctx, login, key); err != nil {
			return hypervisorFailure(err, "provisioner_access")
		}
	}

	// 4. The domain boots the Boot ISO first, before the provisioner can ever start it.
	if p.BootISOID != "" {
		file, err := enroller.isos.File(ctx, p.BootISOID)
		if err != nil {
			return providerAttention("boot_media_not_configured", "The Boot ISO cannot be used: "+err.Error(), "boot_media")
		}
		if _, err := enroller.libvirt.ApplyBootMedia(ctx, login, p.Domain, *file); err != nil {
			return hypervisorFailure(err, "boot_media")
		}
	}

	// 5. A Machine exists for the domain, with the virsh Power Configuration.
	provider, err := e.providers.For(ctx, p.IntegrationID)
	if err != nil {
		return normalizeProviderError(err, "registration")
	}
	registrar, canRegister := provider.(provisioningdomain.MachineRegistrar)
	writer, canWrite := provider.(provisioningdomain.PowerConfigurationWriter)
	controller, canQuery := provider.(provisioningdomain.PowerController)
	if !canRegister || !canWrite || !canQuery || !provider.Capabilities().MachineRegistration {
		return providerFailed("machine_registration_unsupported", "The provisioner cannot register machines with their power settings.", false).StepExecutionResult
	}
	architecture, ok := provisioningdomain.MachineArchitectureOf(machine.Architecture)
	if !ok {
		return providerFailed("architecture_unsupported", fmt.Sprintf(
			"Virtual machine %q has architecture %q; only x86_64 and aarch64 virtual machines can be enrolled.", p.Domain, machine.Architecture), false).StepExecutionResult
	}
	if len(machine.MACAddresses) == 0 {
		return providerFailed("machine_registration_refused", fmt.Sprintf("Virtual machine %q has no network interface to boot from.", p.Domain), false).StepExecutionResult
	}
	virsh, _ := provisioningdomain.DefaultPowerAdapters().ForDriver(provisioningdomain.PowerDriverVirsh)
	power, err := virsh.Normalize(provisioningdomain.PowerConfigurationChange{
		Driver: provisioningdomain.PowerDriverVirsh, Address: provisioningdomain.VirshAddress(login.Account, login.Target.Address), PowerID: p.Domain,
	})
	if err != nil {
		return providerFailed("machine_registration_refused", err.Error(), false).StepExecutionResult
	}
	machineID, err := registrar.FindMachineByMAC(ctx, machine.MACAddresses)
	if err != nil {
		return normalizeProviderError(err, "registration")
	}
	if machineID != "" {
		if err := writer.SetPowerConfiguration(ctx, machineID, power); err != nil {
			return registrationFailure(err)
		}
	} else {
		machineID, err = registrar.RegisterMachine(ctx, provisioningdomain.MachineRegistration{
			Hostname: provisioningdomain.MachineHostnameOf(p.Domain), Architecture: architecture,
			MACAddresses: machine.MACAddresses, Power: power,
		})
		if err != nil {
			return registrationFailure(err)
		}
	}

	// 6. The provisioner can read the domain's power through the virsh driver.
	state, err := controller.QueryPowerState(ctx, machineID)
	if err != nil || (state != provisioningdomain.PowerStateOn && state != provisioningdomain.PowerStateOff) {
		detail := "it reported the power state " + string(state)
		if err != nil {
			detail = err.Error()
		}
		return providerAttention("provisioner_cannot_reach_hypervisor", fmt.Sprintf(
			"The provisioner could not read the power of %q through %s (%s). Let the provisioner's SSH identity log in to %s as %s — set the provisioner Integration's virshSshPublicKey, or authorize its key on the hypervisor — then retry this Task.",
			p.Domain, power.Address, detail, login.Name, login.Account), "power")
	}

	// 7. The Machine's Server is projected.
	server, result := e.awaitVirtualMachineServer(ctx, enroller, serverdomain.Source{
		SiteID: integration.SiteID, IntegrationID: integration.ID, ProviderMachineID: machineID,
	}, p.Domain)
	if result != nil {
		return *result
	}

	// 8. The Server keeps its Boot Media for inspection and every OS deployment.
	if p.BootISOID != "" {
		if err := enroller.bootMedia.EnableApplied(ctx, server.ID, p.BootISOID); err != nil {
			return providerAttention("boot_media_ensure_failed", err.Error(), "boot_media")
		}
	}
	return providerSucceeded()
}

// awaitVirtualMachineServer waits until the provisioner reconciliation projects the Machine.
func (e providerStepExecutor) awaitVirtualMachineServer(ctx context.Context, enroller *virtualMachineEnroller, source serverdomain.Source, domain string) (*serverdomain.Server, *temporalworkflow.StepExecutionResult) {
	wait, poll := virtualMachineProjectionWait, virtualMachineProjectionPoll
	if enroller.projectionWait > 0 {
		wait = enroller.projectionWait
	}
	if enroller.projectionPoll > 0 {
		poll = enroller.projectionPoll
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		server, err := e.servers.FindBySource(ctx, source)
		switch {
		case err == nil:
			return server, nil
		case !errors.Is(err, serverdomain.ErrServerNotFound):
			result := providerAttention("server_not_projected", "The Server projection could not be read: "+err.Error(), "projection")
			return nil, &result
		}
		select {
		case <-ctx.Done():
			result := temporalworkflow.StepExecutionResult{Status: operationdomain.TaskCanceled}
			return nil, &result
		case <-deadline.C:
			result := providerAttention("server_not_projected", fmt.Sprintf(
				"The provisioner registered virtual machine %q, but swallow has not projected its Server after %s. Check that the provisioner Integration is enabled and syncing, then retry this Task.",
				domain, wait), "projection")
			return nil, &result
		case <-ticker.C:
		}
	}
}

// hypervisorFailure maps a libvirt or SSH failure: a missing domain is final, anything else waits
// for the operator.
func hypervisorFailure(err error, stage string) temporalworkflow.StepExecutionResult {
	if errors.Is(err, serverdomain.ErrDomainNotFound) {
		return providerFailed("domain_not_found", err.Error(), false).withStage(stage)
	}
	return providerAttention("hypervisor_unreachable", err.Error(), stage)
}

// registrationFailure maps a refused registration to attention with the provisioner's reason.
func registrationFailure(err error) temporalworkflow.StepExecutionResult {
	var providerErr *provisioningdomain.ProviderError
	if errors.As(err, &providerErr) && providerErr.Kind == provisioningdomain.ProviderErrorRejected {
		return providerAttention("machine_registration_refused", "The provisioner refused the machine: "+providerErr.Detail, "registration")
	}
	if errors.Is(err, provisioningdomain.ErrInvalidPowerConfiguration) {
		return providerAttention("machine_registration_refused", err.Error(), "registration")
	}
	return normalizeProviderError(err, "registration")
}
