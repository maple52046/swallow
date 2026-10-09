package application

import (
	"context"
	"fmt"
	"sort"
	"strings"

	serverdomain "github.com/maple52046/swallow/internal/server/domain"
)

// VirtualMachineUseCase lists the libvirt domains of a Hypervisor (decision 055), so an operator
// can choose virtual machines to enroll by name. It only reads: libvirt over the Deployment Key
// login, and the Server projection to tell which domains are already Servers.
type VirtualMachineUseCase struct {
	servers serverdomain.ServerRepository
	libvirt serverdomain.LibvirtHost
}

// NewVirtualMachineUseCase wires the use case.
func NewVirtualMachineUseCase(servers serverdomain.ServerRepository, libvirt serverdomain.LibvirtHost) *VirtualMachineUseCase {
	return &VirtualMachineUseCase{servers: servers, libvirt: libvirt}
}

// VirtualMachineList is a Hypervisor's domains and the account swallow logged in as.
type VirtualMachineList struct {
	HypervisorServerID string
	Account            string
	Items              []VirtualMachineItem
}

// VirtualMachineItem is one domain and the Server that already has one of its MAC addresses, ""
// when none does.
type VirtualMachineItem struct {
	serverdomain.VirtualMachine
	ServerID string
}

// List reads every domain of the Hypervisor, logging in as account (empty: the Hypervisor's
// effective Server Default User), sorted by name. Errors: ErrServerNotFound and a
// *HypervisorError.
func (uc *VirtualMachineUseCase) List(ctx context.Context, hypervisorID, account string) (*VirtualMachineList, error) {
	hypervisor, err := uc.servers.FindByID(ctx, hypervisorID)
	if err != nil {
		return nil, err
	}
	login, err := serverdomain.HypervisorOf(hypervisor, account)
	if err != nil {
		return nil, err
	}
	machines, err := uc.libvirt.ListDomains(ctx, login)
	if err != nil {
		return nil, err
	}
	byMAC, err := uc.serversByMAC(ctx, hypervisor.Source.SiteID)
	if err != nil {
		return nil, err
	}
	list := &VirtualMachineList{HypervisorServerID: hypervisor.ID, Account: login.Account, Items: make([]VirtualMachineItem, 0, len(machines))}
	for _, machine := range machines {
		item := VirtualMachineItem{VirtualMachine: machine}
		for _, mac := range machine.MACAddresses {
			if id, ok := byMAC[strings.ToLower(mac)]; ok {
				item.ServerID = id
				break
			}
		}
		list.Items = append(list.Items, item)
	}
	sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Name < list.Items[j].Name })
	return list, nil
}

// serversByMAC maps every MAC address of the Site's present Servers to its Server.
func (uc *VirtualMachineUseCase) serversByMAC(ctx context.Context, siteID string) (map[string]string, error) {
	result, err := uc.servers.List(ctx, serverdomain.ListFilter{SiteID: siteID})
	if err != nil {
		return nil, fmt.Errorf("list servers of site %s: %w", siteID, err)
	}
	byMAC := map[string]string{}
	for _, server := range result.Servers {
		for _, mac := range server.Hardware.MACAddresses {
			byMAC[strings.ToLower(mac)] = server.ID
		}
	}
	return byMAC, nil
}
