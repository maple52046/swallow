package storetest

import (
	"context"
	"slices"
	"sync"

	"github.com/maple52046/swallow/internal/core/store"
)

// ChangeFeed is a fake store.ChangeFeed: Publish delivers a change to every active watcher of its
// collection, in publish order. It keeps no position, so a change published while nobody watches
// is lost; tests that need a missed-change path publish a store.ChangeReset. The zero value is
// ready to use and safe for concurrent use.
type ChangeFeed struct {
	mu       sync.Mutex
	watchers map[*feedWatcher]struct{}
}

var _ store.ChangeFeed = (*ChangeFeed)(nil)

type feedWatcher struct {
	collections []string
	changes     chan store.Change
}

// Watch implements store.ChangeFeed. It returns ctx.Err() when ctx ends and handle's error as soon
// as handle fails.
func (f *ChangeFeed) Watch(ctx context.Context, _ string, collections []string, handle func(context.Context, store.Change) error) error {
	w := &feedWatcher{collections: collections, changes: make(chan store.Change, 1024)}
	f.mu.Lock()
	if f.watchers == nil {
		f.watchers = map[*feedWatcher]struct{}{}
	}
	f.watchers[w] = struct{}{}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.watchers, w)
		f.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case change := <-w.changes:
			if err := handle(ctx, change); err != nil {
				return err
			}
		}
	}
}

// Publish delivers change to the watchers of its collection; a ChangeReset reaches every watcher.
func (f *ChangeFeed) Publish(change store.Change) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for w := range f.watchers {
		if change.Operation == store.ChangeReset || slices.Contains(w.collections, change.Collection) {
			w.changes <- change
		}
	}
}

// Watchers reports how many watchers are active, so a test can wait until a subscriber is ready.
func (f *ChangeFeed) Watchers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.watchers)
}
