package application

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	"github.com/maple52046/swallow/internal/core/localrepo/domain/localrepotest"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

var testModules = []localrepodomain.ModuleDeclaration{{
	ModuleID: "kubernetes",
	Sources:  []localrepodomain.Source{debSource("docker-ce", "https://download.example/ubuntu", []string{"noble"}, []string{"stable"})},
	Files:    []localrepodomain.File{{Path: "k0s/k0s-v1", URL: "https://files.example/k0s-v1", SHA256: "aa"}},
	Images:   []string{"quay.io/k0sproject/pause:3.10"},
}}

type controllerFixture struct {
	controller   *Controller
	records      *localrepotest.Store
	repository   *fakeRepository
	integrations *fakeIntegrations
	// factoryErr, when set, is what the Repository factory fails with.
	factoryErr *error
	now        time.Time
}

func newControllerFixture(integrations ...*sitedomain.Integration) controllerFixture {
	repository := newFakeRepository()
	known := map[string]bool{}
	for _, integration := range integrations {
		known[integration.ID] = true
	}
	f := controllerFixture{
		records: localrepotest.NewStore(), repository: repository, integrations: newFakeIntegrations(integrations...),
		factoryErr: new(error), now: time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC),
	}
	f.controller = NewController(fakeSites{ids: map[string]bool{"site-1": true}}, f.integrations, f.records,
		fakeRepositories{repository: repository, known: known, err: f.factoryErr}, testModules)
	f.controller.now = func() time.Time { return f.now }
	return f
}

// condition returns the record's condition of type t, failing the test when it is missing.
func condition(t *testing.T, record *localrepodomain.LocalRepository, conditionType convergence.ConditionType) convergence.Condition {
	t.Helper()
	found, ok := convergence.FindCondition(record.Status.Conditions, conditionType)
	if !ok {
		t.Fatalf("no %s condition in %+v", conditionType, record.Status.Conditions)
	}
	return found
}

// wantCondition checks a condition's status and reason, and that its message mentions mentions.
func wantCondition(t *testing.T, record *localrepodomain.LocalRepository, conditionType convergence.ConditionType, status convergence.ConditionStatus, reason, mentions string) {
	t.Helper()
	got := condition(t, record, conditionType)
	if got.Status != status || got.Reason != reason || !strings.Contains(got.Message, mentions) || got.ObservedGeneration != record.Generation {
		t.Errorf("%s = %+v, want %s/%s mentioning %q for generation %d", conditionType, got, status, reason, mentions, record.Generation)
	}
}

func (f controllerFixture) reconcile(t *testing.T, siteID string) error {
	t.Helper()
	return f.controller.Reconcile(context.Background(), convergence.Request{Key: siteID, Trigger: convergence.TriggerResync})
}

func (f controllerFixture) record(t *testing.T, siteID string) *localrepodomain.LocalRepository {
	t.Helper()
	record, err := f.records.Get(context.Background(), siteID)
	if err != nil {
		t.Fatalf("Get(%s): %v", siteID, err)
	}
	return record
}

// One pass ensures every effective source (declared and pull-through), file, and image of a ready
// registry source, reads the endpoints, and records it all; while a source syncs the pass asks to
// run again soon, and once everything is settled it does not.
func TestControllerEnsuresTheEffectiveSpecAndRetriesWhileSyncing(t *testing.T) {
	f := newControllerFixture(repositoryIntegrationOf("site-1"))
	f.records.Put(localrepodomain.LocalRepository{SiteID: "site-1", Generation: 2, Spec: localrepodomain.Spec{ServeDeployments: true, Sources: []localrepodomain.Source{
		debSource("ubuntu", "http://archive.example/ubuntu/", []string{"noble"}, []string{"main"}),
	}}})
	f.repository.sourceState["ubuntu"] = localrepodomain.SyncSyncing
	f.repository.endpoints = localrepodomain.HostEndpoints{Registry: "10.0.0.5:8080"}

	err := f.reconcile(t, "site-1")
	if !errors.Is(err, errStillSyncing) {
		t.Fatalf("Reconcile while syncing = %v, want errStillSyncing", err)
	}
	wantEnsured := []string{"source docker-ce", "source quay.io", "source ubuntu", "file k0s/k0s-v1", "image quay.io/k0sproject/pause:3.10"}
	if !reflect.DeepEqual(f.repository.ensured, wantEnsured) {
		t.Errorf("ensured %v, want %v", f.repository.ensured, wantEnsured)
	}
	record := f.record(t, "site-1")
	status := record.Status
	if status.IntegrationID != "repo-site-1" || status.ObservedGeneration != 2 || status.Endpoints == nil ||
		status.Endpoints.Registry != "10.0.0.5:8080" || status.ObservedAt == nil || !status.ObservedAt.Equal(f.now) {
		t.Errorf("status = %+v; want the Integration, generation 2, the endpoints, and the pass time", status)
	}
	if len(status.Sources) != 3 || status.Sources[2].Status.State != localrepodomain.SyncSyncing || len(status.Files) != 1 {
		t.Errorf("status sources %+v files %+v; want three sources with ubuntu syncing and one file", status.Sources, status.Files)
	}
	wantCondition(t, record, convergence.ConditionRealized, convergence.ConditionFalse, localrepodomain.ReasonSyncing, "source ubuntu is syncing")
	wantCondition(t, record, localrepodomain.ConditionReady, convergence.ConditionFalse, localrepodomain.ReasonSyncing, "source ubuntu is syncing")
	wantCondition(t, record, convergence.ConditionRequiresAttention, convergence.ConditionFalse, convergence.ReasonAttentionNotNeeded, "")

	f.repository.sourceState["ubuntu"] = localrepodomain.SyncReady
	f.repository.ensured = nil
	if err := f.reconcile(t, "site-1"); err != nil {
		t.Fatalf("Reconcile once settled = %v, want nil", err)
	}
	settled := f.record(t, "site-1")
	wantCondition(t, settled, convergence.ConditionRealized, convergence.ConditionTrue, convergence.ReasonConverged, "")
	wantCondition(t, settled, localrepodomain.ConditionReady, convergence.ConditionTrue, localrepodomain.ReasonReady, "")
	if !slices.Contains(f.repository.ensured, "image quay.io/k0sproject/pause:3.10") {
		t.Errorf("ensured %v, want the image ensured again on every pass", f.repository.ensured)
	}
	wantImages := []localrepodomain.ObservedImage{{Reference: "quay.io/k0sproject/pause:3.10", Status: localrepodomain.ImageStatus{
		Reference: "quay.io/k0sproject/pause:3.10", State: localrepodomain.SyncReady, SizeBytes: 2048,
	}}}
	if got := settled.Status.Images; !reflect.DeepEqual(got, wantImages) {
		t.Errorf("Images = %+v, want %+v", got, wantImages)
	}
}

// A declared image is pending without asking the provider while its registry source is not ready,
// then observed through the provider until it is stored: still being fetched keeps the pass
// retrying, a refusal is that image's failure, a provider that cannot be reached stops the pass,
// and a ready image completes the repository. A pending image whose source failed does not keep
// the pass retrying.
func TestControllerObservesDeclaredImagesUntilStored(t *testing.T) {
	const image = "quay.io/k0sproject/pause:3.10"
	f := newControllerFixture(repositoryIntegrationOf("site-1"))
	f.records.Put(servingRecord("site-1"))
	imageState := func() localrepodomain.ImageStatus {
		t.Helper()
		images := f.record(t, "site-1").Status.Images
		if len(images) != 1 || images[0].Reference != image {
			t.Fatalf("Images = %+v, want the one declared image", images)
		}
		return images[0].Status
	}

	f.repository.sourceState["quay.io"] = localrepodomain.SyncSyncing
	if err := f.reconcile(t, "site-1"); !errors.Is(err, errStillSyncing) || !strings.Contains(err.Error(), "image "+image) {
		t.Fatalf("Reconcile while the source syncs = %v, want errStillSyncing naming the image", err)
	}
	if slices.Contains(f.repository.ensured, "image "+image) || imageState().State != localrepodomain.SyncPending {
		t.Errorf("ensured %v, image %+v; want the image pending and not asked for", f.repository.ensured, imageState())
	}
	wantCondition(t, f.record(t, "site-1"), localrepodomain.ConditionReady, convergence.ConditionFalse, localrepodomain.ReasonSyncing, "image "+image+" is pending")

	f.repository.sourceState["quay.io"] = localrepodomain.SyncReady
	f.repository.imageState[image] = localrepodomain.SyncSyncing
	if err := f.reconcile(t, "site-1"); !errors.Is(err, errStillSyncing) {
		t.Fatalf("Reconcile while the image downloads = %v, want errStillSyncing", err)
	}
	wantCondition(t, f.record(t, "site-1"), convergence.ConditionRealized, convergence.ConditionFalse, localrepodomain.ReasonSyncing, "image "+image+" is syncing")

	f.repository.errs["image "+image] = refused("The registry has no image quay.io/k0sproject/pause:3.10.")
	if err := f.reconcile(t, "site-1"); err != nil {
		t.Fatalf("Reconcile with a refused image = %v, want nil (a refusal is recorded, not retried)", err)
	}
	if got := imageState(); got.State != localrepodomain.SyncFailed || got.Message != "The registry has no image quay.io/k0sproject/pause:3.10." {
		t.Errorf("image = %+v, want failed with the provider's detail", got)
	}
	failed := f.record(t, "site-1")
	wantCondition(t, failed, convergence.ConditionRealized, convergence.ConditionFalse, localrepodomain.ReasonImageFailed, "The registry has no image")
	wantCondition(t, failed, localrepodomain.ConditionReady, convergence.ConditionFalse, localrepodomain.ReasonImageFailed, "image "+image+" failed")

	f.now = f.now.Add(time.Minute)
	f.repository.errs["image "+image] = unavailable("Could not reach Pulp.")
	if err := f.reconcile(t, "site-1"); err == nil {
		t.Fatal("Reconcile with an unreachable provider = nil, want its error for the runtime's backoff")
	}
	stale := f.record(t, "site-1")
	if !reflect.DeepEqual(stale.Status.Images, failed.Status.Images) {
		t.Errorf("images changed while unreachable: %+v", stale.Status.Images)
	}
	wantCondition(t, stale, convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonProviderUnreachable, "Could not reach Pulp")

	delete(f.repository.errs, "image "+image)
	delete(f.repository.imageState, image)
	if err := f.reconcile(t, "site-1"); err != nil {
		t.Fatalf("Reconcile once the image is stored = %v, want nil", err)
	}
	if got := imageState(); got.State != localrepodomain.SyncReady || got.SizeBytes != 2048 {
		t.Errorf("image = %+v, want ready with its size", got)
	}
	wantCondition(t, f.record(t, "site-1"), localrepodomain.ConditionReady, convergence.ConditionTrue, localrepodomain.ReasonReady, "")

	f.repository.errs["source quay.io"] = refused("The registry URL does not answer.")
	if err := f.reconcile(t, "site-1"); err != nil {
		t.Fatalf("Reconcile with the image's source failed = %v, want nil: the image waits for the source", err)
	}
	if got := imageState(); got.State != localrepodomain.SyncPending {
		t.Errorf("image of a failed source = %+v, want pending", got)
	}
	wantCondition(t, f.record(t, "site-1"), localrepodomain.ConditionReady, convergence.ConditionFalse, localrepodomain.ReasonSourceFailed, "image "+image+" is pending")
}

// A refused source is recorded failed with the provider's detail and the pass goes on; a provider
// that cannot be reached stops the pass with an error and records only the conditions, once, so the
// items stay as last observed.
func TestControllerRecordsRefusalsAndKeepsStatusWhenUnreachable(t *testing.T) {
	f := newControllerFixture(repositoryIntegrationOf("site-1"))
	f.records.Put(servingRecord("site-1"))
	f.repository.errs["source docker-ce"] = refused("The upstream URL does not answer.")
	if err := f.reconcile(t, "site-1"); err != nil {
		t.Fatalf("Reconcile with a refused source = %v, want nil (a refusal is recorded, not retried)", err)
	}
	observed := f.record(t, "site-1")
	failed := observed.Status.Sources[0]
	if failed.Source.Name != "docker-ce" || failed.Status.State != localrepodomain.SyncFailed || failed.Status.Message != "The upstream URL does not answer." {
		t.Errorf("docker-ce = %+v, want failed with the provider's detail", failed)
	}
	wantCondition(t, observed, convergence.ConditionRealized, convergence.ConditionFalse, localrepodomain.ReasonSourceFailed, "The upstream URL does not answer.")
	wantCondition(t, observed, localrepodomain.ConditionReady, convergence.ConditionFalse, localrepodomain.ReasonSourceFailed, "source docker-ce failed")

	f.now = f.now.Add(time.Minute)
	f.repository.errs["source docker-ce"] = unavailable("The repository could not be reached.")
	for attempt := range 2 {
		if err := f.reconcile(t, "site-1"); err == nil {
			t.Fatalf("Reconcile %d with an unreachable provider = nil, want its error for the runtime's backoff", attempt)
		}
	}
	stale := f.record(t, "site-1")
	if !reflect.DeepEqual(stale.Status.Sources, observed.Status.Sources) || !stale.Status.ObservedAt.Equal(*observed.Status.ObservedAt) {
		t.Errorf("items or observedAt changed while unreachable: %+v", stale.Status)
	}
	wantCondition(t, stale, convergence.ConditionRealized, convergence.ConditionFalse, convergence.ReasonProviderUnreachable, "The repository could not be reached")
	wantCondition(t, stale, localrepodomain.ConditionReady, convergence.ConditionFalse, convergence.ReasonProviderUnreachable, "could not be reached")
	if f.records.StatusWrites != 2 {
		t.Errorf("status written %d times, want once per pass that completed and once for the unchanged failure", f.records.StatusWrites)
	}
}

// A pass the Integration stops says why in realized, ready, and requires_attention, keeps the
// observed items, and is retried with backoff only when the provider was merely unreachable.
func TestControllerRecordsWhyAPassStopped(t *testing.T) {
	for _, tc := range []struct {
		name      string
		setup     func(f controllerFixture, integration *sitedomain.Integration)
		reason    string
		mentions  string
		attention bool
		retried   bool
	}{
		{"no content origin", func(_ controllerFixture, integration *sitedomain.Integration) {
			delete(integration.Settings, localrepodomain.SettingContentOrigin)
		}, localrepodomain.ReasonContentOriginMissing, "has no contentOrigin setting", true, false},
		{"no credential", func(f controllerFixture, _ *sitedomain.Integration) {
			*f.factoryErr = sitedomain.ErrCredentialNotSet
		}, localrepodomain.ReasonCredentialMissing, "has no credential", true, false},
		{"configuration refused", func(f controllerFixture, _ *sitedomain.Integration) {
			*f.factoryErr = refused("The endpoint is not a URL.")
		}, localrepodomain.ReasonIntegrationInvalid, "The endpoint is not a URL", true, false},
		{"credential refused", func(f controllerFixture, _ *sitedomain.Integration) {
			f.repository.errs["source docker-ce"] = &localrepodomain.ProviderError{Kind: localrepodomain.ProviderErrorAuth, Detail: "401 Unauthorized."}
		}, localrepodomain.ReasonCredentialRejected, "refused the credential", true, false},
		{"provider unreachable", func(f controllerFixture, _ *sitedomain.Integration) {
			f.repository.errs["endpoints"] = unavailable("The repository could not be reached.")
		}, convergence.ReasonProviderUnreachable, "could not be reached", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			integration := repositoryIntegrationOf("site-1")
			f := newControllerFixture(integration)
			f.records.Put(servingRecord("site-1"))
			tc.setup(f, integration)
			err := f.reconcile(t, "site-1")
			if (err != nil) != tc.retried {
				t.Fatalf("Reconcile = %v, want an error for backoff: %v", err, tc.retried)
			}
			record := f.record(t, "site-1")
			if record.Status.IntegrationID != "repo-site-1" || record.Status.ObservedAt != nil || len(record.Status.Sources) > 0 {
				t.Errorf("status = %+v; want only conditions for repo-site-1", record.Status)
			}
			wantCondition(t, record, convergence.ConditionRealized, convergence.ConditionFalse, tc.reason, tc.mentions)
			wantCondition(t, record, localrepodomain.ConditionReady, convergence.ConditionFalse, tc.reason, tc.mentions)
			attention := convergence.ConditionFalse
			if tc.attention {
				attention = convergence.ConditionTrue
			}
			if got := condition(t, record, convergence.ConditionRequiresAttention); got.Status != attention {
				t.Errorf("requires_attention = %+v, want %s", got, attention)
			}
		})
	}
}

// A source swallow ensured before and the effective spec no longer has, or has with another format,
// is removed before anything is ensured; sources observed on another Integration are not.
func TestControllerRemovesRetiredSources(t *testing.T) {
	f := newControllerFixture(repositoryIntegrationOf("site-1"))
	observed := func(name string, format localrepodomain.Format) localrepodomain.ObservedSource {
		return localrepodomain.ObservedSource{Source: localrepodomain.Source{Name: name, Format: format, URL: "https://x.example"}}
	}
	serving := localrepodomain.Spec{ServeDeployments: true}
	f.records.Put(localrepodomain.LocalRepository{SiteID: "site-1", Spec: serving, Status: localrepodomain.Status{
		IntegrationID: "repo-site-1",
		Sources:       []localrepodomain.ObservedSource{observed("docker-ce", localrepodomain.FormatDeb), observed("old", localrepodomain.FormatDeb), observed("quay.io", localrepodomain.FormatPython)},
	}})
	if err := f.reconcile(t, "site-1"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if want := []string{"deb/old", "python/quay.io"}; !reflect.DeepEqual(f.repository.removed, want) {
		t.Errorf("removed %v, want %v", f.repository.removed, want)
	}

	f.repository.removed = nil
	f.records.Put(localrepodomain.LocalRepository{SiteID: "site-1", Spec: serving, Status: localrepodomain.Status{
		IntegrationID: "repo-previous", Sources: []localrepodomain.ObservedSource{observed("old", localrepodomain.FormatDeb)},
	}})
	if err := f.reconcile(t, "site-1"); err != nil {
		t.Fatalf("Reconcile after the Integration changed: %v", err)
	}
	if len(f.repository.removed) > 0 {
		t.Errorf("removed %v from the new Integration, want nothing", f.repository.removed)
	}
}

// Without a repository Integration the status is cleared and the spec kept; once the Site is gone
// too the record is deleted. A disabled Integration is paused: the provider is not called and only
// ready=false integration_disabled is recorded, once.
func TestControllerForgetsAndPauses(t *testing.T) {
	f := newControllerFixture()
	spec := localrepodomain.Spec{Sources: []localrepodomain.Source{debSource("ubuntu", "http://archive.example/ubuntu/", []string{"noble"}, []string{"main"})}}
	f.records.Put(localrepodomain.LocalRepository{SiteID: "site-1", Generation: 1, Spec: spec, Status: localrepodomain.Status{IntegrationID: "repo-gone", ObservedGeneration: 1}})
	if err := f.reconcile(t, "site-1"); err != nil {
		t.Fatalf("Reconcile without an Integration: %v", err)
	}
	if record := f.record(t, "site-1"); record.Status.IntegrationID != "" || !reflect.DeepEqual(record.Spec, spec) {
		t.Errorf("record = %+v, want the status cleared and the spec kept", record)
	}
	f.records.Put(localrepodomain.LocalRepository{SiteID: "site-gone", Generation: 1, Spec: spec})
	if err := f.reconcile(t, "site-gone"); err != nil {
		t.Fatalf("Reconcile of a deleted Site: %v", err)
	}
	if _, err := f.records.Get(context.Background(), "site-gone"); !errors.Is(err, localrepodomain.ErrLocalRepositoryNotFound) {
		t.Errorf("record of a deleted Site: %v, want it deleted", err)
	}

	disabled := repositoryIntegrationOf("site-1")
	disabled.Enabled = false
	paused := newControllerFixture(disabled)
	paused.records.Put(servingRecord("site-1"))
	for range 2 {
		if err := paused.reconcile(t, "site-1"); err != nil {
			t.Fatalf("Reconcile of a disabled Integration: %v", err)
		}
	}
	if len(paused.repository.ensured) > 0 || paused.records.StatusWrites != 1 {
		t.Errorf("disabled: ensured %v, %d status writes; want nothing ensured and one write", paused.repository.ensured, paused.records.StatusWrites)
	}
	wantCondition(t, paused.record(t, "site-1"), localrepodomain.ConditionReady, convergence.ConditionFalse, localrepodomain.ReasonIntegrationDisabled, "is disabled")
}

// A spec that does not serve deployments, written or not, pauses the Local Repository like a
// disabled Integration: the provider is not called (nothing synced, fetched, or removed), the items
// and realized keep their last observation, and only ready=false not_serving is recorded, ahead of
// integration_disabled. The spec write that turns serving on wakes the controller, which converges.
func TestControllerDoesNotConvergeASpecThatDoesNotServe(t *testing.T) {
	f := newControllerFixture(repositoryIntegrationOf("site-1"))
	if err := f.reconcile(t, "site-1"); err != nil {
		t.Fatalf("Reconcile before any spec write: %v", err)
	}
	if len(f.repository.ensured) > 0 {
		t.Errorf("ensured %v before any spec write, want nothing: a spec serves only once a user says so", f.repository.ensured)
	}
	wantCondition(t, f.record(t, "site-1"), localrepodomain.ConditionReady, convergence.ConditionFalse, localrepodomain.ReasonNotServing, "does not serve deployments")

	served := servingRecord("site-1")
	if _, err := f.records.PutSpec(context.Background(), "site-1", 0, served.Spec); err != nil {
		t.Fatalf("PutSpec: %v", err)
	}
	if err := f.reconcile(t, "site-1"); err != nil {
		t.Fatalf("Reconcile while serving: %v", err)
	}
	observed := f.record(t, "site-1")
	wantCondition(t, observed, localrepodomain.ConditionReady, convergence.ConditionTrue, localrepodomain.ReasonReady, "")

	f.now = f.now.Add(time.Minute)
	f.repository.ensured = nil
	writes := f.records.StatusWrites
	if _, err := f.records.PutSpec(context.Background(), "site-1", observed.Generation, localrepodomain.Spec{Sources: served.Spec.Sources}); err != nil {
		t.Fatalf("PutSpec turning serving off: %v", err)
	}
	keys, err := f.controller.KeysForChange(context.Background(), store.Change{Collection: localrepodomain.Collection, Key: "site-1", Operation: store.ChangeUpdate})
	if err != nil || !slices.Equal(keys, []string{"site-1"}) {
		t.Fatalf("KeysForChange after the spec write = %v, %v; want site-1", keys, err)
	}
	for range 2 {
		if err := f.reconcile(t, "site-1"); err != nil {
			t.Fatalf("Reconcile of a spec that does not serve: %v", err)
		}
	}
	paused := f.record(t, "site-1")
	if len(f.repository.ensured) > 0 || len(f.repository.removed) > 0 || f.records.StatusWrites != writes+1 {
		t.Errorf("not serving: ensured %v, removed %v, %d status writes; want nothing called and one write",
			f.repository.ensured, f.repository.removed, f.records.StatusWrites-writes)
	}
	if !reflect.DeepEqual(paused.Status.Sources, observed.Status.Sources) || !paused.Status.ObservedAt.Equal(*observed.Status.ObservedAt) {
		t.Errorf("status items changed while not serving: %+v", paused.Status)
	}
	wantCondition(t, paused, localrepodomain.ConditionReady, convergence.ConditionFalse, localrepodomain.ReasonNotServing, "does not serve deployments")
	if realized := condition(t, paused, convergence.ConditionRealized); realized.Status != convergence.ConditionTrue || realized.Reason != convergence.ReasonConverged {
		t.Errorf("realized = %+v, want its last judgement kept", realized)
	}

	f.integrations.byID["repo-site-1"].Enabled = false
	if err := f.reconcile(t, "site-1"); err != nil {
		t.Fatalf("Reconcile of a disabled Integration that does not serve: %v", err)
	}
	wantCondition(t, f.record(t, "site-1"), localrepodomain.ConditionReady, convergence.ConditionFalse, localrepodomain.ReasonNotServing, "is disabled")
}

// servingRecord is siteID's record at generation 1 with a spec that serves deployments and no
// source of the user's.
func servingRecord(siteID string) localrepodomain.LocalRepository {
	return localrepodomain.LocalRepository{SiteID: siteID, Generation: 1, Spec: localrepodomain.Spec{ServeDeployments: true}}
}

// The controller's own status writes do not wake it; a spec write, a request for it, and a change of
// a repository Integration do.
func TestControllerKeysForChange(t *testing.T) {
	f := newControllerFixture(repositoryIntegrationOf("site-1"), &sitedomain.Integration{ID: "maas-1", SiteID: "site-1", Kind: sitedomain.IntegrationKindProvisioner})
	ctx := context.Background()
	f.records.Put(localrepodomain.LocalRepository{SiteID: "site-1", Generation: 3, Status: localrepodomain.Status{ObservedGeneration: 3}})
	f.records.Put(localrepodomain.LocalRepository{SiteID: "site-2", Generation: 2, Status: localrepodomain.Status{ObservedGeneration: 1}})
	for _, tc := range []struct {
		name   string
		change store.Change
		want   []string
	}{
		{"own status write", store.Change{Collection: localrepodomain.Collection, Key: "site-1", Operation: store.ChangeUpdate}, nil},
		{"spec write", store.Change{Collection: localrepodomain.Collection, Key: "site-2", Operation: store.ChangeUpdate}, []string{"site-2"}},
		{"record deleted", store.Change{Collection: localrepodomain.Collection, Key: "site-2", Operation: store.ChangeDelete}, nil},
		{"request for it", store.Change{Collection: convergence.RequestCollection, Key: ControllerName}, []string{"site-1", "site-2"}},
		{"request for another", store.Change{Collection: convergence.RequestCollection, Key: "platform"}, nil},
		{"repository Integration", store.Change{Collection: "integrations", Key: "repo-site-1"}, []string{"site-1"}},
		{"provisioner Integration", store.Change{Collection: "integrations", Key: "maas-1"}, nil},
		{"deleted Integration", store.Change{Collection: "integrations", Key: "gone"}, []string{"site-1", "site-2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := f.controller.KeysForChange(ctx, tc.change)
			if err != nil {
				t.Fatalf("KeysForChange: %v", err)
			}
			slices.Sort(got)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("KeysForChange(%+v) = %v, want %v", tc.change, got, tc.want)
			}
		})
	}
}
