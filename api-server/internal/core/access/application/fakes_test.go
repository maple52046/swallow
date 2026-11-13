package application

import (
	"context"
	"sort"
	"sync"

	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	authdomain "github.com/maple52046/swallow/internal/core/auth/domain"
)

// memUsers is an in-memory authdomain.UserRepository.
type memUsers struct {
	mu    sync.Mutex
	users map[string]*authdomain.User
}

func newMemUsers(users ...*authdomain.User) *memUsers {
	m := &memUsers{users: map[string]*authdomain.User{}}
	for _, user := range users {
		m.users[user.ID] = user
	}
	return m
}

func (m *memUsers) FindByUsername(_ context.Context, username string) (*authdomain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, user := range m.users {
		if user.Username == username {
			copied := *user
			return &copied, nil
		}
	}
	return nil, authdomain.ErrUserNotFound
}

func (m *memUsers) FindByID(_ context.Context, id string) (*authdomain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[id]
	if !ok {
		return nil, authdomain.ErrUserNotFound
	}
	copied := *user
	return &copied, nil
}

func (m *memUsers) Create(_ context.Context, user *authdomain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.users {
		if existing.Username == user.Username {
			return authdomain.ErrUsernameTaken
		}
	}
	copied := *user
	m.users[user.ID] = &copied
	return nil
}

func (m *memUsers) ExistsByUsername(ctx context.Context, username string) (bool, error) {
	_, err := m.FindByUsername(ctx, username)
	return err == nil, nil
}

func (m *memUsers) List(context.Context) ([]*authdomain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*authdomain.User
	for _, user := range m.users {
		copied := *user
		out = append(out, &copied)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
}

func (m *memUsers) Update(_ context.Context, user *authdomain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[user.ID]; !ok {
		return authdomain.ErrUserNotFound
	}
	copied := *user
	m.users[user.ID] = &copied
	return nil
}

func (m *memUsers) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[id]; !ok {
		return authdomain.ErrUserNotFound
	}
	delete(m.users, id)
	return nil
}

func (m *memUsers) CountByRole(_ context.Context, role authdomain.Role) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, user := range m.users {
		if user.Role == role {
			count++
		}
	}
	return count, nil
}

// memMemberships is an in-memory accessdomain.MembershipRepository.
type memMemberships struct {
	mu   sync.Mutex
	rows []*accessdomain.Membership
}

func (m *memMemberships) Create(_ context.Context, membership *accessdomain.Membership) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if row.PoolID == membership.PoolID && row.UserID == membership.UserID {
			return accessdomain.ErrMembershipExists
		}
	}
	copied := *membership
	m.rows = append(m.rows, &copied)
	return nil
}

func (m *memMemberships) Find(_ context.Context, poolID, userID string) (*accessdomain.Membership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if row.PoolID == poolID && row.UserID == userID {
			copied := *row
			return &copied, nil
		}
	}
	return nil, accessdomain.ErrMembershipNotFound
}

func (m *memMemberships) filter(match func(*accessdomain.Membership) bool) []*accessdomain.Membership {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*accessdomain.Membership
	for _, row := range m.rows {
		if match(row) {
			copied := *row
			out = append(out, &copied)
		}
	}
	return out
}

func (m *memMemberships) ListByPool(_ context.Context, poolID string) ([]*accessdomain.Membership, error) {
	return m.filter(func(r *accessdomain.Membership) bool { return r.PoolID == poolID }), nil
}

func (m *memMemberships) ListByUser(_ context.Context, userID string) ([]*accessdomain.Membership, error) {
	return m.filter(func(r *accessdomain.Membership) bool { return r.UserID == userID }), nil
}

func (m *memMemberships) ListAll(context.Context) ([]*accessdomain.Membership, error) {
	return m.filter(func(*accessdomain.Membership) bool { return true }), nil
}

func (m *memMemberships) Update(_ context.Context, membership *accessdomain.Membership) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, row := range m.rows {
		if row.PoolID == membership.PoolID && row.UserID == membership.UserID {
			copied := *membership
			m.rows[i] = &copied
			return nil
		}
	}
	return accessdomain.ErrMembershipNotFound
}

func (m *memMemberships) remove(match func(*accessdomain.Membership) bool) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.rows[:0]
	removed := 0
	for _, row := range m.rows {
		if match(row) {
			removed++
			continue
		}
		kept = append(kept, row)
	}
	m.rows = kept
	return removed
}

func (m *memMemberships) Delete(_ context.Context, poolID, userID string) error {
	if m.remove(func(r *accessdomain.Membership) bool { return r.PoolID == poolID && r.UserID == userID }) == 0 {
		return accessdomain.ErrMembershipNotFound
	}
	return nil
}

func (m *memMemberships) DeleteByUser(_ context.Context, userID string) error {
	m.remove(func(r *accessdomain.Membership) bool { return r.UserID == userID })
	return nil
}

func (m *memMemberships) DeleteByPool(_ context.Context, poolID string) error {
	m.remove(func(r *accessdomain.Membership) bool { return r.PoolID == poolID })
	return nil
}

// memPolicies is an in-memory accessdomain.PolicyRepository.
type memPolicies struct {
	mu       sync.Mutex
	policies map[string]accessdomain.PoolPolicy
}

func (m *memPolicies) Find(_ context.Context, poolID string) (accessdomain.PoolPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if policy, ok := m.policies[poolID]; ok {
		return policy, nil
	}
	return accessdomain.PoolPolicy{PoolID: poolID}, nil
}

func (m *memPolicies) ListAll(context.Context) ([]accessdomain.PoolPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []accessdomain.PoolPolicy
	for _, policy := range m.policies {
		out = append(out, policy)
	}
	return out, nil
}

func (m *memPolicies) Save(_ context.Context, policy accessdomain.PoolPolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.policies == nil {
		m.policies = map[string]accessdomain.PoolPolicy{}
	}
	m.policies[policy.PoolID] = policy
	return nil
}

func (m *memPolicies) Delete(_ context.Context, poolID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.policies, poolID)
	return nil
}

// memSettings is an in-memory accessdomain.SettingsRepository.
type memSettings struct{ settings accessdomain.Settings }

func (m *memSettings) Find(context.Context) (accessdomain.Settings, error) { return m.settings, nil }

func (m *memSettings) Save(_ context.Context, settings accessdomain.Settings) error {
	m.settings = settings
	return nil
}

// memAudit is an in-memory accessdomain.AuditRepository.
type memAudit struct{ entries []accessdomain.AuditEntry }

func (m *memAudit) Record(_ context.Context, entry accessdomain.AuditEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func (m *memAudit) List(_ context.Context, limit int) ([]accessdomain.AuditEntry, error) {
	var out []accessdomain.AuditEntry
	for i := len(m.entries) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, m.entries[i])
	}
	return out, nil
}

// counterUIDs hands out uids from start, never twice.
type counterUIDs struct{ next int }

func (c *counterUIDs) AllocateUID(context.Context) (int, error) {
	uid := c.next
	c.next++
	return uid, nil
}

// recordingCleanup records the Users whose data was removed.
type recordingCleanup struct{ removed []string }

func (c *recordingCleanup) RemoveUserData(_ context.Context, userID string) error {
	c.removed = append(c.removed, userID)
	return nil
}

// fixedPools is an accessdomain.PoolLookup over a fixed set of Pools.
type fixedPools map[string]accessdomain.PoolRef

func (p fixedPools) Pool(_ context.Context, poolID string) (accessdomain.PoolRef, bool, error) {
	pool, ok := p[poolID]
	return pool, ok, nil
}

// fixedServerPools places Servers in Pools ("" for none).
type fixedServerPools map[string]string

func (p fixedServerPools) PoolsOf(_ context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if pool, ok := p[id]; ok {
			out[id] = pool
		}
	}
	return out, nil
}

func (p fixedServerPools) ServersIn(_ context.Context, pools []string) ([]string, error) {
	var out []string
	for id, pool := range p {
		for _, want := range pools {
			if pool == want {
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// fixedPlatformPools puts Platforms in Pools.
type fixedPlatformPools map[string]string

func (p fixedPlatformPools) PoolOf(_ context.Context, id string) (string, bool, error) {
	pool, ok := p[id]
	return pool, ok, nil
}

// fixedWorkflowTargets gives Workflows their target Servers.
type fixedWorkflowTargets map[string][]string

func (t fixedWorkflowTargets) TargetsOf(_ context.Context, id string) ([]string, []string, bool, error) {
	servers, ok := t[id]
	return servers, nil, ok, nil
}
