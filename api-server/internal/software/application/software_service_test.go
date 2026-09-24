package application

import (
	"context"
	"errors"
	"testing"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	softwaredomain "github.com/maple52046/swallow/internal/software/domain"
)

// fakeAssignmentRepo is an in-memory Software Assignment store keyed by (serverId, kind).
type fakeAssignmentRepo struct {
	items map[string]*softwaredomain.Assignment
}

func newFakeAssignmentRepo() *fakeAssignmentRepo {
	return &fakeAssignmentRepo{items: map[string]*softwaredomain.Assignment{}}
}

func assignmentKey(serverID string, kind softwaredomain.Kind) string {
	return serverID + "/" + string(kind)
}

func (r *fakeAssignmentRepo) Upsert(_ context.Context, assignment *softwaredomain.Assignment) error {
	clone := *assignment
	r.items[assignmentKey(assignment.ServerID, assignment.Kind)] = &clone
	return nil
}

func (r *fakeAssignmentRepo) FindByServerAndKind(_ context.Context, serverID string, kind softwaredomain.Kind) (*softwaredomain.Assignment, error) {
	if item, ok := r.items[assignmentKey(serverID, kind)]; ok {
		return item, nil
	}
	return nil, softwaredomain.ErrAssignmentNotFound
}

func (r *fakeAssignmentRepo) List(_ context.Context, filter softwaredomain.AssignmentFilter) ([]*softwaredomain.Assignment, error) {
	out := []*softwaredomain.Assignment{}
	for _, item := range r.items {
		if filter.ServerID != "" && item.ServerID != filter.ServerID {
			continue
		}
		if filter.Kind != "" && item.Kind != filter.Kind {
			continue
		}
		if !filter.IncludeAbsent && item.State == softwaredomain.StateAbsent {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *fakeAssignmentRepo) SetState(_ context.Context, serverID string, kind softwaredomain.Kind, state softwaredomain.AssignmentState, workflowID string, appliedAt *time.Time) error {
	item, ok := r.items[assignmentKey(serverID, kind)]
	if !ok {
		return softwaredomain.ErrAssignmentNotFound
	}
	item.State = state
	if workflowID != "" {
		item.LastWorkflowID = workflowID
	}
	if appliedAt != nil {
		item.LastAppliedAt = appliedAt
	}
	return nil
}

func (r *fakeAssignmentRepo) ListAll(ctx context.Context) ([]*softwaredomain.Assignment, error) {
	return r.List(ctx, softwaredomain.AssignmentFilter{})
}

// fakeServerRepo implements only FindByID; the rest of the interface is embedded so unused methods
// panic if the test ever reaches them.
type fakeServerRepo struct {
	serverdomain.ServerRepository
	servers map[string]*serverdomain.Server
}

func (r *fakeServerRepo) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	if server, ok := r.servers[id]; ok {
		return server, nil
	}
	return nil, serverdomain.ErrServerNotFound
}

type fakeMembership struct{ kubernetes map[string]bool }

func (m fakeMembership) IsKubernetesMember(_ context.Context, serverID string) (bool, error) {
	return m.kubernetes[serverID], nil
}

type fakeLauncher struct {
	install    bool
	uninstall  bool
	lastLaunch SoftwareLaunch
}

func (l *fakeLauncher) LaunchInstall(_ context.Context, launch SoftwareLaunch) (string, error) {
	l.install = true
	l.lastLaunch = launch
	return "op-install", nil
}

func (l *fakeLauncher) LaunchUninstall(_ context.Context, launch SoftwareLaunch) (string, error) {
	l.uninstall = true
	l.lastLaunch = launch
	return "op-uninstall", nil
}

func deployedServer(id string) *serverdomain.Server {
	return &serverdomain.Server{
		ID:           id,
		Source:       serverdomain.Source{SiteID: "site-1"},
		Provisioning: &serverdomain.ProvisioningStatus{State: "deployed"},
	}
}

func newTestService(servers map[string]*serverdomain.Server, membership map[string]bool) (*SoftwareService, *fakeAssignmentRepo, *fakeLauncher) {
	repo := newFakeAssignmentRepo()
	service := NewSoftwareService(repo, &fakeServerRepo{servers: servers}, fakeMembership{kubernetes: membership})
	launcher := &fakeLauncher{}
	service.AttachLauncher(launcher)
	return service, repo, launcher
}

func TestInstallRejectsUnknownKind(t *testing.T) {
	service, _, _ := newTestService(map[string]*serverdomain.Server{"a": deployedServer("a")}, nil)
	_, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.Kind("nope"), Targets: []InstallTarget{{ServerID: "a"}},
	})
	if !errors.Is(err, softwaredomain.ErrUnknownKind) {
		t.Fatalf("err = %v, want ErrUnknownKind", err)
	}
}

func TestInstallDockerRejectsRoles(t *testing.T) {
	service, _, _ := newTestService(map[string]*serverdomain.Server{"a": deployedServer("a")}, nil)
	_, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindDockerCE, Targets: []InstallTarget{{ServerID: "a", Roles: []softwaredomain.Role{softwaredomain.RoleServer}}},
	})
	if !errors.Is(err, softwaredomain.ErrInvalidRole) {
		t.Fatalf("err = %v, want ErrInvalidRole", err)
	}
}

func TestInstallNFSRequiresRoles(t *testing.T) {
	service, _, _ := newTestService(map[string]*serverdomain.Server{"a": deployedServer("a")}, nil)
	_, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindNFS, Targets: []InstallTarget{{ServerID: "a"}},
	})
	if !errors.Is(err, softwaredomain.ErrInvalidRole) {
		t.Fatalf("err = %v, want ErrInvalidRole", err)
	}
}

func TestInstallNFSServerRequiresExportPath(t *testing.T) {
	service, _, _ := newTestService(map[string]*serverdomain.Server{"a": deployedServer("a")}, nil)
	_, err := service.Install(context.Background(), InstallInput{
		Kind:    softwaredomain.KindNFS,
		Targets: []InstallTarget{{ServerID: "a", Roles: []softwaredomain.Role{softwaredomain.RoleServer}}},
	})
	if !errors.Is(err, softwaredomain.ErrSpecInvalid) {
		t.Fatalf("err = %v, want ErrSpecInvalid", err)
	}
}

func TestInstallRefusesKubernetesMemberForDocker(t *testing.T) {
	service, _, _ := newTestService(
		map[string]*serverdomain.Server{"a": deployedServer("a")},
		map[string]bool{"a": true},
	)
	_, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindDockerCE, Targets: []InstallTarget{{ServerID: "a"}},
	})
	if !errors.Is(err, softwaredomain.ErrKubernetesMember) {
		t.Fatalf("err = %v, want ErrKubernetesMember", err)
	}
}

func TestInstallRefusesMutuallyExclusiveKind(t *testing.T) {
	service, repo, _ := newTestService(map[string]*serverdomain.Server{"a": deployedServer("a")}, nil)
	// Podman already installed on the server.
	_ = repo.Upsert(context.Background(), &softwaredomain.Assignment{
		ServerID: "a", Kind: softwaredomain.KindPodman, State: softwaredomain.StateInstalled,
	})
	_, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindDockerCE, Targets: []InstallTarget{{ServerID: "a"}},
	})
	if !errors.Is(err, softwaredomain.ErrMutuallyExclusive) {
		t.Fatalf("err = %v, want ErrMutuallyExclusive", err)
	}
}

func TestInstallRejectsNonDeployedTarget(t *testing.T) {
	server := deployedServer("a")
	server.Provisioning.State = "ready"
	service, _, _ := newTestService(map[string]*serverdomain.Server{"a": server}, nil)
	_, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindDockerCE, Targets: []InstallTarget{{ServerID: "a"}},
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestInstallNFSMixedRolesBuildsTrustedVarsAndPendingAssignments(t *testing.T) {
	service, repo, launcher := newTestService(map[string]*serverdomain.Server{
		"srv-a": deployedServer("srv-a"),
		"srv-b": deployedServer("srv-b"),
	}, nil)

	operationID, err := service.Install(context.Background(), InstallInput{
		Kind: softwaredomain.KindNFS,
		Targets: []InstallTarget{
			{ServerID: "srv-a", Roles: []softwaredomain.Role{softwaredomain.RoleServer}},
			{ServerID: "srv-b", Roles: []softwaredomain.Role{softwaredomain.RoleClient}},
		},
		Spec: map[string]any{
			"exportPath": "/export/data",
			"source":     "srv-a:/export/data",
			"mountPath":  "/shared",
		},
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if operationID != "op-install" || !launcher.install {
		t.Fatalf("launcher not invoked; operationID=%q install=%v", operationID, launcher.install)
	}

	vars := launcher.lastLaunch.TrustedVars
	if vars["swallow_software_kind"] != "nfs" {
		t.Errorf("swallow_software_kind = %v, want nfs", vars["swallow_software_kind"])
	}
	serverIDs, _ := vars["swallow_nfs_server_ids"].([]string)
	if len(serverIDs) != 1 || serverIDs[0] != "srv-a" {
		t.Errorf("swallow_nfs_server_ids = %v, want [srv-a]", vars["swallow_nfs_server_ids"])
	}
	clientIDs, _ := vars["swallow_nfs_client_ids"].([]string)
	if len(clientIDs) != 1 || clientIDs[0] != "srv-b" {
		t.Errorf("swallow_nfs_client_ids = %v, want [srv-b]", vars["swallow_nfs_client_ids"])
	}
	if vars["swallow_nfs_export_path"] != "/export/data" {
		t.Errorf("swallow_nfs_export_path = %v", vars["swallow_nfs_export_path"])
	}
	roles, _ := vars["swallow_software_roles"].(map[string][]string)
	if len(roles["srv-a"]) != 1 || roles["srv-a"][0] != "server" {
		t.Errorf("roles[srv-a] = %v, want [server]", roles["srv-a"])
	}

	for _, serverID := range []string{"srv-a", "srv-b"} {
		assignment, err := repo.FindByServerAndKind(context.Background(), serverID, softwaredomain.KindNFS)
		if err != nil {
			t.Fatalf("assignment for %s: %v", serverID, err)
		}
		if assignment.State != softwaredomain.StatePending {
			t.Errorf("assignment %s state = %s, want pending", serverID, assignment.State)
		}
		if assignment.LastWorkflowID != "op-install" {
			t.Errorf("assignment %s workflow = %s, want op-install", serverID, assignment.LastWorkflowID)
		}
	}
}

func TestUninstallMarksAssignmentsUninstalling(t *testing.T) {
	service, repo, launcher := newTestService(map[string]*serverdomain.Server{"a": deployedServer("a")}, nil)
	_ = repo.Upsert(context.Background(), &softwaredomain.Assignment{
		ServerID: "a", Kind: softwaredomain.KindNFS, State: softwaredomain.StateInstalled,
		Roles: []softwaredomain.Role{softwaredomain.RoleClient},
		Spec:  map[string]any{"source": "srv:/x", "mountPath": "/shared"},
	})
	operationID, err := service.Uninstall(context.Background(), UninstallInput{
		Kind: softwaredomain.KindNFS, ServerIDs: []string{"a"},
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if operationID != "op-uninstall" || !launcher.uninstall {
		t.Fatalf("uninstall launcher not invoked; operationID=%q", operationID)
	}
	assignment, _ := repo.FindByServerAndKind(context.Background(), "a", softwaredomain.KindNFS)
	if assignment.State != softwaredomain.StateUninstalling {
		t.Errorf("state = %s, want uninstalling", assignment.State)
	}
}

func TestUninstallRejectsMissingAssignment(t *testing.T) {
	service, _, _ := newTestService(map[string]*serverdomain.Server{"a": deployedServer("a")}, nil)
	_, err := service.Uninstall(context.Background(), UninstallInput{
		Kind: softwaredomain.KindNFS, ServerIDs: []string{"a"},
	})
	if !errors.Is(err, softwaredomain.ErrAssignmentNotFound) {
		t.Fatalf("err = %v, want ErrAssignmentNotFound", err)
	}
}
