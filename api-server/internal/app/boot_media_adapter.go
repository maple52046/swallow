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

// bmcEndpointSource reads a Server's BMC endpoint from its provisioner (decision 047). It bridges
// the provisioning context's optional BMCConnectionReader capability to the server domain port;
// the password passes through in memory only.
type bmcEndpointSource struct {
	providers provisioningdomain.ProviderFactory
}

// BMCEndpoint implements serverdomain.BMCEndpointSource.
func (s bmcEndpointSource) BMCEndpoint(ctx context.Context, server *serverdomain.Server) (*serverdomain.BMCEndpoint, error) {
	provider, err := s.providers.For(ctx, server.Source.IntegrationID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", serverdomain.ErrBMCConnectionUnavailable, err)
	}
	reader, ok := provider.(provisioningdomain.BMCConnectionReader)
	if !ok || !provider.Capabilities().BMCConnection {
		return nil, serverdomain.ErrNoBMC
	}
	connection, err := reader.BMCConnection(ctx, server.Source.ProviderMachineID)
	var providerErr *provisioningdomain.ProviderError
	switch {
	case errors.Is(err, provisioningdomain.ErrMachineHasNoBMC):
		return nil, serverdomain.ErrNoBMC
	case errors.As(err, &providerErr) && providerErr.Kind == provisioningdomain.ProviderErrorAuth:
		return nil, serverdomain.ErrBMCCredentialUnavailable
	case errors.Is(err, provisioningdomain.ErrMachineNotFound):
		return nil, serverdomain.ErrNoBMC
	case err != nil:
		return nil, fmt.Errorf("%w: %v", serverdomain.ErrBMCConnectionUnavailable, err)
	}
	return &serverdomain.BMCEndpoint{
		Address: connection.Address, Username: connection.Username, Password: connection.Password,
		PowerType: connection.PowerType, SystemHint: connection.NodeID, HostUUID: server.Hardware.SystemUUID,
	}, nil
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
