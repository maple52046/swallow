package app

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	moduledomain "github.com/maple52046/swallow/internal/core/module/domain"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	provisioningapp "github.com/maple52046/swallow/internal/core/provisioning/application"
	workflowapp "github.com/maple52046/swallow/internal/core/workflow/application"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
	"github.com/maple52046/swallow/modules/platform/slurm"
	"github.com/maple52046/swallow/modules/platform/slurm/slurmapi"
)

// A Slurm scale-in (decision 083) is the Slurm module's declarative scale-in template, compiled with the
// members that leave and the members that stay; only the per-Server release Tasks and the internal
// finalize Task, which need parameters a template cannot carry, are planned here.
const (
	scaleInSlurmKind        workflowdomain.WorkflowKind = "scale-in-slurm"
	scaleInTemplate                                     = "slurm-scale-in"
	completeScaleInTaskKind                             = "complete-member-removal"
	completeScaleInStepID                               = "complete-member-removal"
	jobFinalizeScaleIn                                  = "finalize-scale-in"
	// The scale-in's trusted variables: the leaving nodes with their drain choices.
	varScaleInNodes = "swallow_slurm_scale_in_nodes"
)

// prepareSlurmScaleIn plans the removal of launch.ScaleIn's members from a Slurm Platform: the scale-in
// template's drain (from the primary controller, naming each node by its stored node name, so a
// node that cannot be reached is still drained), reconfigure, and clean Tasks, then a release Task for every member that leaves
// with its Server released, then complete-member-removal. The Workflow targets every member, the
// leaving ones included, and is not a deploy, so the Platform stays active meanwhile.
func (l platformDeploymentLauncher) prepareSlurmScaleIn(ctx context.Context, launch platformdomain.DeploymentLaunch) (*preparedPlatformDeployment, error) {
	if launch.ScaleIn == nil || len(launch.ScaleIn.Removals) == 0 {
		return nil, fmt.Errorf("%w: a scale-in needs members to remove", platformdomain.ErrInvalidDeployment)
	}
	if l.registry == nil {
		return nil, fmt.Errorf("%w: the scale-in template is not available in this process", platformdomain.ErrInvalidDeployment)
	}
	template, ok := l.registry.Template(moduledomain.KindPlatform, slurm.ID, scaleInTemplate)
	if !ok {
		return nil, fmt.Errorf("%w: the Slurm module declares no %s template", platformdomain.ErrInvalidDeployment, scaleInTemplate)
	}
	operationID := launch.OperationID
	if operationID == "" {
		operationID = uuid.NewString()
	}
	primary, _ := launch.TrustedVars["swallow_slurm_primary_controller_id"].(string)
	leaving := make([]string, 0, len(launch.ScaleIn.Removals))
	nodes := make([]any, 0, len(launch.ScaleIn.Removals))
	for _, removal := range launch.ScaleIn.Removals {
		leaving = append(leaving, removal.ServerID)
		timeout := removal.DrainTimeoutSeconds
		if timeout <= 0 {
			timeout = platformdomain.DefaultDrainTimeoutSeconds
		}
		name := launch.ScaleIn.NodeNames[removal.ServerID]
		if name == "" {
			return nil, fmt.Errorf("%w: server %s has no node name to drain it by", platformdomain.ErrInvalidDeployment, removal.ServerID)
		}
		nodes = append(nodes, map[string]any{
			"server_id": removal.ServerID, "name": name, "drain_timeout_seconds": timeout, "force": removal.Force,
		})
	}
	remaining := slices.Clone(launch.TargetServerIDs)
	compiled, err := template.Compile(l.catalog, workflowdomain.TemplateInput{
		Servers: leaving, Access: []string{primary}, Members: remaining,
	})
	if err != nil {
		return nil, fmt.Errorf("plan the scale-in: %w", err)
	}
	trusted := maps.Clone(launch.TrustedVars)
	trusted[varScaleInNodes] = nodes
	trusted = l.ntp.withCheckVars(trusted, launch.TimeSyncCheck)
	var steps []workflowdomain.Task
	var extra map[string]any
	for _, step := range compiled.Tasks {
		if step.Executor != workflowdomain.RunnerKindHostAutomation {
			steps = append(steps, step)
			continue
		}
		playbook, _ := step.Parameters["playbook"].(string)
		vars := maps.Clone(trusted)
		if static, ok := step.Parameters["vars"].(map[string]any); ok {
			maps.Copy(vars, static)
		}
		prepared, err := l.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
			Kind: string(scaleInSlurmKind), PlaybookName: playbook,
			Intent:          step.Name + " on " + launch.Platform.Name,
			TargetServerIDs: serverIDsOf(step.Targets), PlatformID: launch.Platform.ID,
			TrustedVars: vars, SecretVars: launch.SecretVars, RequestedBy: launch.RequestedBy,
		}, operationID, false)
		if err != nil {
			return nil, err
		}
		step.Kind = "ansible-playbook"
		step.Targets = prepared.Targets
		step.Parameters = map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars}
		if playbook == deploySlurmPlaybook {
			// The reconfigure reports the slurmrestd credential like every deploy-slurm run.
			step.Completion = []workflowdomain.Completion{{Type: slurmapi.CredentialCompletion}}
		}
		if extra == nil {
			extra = prepared.ExtraVars
		}
		steps = append(steps, step)
	}
	last := steps[len(steps)-1].ID
	finalizeDeps := []string{last}
	for _, removal := range launch.ScaleIn.Removals {
		if !removal.ReleaseServer {
			continue
		}
		stepID := "release-" + removal.ServerID
		steps = append(steps, workflowdomain.Task{
			ID: stepID, Kind: "release-os", Name: "Release " + removal.ServerID,
			Job: jobReleaseServers, Executor: workflowdomain.RunnerKindProvisioner, DependsOn: []string{last},
			Targets: []workflowdomain.ResourceReference{{Kind: "server", ID: removal.ServerID}},
			Parameters: map[string]any{"request": structToMap(provisioningapp.ReleaseServerInput{
				ServerID: removal.ServerID, Erase: removal.ReleaseOptions.Erase, SecureErase: removal.ReleaseOptions.SecureErase,
				QuickErase: removal.ReleaseOptions.QuickErase, UnbindStaticIPs: removal.ReleaseOptions.UnbindStaticIPs,
				Comment: "Release after leaving platform " + launch.Platform.Name, RequestID: operationID,
			})},
			Completion: []workflowdomain.Completion{{Type: completionServerDeploymentCleared}},
		})
		finalizeDeps = append(finalizeDeps, stepID)
	}
	steps = append(steps, workflowdomain.Task{
		ID: completeScaleInStepID, Kind: completeScaleInTaskKind, Name: "Finalize the member removal",
		Job: jobFinalizeScaleIn, Executor: workflowdomain.RunnerKindInternal, DependsOn: finalizeDeps,
		Targets:    []workflowdomain.ResourceReference{{Kind: "platform", ID: launch.Platform.ID}},
		Parameters: map[string]any{"serverIds": slices.Clone(leaving)},
	})
	jobs := compiled.Jobs
	for _, job := range declareJobs(steps, workflowdomain.RetryTask) {
		if !slices.ContainsFunc(jobs, func(declared workflowdomain.JobDeclaration) bool { return declared.Name == job.Name }) {
			jobs = append(jobs, job)
		}
	}
	targets := append(slices.Clone(remaining), leaving...)
	site := ""
	if launch.Platform != nil {
		site = launch.Platform.SiteID
	}
	return &preparedPlatformDeployment{workflow: workflowapp.CreateWorkflowInput{
		ID: operationID, Kind: scaleInSlurmKind,
		IntentSummary:  fmt.Sprintf("Remove %d members from Slurm Platform %s", len(leaving), launch.Platform.Name),
		IntentSnapshot: map[string]any{"platformName": launch.Platform.Name, "leaving": slices.Clone(leaving), "extraVars": extra},
		Definition:     "platform/slurm/" + scaleInTemplate, DefinitionVersion: template.Version,
		SiteID: site, PlatformID: launch.Platform.ID,
		TargetServerIDs: targets,
		TargetResources: []workflowdomain.ResourceReference{{Kind: "platform", ID: launch.Platform.ID}},
		Steps:           steps, Jobs: jobs,
		RequestedBy: launch.RequestedBy, RequestCorrelation: launch.RequestCorrelation,
		StepSecretValues: map[string]map[string]any{},
	}}, nil
}

// A Slurm reconfigure (decision 083) is the Slurm module's declarative reconfigure template: the
// controllers' configuration rendered again from what the compute nodes recorded and applied in
// place, without redeploying any member. A rescan is the same Workflow detecting its nodes again,
// followed by the gate on them.
const (
	reconfigureSlurmKind workflowdomain.WorkflowKind = "reconfigure-slurm"
	reconfigureTemplate                              = "slurm-reconfigure"
	// varRescanIDs names the compute nodes reconfigure-slurm detects again instead of reading their
	// record.
	varRescanIDs = "swallow_slurm_rescan_ids"
)

// prepareSlurmReconfigure plans a RealizeReconfigure or RealizeRescan launch: the reconfigure
// template over every member and, for a rescan, the slurm-acceptance gate on the rescanned members
// (launch.ChangedServerIDs). It installs nothing, so the Platform's features and their records are
// left as they are, and it is not a deploy, so the Platform stays active meanwhile.
func (l platformDeploymentLauncher) prepareSlurmReconfigure(ctx context.Context, launch platformdomain.DeploymentLaunch) (*preparedPlatformDeployment, error) {
	rescan := launch.Realization == platformdomain.RealizeRescan
	if rescan && len(launch.ChangedServerIDs) == 0 {
		return nil, fmt.Errorf("%w: a rescan needs the members to detect again", platformdomain.ErrInvalidDeployment)
	}
	if l.registry == nil {
		return nil, fmt.Errorf("%w: the reconfigure template is not available in this process", platformdomain.ErrInvalidDeployment)
	}
	template, ok := l.registry.Template(moduledomain.KindPlatform, slurm.ID, reconfigureTemplate)
	if !ok {
		return nil, fmt.Errorf("%w: the Slurm module declares no %s template", platformdomain.ErrInvalidDeployment, reconfigureTemplate)
	}
	operationID := launch.OperationID
	if operationID == "" {
		operationID = uuid.NewString()
	}
	compiled, err := template.Compile(l.catalog, workflowdomain.TemplateInput{Members: slices.Clone(launch.TargetServerIDs)})
	if err != nil {
		return nil, fmt.Errorf("plan the reconfigure: %w", err)
	}
	trusted := maps.Clone(launch.TrustedVars)
	trusted[varRescanIDs] = []string{}
	if rescan {
		trusted[varRescanIDs] = slices.Clone(launch.ChangedServerIDs)
	}
	summary := "Reconfigure Slurm Platform " + launch.Platform.Name
	if rescan {
		summary = fmt.Sprintf("Rescan %d nodes of Slurm Platform %s", len(launch.ChangedServerIDs), launch.Platform.Name)
	}
	steps := make([]workflowdomain.Task, 0, len(compiled.Tasks))
	var extra map[string]any
	site := ""
	for _, step := range compiled.Tasks {
		if step.Executor != workflowdomain.RunnerKindHostAutomation {
			steps = append(steps, step)
			continue
		}
		playbook, _ := step.Parameters["playbook"].(string)
		prepared, err := l.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
			Kind: string(reconfigureSlurmKind), PlaybookName: playbook,
			Intent:          step.Name + " on " + launch.Platform.Name,
			TargetServerIDs: serverIDsOf(step.Targets), PlatformID: launch.Platform.ID,
			TrustedVars: trusted, SecretVars: launch.SecretVars, RequestedBy: launch.RequestedBy,
		}, operationID, false)
		if err != nil {
			return nil, err
		}
		step.Kind = "ansible-playbook"
		step.Targets = prepared.Targets
		step.Parameters = map[string]any{"playbook": prepared.Playbook, "extraVars": prepared.ExtraVars}
		if extra == nil {
			extra, site = prepared.ExtraVars, prepared.SiteID
		}
		steps = append(steps, step)
	}
	jobs := compiled.Jobs
	var gate *composedGate
	if rescan {
		if gate, err = l.gate.compose(ctx, launch, operationID, slurmGateRequest(launch, site),
			[]string{steps[len(steps)-1].ID}, false); err != nil {
			return nil, err
		}
		steps = append(steps, gate.steps...)
		jobs = append(slices.Clone(jobs), gate.jobs...)
	}
	return &preparedPlatformDeployment{workflow: workflowapp.CreateWorkflowInput{
		ID: operationID, Kind: reconfigureSlurmKind,
		IntentSummary:  summary,
		IntentSnapshot: map[string]any{"platformName": launch.Platform.Name, "rescan": slices.Clone(launch.ChangedServerIDs), "extraVars": extra},
		Definition:     "platform/slurm/" + reconfigureTemplate, DefinitionVersion: template.Version,
		SiteID: site, PlatformID: launch.Platform.ID,
		TargetServerIDs: slices.Clone(launch.TargetServerIDs),
		TargetResources: []workflowdomain.ResourceReference{{Kind: "platform", ID: launch.Platform.ID}},
		Steps:           steps, Jobs: jobs,
		RequestedBy: launch.RequestedBy, RequestCorrelation: launch.RequestCorrelation,
		StepSecretValues: map[string]map[string]any{},
	}, gate: gate}, nil
}

// A Slurm scale-out (decision 083) is the Slurm module's declarative scale-out template, which joins
// the new members to the running cluster, placed between the ensure-os Job that installs their OS
// and the composed features and gate, which concern them alone.
const (
	scaleOutSlurmKind workflowdomain.WorkflowKind = "scale-out-slurm"
	scaleOutTemplate                              = "slurm-scale-out"
	// varScaleOutIDs names the members scale-out-slurm installs; it only reads the others.
	varScaleOutIDs = "swallow_slurm_scale_out_ids"
)

// prepareSlurmScaleOut plans the addition of launch.ChangedServerIDs to a deployed Slurm Platform:
// the ensure-os Job (it provisions only the members that need an OS and checks SSH on the others),
// the scale-out template's join Task over every member, the container runtime and monitoring feature
// on the new members (the deploy use case narrowed their lists and left the Platform's Grafana out),
// and the slurm-acceptance gate on the new members. It is not a deploy, so the Platform stays active
// meanwhile.
func (l platformDeploymentLauncher) prepareSlurmScaleOut(ctx context.Context, launch platformdomain.DeploymentLaunch) (*preparedPlatformDeployment, error) {
	if len(launch.ChangedServerIDs) == 0 {
		return nil, fmt.Errorf("%w: a scale-out needs members to add", platformdomain.ErrInvalidDeployment)
	}
	if l.registry == nil {
		return nil, fmt.Errorf("%w: the scale-out template is not available in this process", platformdomain.ErrInvalidDeployment)
	}
	template, ok := l.registry.Template(moduledomain.KindPlatform, slurm.ID, scaleOutTemplate)
	if !ok {
		return nil, fmt.Errorf("%w: the Slurm module declares no %s template", platformdomain.ErrInvalidDeployment, scaleOutTemplate)
	}
	provisionFirst := launch.MachinePreparation.Mode == platformdomain.MachinePreparationProvisionOS
	launch.TrustedVars = l.ntp.withCheckVars(launch.TrustedVars, launch.TimeSyncCheck)
	operationID := launch.OperationID
	if operationID == "" {
		operationID = uuid.NewString()
	}
	compiled, err := template.Compile(l.catalog, workflowdomain.TemplateInput{Members: slices.Clone(launch.TargetServerIDs)})
	if err != nil {
		return nil, fmt.Errorf("plan the scale-out: %w", err)
	}
	trusted := maps.Clone(launch.TrustedVars)
	trusted[varScaleOutIDs] = slices.Clone(launch.ChangedServerIDs)
	var joins []workflowdomain.Task
	var prepared *workflowapp.PreparedAnsibleStep
	for _, step := range compiled.Tasks {
		if step.Executor != workflowdomain.RunnerKindHostAutomation {
			joins = append(joins, step)
			continue
		}
		playbook, _ := step.Parameters["playbook"].(string)
		stepPrepared, err := l.operations.PrepareAnsibleStep(ctx, workflowapp.CreateExecutionInput{
			Kind: string(scaleOutSlurmKind), PlaybookName: playbook,
			Intent:          step.Name + " on " + launch.Platform.Name,
			TargetServerIDs: serverIDsOf(step.Targets), PlatformID: launch.Platform.ID,
			TrustedVars: trusted, SecretVars: launch.SecretVars, RequestedBy: launch.RequestedBy,
		}, operationID, provisionFirst)
		if err != nil {
			return nil, err
		}
		step.Kind = "ansible-playbook"
		step.Targets = stepPrepared.Targets
		step.Parameters = map[string]any{"playbook": stepPrepared.Playbook, "extraVars": stepPrepared.ExtraVars}
		if prepared == nil {
			prepared = stepPrepared
		}
		joins = append(joins, step)
	}
	if prepared == nil || len(joins) == 0 {
		return nil, fmt.Errorf("%w: the %s template runs no playbook", platformdomain.ErrInvalidDeployment, scaleOutTemplate)
	}
	ensure, err := l.buildEnsureOS(ctx, launch, operationID, provisionFirst, prepared)
	if err != nil {
		return nil, err
	}
	// The template's first Task waits for the new members' OS; the others keep the template's order.
	joins[0].DependsOn = append(slices.Clone(ensure.installDeps), joins[0].DependsOn...)
	last := joins[len(joins)-1].ID
	steps := append(slices.Clone(ensure.steps), joins...)

	composed, err := l.planSlurmComposition(ctx, launch, operationID, provisionFirst, last)
	if err != nil {
		return nil, err
	}
	if len(composed.steps) > 0 {
		last = composed.steps[len(composed.steps)-1].ID
	}
	gate, err := l.gate.compose(ctx, launch, operationID, slurmGateRequest(launch, prepared.SiteID), []string{last}, provisionFirst)
	if err != nil {
		return nil, err
	}
	steps = append(append(steps, composed.steps...), gate.steps...)

	summary := fmt.Sprintf("Add %d members to Slurm Platform %s", len(launch.ChangedServerIDs), launch.Platform.Name)
	workflow, err := l.deploymentWorkflowInput(ctx, operationID, scaleOutSlurmKind, summary, launch, prepared, steps, ensure)
	if err != nil {
		return nil, err
	}
	workflow.Definition, workflow.DefinitionVersion = "platform/slurm/"+scaleOutTemplate, template.Version
	workflow.IntentSnapshot["added"] = slices.Clone(launch.ChangedServerIDs)
	// deploymentWorkflowInput seals the deploy's secrets into its install Task; here the join Task
	// runs the Slurm playbook.
	if secrets, ok := workflow.StepSecretValues[installPlatformStepID]; ok {
		delete(workflow.StepSecretValues, installPlatformStepID)
		workflow.StepSecretValues[joins[0].ID] = secrets
	}
	workflow.Jobs = labelGateJobs(withTemplateJobs(workflow.Jobs, compiled.Jobs), gate)
	return &preparedPlatformDeployment{
		workflow: workflow, composed: composed, exporters: ensure.exporters, gate: gate,
		keptHardwareCounters: keptHardwareCounterNodes(launch),
	}, nil
}

// withTemplateJobs replaces the declarations of the Jobs a module template declares with the
// template's own, which carry its labels and retry granularity.
func withTemplateJobs(jobs, declared []workflowdomain.JobDeclaration) []workflowdomain.JobDeclaration {
	byName := map[string]workflowdomain.JobDeclaration{}
	for _, job := range declared {
		byName[job.Name] = job
	}
	for i, job := range jobs {
		if templateJob, ok := byName[job.Name]; ok {
			jobs[i] = templateJob
		}
	}
	return jobs
}
