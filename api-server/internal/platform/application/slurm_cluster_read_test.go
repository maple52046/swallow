package application

import (
	"context"
	"errors"
	"testing"

	platformdomain "github.com/maple52046/swallow/internal/platform/domain"
)

// slurmReadRepo is a minimal PlatformRepository for the cluster-read use case: it embeds the
// interface (unused methods stay nil) and returns one platform from FindByID.
type slurmReadRepo struct {
	platformdomain.PlatformRepository
	platform *platformdomain.Platform
	err      error
}

func (r slurmReadRepo) FindByID(context.Context, string) (*platformdomain.Platform, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.platform, nil
}

// slurmReadFactory returns a fixed reader (or error) regardless of platform.
type slurmReadFactory struct {
	reader platformdomain.PlatformReader
	err    error
}

func (f slurmReadFactory) For(context.Context, *platformdomain.Platform) (platformdomain.PlatformReader, error) {
	return f.reader, f.err
}

// clusterReader implements both PlatformReader and the SlurmClusterReader superset.
type clusterReader struct {
	state *platformdomain.SlurmClusterState
}

func (clusterReader) ListMembers(context.Context) ([]platformdomain.Member, error) { return nil, nil }
func (r clusterReader) GetClusterState(context.Context) (*platformdomain.SlurmClusterState, error) {
	return r.state, nil
}

// plainReader is a PlatformReader that is not a SlurmClusterReader.
type plainReader struct{}

func (plainReader) ListMembers(context.Context) ([]platformdomain.Member, error) { return nil, nil }

func slurmPlatform() *platformdomain.Platform {
	return &platformdomain.Platform{ID: "p1", Type: platformdomain.PlatformTypeSlurm, IntegrationID: "i1"}
}

func TestGetSlurmClusterReadsLiveState(t *testing.T) {
	want := &platformdomain.SlurmClusterState{
		Controllers: []platformdomain.SlurmController{{Hostname: "control-1", Primary: true, Status: "up"}},
	}
	uc := NewGetSlurmClusterUseCase(
		slurmReadRepo{platform: slurmPlatform()},
		slurmReadFactory{reader: clusterReader{state: want}},
	)
	got, err := uc.Execute(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(got.Controllers) != 1 || got.Controllers[0].Hostname != "control-1" {
		t.Fatalf("Execute() = %+v, want the reader's cluster state", got)
	}
}

func TestGetSlurmClusterRejectsNonSlurm(t *testing.T) {
	platform := slurmPlatform()
	platform.Type = platformdomain.PlatformTypeKubernetes
	uc := NewGetSlurmClusterUseCase(slurmReadRepo{platform: platform}, slurmReadFactory{reader: clusterReader{}})

	_, err := uc.Execute(context.Background(), "p1")
	if !errors.Is(err, platformdomain.ErrUnsupportedPlatformType) {
		t.Fatalf("Execute() error = %v, want ErrUnsupportedPlatformType", err)
	}
}

func TestGetSlurmClusterPropagatesNoIntegration(t *testing.T) {
	uc := NewGetSlurmClusterUseCase(
		slurmReadRepo{platform: slurmPlatform()},
		slurmReadFactory{err: platformdomain.ErrNoPlatformIntegration},
	)
	_, err := uc.Execute(context.Background(), "p1")
	if !errors.Is(err, platformdomain.ErrNoPlatformIntegration) {
		t.Fatalf("Execute() error = %v, want ErrNoPlatformIntegration", err)
	}
}

func TestGetSlurmClusterRejectsReaderWithoutClusterState(t *testing.T) {
	uc := NewGetSlurmClusterUseCase(
		slurmReadRepo{platform: slurmPlatform()},
		slurmReadFactory{reader: plainReader{}},
	)
	_, err := uc.Execute(context.Background(), "p1")
	if !errors.Is(err, platformdomain.ErrUnsupportedPlatformType) {
		t.Fatalf("Execute() error = %v, want ErrUnsupportedPlatformType", err)
	}
}
