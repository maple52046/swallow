package sshprobe

import (
	"context"
	"reflect"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestCandidatesPutsSiteUserFirstAndDedupes(t *testing.T) {
	got := Candidates("cloud-user")
	if want := []string{"cloud-user", "ubuntu"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Candidates(cloud-user) = %v, want %v", got, want)
	}
	// A site user equal to a built-in must not duplicate it, and its ordering stays first.
	if got := Candidates("ubuntu"); !reflect.DeepEqual(got, []string{"ubuntu", "cloud-user"}) {
		t.Fatalf("Candidates(ubuntu) = %v, want [ubuntu cloud-user]", got)
	}
	// An empty site user falls back to the built-ins only.
	if got := Candidates(""); !reflect.DeepEqual(got, []string{"cloud-user", "ubuntu"}) {
		t.Fatalf("Candidates(empty) = %v, want [cloud-user ubuntu]", got)
	}
}

// fakeProber returns a fixed outcome per candidate user.
type fakeProber struct{ byUser map[string]Outcome }

func (p fakeProber) Probe(_ context.Context, _ string, _ int, user string, _ ssh.Signer) Outcome {
	return p.byUser[user]
}

func TestResolveReturnsFirstAuthenticatingUser(t *testing.T) {
	prober := fakeProber{byUser: map[string]Outcome{"cloud-user": AuthFailed, "ubuntu": Ready}}
	user, outcome := Resolve(context.Background(), prober, "10.0.0.1", 22, nil, Candidates("cloud-user"))
	if outcome != Ready || user != "ubuntu" {
		t.Fatalf("Resolve = (%q, %v), want (ubuntu, Ready)", user, outcome)
	}
}

func TestResolveReportsAuthFailedWhenNoCandidateAuthenticates(t *testing.T) {
	prober := fakeProber{byUser: map[string]Outcome{"cloud-user": AuthFailed, "ubuntu": AuthFailed}}
	user, outcome := Resolve(context.Background(), prober, "10.0.0.1", 22, nil, Candidates("cloud-user"))
	if outcome != AuthFailed || user != "" {
		t.Fatalf("Resolve = (%q, %v), want (\"\", AuthFailed)", user, outcome)
	}
}

func TestResolveReportsUnreachableWhenHostNeverAnswers(t *testing.T) {
	prober := fakeProber{byUser: map[string]Outcome{"cloud-user": Unreachable, "ubuntu": Unreachable}}
	_, outcome := Resolve(context.Background(), prober, "10.0.0.1", 22, nil, Candidates("cloud-user"))
	if outcome != Unreachable {
		t.Fatalf("Resolve outcome = %v, want Unreachable", outcome)
	}
}
