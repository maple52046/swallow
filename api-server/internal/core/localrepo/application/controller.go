package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/maple52046/swallow/internal/core/convergence"
	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
	"github.com/maple52046/swallow/internal/core/store"
)

// ControllerName is the Local Repository controller's name: its lease is "controller:<name>" and a
// convergence request for it (POST .../local-repository/sync) wakes it for every Site.
const ControllerName = "local-repository"

// Controller converges each Site's Local Repository to its effective spec (glossary: Controller,
// Local Repository; desired-state-model.md §7). Its key is the Site id.
//
// For a Site with an enabled `repository` Integration, one pass removes the sources swallow ensured
// before and no longer wants, ensures every effective source and file and then every declared image
// whose registry source is ready, reads the host endpoints, and records it all as the status, with
// the conditions ready, realized, and requires_attention; it is the status's only writer. Ensure
// calls never wait for a sync, so while any item is pending or syncing the pass returns
// errStillSyncing and the runtime runs it again with its capped backoff; the periodic resync lets
// the provider start the syncs that are due.
//
// A pass the Integration or its provider stops records only the conditions, so the items stay as
// last observed (stale, not wrong): a provider that cannot be reached is realized=false
// provider_unreachable and retried with backoff; an Integration without a credential or a content
// origin, a credential the provider refuses, or a configuration the adapter refuses is
// realized=false with that reason and requires_attention=true, and is tried again when the
// Integration changes or at the next resync. A disabled Integration is paused: nothing is converged
// and only ready=false integration_disabled is recorded. A Site whose Integration is gone has its
// status cleared, and its record deleted once the Site is gone too.
type Controller struct {
	sites        sitedomain.SiteRepository
	integrations sitedomain.IntegrationRepository
	records      localrepodomain.Store
	repositories localrepodomain.RepositoryFactory
	modules      []localrepodomain.ModuleDeclaration
	now          func() time.Time
	log          *slog.Logger
}

var _ convergence.Controller = (*Controller)(nil)

// NewController wires the controller. modules are the compiled modules' declarations, fixed for the
// process; every dependency is required.
func NewController(
	sites sitedomain.SiteRepository,
	integrations sitedomain.IntegrationRepository,
	records localrepodomain.Store,
	repositories localrepodomain.RepositoryFactory,
	modules []localrepodomain.ModuleDeclaration,
) *Controller {
	return &Controller{
		sites: sites, integrations: integrations, records: records, repositories: repositories, modules: modules,
		now: func() time.Time { return time.Now().UTC() },
		log: slog.Default().With("controller", ControllerName),
	}
}

// Name implements convergence.Controller.
func (c *Controller) Name() string { return ControllerName }

// Collections implements convergence.Controller: the records (for spec writes), the Integrations
// (a repository Integration registered, changed, or removed), and convergence requests.
func (c *Controller) Collections() []string {
	return []string{localrepodomain.Collection, "integrations", convergence.RequestCollection}
}

// KeysForChange implements convergence.Controller. A record change matters only when its spec is
// ahead of its status: the controller's own status writes leave them equal, so they do not wake it
// again. An Integration change maps to the Site of a repository Integration; a deleted Integration
// cannot be read, so every key is reconciled and the Site that lost it is cleared.
func (c *Controller) KeysForChange(ctx context.Context, change store.Change) ([]string, error) {
	switch change.Collection {
	case convergence.RequestCollection:
		if change.Key != ControllerName {
			return nil, nil
		}
		return c.Keys(ctx)
	case "integrations":
		integration, err := c.integrations.FindByID(ctx, change.Key)
		if errors.Is(err, sitedomain.ErrIntegrationNotFound) {
			return c.Keys(ctx)
		}
		if err != nil {
			return nil, err
		}
		if integration.Kind != sitedomain.IntegrationKindRepository {
			return nil, nil
		}
		return []string{integration.SiteID}, nil
	case localrepodomain.Collection:
		if change.Operation == store.ChangeDelete {
			return nil, nil
		}
		record, err := c.records.Get(ctx, change.Key)
		if errors.Is(err, localrepodomain.ErrLocalRepositoryNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if record.Generation == record.Status.ObservedGeneration {
			return nil, nil
		}
		return []string{change.Key}, nil
	}
	return nil, nil
}

// Keys implements convergence.Controller: every Site with a repository Integration and every Site
// with a record, so a record whose Integration or Site is gone is cleared.
func (c *Controller) Keys(ctx context.Context) ([]string, error) {
	integrations, err := c.integrations.List(ctx, sitedomain.IntegrationFilter{Kind: sitedomain.IntegrationKindRepository})
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, integration := range integrations {
		keys = appendUnique(keys, integration.SiteID)
	}
	records, err := c.records.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		keys = appendUnique(keys, record.SiteID)
	}
	return keys, nil
}

// errStillSyncing is returned while an item is pending or syncing, so the runtime observes the Site
// again with backoff instead of waiting for the resync.
var errStillSyncing = errors.New("the local repository is still syncing")

// Reconcile implements convergence.Controller.
func (c *Controller) Reconcile(ctx context.Context, request convergence.Request) error {
	siteID := request.Key
	integration, err := repositoryIntegration(ctx, c.integrations, siteID)
	if err != nil {
		return err
	}
	record, err := c.records.Get(ctx, siteID)
	if errors.Is(err, localrepodomain.ErrLocalRepositoryNotFound) {
		record, err = nil, nil
	}
	if err != nil {
		return err
	}
	if integration == nil {
		return c.forget(ctx, siteID, record)
	}
	site := siteState{siteID: siteID, integration: integration}
	var spec localrepodomain.Spec
	if record != nil {
		site.generation, spec = record.Generation, record.Spec
		if record.Status.IntegrationID == integration.ID {
			site.previous = record.Status
		}
	}
	site.previous.IntegrationID = integration.ID
	site.effective = Effective(c.modules, spec.Sources)
	for _, conflict := range site.effective.Conflicts {
		c.log.Warn("declaration left out of the local repository", "site", siteID, "conflict", conflict.Message())
	}

	if !integration.Enabled {
		return c.recordConditions(ctx, site, nil)
	}
	if strings.TrimSpace(integration.Setting(localrepodomain.SettingContentOrigin, "")) == "" {
		return c.stopped(ctx, site, stoppedBy{
			reason: localrepodomain.ReasonContentOriginMissing, attention: true,
			message: fmt.Sprintf("the repository integration %s has no %s setting; set it to the origin hosts read content from",
				integration.Name, localrepodomain.SettingContentOrigin),
		})
	}
	repository, err := c.repositories.For(ctx, integration.ID)
	if err != nil {
		return c.stopped(ctx, site, classify(integration, err))
	}
	run := &pass{repository: repository, previous: site.previous, log: c.log.With("site", siteID)}
	if err := run.removeRetired(ctx, site.effective); err != nil {
		return c.stopped(ctx, site, classify(integration, err))
	}
	if err := run.ensure(ctx, site.effective); err != nil {
		return c.stopped(ctx, site, classify(integration, err))
	}
	if err := run.ensureImages(ctx, site.effective); err != nil {
		return c.stopped(ctx, site, classify(integration, err))
	}
	endpoints, err := repository.Endpoints(ctx)
	if err != nil {
		return c.stopped(ctx, site, classify(integration, err))
	}
	now := c.now()
	status := localrepodomain.Status{
		IntegrationID: integration.ID, ObservedGeneration: site.generation,
		Sources: run.sources, Files: run.files, Images: run.images,
		Endpoints: &endpoints, ObservedAt: &now,
	}
	realized := run.realized()
	status.Conditions = c.judge(site, site.previous.Conditions, status, &realized, now)
	if err := c.records.SetStatus(ctx, siteID, status); err != nil {
		return err
	}
	if len(run.unsettled) > 0 {
		return fmt.Errorf("%w: %s", errStillSyncing, strings.Join(run.unsettled, ", "))
	}
	return nil
}

// siteState is what one Reconcile of a Site works from: its repository Integration, its spec
// generation and effective spec, and the status last recorded for that Integration (with the
// Integration filled in, so conditions recorded before any complete pass belong to it).
type siteState struct {
	siteID      string
	integration *sitedomain.Integration
	generation  int64
	effective   EffectiveSpec
	previous    localrepodomain.Status
}

// judgement is a condition's verdict before it is stamped with a generation and a time. attention,
// on a realized verdict, says whether a user must act (requires_attention).
type judgement struct {
	status    convergence.ConditionStatus
	reason    string
	message   string
	attention bool
}

// stoppedBy is why a pass could not complete: the realized reason and message, whether a user must
// act (requires_attention), and the error to retry with backoff, nil when retrying cannot help
// until the Integration changes.
type stoppedBy struct {
	reason    string
	message   string
	attention bool
	retry     error
}

// classify turns an error that stopped a pass into its reason. A missing or refused credential and
// a configuration the adapter refuses need a user; anything else (the provider down or slow, an
// Integration deleted meanwhile) is retried. Only the adapter's client-safe detail reaches the
// message.
func classify(integration *sitedomain.Integration, err error) stoppedBy {
	var providerErr *localrepodomain.ProviderError
	switch {
	case errors.Is(err, sitedomain.ErrCredentialNotSet):
		return stoppedBy{reason: localrepodomain.ReasonCredentialMissing, attention: true,
			message: fmt.Sprintf("the repository integration %s has no credential; set the provider's admin password", integration.Name)}
	case errors.As(err, &providerErr) && providerErr.Kind == localrepodomain.ProviderErrorAuth:
		return stoppedBy{reason: localrepodomain.ReasonCredentialRejected, attention: true,
			message: withDetail(fmt.Sprintf("the provider refused the credential of the repository integration %s; replace it", integration.Name), providerErr.Detail)}
	case errors.As(err, &providerErr) && providerErr.Kind == localrepodomain.ProviderErrorRejected:
		return stoppedBy{reason: localrepodomain.ReasonIntegrationInvalid, attention: true,
			message: withDetail(fmt.Sprintf("the provider refused the repository integration %s; fix it", integration.Name), providerErr.Detail)}
	case errors.As(err, &providerErr):
		return stoppedBy{reason: convergence.ReasonProviderUnreachable, retry: err,
			message: withDetail("the provider could not be reached; retrying", providerErr.Detail)}
	}
	return stoppedBy{reason: convergence.ReasonProviderUnreachable, retry: err, message: "the provider could not be reached; retrying"}
}

// withDetail appends the adapter's client-safe detail to a condition message, without its final
// period, so messages read the same whether or not the adapter gave one.
func withDetail(sentence, detail string) string {
	detail = strings.TrimSuffix(strings.TrimSpace(detail), ".")
	if detail == "" {
		return sentence
	}
	return sentence + ": " + detail
}

// stopped records why the pass could not complete, keeping the observed items as they were, and
// returns the error the runtime retries with backoff, or nil when only a change of the Integration
// can help. A cancelled ctx records nothing.
func (c *Controller) stopped(ctx context.Context, site siteState, why stoppedBy) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	verdict := judgement{status: convergence.ConditionFalse, reason: why.reason, message: why.message, attention: why.attention}
	if err := c.recordConditions(ctx, site, &verdict); err != nil {
		return err
	}
	return why.retry
}

// recordConditions writes the previous status with new conditions when they changed: realized and
// requires_attention from the given verdict, or kept when it is nil, and ready from the result.
// An unchanged judgement writes nothing, so a retried failure does not churn the store.
func (c *Controller) recordConditions(ctx context.Context, site siteState, realized *judgement) error {
	status := site.previous
	conditions := c.judge(site, status.Conditions, status, realized, c.now())
	if slices.Equal(conditions, status.Conditions) {
		return nil
	}
	status.Conditions = conditions
	return c.records.SetStatus(ctx, site.siteID, status)
}

// judge returns conditions with realized and requires_attention set from realized (kept when it is
// nil) and ready set from the view of status, all for the Site's spec generation.
func (c *Controller) judge(site siteState, conditions []convergence.Condition, status localrepodomain.Status, realized *judgement, now time.Time) []convergence.Condition {
	set := func(t convergence.ConditionType, verdict judgement) {
		conditions, _ = convergence.SetCondition(conditions, convergence.Condition{
			Type: t, Status: verdict.status, Reason: verdict.reason, Message: verdict.message, ObservedGeneration: site.generation,
		}, now)
	}
	if realized != nil {
		set(convergence.ConditionRealized, *realized)
		attention := judgement{status: convergence.ConditionFalse, reason: convergence.ReasonAttentionNotNeeded}
		if realized.attention {
			attention = judgement{status: convergence.ConditionTrue, reason: realized.reason, message: realized.message}
		}
		set(convergence.ConditionRequiresAttention, attention)
	}
	status.Conditions = conditions
	view := buildView(site.siteID, site.integration, &localrepodomain.LocalRepository{Generation: site.generation, Status: status}, site.effective)
	if view.Ready {
		set(localrepodomain.ConditionReady, judgement{status: convergence.ConditionTrue, reason: localrepodomain.ReasonReady})
	} else {
		set(localrepodomain.ConditionReady, judgement{status: convergence.ConditionFalse, reason: view.NotReady[0].Reason, message: view.NotReadyMessage()})
	}
	return conditions
}

// forget handles a Site without a repository Integration: nothing it observed is served any more, so
// the status is cleared; the record goes once the Site is gone, and the spec stays while the Site
// exists so a newly registered Integration starts from it.
func (c *Controller) forget(ctx context.Context, siteID string, record *localrepodomain.LocalRepository) error {
	if record == nil {
		return nil
	}
	_, err := c.sites.FindByID(ctx, siteID)
	if errors.Is(err, sitedomain.ErrSiteNotFound) {
		return c.records.Delete(ctx, siteID)
	}
	if err != nil {
		return err
	}
	if record.Status.IntegrationID == "" && record.Status.ObservedGeneration == record.Generation {
		return nil
	}
	return c.records.SetStatus(ctx, siteID, localrepodomain.Status{ObservedGeneration: record.Generation})
}

// pass is one convergence of a Site against its provider: what it ensured, and what is unsettled.
type pass struct {
	repository localrepodomain.Repository
	previous   localrepodomain.Status
	log        *slog.Logger
	sources    []localrepodomain.ObservedSource
	files      []localrepodomain.ObservedFile
	images     []localrepodomain.ObservedImage
	unsettled  []string
}

// removeRetired removes the sources swallow ensured on this Integration that the effective spec no
// longer has, or has with another format, before anything is ensured, so a source that changes
// format never meets its old self. A refusal is logged and the source forgotten, since repeating it
// cannot succeed; any other failure stops the pass and keeps the source recorded for the retry.
func (p *pass) removeRetired(ctx context.Context, effective EffectiveSpec) error {
	wanted := map[string]localrepodomain.Format{}
	for _, source := range effective.Sources {
		wanted[source.Name] = source.Format
	}
	for _, observed := range p.previous.Sources {
		if format, ok := wanted[observed.Source.Name]; ok && format == observed.Source.Format {
			continue
		}
		err := p.repository.RemoveSource(ctx, observed.Source.Name, observed.Source.Format)
		if err != nil && !rejected(err) {
			return err
		}
		if err != nil {
			p.log.Warn("local repository source not removed", "source", observed.Source.Name, "error", err)
		}
	}
	return nil
}

// ensure ensures every effective source, then every file. A refusal marks that item failed with the
// provider's detail and the pass goes on; any other failure stops the pass, leaving the recorded
// status unchanged.
func (p *pass) ensure(ctx context.Context, effective EffectiveSpec) error {
	for _, source := range effective.Sources {
		observed, err := p.repository.EnsureSource(ctx, source.Source)
		if err != nil && !rejected(err) {
			return err
		}
		if err != nil {
			observed = localrepodomain.SourceStatus{Name: source.Name, Format: source.Format, State: localrepodomain.SyncFailed, Message: detail(err)}
		}
		if !observed.State.Settled() {
			p.unsettled = append(p.unsettled, "source "+source.Name)
		}
		p.sources = append(p.sources, localrepodomain.ObservedSource{Source: source.Source, Status: observed})
	}
	for _, file := range effective.Files {
		observed, err := p.repository.EnsureFile(ctx, file.File)
		if err != nil && !rejected(err) {
			return err
		}
		if err != nil {
			observed = localrepodomain.FileStatus{Path: file.Path, State: localrepodomain.SyncFailed, Message: detail(err)}
		}
		if !observed.State.Settled() {
			p.unsettled = append(p.unsettled, "file "+file.Path)
		}
		p.files = append(p.files, localrepodomain.ObservedFile{File: file.File, Status: observed})
	}
	return nil
}

// ensureImages ensures every declared image whose registry host's oci source this pass observed
// ready; the others are pending without asking the provider, since their pull-through source cannot
// fetch them yet, and an image no oci source serves (its source lost a conflict) is failed. Like
// ensure, a refusal marks that image failed with the provider's detail and the pass goes on, and any
// other failure stops the pass. A pending image whose source failed does not keep the pass
// retrying: it waits for the source, whose failure the conditions report.
func (p *pass) ensureImages(ctx context.Context, effective EffectiveSpec) error {
	ociSources := map[string]localrepodomain.SyncState{}
	for _, observed := range p.sources {
		if observed.Source.Format == localrepodomain.FormatOCI {
			ociSources[observed.Source.Name] = observed.Status.State
		}
	}
	for _, image := range effective.Images {
		observed := localrepodomain.ImageStatus{Reference: image.Reference, State: localrepodomain.SyncPending}
		host, _ := RegistryHost(image.Reference)
		source, served := ociSources[host]
		switch {
		case !served:
			observed.State, observed.Message = localrepodomain.SyncFailed, "No oci source serves the image's registry host."
		case source == localrepodomain.SyncReady:
			status, err := p.repository.EnsureImage(ctx, image.Reference)
			if err != nil && !rejected(err) {
				return err
			}
			if err != nil {
				status = localrepodomain.ImageStatus{Reference: image.Reference, State: localrepodomain.SyncFailed, Message: detail(err)}
			}
			observed = status
		}
		if !observed.State.Settled() && source != localrepodomain.SyncFailed {
			p.unsettled = append(p.unsettled, "image "+image.Reference)
		}
		p.images = append(p.images, localrepodomain.ObservedImage{Reference: image.Reference, Status: observed})
	}
	return nil
}

// realized judges a complete pass: converged when every item is ready; otherwise the reason is a
// failed source, then a failed file, then a failed image (a user may have to fix an upstream), then
// an item still being fetched, and the message names every item that is not ready.
func (p *pass) realized() judgement {
	var problems []string
	reason := ""
	note := func(what string, state localrepodomain.SyncState, message, failed string) {
		if state == localrepodomain.SyncReady {
			return
		}
		problems = append(problems, notReady(what, state, message))
		switch {
		case state == localrepodomain.SyncFailed && (reason == "" || reason == localrepodomain.ReasonSyncing):
			reason = failed
		case reason == "":
			reason = localrepodomain.ReasonSyncing
		}
	}
	for _, observed := range p.sources {
		note("source "+observed.Source.Name, observed.Status.State, observed.Status.Message, localrepodomain.ReasonSourceFailed)
	}
	for _, observed := range p.files {
		note("file "+observed.File.Path, observed.Status.State, observed.Status.Message, localrepodomain.ReasonFileFailed)
	}
	for _, observed := range p.images {
		note("image "+observed.Reference, observed.Status.State, observed.Status.Message, localrepodomain.ReasonImageFailed)
	}
	if reason == "" {
		return judgement{status: convergence.ConditionTrue, reason: convergence.ReasonConverged}
	}
	return judgement{status: convergence.ConditionFalse, reason: reason, message: strings.Join(problems, "; ")}
}

// rejected reports a provider refusal, which repeating the same request cannot overcome.
func rejected(err error) bool {
	var providerErr *localrepodomain.ProviderError
	return errors.As(err, &providerErr) && providerErr.Kind == localrepodomain.ProviderErrorRejected
}

// detail is the user-facing part of a provider error.
func detail(err error) string {
	var providerErr *localrepodomain.ProviderError
	if errors.As(err, &providerErr) && providerErr.Detail != "" {
		return providerErr.Detail
	}
	return err.Error()
}
