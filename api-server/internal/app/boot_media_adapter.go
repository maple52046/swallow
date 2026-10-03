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
	serverapp "github.com/maple52046/swallow/internal/server/application"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// bootMediaISOPath is the fixed path at which the API process serves the installation's Boot
// Media ISO (decision 047). It is part of the published contract (server-detail-actions.md): the
// installation fixes the base URL, swallow fixes the path, and operators never type an ISO URL.
// The directory component is load-bearing: AMI MegaRAC BMCs do not mount an image that sits at a
// server's root.
const bootMediaISOPath = "/boot-media/ipxe/swallow-ipxe.iso"

// bootMediaImage is the installation's Boot Media ISO as configured: the file the API process
// serves and the base URL BMCs reach the API process at. It is read-only and safe for concurrent
// use; Available stats the file on each call so an ISO put in place after startup is picked up.
type bootMediaImage struct {
	isoPath string
	baseURL string
}

// newBootMediaImage validates nothing at startup on purpose: Boot Media is optional, and a
// missing ISO must only disable it (with a reason), not stop the API.
func newBootMediaImage(isoPath, baseURL string) bootMediaImage {
	return bootMediaImage{isoPath: strings.TrimSpace(isoPath), baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/")}
}

// URL implements serverdomain.BootMediaImage.
func (b bootMediaImage) URL() string {
	if b.baseURL == "" {
		return ""
	}
	return b.baseURL + bootMediaISOPath
}

// Available implements serverdomain.BootMediaImage.
func (b bootMediaImage) Available() error {
	switch {
	case b.baseURL == "":
		return fmt.Errorf("%w: set api.bootMedia.baseURL (SWALLOW_API_BOOT_MEDIA_BASE_URL) to the HTTP address BMCs use to reach swallow", serverdomain.ErrBootMediaNotConfigured)
	case b.isoPath == "":
		return fmt.Errorf("%w: set api.bootMedia.isoPath (SWALLOW_API_BOOT_MEDIA_ISO_PATH) to the iPXE ISO file", serverdomain.ErrBootMediaNotConfigured)
	}
	info, err := os.Stat(b.isoPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("%w: the configured iPXE ISO file is missing or empty", serverdomain.ErrBootMediaNotConfigured)
	}
	return nil
}

// serveBootMediaISO returns the unauthenticated GET/HEAD handler for the Boot Media ISO.
//
// It must stay unauthenticated: a BMC mounts the URL with no way to send a bearer token, and it
// streams the ISO on demand at every boot. It must support HTTP Range requests: BMC HTTP
// virtual media (AMI's httpfs2) reads 4–128 KiB ranges and gives up on a server that answers
// whole-file only. net/http's ServeContent provides Range, HEAD, Accept-Ranges, and
// Last-Modified; the ISO is an iPXE loader of a few MiB, so the adaptor's buffering is harmless.
// The file holds no secret, and only this one path is served.
func serveBootMediaISO(image bootMediaImage) fiber.Handler {
	return adaptor.HTTPHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if image.isoPath == "" {
			http.NotFound(w, r)
			return
		}
		file, err := os.Open(image.isoPath)
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
		http.ServeContent(w, r, "swallow-ipxe.iso", info.ModTime(), file)
	})
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
