package delivery

import (
	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/shared/apierror"
)

// subnetDNSRequest is the Subnet DNS write body (provisioner-subnet-dns.md). DNSServers is a
// pointer so a missing field is told apart from [], which clears the setting.
type subnetDNSRequest struct {
	DNSServers *[]string `json:"dnsServers"`
}

// ListSubnets serves GET /provisioning/integrations/{id}/subnets (provisioner-subnet-dns.md): the
// Integration's subnets with their Subnet DNS, read live from its provisioner. The response is not
// cached because it reflects live provisioner state an operator is about to edit.
func (h *ProvisioningHandler) ListSubnets(c *fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if h.subnetDNS == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, "Subnet DNS settings are unavailable."))
	}
	id := c.Params("id")
	if id == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id is required."))
	}
	list, err := h.subnetDNS.List(c.Context(), id)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(list)
}

// SetSubnetDNSServers serves PUT /provisioning/integrations/{id}/subnets/{subnetId}/dns-servers:
// it writes the operator's Subnet DNS through to the provisioner and answers 200 with the subnet
// read back. Address rules belong to the use case, so the API and any later client share one set;
// this adapter only requires the field to be present and an array.
func (h *ProvisioningHandler) SetSubnetDNSServers(c *fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if h.subnetDNS == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, "Subnet DNS settings are unavailable."))
	}
	id, subnetID := c.Params("id"), c.Params("subnetId")
	if id == "" || subnetID == "" {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "id and subnetId are required."))
	}
	var req subnetDNSRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	if req.DNSServers == nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "dnsServers is required; send [] to clear it."))
	}
	item, err := h.subnetDNS.SetDNSServers(c.Context(), id, subnetID, *req.DNSServers)
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}
