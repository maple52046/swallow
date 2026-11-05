package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/core/platform/domain"
)

// syncPlatforms is the PlatformRepository of a membership read: one Platform and the sync state
// last written for it.
type syncPlatforms struct {
	platformdomain.PlatformRepository
	platform *platformdomain.Platform
	state    platformdomain.SyncState
}

func (p *syncPlatforms) FindByID(context.Context, string) (*platformdomain.Platform, error) {
	return p.platform, nil
}

func (p *syncPlatforms) UpdateSyncState(_ context.Context, _, _ string, state platformdomain.SyncState) error {
	p.state = state
	return nil
}

// failingReaders builds a reader whose every membership read fails with err.
type failingReaders struct{ err error }

func (f failingReaders) For(context.Context, *platformdomain.Platform) (platformdomain.PlatformReader, error) {
	return failingReader(f), nil
}

type failingReader struct{ err error }

func (r failingReader) ListMembers(context.Context) ([]platformdomain.Member, error) {
	return nil, r.err
}

// A failed read records one redacted, actionable sentence as both the report's error and the
// Platform's sync.lastError, and keeps the cause for a caller to classify.
func TestMembershipSyncRecordsTheFailureSentence(t *testing.T) {
	readerErr := &platformdomain.ReaderError{
		Kind: platformdomain.ReaderErrorUnavailable, Detail: "Could not reach the platform API.",
		Endpoint: "https://admin:hunter2@10.0.0.5:6443?token=s3cr3t", Operation: "list nodes",
		Transport: platformdomain.TransportRefused, Err: errors.New("dial tcp 10.0.0.5:6443: connect: connection refused"),
	}
	cases := []struct {
		name  string
		cause error
		want  []string
	}{
		{"platform API failure", readerErr, []string{
			"Could not reach the platform API: the connection was refused",
			"list nodes at https://10.0.0.5:6443?token=[REDACTED]",
			"connect: connection refused",
		}},
		{"other failure", errors.New("read credential: password=hunter2 rejected"), []string{"password=[REDACTED] rejected"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			platforms := &syncPlatforms{platform: &platformdomain.Platform{ID: "p1", Name: "lab", IntegrationID: "int-1"}}
			report, err := NewMembershipSyncUseCase(platforms, nil, failingReaders{err: tc.cause}).Execute(context.Background(), "p1")
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if report.Error == nil || *report.Error != platforms.state.LastError {
				t.Fatalf("report error %v, lastError %q, want the same sentence", report.Error, platforms.state.LastError)
			}
			for _, want := range tc.want {
				if !strings.Contains(platforms.state.LastError, want) {
					t.Errorf("lastError = %q, want it to contain %q", platforms.state.LastError, want)
				}
			}
			for _, secret := range []string{"hunter2", "s3cr3t"} {
				if strings.Contains(platforms.state.LastError, secret) {
					t.Errorf("lastError = %q shows %q", platforms.state.LastError, secret)
				}
			}
			if !errors.Is(report.Err, tc.cause) || report.IntegrationID != "int-1" {
				t.Errorf("report keeps cause %v and integration %q, want %v and int-1", report.Err, report.IntegrationID, tc.cause)
			}
		})
	}
}
