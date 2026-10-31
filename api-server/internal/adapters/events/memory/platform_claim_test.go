package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// claimRepo is an in-memory ServerRepository and PlatformClaimStore for the decorator test; only
// the methods the decorator calls are meaningful.
type claimRepo struct {
	serverdomain.ServerRepository
	claims map[string]string
}

func (r *claimRepo) FindByID(_ context.Context, id string) (*serverdomain.Server, error) {
	server := &serverdomain.Server{ID: id}
	if platformID, ok := r.claims[id]; ok {
		server.PlatformClaim = &serverdomain.PlatformClaim{PlatformID: platformID}
	}
	return server, nil
}

func (r *claimRepo) ClaimForPlatform(_ context.Context, platformID string, serverIDs []string, _ time.Time) error {
	for _, id := range serverIDs {
		if holder, ok := r.claims[id]; ok && holder != platformID {
			return &serverdomain.ServerClaimedError{ServerID: id, PlatformID: holder}
		}
	}
	for _, id := range serverIDs {
		r.claims[id] = platformID
	}
	return nil
}

func (r *claimRepo) ReleasePlatformClaims(_ context.Context, platformID string) error {
	for id, holder := range r.claims {
		if holder == platformID {
			delete(r.claims, id)
		}
	}
	return nil
}

func (r *claimRepo) ClaimingPlatformIDs(context.Context) ([]string, error) {
	seen := map[string]bool{}
	ids := []string{}
	for _, holder := range r.claims {
		if !seen[holder] {
			seen[holder] = true
			ids = append(ids, holder)
		}
	}
	return ids, nil
}

func (r *claimRepo) ClaimedServerIDs(_ context.Context, platformID string) ([]string, error) {
	ids := []string{}
	for id, holder := range r.claims {
		if holder == platformID {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

type recordingPublisher struct{ events []serverdomain.ServerEvent }

func (p *recordingPublisher) PublishServerEvent(event serverdomain.ServerEvent) {
	p.events = append(p.events, event)
}

// The decorator persists claims through the inner store and publishes every Server whose claim
// changed, so a list shows the claim (and its release) without a reload.
func TestEventingRepositoryPublishesPlatformClaimChanges(t *testing.T) {
	inner := &claimRepo{claims: map[string]string{"srv-9": "platform-b"}}
	publisher := &recordingPublisher{}
	repo := NewEventingServerRepository(inner, publisher)
	ctx := context.Background()

	if err := repo.ClaimForPlatform(ctx, "platform-a", []string{"srv-1", "srv-2"}, time.Now()); err != nil {
		t.Fatalf("ClaimForPlatform() error = %v", err)
	}
	if len(publisher.events) != 2 || publisher.events[0].Server.PlatformClaim == nil {
		t.Fatalf("published %+v, want both claimed Servers with their claim", publisher.events)
	}

	err := repo.ClaimForPlatform(ctx, "platform-a", []string{"srv-9"}, time.Now())
	var claimed *serverdomain.ServerClaimedError
	if !errors.As(err, &claimed) || claimed.PlatformID != "platform-b" {
		t.Fatalf("ClaimForPlatform(srv-9) error = %v, want a ServerClaimedError naming platform-b", err)
	}
	if len(publisher.events) != 2 {
		t.Errorf("a refused claim published %d more events, want none", len(publisher.events)-2)
	}

	if err := repo.ReleasePlatformClaims(ctx, "platform-a"); err != nil {
		t.Fatalf("ReleasePlatformClaims() error = %v", err)
	}
	if len(publisher.events) != 4 || publisher.events[3].Server.PlatformClaim != nil {
		t.Errorf("published %+v, want the two released Servers without a claim", publisher.events)
	}
	if _, ok := inner.claims["srv-9"]; !ok {
		t.Error("releasing platform-a cleared platform-b's claim")
	}
}
