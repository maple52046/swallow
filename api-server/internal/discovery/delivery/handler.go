package delivery

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/AFDEAPAC/swallow/internal/discovery/application"
	"github.com/AFDEAPAC/swallow/internal/shared/apierror"
)

// defaultExporterPort is node_exporter's port, which is the most common caller.
const defaultExporterPort = 9100

type DiscoveryHandler struct {
	discovery *application.DiscoveryUseCase
}

func NewDiscoveryHandler(discovery *application.DiscoveryUseCase) *DiscoveryHandler {
	return &DiscoveryHandler{discovery: discovery}
}

// PrometheusTargets serves a Prometheus http_sd_config endpoint.
func (h *DiscoveryHandler) PrometheusTargets(c *fiber.Ctx) error {
	port := defaultExporterPort
	if raw := c.Query("port"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 65535 {
			return apierror.Respond(c, apierror.New(apierror.CodeValidation,
				"port must be a number between 1 and 65535."))
		}
		port = parsed
	}

	targets, err := h.discovery.PrometheusTargets(c.Context(), application.DiscoveryInput{
		SiteID:            c.Query("siteId"),
		ProvisioningState: c.Query("provisioningState"),
		Port:              port,
	})
	if err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
	return c.JSON(targets)
}

// AnsibleInventory serves an Ansible dynamic inventory for AWX.
func (h *DiscoveryHandler) AnsibleInventory(c *fiber.Ctx) error {
	inventory, err := h.discovery.AnsibleInventory(c.Context(), application.DiscoveryInput{
		SiteID:            c.Query("siteId"),
		ProvisioningState: c.Query("provisioningState"),
	})
	if err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeInternal, "Internal error."))
	}
	return c.JSON(inventory)
}
