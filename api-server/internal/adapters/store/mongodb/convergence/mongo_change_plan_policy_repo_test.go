package convergence

import (
	"context"
	"errors"
	"testing"
	"time"

	storemongo "github.com/maple52046/swallow/internal/adapters/store/mongodb"
	"github.com/maple52046/swallow/internal/core/convergence"
)

// No stored policy is a domain miss, a saved policy reads back whole, and a second save replaces
// the single document instead of adding one.
func TestChangePlanPolicyRepoStoresOnePolicy(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)
	repo := NewMongoChangePlanPolicyRepo(db)
	if _, err := repo.Find(ctx); !errors.Is(err, convergence.ErrChangePlanPolicyNotFound) {
		t.Fatalf("Find() before any save error = %v, want ErrChangePlanPolicyNotFound", err)
	}

	at := time.Date(2026, 11, 5, 2, 0, 0, 0, time.UTC)
	for _, policy := range []convergence.ChangePlanPolicy{
		{ApproveOnSubmit: false, UpdatedAt: at, UpdatedBy: "admin"},
		{ApproveOnSubmit: true, UpdatedAt: at.Add(time.Minute), UpdatedBy: "ops"},
	} {
		if err := repo.Save(ctx, policy); err != nil {
			t.Fatalf("Save(%+v) error = %v", policy, err)
		}
		got, err := repo.Find(ctx)
		if err != nil || got != policy {
			t.Errorf("Find() after Save(%+v) = %+v, %v", policy, got, err)
		}
	}
	if count, err := db.Collection(ChangePlanPolicyCollection).CountDocuments(ctx, map[string]any{}); err != nil || count != 1 {
		t.Errorf("policy documents = %d, %v; want exactly one", count, err)
	}
}

// A spec write reads the policy inside its transaction, also before the collection exists.
func TestChangePlanPolicyRepoReadsInsideATransaction(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)
	repo := NewMongoChangePlanPolicyRepo(db)
	transactor := storemongo.NewTransactor(db.Client())
	err := transactor.InTransaction(ctx, func(ctx context.Context) error {
		_, err := repo.Find(ctx)
		return err
	})
	if !errors.Is(err, convergence.ErrChangePlanPolicyNotFound) {
		t.Fatalf("Find() in a transaction before any save error = %v, want ErrChangePlanPolicyNotFound", err)
	}
	if err := repo.Save(ctx, convergence.ChangePlanPolicy{ApproveOnSubmit: false, UpdatedBy: "admin"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	var got convergence.ChangePlanPolicy
	if err := transactor.InTransaction(ctx, func(ctx context.Context) error {
		var err error
		got, err = repo.Find(ctx)
		return err
	}); err != nil || got.ApproveOnSubmit {
		t.Errorf("Find() in a transaction = %+v, %v; want the stored switch", got, err)
	}
}
