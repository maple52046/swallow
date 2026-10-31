package convergence

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/maple52046/swallow/internal/core/store"
)

// Trigger says why a controller was asked to reconcile a key. A controller converges spec changes
// on any trigger but corrects drift of auto-correct fields only on TriggerResync (decision 071), so
// routine observation never turns into a stream of provider writes.
type Trigger string

const (
	// TriggerChange: a watched record changed (a spec write or a new observation).
	TriggerChange Trigger = "change"
	// TriggerResync: the periodic resync, run at start and every resync interval.
	TriggerResync Trigger = "resync"
)

// Request asks a controller to reconcile one resource.
type Request struct {
	Key     string
	Trigger Trigger
}

// Controller is the convergence loop for one resource kind (glossary: Controller). Reconcile must
// be level-triggered and idempotent: it reads the resource's current spec and status and acts on
// them, never on the event that woke it, and running it twice has the effect of running it once.
//
// Reconcile returns an error only for a transient failure, which the Runtime retries for that key
// with capped exponential backoff; a permanent failure is recorded by the controller itself (the
// requires_attention condition) and returned as nil. Keys lists every key to reconcile on resync.
// KeysForChange maps a change of one of Collections to the keys it affects; it may return none.
type Controller interface {
	Name() string
	Collections() []string
	KeysForChange(ctx context.Context, change store.Change) ([]string, error)
	Keys(ctx context.Context) ([]string, error)
	Reconcile(ctx context.Context, request Request) error
}

// RuntimeOptions configures a Runtime. Holder identifies this process in the singleton leases and
// must be unique per process. Zero durations take the defaults documented on each field.
type RuntimeOptions struct {
	Holder string
	// ResyncInterval is how often every key is reconciled with TriggerResync (default 10 minutes).
	ResyncInterval time.Duration
	// LeaseTTL is how long a singleton lease lasts without renewal (default 30 seconds); it is
	// renewed every third of it, so another process takes over within one TTL after a crash.
	LeaseTTL time.Duration
	// MaxBackoff caps the per-key retry delay after transient errors (default 5 minutes).
	MaxBackoff time.Duration
	Logger     *slog.Logger
}

// Runtime runs Controllers: for each one it competes for the singleton lease "controller:<name>",
// and while it holds the lease it watches the change feed, runs the periodic resync, and drives a
// work queue that reconciles one key at a time. Losing the lease (or ctx ending) stops that
// controller's work until the lease is won again. A Runtime is used by one Run call.
type Runtime struct {
	feed   store.ChangeFeed
	leases LeaseStore
	opts   RuntimeOptions
}

// NewRuntime returns a Runtime over the store's change feed and singleton leases.
func NewRuntime(feed store.ChangeFeed, leases LeaseStore, opts RuntimeOptions) *Runtime {
	if opts.ResyncInterval <= 0 {
		opts.ResyncInterval = 10 * time.Minute
	}
	if opts.LeaseTTL <= 0 {
		opts.LeaseTTL = 30 * time.Second
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 5 * time.Minute
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Runtime{feed: feed, leases: leases, opts: opts}
}

// Run runs every controller until ctx ends. It returns after all of them stopped and their leases
// were released, so a graceful shutdown hands each controller to another process at once instead of
// after the TTL.
func (r *Runtime) Run(ctx context.Context, controllers ...Controller) {
	var wg sync.WaitGroup
	for _, c := range controllers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.runController(ctx, c)
		}()
	}
	wg.Wait()
}

func (r *Runtime) runController(ctx context.Context, c Controller) {
	name := "controller:" + c.Name()
	log := r.opts.Logger.With("controller", c.Name())
	retry := r.opts.LeaseTTL / 3
	for ctx.Err() == nil {
		held, err := r.leases.TryAcquire(ctx, name, r.opts.Holder, r.opts.LeaseTTL)
		if err != nil && ctx.Err() == nil {
			log.Warn("controller lease unavailable", "error", err)
		}
		if !held {
			sleep(ctx, retry)
			continue
		}
		log.Info("controller active")
		activeCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			r.runActive(activeCtx, c, log)
		}()
		r.renew(activeCtx, name, log)
		cancel()
		<-done
		// The lease is released on a detached context so a shutting-down process hands the
		// controller over immediately; failing to release only delays takeover by one TTL.
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := r.leases.Release(releaseCtx, name, r.opts.Holder); err != nil {
			log.Warn("controller lease release failed", "error", err)
		}
		releaseCancel()
		log.Info("controller inactive")
	}
}

// renew keeps the lease until ctx ends or a renewal fails, then returns so the caller stops the
// controller's work: a process that cannot prove it still holds the lease must not act.
func (r *Runtime) renew(ctx context.Context, name string, log *slog.Logger) {
	ticker := time.NewTicker(r.opts.LeaseTTL / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			held, err := r.leases.TryAcquire(ctx, name, r.opts.Holder, r.opts.LeaseTTL)
			if err != nil || !held {
				if ctx.Err() == nil {
					log.Warn("controller lease lost", "error", err)
				}
				return
			}
		}
	}
}

func (r *Runtime) runActive(ctx context.Context, c Controller, log *slog.Logger) {
	q := newWorkQueue()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		r.watch(ctx, c, q, log)
	}()
	go func() {
		defer wg.Done()
		r.resync(ctx, c, q, log)
	}()
	backoff := map[string]time.Duration{}
	for {
		request, ok := q.next(ctx)
		if !ok {
			break
		}
		err := c.Reconcile(ctx, request)
		if ctx.Err() != nil {
			break
		}
		if err == nil {
			delete(backoff, request.Key)
			continue
		}
		delay := min(max(backoff[request.Key]*2, time.Second), r.opts.MaxBackoff)
		backoff[request.Key] = delay
		log.Warn("reconcile failed; retrying", "key", request.Key, "retryIn", delay, "error", err)
		q.addAfter(request, delay)
	}
	q.close()
	wg.Wait()
}

// watch feeds change-feed events into the queue, reconnecting with backoff when the feed fails. A
// reset (changes may have been missed) enqueues a full resync.
func (r *Runtime) watch(ctx context.Context, c Controller, q *workQueue, log *slog.Logger) {
	delay := time.Second
	for ctx.Err() == nil {
		err := r.feed.Watch(ctx, "controller:"+c.Name(), c.Collections(), func(ctx context.Context, change store.Change) error {
			if change.Operation == store.ChangeReset {
				r.enqueueAll(ctx, c, q, log)
				return nil
			}
			keys, err := c.KeysForChange(ctx, change)
			if err != nil {
				return err
			}
			for _, key := range keys {
				q.add(Request{Key: key, Trigger: TriggerChange})
			}
			return nil
		})
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return
		}
		log.Warn("change feed stopped; reconnecting", "retryIn", delay, "error", err)
		sleep(ctx, delay)
		delay = min(delay*2, r.opts.MaxBackoff)
	}
}

// resync enqueues every key at start and then every ResyncInterval.
func (r *Runtime) resync(ctx context.Context, c Controller, q *workQueue, log *slog.Logger) {
	r.enqueueAll(ctx, c, q, log)
	ticker := time.NewTicker(r.opts.ResyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.enqueueAll(ctx, c, q, log)
		}
	}
}

func (r *Runtime) enqueueAll(ctx context.Context, c Controller, q *workQueue, log *slog.Logger) {
	keys, err := c.Keys(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Warn("resync listing failed", "error", err)
		}
		return
	}
	for _, key := range keys {
		q.add(Request{Key: key, Trigger: TriggerResync})
	}
}

func sleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// workQueue holds at most one pending request per key, in arrival order. Adding a key that is
// already pending keeps its place and upgrades its trigger to resync if either request was a
// resync, so a resync is never lost to a later change event.
type workQueue struct {
	mu      sync.Mutex
	order   []string
	pending map[string]Trigger
	ready   chan struct{}
	closed  bool
}

func newWorkQueue() *workQueue {
	return &workQueue{pending: map[string]Trigger{}, ready: make(chan struct{}, 1)}
}

func (q *workQueue) add(request Request) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	if current, ok := q.pending[request.Key]; ok {
		if request.Trigger == TriggerResync || current == TriggerResync {
			q.pending[request.Key] = TriggerResync
		}
		return
	}
	q.pending[request.Key] = request.Trigger
	q.order = append(q.order, request.Key)
	select {
	case q.ready <- struct{}{}:
	default:
	}
}

func (q *workQueue) addAfter(request Request, delay time.Duration) {
	time.AfterFunc(delay, func() { q.add(request) })
}

// next blocks until a request is pending or ctx ends.
func (q *workQueue) next(ctx context.Context) (Request, bool) {
	for {
		q.mu.Lock()
		if len(q.order) > 0 {
			key := q.order[0]
			q.order = q.order[1:]
			trigger := q.pending[key]
			delete(q.pending, key)
			q.mu.Unlock()
			return Request{Key: key, Trigger: trigger}, true
		}
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return Request{}, false
		case <-q.ready:
		}
	}
}

func (q *workQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
}
