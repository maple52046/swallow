package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

const (
	// PlatformResourceKind is the resource kind of a Platform in Change Plans and conditions.
	PlatformResourceKind = "platform"
	// PlatformControllerName is the controller that converges Platforms to their spec.
	PlatformControllerName = "platform"
)

var (
	// ErrPlatformNotAdopted means the Platform has no spec yet: the platform controller adopts it
	// from its deployment at its first observation.
	ErrPlatformNotAdopted = errors.New("platform has no spec yet")
	// ErrUnsupportedChange means a write asks for a non-destructive change swallow has no automation
	// for yet (desired-state-model §5.2).
	ErrUnsupportedChange = errors.New("swallow cannot carry out this platform change yet")
	// ErrPlatformRemoving means the Platform's uninstall has started, so its spec no longer changes.
	ErrPlatformRemoving = errors.New("platform is being removed")
)

// unsupportedBlocker is the blocker a Change Plan carries for a destructive action swallow has no
// automation for yet.
func unsupportedBlocker(action string) string {
	return "swallow cannot carry out " + action + " yet"
}

// PlatformSpecService writes Platform specs (desired-state-model §5.2). Creating a Platform and
// every destructive change propose a Change Plan in the transaction that writes the spec; a
// non-destructive change is written alone and the platform controller converges it. The Servers a
// spec names are claimed in the same transaction.
type PlatformSpecService struct {
	transactor store.Transactor
	platforms  platformdomain.PlatformRepository
	specs      platformdomain.PlatformSpecStore
	claims     serverdomain.PlatformClaimStore
	deploy     *DeployService
	records    *PlatformService
	plans      *convergence.ChangePlanService
	requester  convergence.Requester
	modules    map[platformdomain.PlatformType]platformdomain.PlatformModule
	now        func() time.Time
}

// NewPlatformSpecService wires the service. claims is required: the members a spec names are claimed,
// and members it drops before they were installed released, in the transaction that writes it.
// requester may be nil (tests): the controller then converges a non-destructive change at its next
// resync.
func NewPlatformSpecService(
	transactor store.Transactor,
	platforms platformdomain.PlatformRepository,
	specs platformdomain.PlatformSpecStore,
	claims serverdomain.PlatformClaimStore,
	deploy *DeployService,
	records *PlatformService,
	plans *convergence.ChangePlanService,
	requester convergence.Requester,
	modules map[platformdomain.PlatformType]platformdomain.PlatformModule,
) *PlatformSpecService {
	return &PlatformSpecService{
		transactor: transactor, platforms: platforms, specs: specs, claims: claims, deploy: deploy,
		records: records, plans: plans, requester: requester, modules: modules,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// CreatePlatformSpecInput is a new Platform declared by its spec.
type CreatePlatformSpecInput struct {
	SiteID      string
	Name        string
	Type        platformdomain.PlatformType
	Spec        platformdomain.PlatformSpec
	RequestedBy string
}

// SpecWriteResult is a written spec: the Platform as stored, and the Change Plan it waits on when
// the change is destructive.
type SpecWriteResult struct {
	Platform *platformdomain.Platform
	Plan     *convergence.ChangePlan
}

// Create validates a new Platform as a deployment would, then stores it at generation 1 with its
// members claimed and its install proposed as a Change Plan. Nothing touches a machine until the
// plan is approved.
func (s *PlatformSpecService) Create(ctx context.Context, input CreatePlatformSpecInput) (*SpecWriteResult, error) {
	module, err := s.module(input.Type)
	if err != nil {
		return nil, err
	}
	spec := input.Spec.Clone()
	spec.Desired = platformdomain.PlatformPresent
	deploy, err := s.deploy.inputFromSpec(module, input.SiteID, input.Name, spec)
	if err != nil {
		return nil, err
	}
	deploy.RequestedBy = input.RequestedBy
	platform, err := s.deploy.Preflight(ctx, deploy)
	if err != nil {
		return nil, err
	}
	changes, err := module.Changes(nil, spec)
	if err != nil {
		return nil, err
	}
	setSpec(platform, &spec)
	var plan *convergence.ChangePlan
	err = s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		if err := s.claim(ctx, platform.ID, spec); err != nil {
			return err
		}
		if err := s.platforms.Create(ctx, platform); err != nil {
			return err
		}
		plan, err = s.plans.Propose(ctx, platformRef(platform.ID), s.draft(platform.ID, platform.Generation, spec, changes, nil))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &SpecWriteResult{Platform: platform, Plan: plan}, nil
}

// UpdateSpec replaces a Platform's spec when expectedGeneration is still current
// (platformdomain.ErrGenerationConflict otherwise). A change to an immutable field is refused, as
// is a non-destructive change swallow cannot carry out yet; a destructive one is proposed as a
// Change Plan, blocked when swallow cannot carry it out yet.
func (s *PlatformSpecService) UpdateSpec(ctx context.Context, platformID string, expectedGeneration int64, desired platformdomain.PlatformSpec) (*SpecWriteResult, error) {
	platform, module, err := s.specified(ctx, platformID)
	if err != nil {
		return nil, err
	}
	if realizing := platform.Status.Realizing; realizing != nil && realizing.Spec.Desired == platformdomain.PlatformAbsent {
		return nil, ErrPlatformRemoving
	}
	if platform.Generation != expectedGeneration {
		return nil, platformdomain.ErrGenerationConflict
	}
	spec := desired.Clone()
	spec.Desired = platformdomain.PlatformPresent
	if stored := platform.Spec.MachinePreparation; spec.MachinePreparation.UserData == "" &&
		spec.MachinePreparation.UserDataMode != "" && spec.MachinePreparation.UserDataMode == stored.UserDataMode {
		// A spec is read without its user data, so writing it back keeps the stored value.
		spec.MachinePreparation.UserData = stored.UserData
	}
	changes, err := module.Changes(platform.Status.Applied, spec)
	if err != nil {
		return nil, err
	}
	blocking, refusing := changes.UnsupportedDestructive()
	if len(refusing) > 0 {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedChange, refusing)
	}
	deploy, err := s.deploy.inputFromSpec(module, platform.SiteID, platform.Name, spec)
	if err != nil {
		return nil, err
	}
	deploy.PlatformID = platform.ID
	if _, err := s.deploy.Preflight(ctx, deploy); err != nil {
		return nil, err
	}
	return s.write(ctx, platform, expectedGeneration, spec, changes, blocking)
}

// Delete declares a Platform absent, removed as removal says. A Platform never deployed (its install
// still waits for approval) is deleted at once, since no machine was touched; otherwise its
// uninstall is proposed as a Change Plan, and the record is deleted once the uninstall succeeds.
// Deleting a Platform already marked absent returns its waiting plan.
func (s *PlatformSpecService) Delete(ctx context.Context, platformID string, removal *platformdomain.PlatformRemoval) (*SpecWriteResult, error) {
	platform, module, err := s.specified(ctx, platformID)
	if err != nil {
		return nil, err
	}
	if platform.Status.Applied == nil {
		if err := s.withdraw(ctx, platform.ID, "the Platform was deleted before it was installed"); err != nil {
			return nil, err
		}
		return &SpecWriteResult{}, s.records.Delete(ctx, platform.ID)
	}
	if platform.Spec.Desired == platformdomain.PlatformAbsent {
		plan, err := s.plans.List(ctx, convergence.ChangePlanFilter{
			Resource: platformRef(platform.ID), States: []convergence.ChangePlanState{convergence.PlanPendingApproval, convergence.PlanApproved},
		})
		if err != nil {
			return nil, err
		}
		result := &SpecWriteResult{Platform: platform}
		if len(plan) > 0 {
			result.Plan = plan[0]
		}
		return result, nil
	}
	spec := platform.Spec.Clone()
	spec.Desired, spec.Removal = platformdomain.PlatformAbsent, removal
	changes, err := module.Changes(platform.Status.Applied, spec)
	if err != nil {
		return nil, err
	}
	return s.write(ctx, platform, platform.Generation, spec, changes, nil)
}

// write stores a validated spec change: the spec, claims for members it adds, and a Change Plan when
// the change is destructive; a non-destructive change instead withdraws an earlier plan and wakes
// the controller.
func (s *PlatformSpecService) write(
	ctx context.Context,
	platform *platformdomain.Platform,
	expectedGeneration int64,
	spec platformdomain.PlatformSpec,
	changes platformdomain.PlatformChanges,
	blocking []string,
) (*SpecWriteResult, error) {
	destructive := changes.Destructive()
	var plan *convergence.ChangePlan
	var generation int64
	err := s.transactor.InTransaction(ctx, func(ctx context.Context) error {
		var err error
		if generation, err = s.specs.WriteSpec(ctx, platform.ID, expectedGeneration, spec); err != nil {
			return err
		}
		if spec.Desired == platformdomain.PlatformPresent {
			if err := s.claim(ctx, platform.ID, spec); err != nil {
				return err
			}
			if err := s.releaseDropped(ctx, platform, spec); err != nil {
				return err
			}
		}
		if !destructive {
			return s.withdraw(ctx, platform.ID, fmt.Sprintf("generation %d needs no approval", generation))
		}
		plan, err = s.plans.Propose(ctx, platformRef(platform.ID), s.draft(platform.ID, generation, spec, changes, blocking))
		return err
	})
	if err != nil {
		return nil, err
	}
	platform.Spec, platform.Generation = &spec, generation
	if !destructive && s.requester != nil {
		if err := s.requester.Request(ctx, PlatformControllerName); err != nil {
			// The controller still converges it at its next resync.
			slog.Warn("request platform convergence after a spec write", "platformId", platform.ID, "error", err)
		}
	}
	return &SpecWriteResult{Platform: platform, Plan: plan}, nil
}

// releaseDropped releases the members the previous spec named that neither the new spec nor the
// applied spec does: they were added and dropped again before any Workflow installed them, so
// nothing on them belongs to the Platform. An applied member keeps its claim until the change that
// removes it is carried out.
func (s *PlatformSpecService) releaseDropped(ctx context.Context, platform *platformdomain.Platform, spec platformdomain.PlatformSpec) error {
	if platform.Spec == nil {
		return nil
	}
	var dropped []string
	for _, member := range platform.Spec.Members {
		if _, kept := spec.Member(member.ServerID); kept {
			continue
		}
		if applied := platform.Status.Applied; applied != nil {
			if _, installed := applied.Member(member.ServerID); installed {
				continue
			}
		}
		if realizing := platform.Status.Realizing; realizing != nil {
			// A Workflow may be installing it right now; the controller releases it once it is not.
			if _, installing := realizing.Spec.Member(member.ServerID); installing {
				continue
			}
		}
		dropped = append(dropped, member.ServerID)
	}
	if len(dropped) == 0 {
		return nil
	}
	return s.claims.ReleaseServerClaims(ctx, platform.ID, dropped)
}

// claim claims every member of spec for the Platform; a member it already claims is left as it is.
// Members a spec drops keep their claim until the change that removes them is carried out, unless
// they were never installed (releaseDropped).
func (s *PlatformSpecService) claim(ctx context.Context, platformID string, spec platformdomain.PlatformSpec) error {
	if len(spec.Members) == 0 {
		return nil
	}
	return s.claims.ClaimForPlatform(ctx, platformID, spec.MemberIDs(), s.now())
}

func (s *PlatformSpecService) withdraw(ctx context.Context, platformID, reason string) error {
	return s.plans.Withdraw(ctx, platformRef(platformID), reason)
}

func (s *PlatformSpecService) draft(platformID string, generation int64, spec platformdomain.PlatformSpec, changes platformdomain.PlatformChanges, blocking []string) convergence.PlanDraft {
	return platformDraft(platformID, generation, s.now(), spec, changes, blocking)
}

func (s *PlatformSpecService) module(platformType platformdomain.PlatformType) (platformdomain.PlatformModule, error) {
	module, ok := s.modules[platformType]
	if !ok {
		return nil, fmt.Errorf("%w: %q", platformdomain.ErrUnsupportedPlatformType, platformType)
	}
	return module, nil
}

// specified loads a Platform that has a spec, with its type's module.
func (s *PlatformSpecService) specified(ctx context.Context, platformID string) (*platformdomain.Platform, platformdomain.PlatformModule, error) {
	platform, err := s.platforms.FindByID(ctx, platformID)
	if err != nil {
		return nil, nil, err
	}
	if platform.Spec == nil {
		return nil, nil, ErrPlatformNotAdopted
	}
	module, err := s.module(platform.Type)
	if err != nil {
		return nil, nil, err
	}
	return platform, module, nil
}

// platformDraft turns classified changes into a plan draft, and the destructive actions swallow
// cannot carry out yet into blockers. A module names a Platform target without its id, which only
// the core knows, so each such target is given the Platform's id.
func platformDraft(platformID string, generation int64, observedAt time.Time, spec platformdomain.PlatformSpec, changes platformdomain.PlatformChanges, blocking []string) convergence.PlanDraft {
	draft := convergence.PlanDraft{Generation: generation, ObservedAt: observedAt}
	if removal := spec.Removal; spec.Desired == platformdomain.PlatformAbsent && removal != nil && removal.ReleaseServers {
		draft.Effects = append(draft.Effects, releaseEffect(len(spec.Members), removal.ReleaseOptions))
	}
	if removal := spec.Removal; spec.Desired == platformdomain.PlatformAbsent && removal != nil && removal.CancelActiveWorkflows {
		draft.Effects = append(draft.Effects, "unfinished Workflows holding the members are canceled first")
	}
	for _, action := range changes.Actions {
		if action.Target.Kind == PlatformResourceKind && action.Target.ID == "" {
			action.Target.ID = platformID
		}
		draft.Actions = append(draft.Actions, action)
	}
	if blocking == nil {
		blocking, _ = changes.UnsupportedDestructive()
	}
	for _, action := range blocking {
		draft.Blockers = append(draft.Blockers, unsupportedBlocker(action))
	}
	return draft
}

// releaseEffect is the effect an uninstall that releases its members carries, written for the admin
// who approves it: how many members go back to the provisioner, that releasing removes their
// operating system, whether their disks are erased and their static IP bindings removed, and that the
// Platform record goes once the release succeeds (an approved uninstall always deletes the record).
// members is the number of members the spec names. Recomputing a plan compares actions, not effects,
// so rewording this text never supersedes an approved plan.
func releaseEffect(members int, options platformdomain.ServerReleaseOptions) string {
	subject, their, systems := fmt.Sprintf("%d members are", members), "their", "operating systems"
	if members == 1 {
		subject, their, systems = "1 member is", "its", "operating system"
	}
	disks := "are not erased"
	if options.Erase || options.SecureErase || options.QuickErase {
		disks = "are erased"
	}
	bindings := "are kept"
	if options.UnbindStaticIPs {
		bindings = "are removed"
	}
	return fmt.Sprintf("%s released to the provisioner, which removes %s %s; %s disks %s and %s static IP bindings %s. "+
		"The Platform record is deleted once the release succeeds.", subject, their, systems, their, disks, their, bindings)
}

func platformRef(platformID string) convergence.ResourceRef {
	return convergence.ResourceRef{Kind: PlatformResourceKind, ID: platformID}
}

// DeployRequestReader reads a deploy request body into the deploy input; the delivery layer owns the
// request format, which a Platform's spec is rendered in.
type DeployRequestReader func(raw []byte) (DeployPlatformInput, error)

// inputFromSpec renders a spec as the deployment it stands for: the type's module writes the spec as
// a deploy request body, which is read as any deploy request is.
func (s *DeployService) inputFromSpec(module platformdomain.PlatformModule, siteID, name string, spec platformdomain.PlatformSpec) (DeployPlatformInput, error) {
	body := module.DeployRequest(spec)
	body["siteId"], body["name"], body["type"] = siteID, name, string(module.PlatformType())
	raw, err := json.Marshal(body)
	if err != nil {
		return DeployPlatformInput{}, fmt.Errorf("%w: %v", platformdomain.ErrInvalidSpec, err)
	}
	input, err := s.readRequest(raw)
	if err != nil {
		return DeployPlatformInput{}, fmt.Errorf("%w: %v", platformdomain.ErrInvalidSpec, err)
	}
	input.MachinePreparation = spec.MachinePreparation
	return input, nil
}

// PlatformPlanner computes and carries out Platform Change Plans (decision 076): it classifies the
// spec against what was last applied, and an approved plan becomes the deploy, uninstall, or GPU
// User Workflow the type's module names.
type PlatformPlanner struct {
	platforms    platformdomain.PlatformRepository
	lifecycle    platformdomain.LifecycleReader
	deploy       *DeployService
	uninstall    *UninstallService
	gpuOperators *GPUOperatorService
	modules      map[platformdomain.PlatformType]platformdomain.PlatformModule
}

// NewPlatformPlanner wires the planner. gpuOperators may be nil, which leaves a GPU Operator change
// unapplied.
func NewPlatformPlanner(
	platforms platformdomain.PlatformRepository,
	lifecycle platformdomain.LifecycleReader,
	deploy *DeployService,
	uninstall *UninstallService,
	gpuOperators *GPUOperatorService,
	modules map[platformdomain.PlatformType]platformdomain.PlatformModule,
) *PlatformPlanner {
	return &PlatformPlanner{
		platforms: platforms, lifecycle: lifecycle, deploy: deploy, uninstall: uninstall,
		gpuOperators: gpuOperators, modules: modules,
	}
}

// ResourceKind is "platform".
func (p *PlatformPlanner) ResourceKind() string { return PlatformResourceKind }

// Controller is the platform controller.
func (p *PlatformPlanner) Controller() string { return PlatformControllerName }

// Plan classifies the Platform's spec against its applied spec.
func (p *PlatformPlanner) Plan(ctx context.Context, platformID string) (convergence.PlanDraft, error) {
	platform, changes, err := p.changes(ctx, platformID)
	if err != nil {
		return convergence.PlanDraft{}, err
	}
	return platformDraft(platform.ID, platform.Generation, time.Now().UTC(), *platform.Spec, changes, nil), nil
}

// Held reports whether a deploy or uninstall Workflow of the Platform is still running; a removal
// that cancels unfinished Workflows does not wait for a deployment.
func (p *PlatformPlanner) Held(ctx context.Context, platformID string) (bool, error) {
	if platform, err := p.platforms.FindByID(ctx, platformID); err == nil && platform.Spec != nil &&
		platform.Spec.Desired == platformdomain.PlatformAbsent && platform.Spec.Removal != nil && platform.Spec.Removal.CancelActiveWorkflows {
		snapshots, err := p.lifecycle.Read(ctx, []string{platformID})
		if err != nil {
			return false, err
		}
		return snapshots[platformID].State == platformdomain.PlatformLifecycleUninstalling, nil
	}
	snapshots, err := p.lifecycle.Read(ctx, []string{platformID})
	if err != nil {
		return false, err
	}
	state := snapshots[platformID].State
	return state == platformdomain.PlatformLifecycleDeploying || state == platformdomain.PlatformLifecycleUninstalling, nil
}

// PrepareApply plans the Workflow the approved plan becomes. A deployment is planned here, reading
// providers; an uninstall or GPU Operator change is validated and launched by PersistApply.
func (p *PlatformPlanner) PrepareApply(ctx context.Context, plan *convergence.ChangePlan) (convergence.PreparedApply, error) {
	platform, changes, err := p.changes(ctx, plan.Resource.ID)
	if err != nil {
		return nil, err
	}
	requestedBy := plan.DecidedBy
	switch changes.Realization {
	case platformdomain.RealizeUninstall:
		input := &UninstallPlatformInput{PlatformID: platform.ID, RequestedBy: requestedBy, DeleteRecord: true}
		if removal := platform.Spec.Removal; removal != nil {
			input.ReleaseServers, input.ReleaseOptions, input.CancelActiveWorkflows = removal.ReleaseServers, removal.ReleaseOptions, removal.CancelActiveWorkflows
		}
		return &platformApply{uninstall: input}, nil
	case platformdomain.RealizeDeploy:
		prepared, err := PrepareSpecDeployment(ctx, p.deploy, p.modules[platform.Type], platform, requestedBy)
		if err != nil {
			return nil, err
		}
		return &platformApply{deploy: prepared}, nil
	case platformdomain.RealizeGPUOperator:
		input, err := gpuOperatorInput(p.deploy, p.modules[platform.Type], platform, requestedBy)
		if err != nil {
			return nil, err
		}
		return &platformApply{gpuOperator: input}, nil
	default:
		return nil, fmt.Errorf("%w: the plan needs no Workflow", convergence.ErrChangePlanState)
	}
}

// PersistApply writes the Workflow inside the transaction that marks the plan applied.
func (p *PlatformPlanner) PersistApply(ctx context.Context, prepared convergence.PreparedApply) (string, error) {
	apply, ok := prepared.(*platformApply)
	if !ok {
		return "", fmt.Errorf("platform planner cannot persist %T", prepared)
	}
	switch {
	case apply.deploy != nil:
		return p.deploy.Persist(ctx, apply.deploy)
	case apply.uninstall != nil:
		result, err := p.uninstall.Uninstall(ctx, *apply.uninstall)
		if err != nil {
			return "", err
		}
		return result.OperationID, nil
	case apply.gpuOperator != nil && p.gpuOperators != nil:
		result, err := p.gpuOperators.Configure(ctx, *apply.gpuOperator)
		if err != nil {
			return "", err
		}
		return result.WorkflowID, nil
	default:
		return "", errors.New("GPU Operator changes are unavailable")
	}
}

func (p *PlatformPlanner) changes(ctx context.Context, platformID string) (*platformdomain.Platform, platformdomain.PlatformChanges, error) {
	platform, err := p.platforms.FindByID(ctx, platformID)
	if err != nil {
		return nil, platformdomain.PlatformChanges{}, err
	}
	module, ok := p.modules[platform.Type]
	if !ok || platform.Spec == nil {
		return nil, platformdomain.PlatformChanges{}, ErrPlatformNotAdopted
	}
	changes, err := module.Changes(platform.Status.Applied, *platform.Spec)
	return platform, changes, err
}

// platformApply is a planned Platform Workflow: exactly one field is set.
type platformApply struct {
	deploy      *PreparedDeploy
	uninstall   *UninstallPlatformInput
	gpuOperator *ConfigureGPUOperatorInput
}

// WorkflowID is the deploy Workflow's preassigned id; the other Workflows get theirs when written.
func (a *platformApply) WorkflowID() string {
	if a.deploy != nil {
		return a.deploy.WorkflowID()
	}
	return ""
}

// PrepareSpecDeployment plans the deploy Workflow of a Platform's current spec onto the stored
// Platform. The deploy is convergent: members already in the Platform are left as they are.
func PrepareSpecDeployment(
	ctx context.Context,
	deploy *DeployService,
	module platformdomain.PlatformModule,
	platform *platformdomain.Platform,
	requestedBy string,
) (*PreparedDeploy, error) {
	if module == nil || platform.Spec == nil {
		return nil, ErrPlatformNotAdopted
	}
	input, err := deploy.inputFromSpec(module, platform.SiteID, platform.Name, *platform.Spec)
	if err != nil {
		return nil, err
	}
	input.PlatformID, input.RequestedBy = platform.ID, requestedBy
	return deploy.Prepare(ctx, input)
}

// gpuOperatorInput is the GPU Operator change the spec's gpuOperator setting asks for; an omitted
// setting is the uninstall intent.
func gpuOperatorInput(deploy *DeployService, module platformdomain.PlatformModule, platform *platformdomain.Platform, requestedBy string) (*ConfigureGPUOperatorInput, error) {
	if module == nil || platform.Spec == nil {
		return nil, ErrPlatformNotAdopted
	}
	input, err := deploy.inputFromSpec(module, platform.SiteID, platform.Name, *platform.Spec)
	if err != nil {
		return nil, err
	}
	settings := platformdomain.GPUOperatorSettings{}
	if input.GPUOperator != nil {
		settings = *input.GPUOperator
	}
	return &ConfigureGPUOperatorInput{PlatformID: platform.ID, Settings: settings, RequestedBy: requestedBy}, nil
}
