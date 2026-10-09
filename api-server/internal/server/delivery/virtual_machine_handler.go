package delivery

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/server/application"
	serverdomain "github.com/maple52046/swallow/internal/server/domain"
	"github.com/maple52046/swallow/internal/shared/apierror"
	"github.com/maple52046/swallow/internal/shared/wire"
)

// VirtualMachineHandler serves GET /api/v1/servers/{id}/virtual-machines of contract
// server-enrollment.md (decision 055): the libvirt domains of a Hypervisor. It is admin-only
// through its route group.
type VirtualMachineHandler struct {
	machines *application.VirtualMachineUseCase
}

// NewVirtualMachineHandler wires the handler to its use case.
func NewVirtualMachineHandler(machines *application.VirtualMachineUseCase) *VirtualMachineHandler {
	return &VirtualMachineHandler{machines: machines}
}

type virtualMachineListResponse struct {
	HypervisorServerID string               `json:"hypervisorServerId"`
	Account            string               `json:"account"`
	Items              []virtualMachineItem `json:"items"`
}

type virtualMachineItem struct {
	Name         string   `json:"name"`
	UUID         string   `json:"uuid"`
	State        string   `json:"state"`
	Architecture string   `json:"architecture"`
	MACAddresses []string `json:"macAddresses"`
	ServerID     *string  `json:"serverId"`
}

// List returns the Hypervisor's domains; `?account=` overrides the login account.
func (h *VirtualMachineHandler) List(c *fiber.Ctx) error {
	list, err := h.machines.List(c.Context(), c.Params("id"), c.Query("account"))
	if err != nil {
		return respondVirtualMachineError(c, err)
	}
	response := virtualMachineListResponse{HypervisorServerID: list.HypervisorServerID, Account: list.Account, Items: make([]virtualMachineItem, 0, len(list.Items))}
	for _, item := range list.Items {
		response.Items = append(response.Items, virtualMachineItem{
			Name: item.Name, UUID: item.UUID, State: item.State, Architecture: item.Architecture,
			MACAddresses: wire.Strings(item.MACAddresses), ServerID: wire.String(item.ServerID),
		})
	}
	return c.JSON(response)
}

// respondVirtualMachineError maps the use case's errors onto the contract's statuses: an account
// that is not a login name is 400; an unknown Server 404; a Server that cannot act as a
// Hypervisor, a missing or rejected Deployment Key, or libvirt the account cannot use 409; an
// unreachable Hypervisor 503.
func respondVirtualMachineError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, serverdomain.ErrServerNotFound):
		return apierror.Respond(c, apierror.New(apierror.CodeNotFound, "Server not found."))
	case errors.Is(err, serverdomain.ErrInvalidDefaultUser):
		return apierror.Respond(c, apierror.New(apierror.CodeValidation, err.Error()))
	case errors.Is(err, serverdomain.ErrHypervisorNotDeployed),
		errors.Is(err, serverdomain.ErrDeploymentKeyMissing),
		errors.Is(err, serverdomain.ErrDeploymentKeyRejected),
		errors.Is(err, serverdomain.ErrLibvirtUnavailable),
		errors.Is(err, serverdomain.ErrLibvirtRefused):
		return apierror.Respond(c, apierror.New(apierror.CodeConflict, err.Error()))
	case errors.Is(err, serverdomain.ErrHostUnreachable):
		return apierror.Respond(c, apierror.New(apierror.CodeProviderUnavailable, err.Error()))
	}
	slog.Error("virtual machine list failed", "server_id", c.Params("id"), "error", err)
	return apierror.Respond(c, apierror.New(apierror.CodeInternal, "The virtual machines could not be listed."))
}
