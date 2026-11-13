package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/maple52046/swallow/config"
	accessstore "github.com/maple52046/swallow/internal/adapters/store/mongodb/access"
	convergencestore "github.com/maple52046/swallow/internal/adapters/store/mongodb/convergence"
	accessapp "github.com/maple52046/swallow/internal/core/access/application"
	accessdelivery "github.com/maple52046/swallow/internal/core/access/delivery"
	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	apikeydomain "github.com/maple52046/swallow/internal/core/apikey/domain"
	authapp "github.com/maple52046/swallow/internal/core/auth/application"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	"github.com/maple52046/swallow/internal/core/convergence"
	infradomain "github.com/maple52046/swallow/internal/core/infrastructure/domain"
	platformapp "github.com/maple52046/swallow/internal/core/platform/application"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
	sshkeydomain "github.com/maple52046/swallow/internal/core/sshkey/domain"
	"github.com/maple52046/swallow/internal/core/store"
	validationapp "github.com/maple52046/swallow/internal/core/validation/application"
	validationdomain "github.com/maple52046/swallow/internal/core/validation/domain"
	workflowdomain "github.com/maple52046/swallow/internal/core/workflow/domain"
)

// accessStores are the access context's MongoDB repositories, shared by the API and the worker.
type accessStores struct {
	memberships *accessstore.MongoMemberships
	policies    *accessstore.MongoPolicies
	settings    *accessstore.MongoSettings
	audit       *accessstore.MongoAudit
	posixIDs    *accessstore.MongoPOSIXIDs
	statuses    *accessstore.MongoDirectoryStatus
}

// newAccessStores opens the access repositories and creates their indexes. start is the configured
// first POSIX id, which only the first allocation records.
func newAccessStores(ctx context.Context, db *mongo.Database, start int) (accessStores, error) {
	memberships, err := accessstore.NewMongoMemberships(ctx, db)
	if err != nil {
		return accessStores{}, err
	}
	audit, err := accessstore.NewMongoAudit(ctx, db)
	if err != nil {
		return accessStores{}, err
	}
	return accessStores{
		memberships: memberships, policies: accessstore.NewMongoPolicies(db), settings: accessstore.NewMongoSettings(db),
		audit: audit, posixIDs: accessstore.NewMongoPOSIXIDs(db, start), statuses: accessstore.NewMongoDirectoryStatus(db),
	}, nil
}

// accessAPI is the API process's access assembly (decision 082): the Authorizer every route is
// checked with, its Fiber Guard, the access handler, and the services other assemblies reach.
type accessAPI struct {
	guard       *accessdelivery.Guard
	handler     *accessdelivery.Handler
	authorizer  *accessapp.Authorizer
	memberships *accessapp.MembershipService
	users       *accessapp.UserService
}

// accessAPIDeps are what newAccessAPI is built from.
type accessAPIDeps struct {
	cfg          config.APIConfig
	db           *mongo.Database
	transactor   store.Transactor
	stores       accessStores
	users        authdomain.UserRepository
	sessions     authdomain.SessionRepository
	apiKeys      apikeydomain.Repository
	sshKeys      sshkeydomain.KeyRepository
	servers      serverdomain.ServerRepository
	pools        infradomain.PoolRepository
	platforms    platformapp.PoolResolver
	orchestrator workflowdomain.WorkflowRepository
}

// newAccessAPI assembles the access context for the API process: it allocates the uidNumbers Users
// created before POSIX identities still lack, and warns when the configured POSIX id start no longer
// matches the one the first allocation recorded.
func newAccessAPI(ctx context.Context, deps accessAPIDeps) (*accessAPI, error) {
	authorizer, err := accessapp.NewAuthorizer(accessapp.AuthorizerDeps{
		Users: deps.users, Memberships: deps.stores.memberships, Servers: serverPools{servers: deps.servers},
		Platforms: deps.platforms, Workflows: workflowTargets{workflows: deps.orchestrator},
	})
	if err != nil {
		return nil, err
	}
	users, err := accessapp.NewUserService(accessapp.UserServiceDeps{
		ServiceAccounts: moduleServiceAccounts, Transactor: deps.transactor, Users: deps.users, UIDs: deps.stores.posixIDs, Memberships: deps.stores.memberships,
		Cleanup: userCleanup{sessions: deps.sessions, apiKeys: deps.apiKeys, sshKeys: deps.sshKeys}, Audit: deps.stores.audit,
	})
	if err != nil {
		return nil, err
	}
	memberships, err := accessapp.NewMembershipService(accessapp.MembershipServiceDeps{
		Transactor: deps.transactor, Users: deps.users, Memberships: deps.stores.memberships, Policies: deps.stores.policies,
		Settings: deps.stores.settings, Audit: deps.stores.audit, Pools: poolLookup{pools: deps.pools},
	})
	if err != nil {
		return nil, err
	}
	if err := users.EnsurePOSIXIdentities(ctx); err != nil {
		return nil, fmt.Errorf("allocate posix identities: %w", err)
	}
	start := deps.cfg.POSIXIDStart
	if recorded, ok, err := deps.stores.posixIDs.RecordedStart(ctx); err != nil {
		return nil, fmt.Errorf("read posix id start: %w", err)
	} else if ok {
		if recorded != start {
			slog.Warn("api.posixIdStart differs from the start recorded at the first allocation; the recorded one stays in effect",
				"configured", start, "recorded", recorded)
		}
		start = recorded
	}
	handler := accessdelivery.NewHandler(accessdelivery.HandlerDeps{
		Users: users, Memberships: memberships, Statuses: deps.stores.statuses,
		Requests: convergencestore.NewMongoRequester(deps.db),
		Directory: accessdelivery.DirectoryInfo{
			Configured: deps.cfg.Directory.Configured(), HostURL: deps.cfg.Directory.HostURL, BaseDN: deps.cfg.Directory.BaseDN,
		},
		POSIXIDStart: func() int { return start },
	})
	return &accessAPI{guard: accessdelivery.NewGuard(authorizer), handler: handler, authorizer: authorizer, memberships: memberships, users: users}, nil
}

// moduleServiceAccounts are the local accounts swallow's modules and the installation create on
// Servers (Slurm, MUNGE, the monitoring software, container and VM runtimes, the directory client,
// the co-located MAAS and PostgreSQL). No User may take one of these names
// (identity-and-access.md §3.1); a module that adds a service account adds its name here.
var moduleServiceAccounts = []string{
	"slurm", "munge", "prometheus", "grafana", "omnistat", "victoriametrics", "docker",
	"libvirt-qemu", "libvirt-dnsmasq", "maas", "postgres", "sssd", "nslcd", "openldap",
}

// serverPools adapts the Server repository to the access context's ServerPools port.
type serverPools struct {
	servers serverdomain.ServerRepository
}

// PoolsOf implements accessdomain.ServerPools; absent Servers are known too, so a list a caller saw
// still authorizes its rows.
func (p serverPools) PoolsOf(ctx context.Context, serverIDs []string) (map[string]string, error) {
	result, err := p.servers.List(ctx, serverdomain.ListFilter{IDs: serverIDs, IncludeAbsent: true})
	if err != nil {
		return nil, err
	}
	pools := make(map[string]string, len(result.Servers))
	for _, server := range result.Servers {
		pools[server.ID] = server.Spec.PoolID
	}
	return pools, nil
}

// ServersIn implements accessdomain.ServerPools.
func (p serverPools) ServersIn(ctx context.Context, poolIDs []string) ([]string, error) {
	result, err := p.servers.List(ctx, serverdomain.ListFilter{PoolIDs: poolIDs, RestrictPools: true, IncludeAbsent: true})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(result.Servers))
	for _, server := range result.Servers {
		ids = append(ids, server.ID)
	}
	return ids, nil
}

// workflowTargets adapts the Workflow repository to the access context's WorkflowTargets port: a
// Workflow's target Servers, and the Platforms it names (its Platform and its Platform resources).
type workflowTargets struct {
	workflows workflowdomain.WorkflowRepository
}

// TargetsOf implements accessdomain.WorkflowTargets.
func (t workflowTargets) TargetsOf(ctx context.Context, workflowID string) ([]string, []string, bool, error) {
	workflow, err := t.workflows.FindByID(ctx, workflowID)
	if errors.Is(err, workflowdomain.ErrWorkflowNotFound) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	var platforms []string
	if workflow.PlatformID != "" {
		platforms = append(platforms, workflow.PlatformID)
	}
	for _, resource := range workflow.TargetResources {
		if resource.Kind == "platform" && resource.ID != workflow.PlatformID {
			platforms = append(platforms, resource.ID)
		}
	}
	return workflow.TargetServerIDs, platforms, true, nil
}

// poolLookup adapts the Pool repository to the access context's PoolLookup port.
type poolLookup struct {
	pools infradomain.PoolRepository
}

// Pool implements accessdomain.PoolLookup.
func (l poolLookup) Pool(ctx context.Context, poolID string) (accessdomain.PoolRef, bool, error) {
	pool, err := l.pools.FindByID(ctx, poolID)
	if errors.Is(err, infradomain.ErrPoolNotFound) {
		return accessdomain.PoolRef{}, false, nil
	}
	if err != nil {
		return accessdomain.PoolRef{}, false, err
	}
	return accessdomain.PoolRef{ID: pool.ID, SiteID: pool.SiteID, Name: pool.Name}, true, nil
}

// userCleanup removes a deleted User's Sessions, API Keys, and Access Keys, inside the transaction
// that deletes the User.
type userCleanup struct {
	sessions authdomain.SessionRepository
	apiKeys  apikeydomain.Repository
	sshKeys  sshkeydomain.KeyRepository
}

// RemoveUserData implements accessdomain.UserCleanup.
func (c userCleanup) RemoveUserData(ctx context.Context, userID string) error {
	if err := c.sessions.DeleteByUser(ctx, userID); err != nil {
		return err
	}
	if err := c.apiKeys.DeleteAllForUser(ctx, userID); err != nil {
		return err
	}
	return c.sshKeys.DeleteAllAccess(ctx, userID)
}

// meMemberships adapts the membership service to auth-me's MembershipReader.
type meMemberships struct {
	memberships *accessapp.MembershipService
}

// MembershipsOf implements authapp.MembershipReader.
func (m meMemberships) MembershipsOf(ctx context.Context, userID string) ([]authapp.MembershipOfCaller, error) {
	memberships, err := m.memberships.Memberships(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]authapp.MembershipOfCaller, 0, len(memberships))
	for _, membership := range memberships {
		out = append(out, authapp.MembershipOfCaller{
			PoolID: membership.PoolID, PoolName: membership.PoolName, SiteID: membership.SiteID, Role: string(membership.Role),
		})
	}
	return out, nil
}

// changePlanAccess decides the Change Plan routes (change-plans.md, Authorization): a Platform's plan
// through the Platform's Pool, every other kind for admins only.
type changePlanAccess struct{}

// CanRead implements convergencedelivery.ResourceAccess.
func (changePlanAccess) CanRead(c *fiber.Ctx, resource convergence.ResourceRef) (bool, error) {
	visibility, ok := accessdelivery.VisibilityFrom(c)
	if !ok {
		return false, errors.New("change plan route has no access scope")
	}
	if visibility.Admin() {
		return true, nil
	}
	if resource.Kind != "platform" {
		return false, nil
	}
	return visibility.CanReadPlatform(c.UserContext(), resource.ID)
}

// CanDecide implements convergencedelivery.ResourceAccess.
func (changePlanAccess) CanDecide(c *fiber.Ctx, resource convergence.ResourceRef) (bool, error) {
	visibility, ok := accessdelivery.VisibilityFrom(c)
	if !ok {
		return false, errors.New("change plan route has no access scope")
	}
	if visibility.Admin() {
		return true, nil
	}
	if resource.Kind != "platform" {
		return false, nil
	}
	err := visibility.RequirePlatform(c.UserContext(), accessdomain.Operate, resource.ID)
	if errors.Is(err, accessdomain.ErrNotVisible) || errors.Is(err, accessdomain.ErrForbidden) {
		return false, nil
	}
	return err == nil, err
}

// The body extractors below read the Server ids a route acts on from its request body, for the
// Guard's body checks; each follows the route's request schema in its contract. A body that does not
// parse yields no ids, and the handler then refuses it.

// serverIDsField reads a top-level "serverIds" array.
func serverIDsField(body []byte) []string {
	var request struct {
		ServerIDs []string `json:"serverIds"`
	}
	_ = json.Unmarshal(body, &request) // An unreadable body has no ids; the handler answers 400.
	return request.ServerIDs
}

// softwareInstallServers reads POST /software/assignments' "assignments[].serverId".
func softwareInstallServers(body []byte) []string {
	var request struct {
		Assignments []struct {
			ServerID string `json:"serverId"`
		} `json:"assignments"`
	}
	_ = json.Unmarshal(body, &request) // An unreadable body has no ids; the handler answers 400.
	ids := make([]string, 0, len(request.Assignments))
	for _, assignment := range request.Assignments {
		ids = append(ids, assignment.ServerID)
	}
	return ids
}

// platformSpecMembers reads "spec.members[].serverId" of a Platform create or spec write.
func platformSpecMembers(body []byte) []string {
	var request struct {
		Spec struct {
			Members []struct {
				ServerID string `json:"serverId"`
			} `json:"members"`
		} `json:"spec"`
	}
	_ = json.Unmarshal(body, &request) // An unreadable body has no ids; the handler answers 400.
	ids := make([]string, 0, len(request.Spec.Members))
	for _, member := range request.Spec.Members {
		ids = append(ids, member.ServerID)
	}
	return ids
}

// splitIDs splits a comma-separated query value, dropping empty items.
func splitIDs(raw string) []string {
	var ids []string
	for _, id := range strings.Split(raw, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// readSecretFile reads a secret from a file path, trimming one trailing newline; it never logs it.
func readSecretFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

// metricScopeCheck admits a metrics query the caller may read (server-metrics.md, Authorization):
// every Server it names, the Pool, or the Platform; a Zone is admin-only. A query naming no scope is
// left to the handler, which refuses it.
func metricScopeCheck(c *fiber.Ctx, v *accessdelivery.Visibility) error {
	ctx := c.UserContext()
	if ids := splitIDs(c.Query("serverIds")); len(ids) > 0 {
		if err := v.RequireServers(ctx, accessdomain.Read, ids); err != nil {
			return err
		}
	}
	if pool := strings.TrimSpace(c.Query("poolId")); pool != "" && !v.ReadsPool(pool) {
		return accessdomain.ErrNotVisible
	}
	if platform := strings.TrimSpace(c.Query("platformId")); platform != "" {
		if err := v.RequirePlatform(ctx, accessdomain.Read, platform); err != nil {
			return err
		}
	}
	if strings.TrimSpace(c.Query("zoneId")) != "" {
		return accessdomain.ErrForbidden
	}
	return nil
}

// validationStartCheck admits starting a Validation Run the caller may operate: every target
// Server, or the target Platform (validation.md, Authorization).
func validationStartCheck(c *fiber.Ctx, v *accessdelivery.Visibility) error {
	var request struct {
		ServerIDs  []string `json:"serverIds"`
		PlatformID string   `json:"platformId"`
	}
	_ = json.Unmarshal(c.Body(), &request) // An unreadable body names no target; the handler answers 400.
	if err := v.RequireServers(c.UserContext(), accessdomain.Operate, request.ServerIDs); err != nil {
		return err
	}
	if request.PlatformID != "" {
		return v.RequirePlatform(c.UserContext(), accessdomain.Operate, request.PlatformID)
	}
	return nil
}

// validationRunAccess checks the run routes against a run's targets: its Servers, a Platform run's
// members, and its Platform.
type validationRunAccess struct {
	runs *validationapp.Service
}

// check returns the Guard check for access to the run named by the runId parameter. An unknown run
// is left to the handler, which answers 404 for it as for any caller.
func (a validationRunAccess) check(access accessdomain.Access) func(c *fiber.Ctx, v *accessdelivery.Visibility) error {
	return func(c *fiber.Ctx, v *accessdelivery.Visibility) error {
		run, err := a.runs.Get(c.UserContext(), c.Params("runId"))
		if errors.Is(err, validationdomain.ErrRunNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		servers := append(append([]string(nil), run.Target.ServerIDs...), run.Target.MemberIDs...)
		if len(servers) == 0 && run.Target.PlatformID == "" {
			return accessdomain.ErrNotVisible
		}
		if err := v.RequireServers(c.UserContext(), access, servers); err != nil {
			return err
		}
		if run.Target.PlatformID != "" && access == accessdomain.Operate {
			return v.RequirePlatform(c.UserContext(), access, run.Target.PlatformID)
		}
		return nil
	}
}
