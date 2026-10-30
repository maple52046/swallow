package application

import (
	"context"
	"errors"
	"testing"

	"github.com/maple52046/swallow/internal/core/store"
	"github.com/maple52046/swallow/internal/core/store/storetest"
	operationdomain "github.com/maple52046/swallow/internal/operation/domain"
)

// txRecordingWorkflowRepo records whether each write of a Workflow acceptance happened inside a
// store transaction. The embedded interface makes any method the test does not expect panic.
type txRecordingWorkflowRepo struct {
	operationdomain.WorkflowRepository
	appendErr  error
	writesInTx []bool
}

func (r *txRecordingWorkflowRepo) List(context.Context, operationdomain.WorkflowFilter) ([]*operationdomain.Workflow, int, error) {
	return nil, 0, nil
}

func (r *txRecordingWorkflowRepo) Create(ctx context.Context, _ *operationdomain.Workflow) error {
	r.writesInTx = append(r.writesInTx, store.InTransaction(ctx))
	return nil
}

func (r *txRecordingWorkflowRepo) AppendEvent(ctx context.Context, _ operationdomain.TimelineEvent) error {
	r.writesInTx = append(r.writesInTx, store.InTransaction(ctx))
	return r.appendErr
}

type txRecordingSecrets struct {
	operationdomain.OperationSecretRepository
	writesInTx []bool
}

func (s *txRecordingSecrets) Store(ctx context.Context, operationID, name string, _ any) (string, error) {
	s.writesInTx = append(s.writesInTx, store.InTransaction(ctx))
	return operationID + "/" + name, nil
}

func acceptanceInput() CreateWorkflowInput {
	return CreateWorkflowInput{
		Kind: operationdomain.WorkflowKindDeployOS, Definition: "deploy-os", DefinitionVersion: 1,
		SiteID: "site-1", TargetServerIDs: []string{"srv-1"},
		Steps:        []operationdomain.Task{{ID: "provision-os", Kind: "provision-os", Name: "Provision OS"}},
		SecretValues: map[string]any{"password": "s3cret"},
	}
}

// Accepting a Workflow writes its secrets, its record, and its first event in one transaction,
// so a failure leaves none of them behind (decision 069).
func TestCreateWritesSecretsWorkflowAndEventInOneTransaction(t *testing.T) {
	repo := &txRecordingWorkflowRepo{}
	secrets := &txRecordingSecrets{}
	tx := &storetest.Transactor{}
	service := NewWorkflowService(tx, repo, nil, secrets)

	if _, err := service.Create(context.Background(), acceptanceInput()); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got := tx.Calls(); got != 1 {
		t.Errorf("transactions = %d, want 1", got)
	}
	for _, writes := range [][]bool{secrets.writesInTx, repo.writesInTx} {
		for i, inTx := range writes {
			if !inTx {
				t.Errorf("write %d happened outside the transaction", i)
			}
		}
	}
	if len(secrets.writesInTx) != 1 || len(repo.writesInTx) != 2 {
		t.Errorf("writes: secrets=%d workflow=%d, want 1 and 2 (record and event)", len(secrets.writesInTx), len(repo.writesInTx))
	}
}

// A failed timeline write now fails the acceptance instead of being ignored, because it is part
// of the same unit of work.
func TestCreateFailsWhenItsFirstEventCannotBeRecorded(t *testing.T) {
	errStore := errors.New("store unavailable")
	repo := &txRecordingWorkflowRepo{appendErr: errStore}
	service := NewWorkflowService(&storetest.Transactor{}, repo, nil, &txRecordingSecrets{})

	if _, err := service.Create(context.Background(), acceptanceInput()); !errors.Is(err, errStore) {
		t.Errorf("Create() error = %v, want %v", err, errStore)
	}
}
