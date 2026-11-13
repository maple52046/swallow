package tests

import (
	"context"
	"sync"

	accessdomain "github.com/maple52046/swallow/internal/core/access/domain"
	serverdomain "github.com/maple52046/swallow/internal/core/server/domain"
)

// fakeMemberships is an in-memory accessdomain.MembershipRepository keyed by (pool, user).
type fakeMemberships struct {
	mu   sync.Mutex
	byID map[string]*accessdomain.Membership
}

func newFakeMemberships() *fakeMemberships {
	return &fakeMemberships{byID: map[string]*accessdomain.Membership{}}
}

func membershipKey(poolID, userID string) string { return poolID + "\x00" + userID }

// add gives userID the role in poolID, as an owner or an admin would.
func (m *fakeMemberships) add(poolID, userID string, role accessdomain.PoolRole) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byID[membershipKey(poolID, userID)] = &accessdomain.Membership{
		ID: membershipKey(poolID, userID), PoolID: poolID, UserID: userID, Role: role,
		Sudo: accessdomain.SudoInherit, SudoPasswordless: accessdomain.SudoInherit,
	}
}

func (m *fakeMemberships) Create(_ context.Context, membership *accessdomain.Membership) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := membershipKey(membership.PoolID, membership.UserID)
	if _, ok := m.byID[key]; ok {
		return accessdomain.ErrMembershipExists
	}
	copied := *membership
	m.byID[key] = &copied
	return nil
}

func (m *fakeMemberships) Find(_ context.Context, poolID, userID string) (*accessdomain.Membership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	membership, ok := m.byID[membershipKey(poolID, userID)]
	if !ok {
		return nil, accessdomain.ErrMembershipNotFound
	}
	copied := *membership
	return &copied, nil
}

func (m *fakeMemberships) list(match func(*accessdomain.Membership) bool) []*accessdomain.Membership {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*accessdomain.Membership
	for _, membership := range m.byID {
		if match(membership) {
			copied := *membership
			out = append(out, &copied)
		}
	}
	return out
}

func (m *fakeMemberships) ListByPool(_ context.Context, poolID string) ([]*accessdomain.Membership, error) {
	return m.list(func(x *accessdomain.Membership) bool { return x.PoolID == poolID }), nil
}

func (m *fakeMemberships) ListByUser(_ context.Context, userID string) ([]*accessdomain.Membership, error) {
	return m.list(func(x *accessdomain.Membership) bool { return x.UserID == userID }), nil
}

func (m *fakeMemberships) ListAll(context.Context) ([]*accessdomain.Membership, error) {
	return m.list(func(*accessdomain.Membership) bool { return true }), nil
}

func (m *fakeMemberships) Update(_ context.Context, membership *accessdomain.Membership) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := membershipKey(membership.PoolID, membership.UserID)
	if _, ok := m.byID[key]; !ok {
		return accessdomain.ErrMembershipNotFound
	}
	copied := *membership
	m.byID[key] = &copied
	return nil
}

func (m *fakeMemberships) Delete(_ context.Context, poolID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := membershipKey(poolID, userID)
	if _, ok := m.byID[key]; !ok {
		return accessdomain.ErrMembershipNotFound
	}
	delete(m.byID, key)
	return nil
}

func (m *fakeMemberships) DeleteByUser(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, membership := range m.byID {
		if membership.UserID == userID {
			delete(m.byID, key)
		}
	}
	return nil
}

func (m *fakeMemberships) DeleteByPool(_ context.Context, poolID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, membership := range m.byID {
		if membership.PoolID == poolID {
			delete(m.byID, key)
		}
	}
	return nil
}

// fakeServerPools is accessdomain.ServerPools over the fake Server repository.
type fakeServerPools struct{ servers *fakeServerRepo }

func (p fakeServerPools) PoolsOf(ctx context.Context, ids []string) (map[string]string, error) {
	result, err := p.servers.List(ctx, serverdomain.ListFilter{IDs: ids, IncludeAbsent: true})
	if err != nil {
		return nil, err
	}
	pools := map[string]string{}
	for _, server := range result.Servers {
		pools[server.ID] = server.Spec.PoolID
	}
	return pools, nil
}

func (p fakeServerPools) ServersIn(ctx context.Context, poolIDs []string) ([]string, error) {
	result, err := p.servers.List(ctx, serverdomain.ListFilter{PoolIDs: poolIDs, RestrictPools: true, IncludeAbsent: true})
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, server := range result.Servers {
		ids = append(ids, server.ID)
	}
	return ids, nil
}

// noWorkflowTargets knows no Workflow; these routes serve none.
type noWorkflowTargets struct{}

func (noWorkflowTargets) TargetsOf(context.Context, string) ([]string, []string, bool, error) {
	return nil, nil, false, nil
}
