package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	provisioningdomain "github.com/maple52046/swallow/internal/core/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// BootISOItem is the published shape of a Boot ISO (contract boot-isos.md).
type BootISOItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	SiteID        string `json:"siteId"`
	IntegrationID string `json:"integrationId"`
	RackAddress   string `json:"rackAddress"`
	ChainURL      string `json:"chainUrl"`
	UEFIChainURL  string `json:"uefiChainUrl"`
	Script        string `json:"script"`
	GRUBConfig    string `json:"grubConfig"`
	IPXEVersion   string `json:"ipxeVersion"`
	SizeBytes     int64  `json:"sizeBytes"`
	SHA256        string `json:"sha256"`
	URL           string `json:"url"`
	InUseBy       int    `json:"inUseBy"`
	CreatedAt     string `json:"createdAt"`
	CreatedBy     string `json:"createdBy,omitempty"`
	RebuiltAt     string `json:"rebuiltAt,omitempty"`
	RebuiltBy     string `json:"rebuiltBy,omitempty"`
}

// BootISOBuilderItem reports whether this installation can build Boot ISOs, and why not.
type BootISOBuilderItem struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// BootISOList is the list response: the builder's availability and the Boot ISOs.
type BootISOList struct {
	Builder BootISOBuilderItem `json:"builder"`
	Items   []BootISOItem      `json:"items"`
}

// CreateBootISOInput is a build request. CreatedBy is the authenticated user's username.
type CreateBootISOInput struct {
	Name          string
	IntegrationID string
	RackAddress   string
	CreatedBy     string
}

// BootISOService builds, rebuilds, lists, and deletes Boot ISOs (decision 047).
//
// A build renders, through the Integration's provisioner's BootChainRenderer, the content that
// chains to the given boot server, packages it synchronously through BootISOBuilder (seconds), and
// only then stores the record, so a listed Boot ISO always has a file; a record that cannot be
// stored removes the file again. A rebuild renders the current templates for the ISO's stored
// address and replaces the file under the same id. Deletion is refused while a Server's enabled
// Boot Media uses the ISO (BootISOUsage). Authorization (admin) is the delivery layer's job.
type BootISOService struct {
	isos         provisioningdomain.BootISORepository
	integrations provisioningdomain.ProvisionerIntegrationReader
	builder      provisioningdomain.BootISOBuilder
	usage        provisioningdomain.BootISOUsage
	renderers    provisioningdomain.BootChainRenderers
	now          func() time.Time
	newID        func() string
	// rebuilds serializes rebuilds: two rebuilds of the same id would race on its one file, and a
	// rebuild is a rare user action, so one at a time per process costs nothing.
	rebuilds sync.Mutex
}

// NewBootISOService wires the service; renderers are the chain renderers of the provisioners this
// installation includes, by provider kind.
func NewBootISOService(
	isos provisioningdomain.BootISORepository,
	integrations provisioningdomain.ProvisionerIntegrationReader,
	builder provisioningdomain.BootISOBuilder,
	usage provisioningdomain.BootISOUsage,
	renderers provisioningdomain.BootChainRenderers,
) *BootISOService {
	return &BootISOService{
		isos: isos, integrations: integrations, builder: builder, usage: usage, renderers: renderers,
		now: func() time.Time { return time.Now().UTC() }, newID: uuid.NewString,
	}
}

// Create builds a Boot ISO for a provisioner Integration's boot server and stores it.
//
// Errors: ErrInvalidBootISO (name, boot server address, or a provisioner without Boot ISOs), the
// integration reader's not-found and ErrIntegrationNotProvisioner, ErrBootISONameTaken,
// ErrBootISOBuilderUnavailable, and ErrBootISOBuildFailed.
func (s *BootISOService) Create(ctx context.Context, input CreateBootISOInput) (*BootISOItem, error) {
	name, err := provisioningdomain.ValidateBootISOName(input.Name)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.IntegrationID) == "" {
		return nil, fmt.Errorf("%w: integrationId is required", provisioningdomain.ErrInvalidBootISO)
	}
	integration, err := s.integrations.Find(ctx, strings.TrimSpace(input.IntegrationID))
	if err != nil {
		return nil, err
	}
	chain, err := s.renderChain(integration, input.RackAddress)
	if err != nil {
		return nil, err
	}
	if err := s.builder.Available(); err != nil {
		return nil, err
	}
	// Fail fast on a taken name before spending a build; the unique index still decides a race.
	existing, err := s.isos.List(ctx, provisioningdomain.BootISOFilter{IntegrationID: integration.ID})
	if err != nil {
		return nil, err
	}
	for _, iso := range existing {
		if strings.EqualFold(iso.Name, name) {
			return nil, provisioningdomain.ErrBootISONameTaken
		}
	}

	rack := strings.TrimSpace(input.RackAddress)
	id := s.newID()
	artifact, err := s.builder.Build(ctx, id, chain.Content)
	if err != nil {
		return nil, err
	}
	iso := &provisioningdomain.BootISO{
		ID: id, Name: name, IntegrationID: integration.ID, RackAddress: rack,
		ChainURL: chain.ChainURL, UEFIChainURL: chain.UEFIChainURL,
		Script: chain.Content.IPXEScript, GRUBConfig: chain.Content.GRUBConfig,
		IPXEVersion: artifact.IPXEVersion, SizeBytes: artifact.SizeBytes, SHA256: artifact.SHA256,
		CreatedAt: s.now(), CreatedBy: input.CreatedBy,
	}
	if err := s.isos.Create(ctx, iso); err != nil {
		if removeErr := s.builder.Remove(id); removeErr != nil {
			slog.Warn("remove unrecorded boot ISO", "isoId", id, "error", removeErr)
		}
		return nil, err
	}
	item := s.item(iso, integration.SiteID, 0)
	return &item, nil
}

// Rebuild renders the current templates for a Boot ISO's stored address, replaces its file in
// place, and records the rebuild; the id and the URL Boot Media mounts stay the same, so every
// Server using the ISO boots the new script from its next boot. rebuiltBy is the authenticated
// user's username.
//
// The file is replaced before the record. A failed build leaves both as they were; a record that
// cannot be stored after a successful build leaves the new file served under the old record,
// which repeating the rebuild converges. Errors: ErrBootISONotFound, the integration reader's
// errors, ErrInvalidBootISO (a stored address the current rules refuse), ErrBootISOBuilderUnavailable,
// ErrBootISOBuildFailed.
func (s *BootISOService) Rebuild(ctx context.Context, id, rebuiltBy string) (*BootISOItem, error) {
	s.rebuilds.Lock()
	defer s.rebuilds.Unlock()

	iso, err := s.isos.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	integration, err := s.integrations.Find(ctx, iso.IntegrationID)
	if err != nil {
		return nil, err
	}
	chain, err := s.renderChain(integration, iso.RackAddress)
	if err != nil {
		return nil, err
	}
	if err := s.builder.Available(); err != nil {
		return nil, err
	}
	artifact, err := s.builder.Build(ctx, iso.ID, chain.Content)
	if err != nil {
		return nil, err
	}
	rebuilt := *iso
	rebuilt.ChainURL, rebuilt.UEFIChainURL = chain.ChainURL, chain.UEFIChainURL
	rebuilt.Script, rebuilt.GRUBConfig = chain.Content.IPXEScript, chain.Content.GRUBConfig
	rebuilt.IPXEVersion, rebuilt.SizeBytes, rebuilt.SHA256 = artifact.IPXEVersion, artifact.SizeBytes, artifact.SHA256
	rebuilt.RebuiltAt, rebuilt.RebuiltBy = s.now(), rebuiltBy
	if err := s.isos.Update(ctx, &rebuilt); err != nil {
		return nil, err
	}
	item, err := s.view(ctx, &rebuilt, map[string]string{})
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// List returns the Boot ISOs, optionally narrowed to a Site and/or Integration, with the
// builder's availability. Errors: the integration reader's errors.
func (s *BootISOService) List(ctx context.Context, siteID, integrationID string) (*BootISOList, error) {
	list := &BootISOList{Items: []BootISOItem{}}
	if err := s.builder.Available(); err != nil {
		list.Builder.Reason = err.Error()
	} else {
		list.Builder.Available = true
	}

	filter := provisioningdomain.BootISOFilter{}
	siteByIntegration := map[string]string{}
	if siteID != "" {
		integrations, err := s.integrations.ListBySite(ctx, siteID)
		if err != nil {
			return nil, err
		}
		if len(integrations) == 0 {
			return list, nil
		}
		for _, integration := range integrations {
			filter.IntegrationIDs = append(filter.IntegrationIDs, integration.ID)
			siteByIntegration[integration.ID] = integration.SiteID
		}
	}
	if integrationID != "" {
		integration, err := s.integrations.Find(ctx, integrationID)
		if err != nil {
			return nil, err
		}
		if siteID != "" && integration.SiteID != siteID {
			return list, nil
		}
		filter = provisioningdomain.BootISOFilter{IntegrationID: integration.ID}
		siteByIntegration[integration.ID] = integration.SiteID
	}

	isos, err := s.isos.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, iso := range isos {
		item, err := s.view(ctx, iso, siteByIntegration)
		if err != nil {
			return nil, err
		}
		list.Items = append(list.Items, item)
	}
	return list, nil
}

// Get returns one Boot ISO. Errors: ErrBootISONotFound.
func (s *BootISOService) Get(ctx context.Context, id string) (*BootISOItem, error) {
	iso, err := s.isos.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	item, err := s.view(ctx, iso, map[string]string{})
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// Delete removes a Boot ISO and its file. It is refused while a Server's enabled Boot Media uses
// the ISO. A file that cannot be removed is only logged: the record is gone, so the ISO is no
// longer offered. Errors: ErrBootISONotFound, ErrBootISOInUse.
func (s *BootISOService) Delete(ctx context.Context, id string) error {
	if _, err := s.isos.FindByID(ctx, id); err != nil {
		return err
	}
	inUse, err := s.usage.CountEnabledUsing(ctx, id)
	if err != nil {
		return err
	}
	if inUse > 0 {
		return fmt.Errorf("%w: %d Server(s) have Boot Media enabled with it; disable Boot Media on them or switch them to another Boot ISO first", provisioningdomain.ErrBootISOInUse, inUse)
	}
	if err := s.isos.Delete(ctx, id); err != nil {
		return err
	}
	if err := s.builder.Remove(id); err != nil {
		slog.Warn("remove boot ISO file", "isoId", id, "error", err)
	}
	return nil
}

// renderChain renders what a Boot ISO for integration's provisioner chains to at address.
func (s *BootISOService) renderChain(
	integration *provisioningdomain.ProvisionerIntegration,
	address string,
) (provisioningdomain.BootChain, error) {
	renderer, ok := s.renderers[integration.ProviderKind]
	if !ok {
		return provisioningdomain.BootChain{}, fmt.Errorf("%w: this provisioner does not support Boot ISOs", provisioningdomain.ErrInvalidBootISO)
	}
	return renderer.RenderBootChain(address)
}

// view resolves the Site and usage count of a stored Boot ISO. siteByIntegration caches Sites
// already known from the request's filter.
func (s *BootISOService) view(ctx context.Context, iso *provisioningdomain.BootISO, siteByIntegration map[string]string) (BootISOItem, error) {
	siteID, ok := siteByIntegration[iso.IntegrationID]
	if !ok {
		integration, err := s.integrations.Find(ctx, iso.IntegrationID)
		switch {
		case err == nil:
			siteID = integration.SiteID
		case errors.Is(err, provisioningdomain.ErrIntegrationNotProvisioner):
		default:
			// An Integration deleted after the build leaves its Boot ISO listable for deletion.
			slog.Warn("resolve boot ISO integration", "isoId", iso.ID, "integrationId", iso.IntegrationID, "error", err)
		}
		siteByIntegration[iso.IntegrationID] = siteID
	}
	inUse, err := s.usage.CountEnabledUsing(ctx, iso.ID)
	if err != nil {
		return BootISOItem{}, err
	}
	return s.item(iso, siteID, inUse), nil
}

// item shapes a record for the API. uefiChainUrl is reported only for an ISO that carries a GRUB:
// one built before decision 047 — including one rebuilt while its UEFI path still went through
// iPXE, which recorded a UEFI URL but no GRUB — must read as needing a rebuild.
func (s *BootISOService) item(iso *provisioningdomain.BootISO, siteID string, inUse int) BootISOItem {
	uefiChainURL := ""
	if iso.GRUBConfig != "" {
		uefiChainURL = iso.UEFIChainURL
	}
	item := BootISOItem{
		ID: iso.ID, Name: iso.Name, SiteID: siteID, IntegrationID: iso.IntegrationID,
		RackAddress: iso.RackAddress, ChainURL: iso.ChainURL, UEFIChainURL: uefiChainURL,
		Script: iso.Script, GRUBConfig: iso.GRUBConfig, IPXEVersion: iso.IPXEVersion,
		SizeBytes: iso.SizeBytes, SHA256: iso.SHA256, URL: s.builder.URL(iso.ID), InUseBy: inUse,
		CreatedAt: wire.Time(iso.CreatedAt), CreatedBy: iso.CreatedBy, RebuiltBy: iso.RebuiltBy,
	}
	if !iso.RebuiltAt.IsZero() {
		item.RebuiltAt = wire.Time(iso.RebuiltAt)
	}
	return item
}
