package store_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/maple52046/swallow/internal/core/store"
	"github.com/maple52046/swallow/internal/core/store/storetest"
)

func TestAfterCommitOutsideTransactionRunsImmediately(t *testing.T) {
	ran := false
	store.AfterCommit(context.Background(), func() { ran = true })
	if !ran {
		t.Error("AfterCommit outside a transaction did not run the hook immediately")
	}
}

func TestAfterCommitRunsHooksInOrderOnlyAfterCommit(t *testing.T) {
	var tx storetest.Transactor
	var order []string
	err := tx.InTransaction(context.Background(), func(ctx context.Context) error {
		if !store.InTransaction(ctx) {
			t.Error("InTransaction(ctx) = false inside the unit of work")
		}
		store.AfterCommit(ctx, func() { order = append(order, "first") })
		store.AfterCommit(ctx, func() { order = append(order, "second") })
		if len(order) != 0 {
			t.Errorf("hooks ran before the commit: %v", order)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("InTransaction() error = %v", err)
	}
	if want := []string{"first", "second"}; !reflect.DeepEqual(order, want) {
		t.Errorf("hooks ran %v, want %v", order, want)
	}
}

func TestFailedUnitOfWorkDropsHooksAndReturnsItsError(t *testing.T) {
	errDomain := errors.New("conflict")
	tests := []struct {
		name    string
		tx      *storetest.Transactor
		fnErr   error
		wantErr error
	}{
		{name: "fn fails", tx: &storetest.Transactor{}, fnErr: errDomain, wantErr: errDomain},
		{name: "commit fails", tx: &storetest.Transactor{CommitErr: errDomain}, wantErr: errDomain},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ran := false
			err := tc.tx.InTransaction(context.Background(), func(ctx context.Context) error {
				store.AfterCommit(ctx, func() { ran = true })
				return tc.fnErr
			})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("InTransaction() error = %v, want %v", err, tc.wantErr)
			}
			if ran {
				t.Error("a hook ran although the unit of work did not commit")
			}
		})
	}
}

func TestNestedInTransactionJoinsTheOuterUnitOfWork(t *testing.T) {
	var tx storetest.Transactor
	ran := false
	err := tx.InTransaction(context.Background(), func(ctx context.Context) error {
		return tx.InTransaction(ctx, func(inner context.Context) error {
			store.AfterCommit(inner, func() { ran = true })
			return nil
		})
	})
	if err != nil {
		t.Fatalf("InTransaction() error = %v", err)
	}
	if got := tx.Calls(); got != 1 {
		t.Errorf("Calls() = %d, want 1 (the nested call joins)", got)
	}
	if !ran {
		t.Error("the nested hook did not run after the outer commit")
	}
}

func TestResetDropsHooksOfARetriedAttempt(t *testing.T) {
	ctx, scope := store.WithCommitScope(context.Background())
	attempts := 0
	store.AfterCommit(ctx, func() { attempts++ })
	scope.Reset()
	store.AfterCommit(ctx, func() { attempts++ })
	scope.Run()
	scope.Run()
	if attempts != 1 {
		t.Errorf("hooks ran %d times, want 1 (only the last attempt, once)", attempts)
	}
}
