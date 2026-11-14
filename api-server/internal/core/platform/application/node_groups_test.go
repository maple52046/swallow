package application

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/maple52046/swallow/internal/core/convergence"
	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// groupedModule is a Platform module with node groups, kept in the spec's "groups" setting.
type groupedModule struct{}

func (groupedModule) PlatformType() platformdomain.PlatformType                { return "grouped" }
func (groupedModule) DeployRequest(platformdomain.PlatformSpec) map[string]any { return nil }
func (groupedModule) SpecFromDeployRequest(map[string]any) (platformdomain.PlatformSpec, error) {
	return platformdomain.PlatformSpec{}, nil
}
func (groupedModule) Changes(*platformdomain.PlatformSpec, platformdomain.PlatformSpec) (platformdomain.PlatformChanges, error) {
	return platformdomain.PlatformChanges{}, nil
}
func (groupedModule) ReportsMembership(platformdomain.PlatformMember) bool { return true }
func (groupedModule) AccessServers(platformdomain.PlatformSpec) []string   { return nil }

func (groupedModule) NodeGroups(spec platformdomain.PlatformSpec) []platformdomain.NodeGroup {
	groups, _ := spec.Settings["groups"].([]platformdomain.NodeGroup)
	return slices.Clone(groups)
}

func (m groupedModule) WithGroupTemplates(spec platformdomain.PlatformSpec, templates map[string]string) platformdomain.PlatformSpec {
	out := spec.Clone()
	groups := m.NodeGroups(spec)
	for i := range groups {
		if id, ok := templates[groups[i].Name]; ok {
			groups[i].TemplateID = id
		}
	}
	out.Settings["groups"] = groups
	return out
}

func (groupedModule) UpgradeSpec(spec platformdomain.PlatformSpec) (platformdomain.PlatformSpec, bool) {
	return spec, false
}

// groupedSpec is a provision_os spec with a login group on l1 and a compute group on g1 and g2.
func groupedSpec(groups ...platformdomain.NodeGroup) platformdomain.PlatformSpec {
	if len(groups) == 0 {
		groups = []platformdomain.NodeGroup{
			{Name: "login", Role: "login", Members: []string{"l1"}},
			{Name: "gpu", Role: "compute", Members: []string{"g1", "g2"}},
		}
	}
	spec := platformdomain.PlatformSpec{
		Desired:            platformdomain.PlatformPresent,
		MachinePreparation: platformdomain.MachinePreparation{Mode: platformdomain.MachinePreparationProvisionOS, ImageID: "img"},
		Settings:           map[string]any{"groups": groups},
	}
	for _, group := range groups {
		for _, id := range group.Members {
			if _, ok := spec.Member(id); !ok {
				spec.Members = append(spec.Members, platformdomain.PlatformMember{ServerID: id, Roles: []string{group.Role}})
			}
		}
	}
	return spec
}

func groupServer(id, integration string, cores int, gpus ...serverdomain.GPU) *serverdomain.Server {
	server := deployedServer(id, id, "site-1")
	server.Source.IntegrationID = integration
	server.Observed.CPUCores, server.Observed.GPUs = cores, gpus
	return server
}

func computeGPU(model string, count int) serverdomain.GPU {
	return serverdomain.GPU{Vendor: "Advanced Micro Devices, Inc. [AMD/ATI]", Model: model, Count: count, Kind: serverdomain.GPUKindCompute}
}

// A group without a template gets one the Platform generates, named <platform>-<group> in its
// members' Integration, with a suffix when the name is taken; a Platform whose machine preparation
// names a template gives it to every group instead; an existing_os spec installs nothing and gets
// none.
func TestEveryNodeGroupGetsATemplate(t *testing.T) {
	service, _, _ := newDeployHarness(groupServer("l1", "maas-1", 8), groupServer("g1", "maas-1", 64), groupServer("g2", "maas-1", 64))
	templates := &fakeTemplates{names: map[string]bool{"maas-1/lab-login": true}}
	service.templates = templates

	spec, err := service.assignGeneratedTemplates(context.Background(), groupedModule{}, "lab", groupedSpec())
	if err != nil {
		t.Fatal(err)
	}
	generated := spec.MachinePreparation.GeneratedTemplates
	if len(generated) != 2 || generated[1].Name != "lab-gpu" || generated[1].IntegrationID != "maas-1" || generated[1].Group != "gpu" ||
		!strings.HasPrefix(generated[0].Name, "lab-login-") {
		t.Fatalf("generated = %+v", generated)
	}
	for _, group := range (groupedModule{}).NodeGroups(spec) {
		if _, ok := spec.MachinePreparation.Generated(group.TemplateID); !ok {
			t.Errorf("group %s template %q is not one the spec generates", group.Name, group.TemplateID)
		}
	}

	withTemplate := groupedSpec()
	withTemplate.MachinePreparation.TemplateID = "t-1"
	spec, _ = service.assignGeneratedTemplates(context.Background(), groupedModule{}, "lab", withTemplate)
	for _, group := range (groupedModule{}).NodeGroups(spec) {
		if group.TemplateID != "t-1" {
			t.Errorf("group %s template = %q, want the machine preparation's t-1", group.Name, group.TemplateID)
		}
	}
	if len(spec.MachinePreparation.GeneratedTemplates) != 0 {
		t.Errorf("a Platform with a template generated %+v", spec.MachinePreparation.GeneratedTemplates)
	}

	existing := groupedSpec()
	existing.MachinePreparation = platformdomain.MachinePreparation{Mode: platformdomain.MachinePreparationExistingOS}
	if spec, _ = service.assignGeneratedTemplates(context.Background(), groupedModule{}, "lab", existing); len(spec.MachinePreparation.GeneratedTemplates) != 0 {
		t.Errorf("an existing_os spec generated %+v", spec.MachinePreparation.GeneratedTemplates)
	}
}

// One generated template cannot install members of two provisioner Integrations.
func TestAGroupAcrossIntegrationsIsRefused(t *testing.T) {
	service, _, _ := newDeployHarness(groupServer("l1", "maas-1", 8), groupServer("g1", "maas-1", 64), groupServer("g2", "maas-2", 64))
	service.templates = &fakeTemplates{}
	if _, err := service.assignGeneratedTemplates(context.Background(), groupedModule{}, "lab", groupedSpec()); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Errorf("assignGeneratedTemplates() error = %v, want ErrInvalidDeployment", err)
	}
}

// A template the spec generates is planned as create-template; on an applied Platform it makes the
// change wait for a Change Plan, and a template that already exists plans nothing.
func TestGeneratedTemplatesNeedApprovalOnAnAppliedPlatform(t *testing.T) {
	spec := groupedSpec()
	spec.MachinePreparation.GeneratedTemplates = []platformdomain.GeneratedTemplate{{ID: "t-gpu", Name: "lab-gpu", IntegrationID: "maas-1", Group: "gpu"}}
	templates := &fakeTemplates{}

	var first platformdomain.PlatformChanges
	if err := templateChanges(context.Background(), templates, false, spec, &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Actions) != 1 || first.Actions[0].Action != "create-template" || first.NeedsApproval {
		t.Errorf("first deploy = %+v; want create-template without a separate approval", first)
	}
	var applied platformdomain.PlatformChanges
	_ = templateChanges(context.Background(), templates, true, spec, &applied)
	if !applied.NeedsApproval || !applied.Destructive() {
		t.Errorf("applied Platform = %+v; want it to wait for a Change Plan", applied)
	}
	_ = ensureGeneratedTemplates(context.Background(), templates, &spec)
	var again platformdomain.PlatformChanges
	_ = templateChanges(context.Background(), templates, true, spec, &again)
	if len(again.Actions) != 0 || again.NeedsApproval {
		t.Errorf("an existing template = %+v; want nothing planned", again)
	}
}

// A member is installed from its group's template, its compute group's when it is in several; a
// generated template not created yet installs from the machine preparation; a template that does
// not exist is refused.
func TestMembersResolveTheirGroupTemplate(t *testing.T) {
	service, _, _ := newDeployHarness()
	service.templates = &fakeTemplates{templates: map[string]*platformdomain.DeploymentTemplateIntent{
		"t-login": {ID: "t-login", IntegrationID: "maas-1"}, "t-gpu": {ID: "t-gpu", IntegrationID: "maas-1"},
	}}
	spec := groupedSpec(
		platformdomain.NodeGroup{Name: "login", Role: "login", TemplateID: "t-login", Members: []string{"g1"}},
		platformdomain.NodeGroup{Name: "gpu", Role: "compute", TemplateID: "t-gpu", Members: []string{"g1", "g2"}},
	)
	spec.Members[0].Roles = []string{"login", "compute"}
	input := DeployPlatformInput{module: groupedModule{}, spec: &spec}
	preparation, resolved, err := service.resolveMemberTemplates(context.Background(), input, spec.MachinePreparation)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.byServer["g1"] != "t-gpu" || resolved.byServer["g2"] != "t-gpu" {
		t.Errorf("member templates = %v, want the compute group's", resolved.byServer)
	}
	if assignment, ok := preparation.Assignment("g1"); !ok || assignment.TemplateID != "t-gpu" {
		t.Errorf("g1 assignment = %+v", assignment)
	}

	pending := groupedSpec(platformdomain.NodeGroup{Name: "gpu", Role: "compute", TemplateID: "t-new", Members: []string{"g1"}})
	pending.MachinePreparation.GeneratedTemplates = []platformdomain.GeneratedTemplate{{ID: "t-new", Group: "gpu"}}
	input = DeployPlatformInput{module: groupedModule{}, spec: &pending}
	if _, resolved, err = service.resolveMemberTemplates(context.Background(), input, pending.MachinePreparation); err != nil || resolved.byServer["g1"] != "" {
		t.Errorf("a template not created yet = %v, %v; want the machine preparation", resolved.byServer, err)
	}

	missing := groupedSpec(platformdomain.NodeGroup{Name: "gpu", Role: "compute", TemplateID: "t-gone", Members: []string{"g1"}})
	input = DeployPlatformInput{module: groupedModule{}, spec: &missing}
	if _, _, err = service.resolveMemberTemplates(context.Background(), input, missing.MachinePreparation); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Errorf("a missing template error = %v, want ErrInvalidDeployment", err)
	}
}

// A Server this change adds must meet its template's hardware floor, a member that stays is not
// rechecked, and a Server of another Integration is refused; a compute group mixing GPU models is
// only a warning.
func TestMemberTemplatesCheckTheHardwareFloor(t *testing.T) {
	service, _, _ := newDeployHarness()
	intent := &platformdomain.DeploymentTemplateIntent{ID: "t-gpu", IntegrationID: "maas-1", Name: "gpu",
		Minimum: &platformdomain.TemplateMinimum{CPUCores: 32, GPUCount: 8}}
	resolved := memberTemplates{
		byServer: map[string]string{"g1": "t-gpu", "g2": "t-gpu"},
		intents:  map[string]*platformdomain.DeploymentTemplateIntent{"t-gpu": intent},
		groups:   []platformdomain.NodeGroup{{Name: "gpu", Role: "compute", TemplateID: "t-gpu", Members: []string{"g1", "g2"}}},
	}
	big := groupServer("g1", "maas-1", 64, computeGPU("MI300X", 8))
	small := groupServer("g2", "maas-1", 16, computeGPU("MI250X", 2))

	_, err := service.checkMemberTemplates(resolved, []*serverdomain.Server{big, small}, []string{"g2"})
	if !errors.Is(err, platformdomain.ErrInvalidDeployment) || !strings.Contains(err.Error(), "CPU 16 cores (minimum 32)") ||
		!strings.Contains(err.Error(), "2 compute GPUs (minimum 8)") {
		t.Errorf("an added Server below the floor: error = %v", err)
	}
	warnings, err := service.checkMemberTemplates(resolved, []*serverdomain.Server{big, small}, []string{})
	if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "mixes GPU models") {
		t.Errorf("members that stay = %v, %v; want only the mixed-model warning", warnings, err)
	}
	other := groupServer("g2", "maas-2", 64, computeGPU("MI300X", 8))
	if _, err = service.checkMemberTemplates(resolved, []*serverdomain.Server{big, other}, nil); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Errorf("a Server of another Integration: error = %v", err)
	}
	unknown := groupServer("g2", "maas-1", 0)
	if _, err = service.checkMemberTemplates(resolved, []*serverdomain.Server{big, unknown}, []string{"g2"}); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Errorf("a Server without inventory: error = %v; want missing inventory to count as not met", err)
	}
}

// Removing a partition with jobs, or one whose jobs cannot be counted, is blocked, saying to propose
// the removal again since a blocked plan is not recomputed; an empty one is not.
func TestRemovingABusyPartitionIsBlocked(t *testing.T) {
	service, _, _ := newDeployHarness()
	platform := &platformdomain.Platform{ID: "p1"}
	changes := func() *platformdomain.PlatformChanges {
		return &platformdomain.PlatformChanges{Actions: []convergence.PlannedAction{{Action: "remove-partition", Target: convergence.ResourceRef{Kind: "partition", ID: "debug"}}}}
	}
	service.readers = &fakeQueueReaders{jobs: map[string]int{"debug": 3}}
	busy := changes()
	if err := service.queueBlockers(context.Background(), platform, busy); err != nil || len(busy.Blockers) != 1 ||
		busy.Blockers[0] != "partition debug still has 3 jobs queued or running; propose its removal again once they end" {
		t.Errorf("a busy partition = %+v, %v", busy.Blockers, err)
	}
	service.readers = &fakeQueueReaders{jobs: map[string]int{"debug": 1}}
	if one := changes(); service.queueBlockers(context.Background(), platform, one) != nil || !strings.Contains(one.Blockers[0], "still has 1 job queued") {
		t.Errorf("one job = %+v", one.Blockers)
	}
	service.readers = &fakeQueueReaders{err: errors.New("slurmrestd is down")}
	unknown := changes()
	if _ = service.queueBlockers(context.Background(), platform, unknown); len(unknown.Blockers) != 1 || !strings.Contains(unknown.Blockers[0], "cannot be counted") {
		t.Errorf("an unreadable queue = %+v", unknown.Blockers)
	}
	service.readers = &fakeQueueReaders{jobs: map[string]int{}}
	empty := changes()
	if _ = service.queueBlockers(context.Background(), platform, empty); len(empty.Blockers) != 0 {
		t.Errorf("an empty partition = %+v", empty.Blockers)
	}
}

// A leaving member is named by its membership in this Platform, else by its hostname, both stored by
// swallow; a member with neither cannot be drained by name and is refused.
func TestLeavingMembersAreNamedFromStoredFacts(t *testing.T) {
	member := groupServer("g1", "maas-1", 64)
	member.Observed.Hostname = "gpu-01-host"
	member.Membership = &serverdomain.MembershipStatus{PlatformID: "p1", NodeName: "gpu-01"}
	other := groupServer("g2", "maas-1", 64)
	other.Observed.Hostname = "gpu-02"
	other.Membership = &serverdomain.MembershipStatus{PlatformID: "another", NodeName: "elsewhere"}
	nameless := groupServer("g3", "maas-1", 64)
	nameless.Observed.Hostname = ""
	service, _, _ := newDeployHarness(member, other, nameless)

	names, err := service.memberNodeNames(context.Background(), "p1", []platformdomain.MemberRemoval{{ServerID: "g1"}, {ServerID: "g2"}})
	if err != nil || names["g1"] != "gpu-01" || names["g2"] != "gpu-02" {
		t.Errorf("memberNodeNames() = %v, %v; want the membership name, then the hostname", names, err)
	}
	if _, err := service.memberNodeNames(context.Background(), "p1", []platformdomain.MemberRemoval{{ServerID: "g3"}}); !errors.Is(err, platformdomain.ErrInvalidDeployment) {
		t.Errorf("a member without a name: error = %v, want ErrInvalidDeployment", err)
	}
}
