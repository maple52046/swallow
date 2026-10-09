package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"

	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	provisioninginfra "github.com/maple52046/swallow/internal/provisioning/infra"
	serverapp "github.com/maple52046/swallow/internal/server/application"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// bootISORoute is the path pattern at which the API process serves Boot ISOs (decision 049). It
// is part of the published contract (server-detail-actions.md, boot-isos.md): the installation
// fixes the base URL, swallow derives the rest from the Boot ISO's id, and operators never type
// an ISO URL. The directory component is load-bearing: AMI MegaRAC BMCs do not mount an image
// that sits at a server's root.
const bootISORoute = "/boot-media/ipxe/:id/" + provisioninginfra.BootISOFileName

// legacyBootMediaISOPath is where the retired installation ISO was served (decision 047). Only
// its URL is still needed, to eject it from a BMC that a setting enabled before Boot ISOs had
// mounted; the route itself is gone.
const legacyBootMediaISOPath = "/boot-media/ipxe/swallow-ipxe.iso"

// bootISOCatalog is the server context's view of Boot ISOs (serverdomain.BootISOResolver): the
// provisioning context's records, served through the Boot ISO builder's files and URLs. It is
// read-only and safe for concurrent use.
type bootISOCatalog struct {
	isos    provisioningdomain.BootISORepository
	files   *provisioninginfra.GenfsimgBuilder
	baseURL string
}

func newBootISOCatalog(isos provisioningdomain.BootISORepository, files *provisioninginfra.GenfsimgBuilder, baseURL string) bootISOCatalog {
	return bootISOCatalog{isos: isos, files: files, baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/")}
}

// Resolve implements serverdomain.BootISOResolver.
func (c bootISOCatalog) Resolve(ctx context.Context, id string) (*serverdomain.BootISOImage, error) {
	iso, err := c.isos.FindByID(ctx, id)
	if errors.Is(err, provisioningdomain.ErrBootISONotFound) {
		return nil, serverdomain.ErrBootISOUnknown
	}
	if err != nil {
		return nil, err
	}
	image := &serverdomain.BootISOImage{ID: iso.ID, Name: iso.Name, IntegrationID: iso.IntegrationID, URL: c.files.URL(iso.ID)}
	switch {
	case c.baseURL == "":
		return image, fmt.Errorf("%w: set api.bootMedia.baseURL (SWALLOW_API_BOOT_MEDIA_BASE_URL) to the HTTP address BMCs use to reach swallow", serverdomain.ErrBootMediaNotConfigured)
	case !c.files.Served(iso.ID):
		return image, fmt.Errorf("%w: the file of Boot ISO %q is missing; build it again", serverdomain.ErrBootMediaNotConfigured, iso.Name)
	}
	return image, nil
}

// File implements serverdomain.BootISOResolver.
func (c bootISOCatalog) File(ctx context.Context, id string) (*serverdomain.BootISOFile, error) {
	iso, err := c.isos.FindByID(ctx, id)
	if errors.Is(err, provisioningdomain.ErrBootISONotFound) {
		return nil, serverdomain.ErrBootISOUnknown
	}
	if err != nil {
		return nil, err
	}
	missing := fmt.Errorf("%w: the file of Boot ISO %q is missing; build it again", serverdomain.ErrBootMediaNotConfigured, iso.Name)
	path, ok := c.files.FilePath(iso.ID)
	if !ok {
		return nil, missing
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, missing
	}
	return &serverdomain.BootISOFile{ID: iso.ID, Name: iso.Name, IntegrationID: iso.IntegrationID, Path: path, Size: info.Size()}, nil
}

// URL implements serverdomain.BootISOResolver.
func (c bootISOCatalog) URL(id string) string {
	if c.baseURL == "" {
		return ""
	}
	if id == "" {
		return c.baseURL + legacyBootMediaISOPath
	}
	return c.files.URL(id)
}

// bootISOUsage answers the provisioning context's "is this Boot ISO in use" from the Server
// projection (provisioningdomain.BootISOUsage).
type bootISOUsage struct {
	servers interface {
		CountBootMediaUsing(ctx context.Context, isoID string) (int, error)
	}
}

// CountEnabledUsing implements provisioningdomain.BootISOUsage.
func (u bootISOUsage) CountEnabledUsing(ctx context.Context, isoID string) (int, error) {
	return u.servers.CountBootMediaUsing(ctx, isoID)
}

// serveBootISO returns the unauthenticated GET/HEAD handler for Boot ISO files.
//
// It must stay unauthenticated: a BMC mounts the URL with no way to send a bearer token, and it
// streams the ISO on demand at every boot. It must support HTTP Range requests: BMC HTTP
// virtual media (AMI's httpfs2) reads 4–128 KiB ranges and gives up on a server that answers
// whole-file only. net/http's ServeContent provides Range, HEAD, Accept-Ranges, and
// Last-Modified; an ISO is an iPXE loader of a few MiB, so the adaptor's buffering is harmless.
// The id is checked against the Boot ISO id shape before any path is built, so the handler can
// only ever read <boot media dir>/<uuid>/swallow-ipxe.iso; the files hold no secret.
func serveBootISO(files *provisioninginfra.GenfsimgBuilder) fiber.Handler {
	return func(c *fiber.Ctx) error {
		path, ok := files.FilePath(c.Params("id"))
		if !ok {
			return c.SendStatus(fiber.StatusNotFound)
		}
		return adaptor.HTTPHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			file, err := os.Open(path)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer file.Close()
			info, err := file.Stat()
			if err != nil || !info.Mode().IsRegular() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/x-iso9660-image")
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeContent(w, r, provisioninginfra.BootISOFileName, info.ModTime(), file)
		})(c)
	}
}

// bootMediaEndpointSource reads a Server's Boot Media method from its provisioner's Power
// Configuration (decisions 047, 054, and 055). It bridges the provisioning context's power adapters
// to the server domain port: the BMC extension of a bmc-family driver (not a VM-host member) yields
// the redfish endpoint, and a virsh driver not owned by a VM host yields libvirt, with the swallow
// Server of the same Site that the address's host names as its Hypervisor. Any other machine has no
// method, with no driver-name check here. The BMC password passes through in memory only.
type bootMediaEndpointSource struct {
	providers provisioningdomain.ProviderFactory
	adapters  *provisioningdomain.PowerAdapterRegistry
	servers   serverdomain.ServerRepository
}

// newBootMediaEndpointSource wires the source with the supported power driver families.
func newBootMediaEndpointSource(providers provisioningdomain.ProviderFactory, servers serverdomain.ServerRepository) bootMediaEndpointSource {
	return bootMediaEndpointSource{providers: providers, adapters: provisioningdomain.DefaultPowerAdapters(), servers: servers}
}

// BootMediaEndpoint implements serverdomain.BootMediaEndpointSource.
func (s bootMediaEndpointSource) BootMediaEndpoint(ctx context.Context, server *serverdomain.Server) (*serverdomain.BootMediaEndpoint, error) {
	provider, err := s.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", serverdomain.ErrBMCConnectionUnavailable, err)
	}
	reader, ok := provider.(provisioningdomain.PowerConfigurationReader)
	if !ok || !provider.Capabilities().PowerConfiguration {
		return nil, serverdomain.ErrNoBMC
	}
	config, err := reader.PowerConfiguration(ctx, server.Source.ProviderMachineID)
	var providerErr *provisioningdomain.ProviderError
	switch {
	case errors.As(err, &providerErr) && providerErr.Kind == provisioningdomain.ProviderErrorAuth:
		return nil, serverdomain.ErrBMCCredentialUnavailable
	case errors.Is(err, provisioningdomain.ErrMachineNotFound):
		return nil, serverdomain.ErrNoBMC
	case err != nil:
		return nil, fmt.Errorf("%w: %v", serverdomain.ErrBMCConnectionUnavailable, err)
	}
	if bmc, hasBMC := s.adapters.BMCAdapter(*config); hasBMC {
		access, ok := bmc.BMCAccess(*config)
		if !ok {
			return nil, serverdomain.ErrNoBMC
		}
		return &serverdomain.BootMediaEndpoint{Method: serverdomain.BootMediaMethodRedfish, BMC: &serverdomain.BMCEndpoint{
			Address: access.Address, Username: access.Username, Password: access.Password,
			PowerType: string(access.Driver), SystemHint: access.SystemHint, HostUUID: server.Hardware.SystemUUID,
		}}, nil
	}
	if config.Driver != provisioningdomain.PowerDriverVirsh || config.ManagedBy != "" || config.PowerID == "" {
		return nil, serverdomain.ErrNoBMC
	}
	account, host, err := provisioningdomain.VirshHostOf(config.Address)
	if err != nil {
		return nil, serverdomain.ErrNoBMC
	}
	hypervisor, err := s.hypervisor(ctx, server, host)
	if err != nil {
		return nil, err
	}
	return &serverdomain.BootMediaEndpoint{Method: serverdomain.BootMediaMethodLibvirt, Libvirt: &serverdomain.LibvirtEndpoint{
		Hypervisor: hypervisor, Host: host, Account: account, Domain: config.PowerID,
	}}, nil
}

// hypervisor finds the present Server of the virtual machine's Site that host names, or nil.
func (s bootMediaEndpointSource) hypervisor(ctx context.Context, server *serverdomain.Server, host string) (*serverdomain.Server, error) {
	result, err := s.servers.List(ctx, serverdomain.ListFilter{SiteID: server.Source.SiteID})
	if err != nil {
		return nil, fmt.Errorf("find hypervisor %s: %w", host, err)
	}
	for _, candidate := range result.Servers {
		if candidate.ID != server.ID && candidate.HostMatches(host) {
			return candidate, nil
		}
	}
	return nil, nil
}

// runRedfishCapabilitySweep probes Servers whose Redfish capability is missing or older than
// window, every interval, until ctx is cancelled — the enrollment-time Redfish detection
// (decision 047): a Server the reconciler just projected is probed on the next pass. It runs in
// the API process only; one pass at a time, a few BMCs in parallel, failures logged per Server.
func runRedfishCapabilitySweep(ctx context.Context, bootMedia *serverapp.BootMediaUseCase, interval, window time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := bootMedia.ProbeStale(ctx, window, 4); err != nil && ctx.Err() == nil {
			slog.Warn("redfish capability sweep", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
