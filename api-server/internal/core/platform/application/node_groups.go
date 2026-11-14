package application

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/maple52046/swallow/internal/core/convergence"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// templateResourceKind is the Change Plan target kind of a Deployment Template a Platform creates.
const templateResourceKind = "deployment-template"

// memberTemplates is how a deployment's members are installed: each member's Deployment Template
// (by server id), the templates that exist, and the groups they came from.
type memberTemplates struct {
	byServer map[string]string
	intents  map[string]*platformdomain.DeploymentTemplateIntent
	groups   []platformdomain.NodeGroup
}

// resolveMemberTemplates gives every member of a node group whose template exists that template as
// its member-level reference (MachineNetworkAssignment.TemplateID), unless its assignment already
// names one, so the launcher installs it from the template and keeps only its network from the
// machine preparation (decision 083). A group whose template is one the Platform generates and has not
// created yet installs from the machine preparation, which holds the same content. A group naming a
// template that does not exist is ErrInvalidDeployment.
func (s *DeployService) resolveMemberTemplates(ctx context.Context, input DeployPlatformInput, preparation platformdomain.MachinePreparation) (platformdomain.MachinePreparation, memberTemplates, error) {
	resolved := memberTemplates{byServer: map[string]string{}, intents: map[string]*platformdomain.DeploymentTemplateIntent{}}
	grouped, ok := input.module.(platformdomain.NodeGroupModule)
	if !ok || input.spec == nil {
		return preparation, resolved, nil
	}
	resolved.groups = grouped.NodeGroups(*input.spec)
	lookup := func(id string) (*platformdomain.DeploymentTemplateIntent, error) {
		if intent, seen := resolved.intents[id]; seen {
			return intent, nil
		}
		intent, err := s.templates.Find(ctx, id)
		if err != nil {
			return nil, err
		}
		resolved.intents[id] = intent
		return intent, nil
	}
	for _, assignment := range preparation.Assignments {
		if assignment.TemplateID != "" {
			intent, err := lookup(assignment.TemplateID)
			if err != nil {
				return preparation, resolved, err
			}
			if intent == nil {
				return preparation, resolved, fmt.Errorf("%w: server %s names Deployment Template %s, which does not exist",
					platformdomain.ErrInvalidDeployment, assignment.ServerID, assignment.TemplateID)
			}
			resolved.byServer[assignment.ServerID] = assignment.TemplateID
		}
	}
	// A Server in several groups (a controller that also computes) is installed from its compute
	// group's template, the group that scales.
	ordered := slices.Clone(resolved.groups)
	slices.SortStableFunc(ordered, func(a, b platformdomain.NodeGroup) int {
		return boolOrder(a.Role != "compute") - boolOrder(b.Role != "compute")
	})
	for _, group := range ordered {
		if group.TemplateID == "" {
			continue
		}
		intent, err := lookup(group.TemplateID)
		if err != nil {
			return preparation, resolved, err
		}
		if intent == nil {
			if _, generated := preparation.Generated(group.TemplateID); generated {
				continue
			}
			return preparation, resolved, fmt.Errorf("%w: node group %s names Deployment Template %s, which does not exist",
				platformdomain.ErrInvalidDeployment, group.Name, group.TemplateID)
		}
		for _, id := range group.Members {
			if _, set := resolved.byServer[id]; !set {
				resolved.byServer[id] = group.TemplateID
			}
		}
	}
	out := preparation
	out.Assignments = slices.Clone(preparation.Assignments)
	for _, member := range input.spec.Members {
		templateID := resolved.byServer[member.ServerID]
		if templateID == "" {
			continue
		}
		if index := slices.IndexFunc(out.Assignments, func(a platformdomain.MachineNetworkAssignment) bool { return a.ServerID == member.ServerID }); index >= 0 {
			out.Assignments[index].TemplateID = templateID
		} else {
			out.Assignments = append(out.Assignments, platformdomain.MachineNetworkAssignment{ServerID: member.ServerID, TemplateID: templateID})
		}
	}
	return out, resolved, nil
}

func boolOrder(value bool) int {
	if value {
		return 1
	}
	return 0
}

// checkMemberTemplates checks the members against their templates once the targets are loaded:
// every member of a group belongs to its template's provisioner Integration, and each member this
// change adds (changed) meets its template's hardware floor; a compute group mixing GPU models is a
// warning, unless its template names a model, whose floor then refuses the odd ones. Every refusal
// is ErrInvalidDeployment naming the Server and what it lacks (fail-closed on missing inventory).
func (s *DeployService) checkMemberTemplates(resolved memberTemplates, targets []*serverdomain.Server, changed []string) ([]string, error) {
	byID := make(map[string]*serverdomain.Server, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}
	for id, templateID := range resolved.byServer {
		intent, server := resolved.intents[templateID], byID[id]
		if intent == nil || server == nil {
			continue
		}
		if server.Source.IntegrationID != intent.IntegrationID {
			return nil, fmt.Errorf("%w: server %s belongs to another provisioner Integration than Deployment Template %s",
				platformdomain.ErrInvalidDeployment, server.DisplayName(), intent.Name)
		}
		if intent.Minimum != nil && slices.Contains(changed, id) {
			if shortfalls := templateShortfalls(server, *intent.Minimum, s.accelerator); len(shortfalls) > 0 {
				return nil, fmt.Errorf("%w: server %s does not meet the minimum hardware of Deployment Template %s: %s",
					platformdomain.ErrInvalidDeployment, server.DisplayName(), intent.Name, strings.Join(shortfalls, "; "))
			}
		}
	}
	var warnings []string
	for _, group := range resolved.groups {
		if group.Role != "compute" {
			continue
		}
		intent := resolved.intents[group.TemplateID]
		if intent != nil && intent.Minimum != nil && intent.Minimum.GPUModel != "" {
			continue
		}
		models := map[string][]string{}
		for _, id := range group.Members {
			server := byID[id]
			if server == nil {
				continue
			}
			model := computeGPUModel(server, s.accelerator)
			models[model] = append(models[model], server.DisplayName())
		}
		if len(models) > 1 {
			parts := make([]string, 0, len(models))
			for model, servers := range models {
				if model == "" {
					model = "no model"
				}
				parts = append(parts, fmt.Sprintf("%s (%s)", model, strings.Join(servers, ", ")))
			}
			slices.Sort(parts)
			warnings = append(warnings, fmt.Sprintf("node group %s mixes GPU models: %s; set its template's GPU model to keep it to one",
				group.Name, strings.Join(parts, "; ")))
		}
	}
	return warnings, nil
}

// templateShortfalls lists what server lacks against minimum. A dimension the inventory does not
// report counts as not met.
func templateShortfalls(server *serverdomain.Server, minimum platformdomain.TemplateMinimum, accelerator platformdomain.Accelerator) []string {
	actual := server.Observed
	var shortfalls []string
	if minimum.CPUCores > 0 && actual.CPUCores < minimum.CPUCores {
		shortfalls = append(shortfalls, fmt.Sprintf("CPU %d cores (minimum %d)", actual.CPUCores, minimum.CPUCores))
	}
	if minimum.MemoryMiB > 0 && actual.MemoryMiB < minimum.MemoryMiB {
		shortfalls = append(shortfalls, fmt.Sprintf("memory %d MiB (minimum %d MiB)", actual.MemoryMiB, minimum.MemoryMiB))
	}
	if minimum.StorageGB > 0 && actual.StorageGB < float64(minimum.StorageGB) {
		shortfalls = append(shortfalls, fmt.Sprintf("storage %.0f GB (minimum %d GB)", actual.StorageGB, minimum.StorageGB))
	}
	count := computeGPUCount(server, accelerator)
	if minimum.GPUCount > 0 && count < minimum.GPUCount {
		shortfalls = append(shortfalls, fmt.Sprintf("%d compute GPUs (minimum %d)", count, minimum.GPUCount))
	}
	if want := strings.ToLower(strings.TrimSpace(minimum.GPUModel)); want != "" {
		model := strings.ToLower(computeGPUModel(server, accelerator))
		switch {
		case model == "":
			shortfalls = append(shortfalls, fmt.Sprintf("no GPU model in its inventory (template wants %s)", minimum.GPUModel))
		case !strings.Contains(model, want):
			shortfalls = append(shortfalls, fmt.Sprintf("GPU model %s (template wants %s)", computeGPUModel(server, accelerator), minimum.GPUModel))
		}
	}
	return shortfalls
}

// computeGPUCount counts the Accelerator's compute GPUs in server's inventory.
func computeGPUCount(server *serverdomain.Server, accelerator platformdomain.Accelerator) int {
	count := 0
	for _, gpu := range server.Observed.GPUs {
		if accelerator != nil && accelerator.IsCompute(gpu) {
			count += gpu.Count
		}
	}
	return count
}

// computeGPUModel is the model name of server's first compute GPU group, "" when it has none or
// the inventory names none.
func computeGPUModel(server *serverdomain.Server, accelerator platformdomain.Accelerator) string {
	for _, gpu := range server.Observed.GPUs {
		if accelerator != nil && accelerator.IsCompute(gpu) {
			return strings.TrimSpace(gpu.Model)
		}
	}
	return ""
}

// topologyTargets describes the targets to the module's topology planner.
func topologyTargets(targets []*serverdomain.Server, accelerator platformdomain.Accelerator) []platformdomain.TopologyTarget {
	out := make([]platformdomain.TopologyTarget, 0, len(targets))
	for _, server := range targets {
		target := platformdomain.TopologyTarget{
			ServerID: server.ID, ComputeGPUs: computeGPUCount(server, accelerator),
			CPUCores: server.Observed.CPUCores, MemoryMiB: server.Observed.MemoryMiB,
		}
		for _, gpu := range server.Observed.GPUs {
			if accelerator != nil && accelerator.IsCompute(gpu) && strings.TrimSpace(gpu.Model) != "" {
				target.GPUModels = append(target.GPUModels, strings.TrimSpace(gpu.Model))
			}
		}
		out = append(out, target)
	}
	return out
}

// memberTemplateRecords are the records a deployment writes for the members it installs: each Server
// it provisions from an existing template, with the template's current digest.
func memberTemplateRecords(resolved memberTemplates, provisioned []string, now time.Time) []platformdomain.MemberTemplateRecord {
	var records []platformdomain.MemberTemplateRecord
	for _, id := range provisioned {
		templateID := resolved.byServer[id]
		intent := resolved.intents[templateID]
		if intent == nil {
			// Installed from the machine preparation: a record from an earlier install is no longer true.
			records = append(records, platformdomain.MemberTemplateRecord{ServerID: id})
			continue
		}
		records = append(records, platformdomain.MemberTemplateRecord{ServerID: id, TemplateID: templateID, Digest: intent.Digest, DeployedAt: now})
	}
	return records
}

// assignGeneratedTemplates gives every node group without a template one, before the spec is written
// (decision 083): the Platform's machine preparation template when it names one, otherwise a template the
// Platform generates from the machine preparation, named <platform>-<group> in the provisioner
// Integration of the group's members (a suffix keeps the name unique there). Only a provision_os
// spec installs members, so only it gets templates. Members of one group in different Integrations
// are ErrInvalidDeployment.
func (s *DeployService) assignGeneratedTemplates(ctx context.Context, module platformdomain.PlatformModule, platformName string, spec platformdomain.PlatformSpec) (platformdomain.PlatformSpec, error) {
	grouped, ok := module.(platformdomain.NodeGroupModule)
	if !ok || spec.MachinePreparation.Mode != platformdomain.MachinePreparationProvisionOS {
		return spec, nil
	}
	assigned := map[string]string{}
	for _, group := range grouped.NodeGroups(spec) {
		if group.TemplateID != "" {
			continue
		}
		if existing := spec.MachinePreparation.TemplateID; existing != "" {
			assigned[group.Name] = existing
			continue
		}
		integrationID := ""
		for _, id := range group.Members {
			server, err := s.servers.FindByID(ctx, id)
			if err != nil {
				return spec, err
			}
			switch {
			case integrationID == "":
				integrationID = server.Source.IntegrationID
			case server.Source.IntegrationID != integrationID:
				return spec, fmt.Errorf("%w: node group %s has members in different provisioner Integrations; one template cannot install them",
					platformdomain.ErrInvalidDeployment, group.Name)
			}
		}
		if integrationID == "" {
			continue
		}
		id := uuid.NewString()
		name := platformName + "-" + group.Name
		taken, err := s.templates.NameTaken(ctx, integrationID, name, id)
		if err != nil {
			return spec, err
		}
		if taken {
			name = name + "-" + id[:8]
		}
		spec.MachinePreparation.GeneratedTemplates = append(spec.MachinePreparation.GeneratedTemplates,
			platformdomain.GeneratedTemplate{ID: id, Name: name, IntegrationID: integrationID, Group: group.Name})
		assigned[group.Name] = id
	}
	if len(assigned) == 0 {
		return spec, nil
	}
	return grouped.WithGroupTemplates(spec, assigned), nil
}

// templateChanges adds a create-template action for each template the spec generates that does not
// exist yet, and, when the Platform is already applied, makes the change wait for a Change Plan so
// the templates are not created without anyone seeing them (decision 083).
func templateChanges(ctx context.Context, templates platformdomain.DeploymentTemplates, applied bool, spec platformdomain.PlatformSpec, changes *platformdomain.PlatformChanges) error {
	if spec.Desired != platformdomain.PlatformPresent {
		return nil
	}
	for _, generated := range spec.MachinePreparation.GeneratedTemplates {
		intent, err := templates.Find(ctx, generated.ID)
		if err != nil {
			return err
		}
		if intent != nil {
			continue
		}
		changes.Actions = append(changes.Actions, convergence.PlannedAction{
			Target: convergence.ResourceRef{Kind: templateResourceKind, ID: generated.ID}, Action: "create-template", To: generated.Name,
			Reason: fmt.Sprintf("node group %s installs its new members from this template, created from the machine preparation", generated.Group),
		})
		if applied {
			changes.NeedsApproval = true
		}
	}
	return nil
}

// ensureGeneratedTemplates creates the templates spec generates that do not exist yet; it runs in
// the transaction that accepts the realization.
func ensureGeneratedTemplates(ctx context.Context, templates platformdomain.DeploymentTemplates, spec *platformdomain.PlatformSpec) error {
	if spec == nil || len(spec.MachinePreparation.GeneratedTemplates) == 0 {
		return nil
	}
	return templates.EnsureGenerated(ctx, spec.MachinePreparation.GeneratedTemplates, spec.MachinePreparation)
}

// addedMembers lists the members desired has that applied does not, in desired order; with no
// applied spec every member is added. The result is never nil for an applied spec, so "none added"
// stays distinct from "not known" (nil, every member).
func addedMembers(applied *platformdomain.PlatformSpec, desired platformdomain.PlatformSpec) []string {
	ids := []string{}
	for _, member := range desired.Members {
		if applied == nil {
			ids = append(ids, member.ServerID)
			continue
		}
		if _, ok := applied.Member(member.ServerID); !ok {
			ids = append(ids, member.ServerID)
		}
	}
	return ids
}

// removedMembers lists the members applied has that desired drops, with their removal choices.
func removedMembers(applied *platformdomain.PlatformSpec, desired platformdomain.PlatformSpec) []platformdomain.MemberRemoval {
	if applied == nil {
		return nil
	}
	var removals []platformdomain.MemberRemoval
	for _, member := range applied.Members {
		if _, kept := desired.Member(member.ServerID); !kept {
			removals = append(removals, desired.MemberRemoval(member.ServerID))
		}
	}
	return removals
}

// queueBlockers blocks removing a scheduling queue (a Slurm partition) that still has work queued
// or running, naming the count, read live through the Platform's reader. A count that cannot be
// read blocks too: swallow does not guess that a queue is empty.
func (s *DeployService) queueBlockers(ctx context.Context, platform *platformdomain.Platform, changes *platformdomain.PlatformChanges) error {
	var queues []string
	for _, action := range changes.Actions {
		if action.Action == "remove-partition" {
			queues = append(queues, action.Target.ID)
		}
	}
	if len(queues) == 0 {
		return nil
	}
	counts, err := s.queuedJobs(ctx, platform)
	if err != nil {
		for _, queue := range queues {
			changes.Blockers = append(changes.Blockers, fmt.Sprintf("the jobs in partition %s cannot be counted (%v); it is removed only when it is known to be empty", queue, err))
		}
		return nil
	}
	for _, queue := range queues {
		if count := counts[queue]; count > 0 {
			jobs := "jobs"
			if count == 1 {
				jobs = "job"
			}
			changes.Blockers = append(changes.Blockers, fmt.Sprintf("partition %s still has %d %s queued or running; propose its removal again once they end", queue, count, jobs))
		}
	}
	return nil
}

func (s *DeployService) queuedJobs(ctx context.Context, platform *platformdomain.Platform) (map[string]int, error) {
	if s.readers == nil {
		return nil, fmt.Errorf("no platform reader")
	}
	reader, err := s.readers.For(ctx, platform)
	if err != nil {
		return nil, err
	}
	queues, ok := reader.(platformdomain.QueueReader)
	if !ok {
		return nil, fmt.Errorf("the platform's reader does not count queued work")
	}
	return queues.QueuedJobs(ctx)
}

// memberNodeNames is each leaving member's node name in the Platform's runtime: the name its
// observed membership in this Platform reports, else its observed hostname (the name the
// deployment gave the node). It reads only what swallow stores, never the host, so a member whose
// machine failed can still be removed. A member with neither is ErrInvalidDeployment.
func (s *DeployService) memberNodeNames(ctx context.Context, platformID string, removals []platformdomain.MemberRemoval) (map[string]string, error) {
	names := make(map[string]string, len(removals))
	for _, removal := range removals {
		server, err := s.servers.FindByID(ctx, removal.ServerID)
		if err != nil {
			return nil, err
		}
		name := ""
		if membership := server.Membership; membership != nil && membership.PlatformID == platformID {
			name = strings.TrimSpace(membership.NodeName)
		}
		if name == "" {
			name = strings.TrimSpace(server.Observed.Hostname)
		}
		if name == "" {
			return nil, fmt.Errorf("%w: server %s has no recorded node name in the Platform or hostname, so it cannot be drained by name",
				platformdomain.ErrInvalidDeployment, server.DisplayName())
		}
		names[removal.ServerID] = name
	}
	return names, nil
}
