package command

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// serversVirtualMachinesCmd groups libvirt virtual-machine enrollment (server-enrollment.md
// "Virtual machines (libvirt)", decision 055). A Hypervisor is a deployed swallow Server; swallow
// logs in to it with the Deployment Key to read its domains, and enrolls the ones named into a
// provisioner as an enroll-virtual-machines Workflow. Virtual machines are named, never typed in as
// MAC addresses: swallow reads those from libvirt.
func serversVirtualMachinesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "virtual-machines",
		Aliases: []string{"vms"},
		Short:   "List a hypervisor's libvirt virtual machines and enroll them by name",
	}

	list := &cobra.Command{
		Use:   "list <hypervisorServerId>",
		Short: "List the hypervisor's libvirt domains, their state, MACs, and the Server already enrolled for each",
		Long: "List every libvirt domain of a hypervisor — a deployed swallow Server that swallow reaches over\n" +
			"SSH with the Deployment Key, as its Server Default User unless --account names another. The\n" +
			"account must be allowed to run virsh against qemu:///system (on Ubuntu, the libvirt group).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			account, _ := cmd.Flags().GetString("account")
			q := newQuery().set("account", strings.TrimSpace(account)).build()
			return getJSON(cmd, fmt.Sprintf("servers/%s/virtual-machines", args[0]), q)
		},
	}
	list.Flags().String("account", "", "login account on the hypervisor (default: its Server Default User)")

	enroll := &cobra.Command{
		Use:   "enroll <hypervisorServerId> <domain>...",
		Short: "Enroll libvirt virtual machines into a provisioner by domain name",
		Long: "Enroll the named libvirt domains of the hypervisor into the provisioner. The request returns\n" +
			"at once with the id of an enroll-virtual-machines Workflow that, for each domain in parallel:\n" +
			"checks it is shut off, lets the provisioner's SSH key log in to the hypervisor account, puts\n" +
			"the Boot ISO on its CD-ROM first (with --boot-iso, for a network the provisioner's DHCP does\n" +
			"not serve), and registers it with the virsh power driver without commissioning it. Automatic\n" +
			"hardware inspection then takes each new Server to ready. Follow it with\n" +
			"`swallow workflows get <workflowId>`; a Task waiting for attention says what to fix, then\n" +
			"`swallow workflows task retry <workflowId> <taskId>` resumes it.\n\n" +
			"A running domain asks for attention unless --power-off-running lets swallow stop it (a hard\n" +
			"power-off).",
		Example: "  swallow servers virtual-machines enroll srv-hv-1 lab-afde-mi308-1 lab-afde-mi308-2 \\\n" +
			"    --integration maas-a --boot-iso 6b3f0c1e-4f7a-4f53-9d2a-2a7f1d0c9e11",
		// The arguments are checked by virtualMachineEnrollmentBody, because --file replaces them.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, ok, err := readBodyFile(cmd)
			if err != nil {
				return err
			}
			if !ok {
				body, err = virtualMachineEnrollmentBody(cmd, args)
				if err != nil {
					return err
				}
			}
			return sendJSON(cmd, "POST", "provisioning/virtual-machine-enrollments", nil, body)
		},
	}
	addFileFlag(enroll, "enrollment body ({integrationId, hypervisorServerId, domains, bootIsoId, account, powerOffRunning}); omit to use the arguments and flags")
	enroll.Flags().String("integration", "", "provisioner Integration id the virtual machines enroll into")
	enroll.Flags().String("boot-iso", "", "Boot ISO id of that provisioner, for a network its DHCP does not serve (swallow provisioning boot-isos list)")
	enroll.Flags().String("account", "", "login account on the hypervisor, also used by the provisioner's virsh driver (default: its Server Default User)")
	enroll.Flags().Bool("power-off-running", false, "stop a running domain instead of asking for attention")

	cmd.AddCommand(list, enroll)
	return cmd
}

// virtualMachineEnrollmentBody builds the enrollment request from the hypervisor and domain
// arguments and the flags, for a command run without --file. Optional fields are omitted when not
// given, so the contract's defaults apply; the domain names are sent as typed, and the API
// validates them.
func virtualMachineEnrollmentBody(cmd *cobra.Command, args []string) (map[string]any, error) {
	if len(args) < 2 {
		return nil, errors.New("give the hypervisor Server id and at least one domain name, or pass --file")
	}
	integration, _ := cmd.Flags().GetString("integration")
	if strings.TrimSpace(integration) == "" {
		return nil, errors.New("--integration is required (the provisioner Integration id), or pass --file")
	}
	body := map[string]any{
		"integrationId":      strings.TrimSpace(integration),
		"hypervisorServerId": args[0],
		"domains":            args[1:],
	}
	if iso, _ := cmd.Flags().GetString("boot-iso"); strings.TrimSpace(iso) != "" {
		body["bootIsoId"] = strings.TrimSpace(iso)
	}
	if account, _ := cmd.Flags().GetString("account"); strings.TrimSpace(account) != "" {
		body["account"] = strings.TrimSpace(account)
	}
	if off, _ := cmd.Flags().GetBool("power-off-running"); off {
		body["powerOffRunning"] = true
	}
	return body, nil
}
