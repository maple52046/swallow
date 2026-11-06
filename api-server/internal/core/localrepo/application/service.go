package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/maple52046/swallow/internal/core/convergence"
	localrepodomain "github.com/maple52046/swallow/internal/core/localrepo/domain"
	sitedomain "github.com/maple52046/swallow/internal/core/site/domain"
)

// MaxContentPageSize is the largest content page Browse asks the provider for.
const MaxContentPageSize = 100

// sourceNamePattern is the shape of a Repository Source name: it becomes part of the address hosts
// read the source at, so it is lower case, starts and ends with a letter or digit, and may hold the
// dots of a registry host.
var sourceNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,61}[a-z0-9])?$`)

// PutSpecInput is a user's replacement spec: the generation the writer read and the full list of
// the Site's own Repository Sources, still unvalidated.
type PutSpecInput struct {
	Generation int64
	Sources    []localrepodomain.Source
}

// Service is the Local Repository use cases the API serves (contract local-repository.md): read a
// Site's repository, browse its content, replace the user's spec, and ask for an immediate
// convergence. Authorization (admin) is enforced by the routes.
//
// Reads and spec writes use the store only; the controller in the worker converges the provider.
// Browse is the one live provider read. Every use case answers sitedomain.ErrSiteNotFound for an
// unknown Site and localrepodomain.ErrNoRepositoryIntegration for a Site without a `repository`
// Integration, so a Site has a Local Repository exactly while it has the Integration.
type Service struct {
	sites        sitedomain.SiteRepository
	integrations sitedomain.IntegrationRepository
	records      localrepodomain.Store
	repositories localrepodomain.RepositoryFactory
	requester    convergence.Requester
	modules      []localrepodomain.ModuleDeclaration
}

// NewService wires the use cases. modules are the compiled modules' declarations; requester writes
// the convergence request the worker's controller watches. Every dependency is required.
func NewService(
	sites sitedomain.SiteRepository,
	integrations sitedomain.IntegrationRepository,
	records localrepodomain.Store,
	repositories localrepodomain.RepositoryFactory,
	requester convergence.Requester,
	modules []localrepodomain.ModuleDeclaration,
) *Service {
	return &Service{
		sites: sites, integrations: integrations, records: records, repositories: repositories,
		requester: requester, modules: modules,
	}
}

// Get returns the Site's Local Repository: the user's spec and the effective spec with the status
// the controller last recorded.
func (s *Service) Get(ctx context.Context, siteID string) (*View, error) {
	integration, err := s.integration(ctx, siteID)
	if err != nil {
		return nil, err
	}
	record, err := s.record(ctx, siteID)
	if err != nil {
		return nil, err
	}
	view := buildView(siteID, integration, record, Effective(s.modules, specSources(record)))
	return &view, nil
}

// Browse reads one page of the Site's content of query.Format live from the provider. An invalid
// query is ErrInvalidContentQuery; provider failures are *localrepodomain.ProviderError, and the
// factory's sitedomain.ErrCredentialNotSet passes through.
func (s *Service) Browse(ctx context.Context, siteID string, query localrepodomain.ContentQuery) (localrepodomain.ContentPage, error) {
	query.Source, query.Search = strings.TrimSpace(query.Source), strings.TrimSpace(query.Search)
	switch {
	case !query.Format.Valid():
		return localrepodomain.ContentPage{}, fmt.Errorf("%w: format must be one of %v", localrepodomain.ErrInvalidContentQuery, localrepodomain.Formats)
	case query.Offset < 0 || query.Limit < 1 || query.Limit > MaxContentPageSize:
		return localrepodomain.ContentPage{}, fmt.Errorf("%w: the page is out of range", localrepodomain.ErrInvalidContentQuery)
	}
	integration, err := s.integration(ctx, siteID)
	if err != nil {
		return localrepodomain.ContentPage{}, err
	}
	repository, err := s.repositories.For(ctx, integration.ID)
	if err != nil {
		return localrepodomain.ContentPage{}, err
	}
	return repository.Browse(ctx, query)
}

// PutSpec validates input and stores its sources as the Site's spec when input.Generation is the
// stored generation, returning the Site's Local Repository. An invalid source, or one that
// contradicts what a module declares, is ErrInvalidSpec; a stale generation is
// ErrGenerationConflict. The provider is not called; the controller converges the new spec.
func (s *Service) PutSpec(ctx context.Context, siteID string, input PutSpecInput) (*View, error) {
	sources, err := normalizeSources(input.Sources)
	if err != nil {
		return nil, err
	}
	if input.Generation < 0 {
		return nil, fmt.Errorf("%w: generation must not be negative", localrepodomain.ErrInvalidSpec)
	}
	var contradictions []string
	for _, conflict := range Effective(s.modules, sources).Conflicts {
		if conflict.InvolvesUser() {
			contradictions = append(contradictions, conflict.Message())
		}
	}
	if len(contradictions) > 0 {
		return nil, fmt.Errorf("%w: %s", localrepodomain.ErrInvalidSpec, strings.Join(contradictions, "; "))
	}
	integration, err := s.integration(ctx, siteID)
	if err != nil {
		return nil, err
	}
	record, err := s.records.PutSpec(ctx, siteID, input.Generation, localrepodomain.Spec{Sources: sources})
	if err != nil {
		return nil, err
	}
	view := buildView(siteID, integration, record, Effective(s.modules, record.Spec.Sources))
	return &view, nil
}

// RequestSync asks the worker's controller to converge every Site's Local Repository now rather than
// at its next resync. It returns once the request is stored; the result appears in a later read.
func (s *Service) RequestSync(ctx context.Context, siteID string) error {
	if _, err := s.integration(ctx, siteID); err != nil {
		return err
	}
	return s.requester.Request(ctx, ControllerName)
}

// integration returns the Site's repository Integration, or the errors that mean the Site has no
// Local Repository.
func (s *Service) integration(ctx context.Context, siteID string) (*sitedomain.Integration, error) {
	if _, err := s.sites.FindByID(ctx, siteID); err != nil {
		return nil, err
	}
	integration, err := repositoryIntegration(ctx, s.integrations, siteID)
	if err != nil {
		return nil, err
	}
	if integration == nil {
		return nil, localrepodomain.ErrNoRepositoryIntegration
	}
	return integration, nil
}

// record returns the Site's record, nil when none was written or observed yet.
func (s *Service) record(ctx context.Context, siteID string) (*localrepodomain.LocalRepository, error) {
	record, err := s.records.Get(ctx, siteID)
	if errors.Is(err, localrepodomain.ErrLocalRepositoryNotFound) {
		return nil, nil
	}
	return record, err
}

// specSources is the user's sources of record, none for a Site without a record.
func specSources(record *localrepodomain.LocalRepository) []localrepodomain.Source {
	if record == nil {
		return nil
	}
	return record.Spec.Sources
}

// normalizeSources trims and validates the user's sources: a unique name of sourceNamePattern, a
// known format, an http(s) URL without credentials (the spec is stored and shown in plain text),
// suites and components for a deb source and only there, architectures only for OS package sources,
// and list values without blanks, spaces, or repeats.
func normalizeSources(input []localrepodomain.Source) ([]localrepodomain.Source, error) {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", localrepodomain.ErrInvalidSpec, fmt.Sprintf(format, args...))
	}
	var sources []localrepodomain.Source
	seen := map[string]bool{}
	for i, source := range input {
		source.Name, source.URL = strings.TrimSpace(source.Name), strings.TrimSpace(source.URL)
		if !sourceNamePattern.MatchString(source.Name) {
			return nil, invalid("sources[%d].name %q must be 1 to 63 lower-case letters, digits, '.', '_', or '-', starting and ending with a letter or digit", i, source.Name)
		}
		if seen[source.Name] {
			return nil, invalid("source %q is listed twice", source.Name)
		}
		seen[source.Name] = true
		if !source.Format.Valid() {
			return nil, invalid("source %q format must be one of %v", source.Name, localrepodomain.Formats)
		}
		parsed, err := url.Parse(source.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, invalid("source %q url must be an http or https URL", source.Name)
		}
		if parsed.User != nil {
			return nil, invalid("source %q url must not carry credentials", source.Name)
		}
		lists := []struct {
			field  string
			values *[]string
			allow  bool
		}{
			{"suites", &source.Suites, source.Format == localrepodomain.FormatDeb},
			{"components", &source.Components, source.Format == localrepodomain.FormatDeb},
			{"architectures", &source.Architectures, source.Format == localrepodomain.FormatDeb || source.Format == localrepodomain.FormatRPM},
		}
		for _, list := range lists {
			values, err := normalizeList(*list.values)
			if err != nil {
				return nil, invalid("source %q %s: %v", source.Name, list.field, err)
			}
			if len(values) > 0 && !list.allow {
				return nil, invalid("source %q of format %s takes no %s", source.Name, source.Format, list.field)
			}
			*list.values = values
		}
		if source.Format == localrepodomain.FormatDeb && (len(source.Suites) == 0 || len(source.Components) == 0) {
			return nil, invalid("deb source %q needs at least one suite and one component", source.Name)
		}
		sources = append(sources, source)
	}
	return sources, nil
}

// normalizeList trims each value and refuses blanks, values with spaces, and repeats; nil stays nil.
func normalizeList(values []string) ([]string, error) {
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.ContainsAny(value, " \t\r\n") {
			return nil, fmt.Errorf("%q is not a single word", value)
		}
		if slices.Contains(out, value) {
			return nil, fmt.Errorf("%q is listed twice", value)
		}
		out = append(out, value)
	}
	return out, nil
}
