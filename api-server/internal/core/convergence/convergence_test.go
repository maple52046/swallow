package convergence

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/store"
	"github.com/maple52046/swallow/internal/core/store/storetest"
)

func TestSetConditionKeepsTransitionTimeUntilStatusChanges(t *testing.T) {
	t0 := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	conds, changed := SetCondition(nil, Condition{Type: ConditionRealized, Status: ConditionFalse, Reason: ReasonConverging, ObservedGeneration: 1}, t0)
	if !changed || conds[0].LastTransitionTime != t0 {
		t.Fatalf("new condition = %+v, changed=%v; want transition at t0", conds, changed)
	}
	if _, changed := SetCondition(conds, Condition{Type: ConditionRealized, Status: ConditionFalse, Reason: ReasonConverging, ObservedGeneration: 1}, t1); changed {
		t.Error("an identical condition reported a change; controllers would write status for nothing")
	}
	same, changed := SetCondition(conds, Condition{Type: ConditionRealized, Status: ConditionFalse, Reason: ReasonProviderUnreachable, ObservedGeneration: 1}, t1)
	if !changed || same[0].LastTransitionTime != t0 {
		t.Errorf("reason change = %+v, changed=%v; want the t0 transition kept", same, changed)
	}
	flipped, _ := SetCondition(same, Condition{Type: ConditionRealized, Status: ConditionTrue, Reason: ReasonConverged, ObservedGeneration: 1}, t1)
	if flipped[0].LastTransitionTime != t1 {
		t.Errorf("status change kept transition %v, want %v", flipped[0].LastTransitionTime, t1)
	}
	if conds[0].Reason != ReasonConverging {
		t.Error("SetCondition modified its input slice")
	}
}

// memoryLeases is an in-process LeaseStore with the same semantics as the store implementation.
type memoryLeases struct {
	mu      sync.Mutex
	holders map[string]string
	expiry  map[string]time.Time
}

func (m *memoryLeases) TryAcquire(_ context.Context, name, holder string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.holders == nil {
		m.holders, m.expiry = map[string]string{}, map[string]time.Time{}
	}
	now := time.Now()
	if current, ok := m.holders[name]; ok && current != holder && now.Before(m.expiry[name]) {
		return false, nil
	}
	m.holders[name], m.expiry[name] = holder, now.Add(ttl)
	return true, nil
}

func (m *memoryLeases) Release(_ context.Context, name, holder string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.holders[name] == holder {
		delete(m.holders, name)
	}
	return nil
}

// recordingController reconciles by recording requests; failFirst makes the first Reconcile of each
// key fail transiently.
type recordingController struct {
	keys      []string
	failFirst bool

	mu       sync.Mutex
	requests []Request
	failed   map[string]bool
}

func (c *recordingController) Name() string          { return "test" }
func (c *recordingController) Collections() []string { return []string{"things"} }
func (c *recordingController) Keys(context.Context) ([]string, error) {
	return c.keys, nil
}
func (c *recordingController) KeysForChange(_ context.Context, change store.Change) ([]string, error) {
	return []string{change.Key}, nil
}
func (c *recordingController) Reconcile(_ context.Context, request Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request)
	if c.failFirst {
		if c.failed == nil {
			c.failed = map[string]bool{}
		}
		if !c.failed[request.Key] {
			c.failed[request.Key] = true
			return errors.New("provider unreachable")
		}
	}
	return nil
}

func (c *recordingController) seen() []Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.requests)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The runtime resyncs every key when it becomes active and reconciles a changed record with the
// change trigger, so a controller can tell routine observation from its scheduled resync.
func TestRuntimeResyncsOnStartAndReconcilesChanges(t *testing.T) {
	feed := &storetest.ChangeFeed{}
	controller := &recordingController{keys: []string{"a", "b"}}
	runtime := NewRuntime(feed, &memoryLeases{}, RuntimeOptions{Holder: "p1", ResyncInterval: time.Hour, LeaseTTL: 300 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runtime.Run(ctx, controller); close(done) }()

	waitFor(t, "the startup resync", func() bool { return len(controller.seen()) >= 2 })
	for _, request := range controller.seen()[:2] {
		if request.Trigger != TriggerResync {
			t.Errorf("startup request %+v, want trigger resync", request)
		}
	}
	waitFor(t, "the change feed watcher", func() bool { return feed.Watchers() == 1 })
	feed.Publish(store.Change{Collection: "things", Key: "c", Operation: store.ChangeUpdate})
	waitFor(t, "the change", func() bool { return len(controller.seen()) >= 3 })
	if got := controller.seen()[2]; got != (Request{Key: "c", Trigger: TriggerChange}) {
		t.Errorf("change request = %+v, want key c with trigger change", got)
	}
	feed.Publish(store.Change{Operation: store.ChangeReset})
	waitFor(t, "the reset resync", func() bool { return len(controller.seen()) >= 5 })

	cancel()
	<-done
}

// A transient error is retried for the same key with backoff instead of waiting for the next resync.
func TestRuntimeRetriesTransientErrors(t *testing.T) {
	controller := &recordingController{keys: []string{"a"}, failFirst: true}
	runtime := NewRuntime(&storetest.ChangeFeed{}, &memoryLeases{}, RuntimeOptions{Holder: "p1", ResyncInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runtime.Run(ctx, controller)
	waitFor(t, "the retry", func() bool { return len(controller.seen()) >= 2 })
	if got := controller.seen()[1]; got.Key != "a" || got.Trigger != TriggerResync {
		t.Errorf("retry = %+v, want the failed resync of a", got)
	}
}

// Only the lease holder reconciles; when it stops, another process takes the controller over.
func TestRuntimeRunsOneActiveInstance(t *testing.T) {
	leases := &memoryLeases{}
	first := &recordingController{keys: []string{"a"}}
	second := &recordingController{keys: []string{"a"}}
	opts := RuntimeOptions{ResyncInterval: time.Hour, LeaseTTL: 300 * time.Millisecond}
	opts.Holder = "p1"
	r1 := NewRuntime(&storetest.ChangeFeed{}, leases, opts)
	opts.Holder = "p2"
	r2 := NewRuntime(&storetest.ChangeFeed{}, leases, opts)

	ctx1, cancel1 := context.WithCancel(context.Background())
	done1 := make(chan struct{})
	go func() { r1.Run(ctx1, first); close(done1) }()
	waitFor(t, "the first instance", func() bool { return len(first.seen()) == 1 })

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go r2.Run(ctx2, second)
	time.Sleep(500 * time.Millisecond)
	if n := len(second.seen()); n != 0 {
		t.Fatalf("the second instance reconciled %d times while the first held the lease", n)
	}
	cancel1()
	<-done1
	waitFor(t, "the takeover", func() bool { return len(second.seen()) == 1 })
}

// The single-target rules: a spec change converges on any trigger, drift waits for the resync, a
// rejection stops until the spec changes, and a transient error is returned for retry.
func TestConvergeSingleTarget(t *testing.T) {
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	calls := 0
	realize := func(remaining string, err error) func(context.Context) (string, error) {
		return func(context.Context) (string, error) { calls++; return remaining, err }
	}
	rejected := func(err error) (string, bool) { return err.Error(), err.Error() == "refused" }
	base := SingleTarget{Now: now, Generation: 2, ObservedGeneration: 1, Rejected: rejected}

	pending := base
	pending.Trigger, pending.Drift, pending.Realize = TriggerChange, "differs", realize("", nil)
	r := ConvergeSingleTarget(context.Background(), pending)
	if r.Err != nil || r.ObservedGeneration != 2 || !IsTrue(r.Conditions, ConditionRealized) || calls != 1 {
		t.Fatalf("pending change: %+v (calls %d)", r, calls)
	}

	drift := base
	drift.ObservedGeneration, drift.Conditions = 2, r.Conditions
	drift.Trigger, drift.Drift, drift.Realize = TriggerChange, "changed outside", realize("", nil)
	r = ConvergeSingleTarget(context.Background(), drift)
	if calls != 1 || !IsTrue(r.Conditions, ConditionDrifted) || r.Events[0].Type != EventDriftDetected {
		t.Fatalf("drift on change: %+v (calls %d)", r, calls)
	}
	drift.Conditions, drift.Trigger = r.Conditions, TriggerResync
	r = ConvergeSingleTarget(context.Background(), drift)
	if calls != 2 || IsTrue(r.Conditions, ConditionDrifted) || r.Events[0].Type != EventDriftCorrected {
		t.Fatalf("drift at resync: %+v (calls %d)", r, calls)
	}

	refusal := base
	refusal.Trigger, refusal.Drift, refusal.Realize = TriggerChange, "differs", realize("", errors.New("refused"))
	r = ConvergeSingleTarget(context.Background(), refusal)
	if r.Err != nil || !IsTrue(r.Conditions, ConditionRequiresAttention) {
		t.Fatalf("rejection: %+v", r)
	}
	refusal.Conditions, refusal.Trigger = r.Conditions, TriggerResync
	before := calls
	ConvergeSingleTarget(context.Background(), refusal)
	if calls != before {
		t.Fatal("a rejected change was retried before the spec changed")
	}

	transient := base
	transient.Trigger, transient.Drift, transient.Realize = TriggerChange, "differs", realize("", errors.New("timeout"))
	if r = ConvergeSingleTarget(context.Background(), transient); r.Err == nil {
		t.Fatal("a transient error must be returned")
	}
	stuck := base
	stuck.Trigger, stuck.Drift, stuck.Realize = TriggerChange, "differs", realize("still differs", nil)
	if r = ConvergeSingleTarget(context.Background(), stuck); r.Err == nil || IsTrue(r.Conditions, ConditionRealized) {
		t.Fatalf("a target that still differs: %+v", r)
	}
}
