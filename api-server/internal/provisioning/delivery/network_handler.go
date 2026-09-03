package delivery

import (
	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/provisioning/application"
	provisioningdomain "github.com/maple52046/swallow/internal/provisioning/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
)

type networkMutationRequest struct {
	Mode           string `json:"mode"`
	SubnetID       string `json:"subnetId"`
	IPAddress      string `json:"ipAddress"`
	DefaultGateway bool   `json:"defaultGateway"`
}

type inspectNetworksRequest struct {
	ServerIDs []string `json:"serverIds"`
}
type networkSuggestionResponse struct {
	Mode           string `json:"mode"`
	InterfaceID    string `json:"interfaceId"`
	SubnetID       string `json:"subnetId"`
	IPAddress      string `json:"ipAddress"`
	DefaultGateway bool   `json:"defaultGateway"`
}
type networkSubnetResponse struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	CIDR           string `json:"cidr"`
	GatewayAddress string `json:"gatewayAddress"`
	Managed        bool   `json:"managed"`
}
type networkLinkResponse struct {
	ID             string `json:"id"`
	State          string `json:"configurationState"`
	ProviderMode   string `json:"rawProviderMode"`
	SubnetID       string `json:"subnetId"`
	SubnetName     string `json:"subnetName"`
	CIDR           string `json:"cidr"`
	IPAddress      string `json:"ipAddress"`
	DefaultGateway bool   `json:"defaultGateway"`
}
type networkInterfaceResponse struct {
	ID               string                  `json:"id"`
	Name             string                  `json:"name"`
	MACAddress       string                  `json:"macAddress"`
	Boot             bool                    `json:"boot"`
	PhysicalState    string                  `json:"physicalState"`
	State            string                  `json:"configurationState"`
	ProviderMode     string                  `json:"rawProviderMode"`
	Links            []networkLinkResponse   `json:"links"`
	AvailableSubnets []networkSubnetResponse `json:"availableSubnets"`
}
type machineNetworkResponse struct {
	Interfaces []networkInterfaceResponse `json:"interfaces"`
}
type networkTargetResponse struct {
	ServerID       string                    `json:"serverId"`
	Editable       bool                      `json:"editable"`
	DisabledReason string                    `json:"disabledReason"`
	Suggestion     networkSuggestionResponse `json:"suggestion"`
	Network        machineNetworkResponse    `json:"network"`
}
type inspectNetworksResponse struct {
	Targets []networkTargetResponse         `json:"targets"`
	Issues  []deploymentTargetIssueResponse `json:"issues"`
}

func toNetworkTargetResponse(target application.NetworkTarget) networkTargetResponse {
	interfaces := make([]networkInterfaceResponse, 0)
	if target.Network != nil {
		interfaces = make([]networkInterfaceResponse, 0, len(target.Network.Interfaces))
		for _, iface := range target.Network.Interfaces {
			links := make([]networkLinkResponse, 0, len(iface.Links))
			for _, link := range iface.Links {
				links = append(links, networkLinkResponse{ID: link.ID, State: string(link.State), ProviderMode: link.ProviderMode, SubnetID: link.SubnetID, SubnetName: link.SubnetName, CIDR: link.CIDR, IPAddress: link.IPAddress, DefaultGateway: link.DefaultGateway})
			}
			subnets := make([]networkSubnetResponse, 0, len(iface.AvailableSubnets))
			for _, subnet := range iface.AvailableSubnets {
				subnets = append(subnets, networkSubnetResponse{ID: subnet.ID, Name: subnet.Name, CIDR: subnet.CIDR, GatewayAddress: subnet.GatewayAddress, Managed: subnet.Managed})
			}
			interfaces = append(interfaces, networkInterfaceResponse{ID: iface.ID, Name: iface.Name, MACAddress: iface.MACAddress, Boot: iface.Boot, PhysicalState: string(iface.PhysicalState), State: string(iface.State), ProviderMode: iface.ProviderMode, Links: links, AvailableSubnets: subnets})
		}
	}
	return networkTargetResponse{
		ServerID:       target.ServerID,
		Editable:       target.Editable,
		DisabledReason: target.DisabledReason,
		Suggestion: networkSuggestionResponse{
			Mode:           string(target.Suggestion.Mode),
			InterfaceID:    target.Suggestion.InterfaceID,
			SubnetID:       target.Suggestion.SubnetID,
			IPAddress:      target.Suggestion.IPAddress,
			DefaultGateway: target.Suggestion.DefaultGateway,
		},
		Network: machineNetworkResponse{Interfaces: interfaces},
	}
}

func networkLinkRequest(interfaceID, linkID string, req networkMutationRequest) provisioningdomain.NetworkLinkRequest {
	return provisioningdomain.NetworkLinkRequest{InterfaceID: interfaceID, LinkID: linkID, Mode: provisioningdomain.NetworkLinkMode(req.Mode), SubnetID: req.SubnetID, IPAddress: req.IPAddress, DefaultGateway: req.DefaultGateway}
}

// GetNetwork returns one Server's typed, live NIC and subnet-link state.
func (h *ProvisioningHandler) GetNetwork(c *fiber.Ctx) error {
	item, err := h.networks.Get(c.Context(), c.Params("id"))
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(toNetworkTargetResponse(*item))
}

// InspectNetworks returns deployment suggestions for up to one hundred Servers.
func (h *ProvisioningHandler) InspectNetworks(c *fiber.Ctx) error {
	var req inspectNetworksRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	result, err := h.networks.Inspect(c.Context(), req.ServerIDs)
	if err != nil {
		return RespondError(c, err)
	}
	response := inspectNetworksResponse{Targets: make([]networkTargetResponse, 0, len(result.Targets)), Issues: make([]deploymentTargetIssueResponse, 0, len(result.Issues))}
	for _, target := range result.Targets {
		response.Targets = append(response.Targets, toNetworkTargetResponse(target))
	}
	for _, issue := range result.Issues {
		response.Issues = append(response.Issues, deploymentTargetIssueResponse{ServerID: issue.ServerID, Code: issue.Code, Message: issue.Message})
	}
	return c.JSON(response)
}

// CreateNetworkLink creates one explicit NIC subnet link for a Ready Server.
func (h *ProvisioningHandler) CreateNetworkLink(c *fiber.Ctx) error {
	var req networkMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	item, err := h.networks.CreateLink(c.Context(), c.Params("id"), networkLinkRequest(c.Params("interfaceId"), "", req))
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(toNetworkTargetResponse(*item))
}

// ReplaceNetworkLink replaces exactly one addressed NIC subnet link.
func (h *ProvisioningHandler) ReplaceNetworkLink(c *fiber.Ctx) error {
	var req networkMutationRequest
	if err := c.BodyParser(&req); err != nil {
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, "Invalid request body."))
	}
	item, err := h.networks.ReplaceLink(c.Context(), c.Params("id"), networkLinkRequest(c.Params("interfaceId"), c.Params("linkId"), req))
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(toNetworkTargetResponse(*item))
}

// DeleteNetworkLink unbinds exactly one addressed NIC subnet link.
func (h *ProvisioningHandler) DeleteNetworkLink(c *fiber.Ctx) error {
	item, err := h.networks.DeleteLink(c.Context(), c.Params("id"), c.Params("interfaceId"), c.Params("linkId"))
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(toNetworkTargetResponse(*item))
}

// ListProvisioningTasks returns durable task activity for one Server.
func (h *ProvisioningHandler) ListProvisioningTasks(c *fiber.Ctx) error {
	items, err := h.tasks.ListByServer(c.Context(), c.Params("id"))
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(items)
}

// GetProvisioningTask returns one cleanup task without its internal snapshot.
func (h *ProvisioningHandler) GetProvisioningTask(c *fiber.Ctx) error {
	item, err := h.tasks.Get(c.Context(), c.Params("id"))
	if err != nil {
		return RespondError(c, err)
	}
	return c.JSON(item)
}

// RetryProvisioningTask requeues failed cleanup without repeating Release.
func (h *ProvisioningHandler) RetryProvisioningTask(c *fiber.Ctx) error {
	item, err := h.tasks.Retry(c.Context(), c.Params("id"))
	if err != nil {
		return RespondError(c, err)
	}
	return c.Status(fiber.StatusAccepted).JSON(item)
}
