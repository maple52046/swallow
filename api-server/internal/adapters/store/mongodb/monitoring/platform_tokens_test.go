package monitoring

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/adapters/store/mongodb/mongotest"
	monitoringdomain "github.com/maple52046/swallow/internal/core/monitoring/domain"
)

// Saving replaces the Platform's hash; a Platform without one is ErrPlatformTokenNotFound.
func TestPlatformTokenStoreKeepsOneHashPerPlatform(t *testing.T) {
	_, db := mongotest.Database(t, "platform_tokens")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store := NewMongoPlatformTokenStore(db)

	if _, err := store.TokenHash(ctx, "p-1"); !errors.Is(err, monitoringdomain.ErrPlatformTokenNotFound) {
		t.Fatalf("TokenHash() before any token: error = %v, want ErrPlatformTokenNotFound", err)
	}
	for _, hash := range [][]byte{[]byte("first"), []byte("second")} {
		if err := store.SaveToken(ctx, "p-1", hash, time.Now()); err != nil {
			t.Fatalf("SaveToken(%s) error = %v", hash, err)
		}
	}
	got, err := store.TokenHash(ctx, "p-1")
	if err != nil || !bytes.Equal(got, []byte("second")) {
		t.Fatalf("TokenHash() = %q, %v; want the latest hash", got, err)
	}
	if count, _ := db.Collection(collection).CountDocuments(ctx, map[string]any{}); count != 1 {
		t.Errorf("documents = %d, want one per Platform", count)
	}
}
