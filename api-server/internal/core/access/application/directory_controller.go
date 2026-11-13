package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
	"github.com/maple52046/swallow/internal/core/convergence"
	"github.com/maple52046/swallow/internal/core/store"
)

// DirectoryControllerName is the Directory controller's name: its singleton lease is
// "controller:directory", and a convergence request with this key asks it to converge now.
const DirectoryControllerName = "directory"

// directoryKey is the controller's key: the Directory is one resource. directoryRequestKey is the
// key a convergence request (POST /access/directory/sync) enqueues: the same resource, but a
// Reconcile under it always reaches the Directory, like a resync, instead of skipping unchanged
// content.
const (
	directoryKey        = "directory"
	directoryRequestKey = "directory:requested"
)

// DirectoryController realizes the Directory content swallow computes from its store
// (identity-and-access.md §7.3; glossary Directory, Controller). It has one key. Each Reconcile
// reads every input, allocates the groups the content needs, projects the content, and applies it
// through the Directory provider interface, then records the status.
//
// A change event whose content hashes like the last one applied (Servers are rewritten by every
// reconcile pass) skips the Directory; a resync always applies, which repairs anything changed in
// the Directory behind swallow's back. An unreachable Directory is returned as an error, so the
// runtime retries with backoff; a Directory that rejects a change is recorded as requires_attention
// and not retried until the content changes or the next resync.
type DirectoryController struct {
	users       authdomain.UserRepository
	memberships accessdomain.MembershipRepository
	policies    accessdomain.PolicyRepository
	settings    accessdomain.SettingsRepository
	inputs      accessdomain.DirectoryInputs
	groups      accessdomain.GroupAllocator
	directory   accessdomain.Directory
	statuses    accessdomain.DirectoryStatusRepository
	tx          store.Transactor
	now         func() time.Time
}

// DirectoryControllerDeps are the controller's dependencies; every field is required.
type DirectoryControllerDeps struct {
	Users       authdomain.UserRepository
	Memberships accessdomain.MembershipRepository
	Policies    accessdomain.PolicyRepository
	Settings    accessdomain.SettingsRepository
	Inputs      accessdomain.DirectoryInputs
	Groups      accessdomain.GroupAllocator
	Directory   accessdomain.Directory
	Statuses    accessdomain.DirectoryStatusRepository
	Transactor  store.Transactor
}

var _ convergence.Controller = (*DirectoryController)(nil)

// NewDirectoryController wires the controller, refusing a missing dependency.
func NewDirectoryController(deps DirectoryControllerDeps) (*DirectoryController, error) {
	if deps.Users == nil || deps.Memberships == nil || deps.Policies == nil || deps.Settings == nil ||
		deps.Inputs == nil || deps.Groups == nil || deps.Directory == nil || deps.Statuses == nil || deps.Transactor == nil {
		return nil, errors.New("directory controller: every dependency is required")
	}
	return &DirectoryController{
		users: deps.Users, memberships: deps.Memberships, policies: deps.Policies, settings: deps.Settings,
		inputs: deps.Inputs, groups: deps.Groups, directory: deps.Directory, statuses: deps.Statuses, tx: deps.Transactor,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

// Name implements convergence.Controller.
func (c *DirectoryController) Name() string { return DirectoryControllerName }

// Collections implements convergence.Controller: every collection whose change can change the
// Directory content, and the convergence requests.
func (c *DirectoryController) Collections() []string {
	return []string{
		"users", "pool_memberships", "pool_access_policies", "access_settings", "ssh_keys",
		"servers", "platforms", "pools", convergence.RequestCollection,
	}
}

// KeysForChange implements convergence.Controller: a change of an input converges the one key, a
// request for this controller enqueues the request key, and a request for another controller
// converges nothing.
func (c *DirectoryController) KeysForChange(_ context.Context, change store.Change) ([]string, error) {
	if change.Collection == convergence.RequestCollection {
		if change.Key != DirectoryControllerName {
			return nil, nil
		}
		return []string{directoryRequestKey}, nil
	}
	return []string{directoryKey}, nil
}

// Keys implements convergence.Controller.
func (c *DirectoryController) Keys(context.Context) ([]string, error) {
	return []string{directoryKey}, nil
}

// Reconcile implements convergence.Controller. A change whose content hashes like the content last
// applied (or last rejected) is skipped; a resync and a convergence request always apply.
func (c *DirectoryController) Reconcile(ctx context.Context, request convergence.Request) error {
	content, err := c.content(ctx)
	if err != nil {
		return err
	}
	status, err := c.statuses.Find(ctx)
	if err != nil {
		return fmt.Errorf("read directory status: %w", err)
	}
	hash := content.Hash()
	forced := request.Trigger == convergence.TriggerResync || request.Key == directoryRequestKey
	if !forced && hash == status.ContentHash {
		return nil
	}
	now := c.now()
	result, applyErr := c.directory.Apply(ctx, content)
	status.ObservedAt = &now
	switch {
	case applyErr == nil:
		status.ContentHash = hash
		status.LastAppliedAt = &now
		status.Users, status.Groups, status.SudoRules, status.Hosts = len(content.Users), len(content.Groups), len(content.SudoRules), content.HostCount()
		status.Conditions = setConditions(status.Conditions, now, convergence.ConditionTrue, convergence.ReasonConverged, "", false)
		if result.Added+result.Modified+result.Deleted > 0 {
			slog.Info("directory applied", "added", result.Added, "modified", result.Modified, "deleted", result.Deleted)
		}
	case errors.Is(applyErr, accessdomain.ErrDirectoryRejected):
		status.ContentHash = hash
		status.Conditions = setConditions(status.Conditions, now, convergence.ConditionFalse, convergence.ReasonProviderRejected, applyErr.Error(), true)
	default:
		status.Conditions = setConditions(status.Conditions, now, convergence.ConditionFalse, convergence.ReasonProviderUnreachable, applyErr.Error(), false)
	}
	if err := c.statuses.Save(ctx, status); err != nil {
		return fmt.Errorf("save directory status: %w", err)
	}
	if applyErr != nil && !errors.Is(applyErr, accessdomain.ErrDirectoryRejected) {
		return applyErr
	}
	return nil
}

// content reads every input, allocates missing groups, and projects the Directory content.
func (c *DirectoryController) content(ctx context.Context) (accessdomain.DirectoryContent, error) {
	var in ProjectionInput
	var err error
	if in.Users, err = c.users.List(ctx); err != nil {
		return accessdomain.DirectoryContent{}, fmt.Errorf("list users: %w", err)
	}
	if in.Memberships, err = c.memberships.ListAll(ctx); err != nil {
		return accessdomain.DirectoryContent{}, fmt.Errorf("list memberships: %w", err)
	}
	policies, err := c.policies.ListAll(ctx)
	if err != nil {
		return accessdomain.DirectoryContent{}, fmt.Errorf("list pool policies: %w", err)
	}
	in.Policies = make(map[string]accessdomain.PoolPolicy, len(policies))
	for _, policy := range policies {
		in.Policies[policy.PoolID] = policy
	}
	if in.Settings, err = c.settings.Find(ctx); err != nil {
		return accessdomain.DirectoryContent{}, fmt.Errorf("read access settings: %w", err)
	}
	if in.AccessKeys, err = c.inputs.AccessKeys(ctx); err != nil {
		return accessdomain.DirectoryContent{}, fmt.Errorf("list access keys: %w", err)
	}
	if in.Servers, err = c.inputs.Servers(ctx); err != nil {
		return accessdomain.DirectoryContent{}, fmt.Errorf("list servers: %w", err)
	}
	if in.Pools, err = c.inputs.Pools(ctx); err != nil {
		return accessdomain.DirectoryContent{}, fmt.Errorf("list pools: %w", err)
	}
	if in.Limits, err = c.inputs.HostAccessLimits(ctx); err != nil {
		return accessdomain.DirectoryContent{}, fmt.Errorf("read host access limits: %w", err)
	}
	if in.Groups, err = c.ensureGroups(ctx, in.Pools); err != nil {
		return accessdomain.DirectoryContent{}, fmt.Errorf("allocate groups: %w", err)
	}
	return Project(in), nil
}

// ensureGroups returns every group allocation, first allocating the installation-wide groups and
// both groups of every Pool that has none. Allocations are permanent, so a deleted Pool's groups
// stay allocated (and unused) and a renamed Pool keeps its group name.
func (c *DirectoryController) ensureGroups(ctx context.Context, pools []accessdomain.DirectoryPool) (map[string]accessdomain.GroupAllocation, error) {
	groups, err := c.groups.Groups(ctx)
	if err != nil {
		return nil, err
	}
	taken := make(map[string]bool, len(groups))
	for _, group := range groups {
		taken[group.Name] = true
	}
	allocate := func(key, name string) error {
		if _, ok := groups[key]; ok {
			return nil
		}
		var allocated accessdomain.GroupAllocation
		err := c.tx.InTransaction(ctx, func(ctx context.Context) error {
			var err error
			allocated, err = c.groups.Allocate(ctx, key, name)
			return err
		})
		if err != nil {
			return err
		}
		groups[key] = allocated
		taken[allocated.Name] = true
		return nil
	}
	if err := allocate(groupKeyUsers, GroupUsers); err != nil {
		return nil, err
	}
	if err := allocate(groupKeyAdmins, GroupAdmins); err != nil {
		return nil, err
	}
	for _, pool := range pools {
		if _, ok := groups[groupKeyPoolPrefix+pool.ID]; !ok {
			if err := allocate(groupKeyPoolPrefix+pool.ID, poolGroupName(pool.Name, taken)); err != nil {
				return nil, err
			}
		}
		if err := allocate(groupKeyOwnerPrefix+pool.ID, groups[groupKeyPoolPrefix+pool.ID].Name+"-owners"); err != nil {
			return nil, err
		}
	}
	return groups, nil
}

// setConditions sets the realized and requires_attention conditions of the Directory status.
func setConditions(conditions []convergence.Condition, now time.Time, realized convergence.ConditionStatus, reason, message string, attention bool) []convergence.Condition {
	conditions, _ = convergence.SetCondition(conditions, convergence.Condition{
		Type: convergence.ConditionRealized, Status: realized, Reason: reason, Message: message,
	}, now)
	attentionStatus, attentionReason := convergence.ConditionFalse, convergence.ReasonAttentionNotNeeded
	if attention {
		attentionStatus, attentionReason = convergence.ConditionTrue, reason
	}
	conditions, _ = convergence.SetCondition(conditions, convergence.Condition{
		Type: convergence.ConditionRequiresAttention, Status: attentionStatus, Reason: attentionReason, Message: message,
	}, now)
	return conditions
}
