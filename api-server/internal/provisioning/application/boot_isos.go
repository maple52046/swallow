package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
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
	Script        string `json:"script"`
	IPXEVersion   string `json:"ipxeVersion"`
	SizeBytes     int64  `json:"sizeBytes"`
	SHA256        string `json:"sha256"`
	URL           string `json:"url"`
	InUseBy       int    `json:"inUseBy"`
	CreatedAt     string `json:"createdAt"`
	CreatedBy     string `json:"createdBy,omitempty"`
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

// CreateBootISOInput is a build request. CreatedBy is the authenticated operator's username.
type CreateBootISOInput struct {
	Name          string
	IntegrationID string
	RackAddress   string
	CreatedBy     string
}

// BootISOService builds, lists, and deletes Boot ISOs (decision 049).
//
// A build renders the fixed iPXE template for the given MAAS rack, packages it synchronously
// through BootISOBuilder (seconds), and only then stores the record, so a listed Boot ISO always
// has a file; a record that cannot be stored removes the file again. Deletion is refused while a
// Server's enabled Boot Media uses the ISO (BootISOUsage). Authorization (admin) is the delivery
// layer's job.
type BootISOService struct {
	isos         provisioningdomain.BootISORepository
	integrations provisioningdomain.ProvisionerIntegrationReader
	builder      provisioningdomain.BootISOBuilder
	usage        provisioningdomain.BootISOUsage
	now          func() time.Time
	newID        func() string
}

// NewBootISOService wires the service.
func NewBootISOService(
	isos provisioningdomain.BootISORepository,
	integrations provisioningdomain.ProvisionerIntegrationReader,
	builder provisioningdomain.BootISOBuilder,
	usage provisioningdomain.BootISOUsage,
) *BootISOService {
	return &BootISOService{
		isos: isos, integrations: integrations, builder: builder, usage: usage,
		now: func() time.Time { return time.Now().UTC() }, newID: uuid.NewString,
	}
}

// Create builds a Boot ISO for a provisioner Integration's MAAS rack and stores it.
//
// Errors: ErrInvalidBootISO (name or rack address), the integration reader's not-found and
// ErrIntegrationNotProvisioner, ErrBootISONameTaken, ErrBootISOBuilderUnavailable, and
// ErrBootISOBuildFailed.
func (s *BootISOService) Create(ctx context.Context, input CreateBootISOInput) (*BootISOItem, error) {
	name, err := provisioningdomain.ValidateBootISOName(input.Name)
	if err != nil {
		return nil, err
	}
	host, port, err := provisioningdomain.ParseMAASRackAddress(input.RackAddress)
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
	script := provisioningdomain.RenderBootISOScript(host, port)
	id := s.newID()
	artifact, err := s.builder.Build(ctx, id, script)
	if err != nil {
		return nil, err
	}
	iso := &provisioningdomain.BootISO{
		ID: id, Name: name, IntegrationID: integration.ID,
		RackAddress: rack, ChainURL: provisioningdomain.MAASChainURL(host, port), Script: script,
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

func (s *BootISOService) item(iso *provisioningdomain.BootISO, siteID string, inUse int) BootISOItem {
	return BootISOItem{
		ID: iso.ID, Name: iso.Name, SiteID: siteID, IntegrationID: iso.IntegrationID,
		RackAddress: iso.RackAddress, ChainURL: iso.ChainURL, Script: iso.Script, IPXEVersion: iso.IPXEVersion,
		SizeBytes: iso.SizeBytes, SHA256: iso.SHA256, URL: s.builder.URL(iso.ID), InUseBy: inUse,
		CreatedAt: wire.Time(iso.CreatedAt), CreatedBy: iso.CreatedBy,
	}
}
