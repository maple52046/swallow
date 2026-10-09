package command

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// serversPowerConfigurationCmd groups one Server's Power Configuration (server-detail-actions.md
// "Power Configuration", decision 054): the power driver its provisioner uses and the driver's
// parameters. The provisioner owns it; swallow reads it live and writes it through, so `set`
// changes the provisioner (for MAAS, the Machine's power_type and power_parameters). The password
// is write-only: the API never returns it and these commands never print it.
func serversPowerConfigurationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "power-configuration",
		Short: "Read or set a Server's Power Configuration (power driver and parameters)",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "get <serverId>",
		Short: "Read the power driver, its family and control, and its parameters (no password)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, fmt.Sprintf("servers/%s/power-configuration", args[0]), nil)
		},
	})

	set := &cobra.Command{
		Use:   "set <serverId>",
		Short: "Replace the Power Configuration at the provisioner",
		Long: "Replace the Server's Power Configuration. The driver decides the parameters:\n" +
			"  virsh          --address qemu+ssh://[user@]host[:port]/system --power-id <domain name or UUID>\n" +
			"  ipmi, redfish  --address <BMC host, IP, or URL> [--username <account>]\n" +
			"The provisioner itself connects with these values: for virsh the MAAS rack controller needs SSH\n" +
			"access to the hypervisor account; put SSH options in its SSH configuration, not in the URI.\n\n" +
			"The password is write-only. Without a password flag the provisioner keeps the stored password\n" +
			"when the driver is unchanged and clears it when the driver changes. Prefer --password-stdin to\n" +
			"keep it out of shell history; --clear-password removes it.\n\n" +
			"Setting the configuration does not switch power or resume an inspection waiting for attention:\n" +
			"check `swallow servers power-state`, then retry with `swallow servers inspect`.",
		Example: "  swallow servers power-configuration set srv-1 --driver virsh \\\n" +
			"    --address qemu+ssh://maas@tainan-ci.lab/system --power-id simple-pig\n" +
			"  printf '%s' \"$BMC_PASSWORD\" | swallow servers power-configuration set srv-2 --driver ipmi \\\n" +
			"    --address 10.0.0.5 --username maas --password-stdin",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, ok, err := readBodyFile(cmd)
			if err != nil {
				return err
			}
			if !ok {
				body, err = powerConfigurationBodyFromFlags(cmd, os.Stdin)
				if err != nil {
					return err
				}
			}
			return sendJSON(cmd, "PUT", fmt.Sprintf("servers/%s/power-configuration", args[0]), nil, body)
		},
	}
	addFileFlag(set, "Power Configuration body ({driver, address, powerId, username, password}); omit to use the flags below")
	set.Flags().String("driver", "", "power driver: ipmi, redfish, or virsh")
	set.Flags().String("address", "", "BMC address, or for virsh the hypervisor URI qemu+ssh://[user@]host[:port]/system")
	set.Flags().String("power-id", "", "virsh only: the libvirt domain name or UUID")
	set.Flags().String("username", "", "ipmi/redfish only: the BMC account")
	set.Flags().String("password", "", "driver password (visible in shell history; prefer --password-stdin)")
	set.Flags().Bool("password-stdin", false, "read the driver password from standard input")
	set.Flags().Bool("clear-password", false, "remove the stored driver password")
	cmd.AddCommand(set)

	return cmd
}

// powerConfigurationBodyFromFlags builds the PUT body from flags. Only the parameters given are
// sent; the API refuses parameters that do not apply to the driver, so the command does not
// second-guess the contract's per-driver rules. The password key follows the contract's write-only
// semantics: absent unless a password flag is given, "" for --clear-password. stdin is where
// --password-stdin reads; a trailing newline is dropped so `echo` works.
func powerConfigurationBodyFromFlags(cmd *cobra.Command, stdin io.Reader) (map[string]any, error) {
	driver, _ := cmd.Flags().GetString("driver")
	if strings.TrimSpace(driver) == "" {
		return nil, errors.New("--driver is required (ipmi, redfish, or virsh), or pass --file")
	}
	body := map[string]any{"driver": strings.TrimSpace(driver)}
	for flag, key := range map[string]string{"address": "address", "power-id": "powerId", "username": "username"} {
		if value, _ := cmd.Flags().GetString(flag); value != "" {
			body[key] = value
		}
	}

	fromStdin, _ := cmd.Flags().GetBool("password-stdin")
	clear, _ := cmd.Flags().GetBool("clear-password")
	given := cmd.Flags().Changed("password")
	chosen := 0
	for _, set := range []bool{fromStdin, clear, given} {
		if set {
			chosen++
		}
	}
	if chosen > 1 {
		return nil, errors.New("use only one of --password, --password-stdin, and --clear-password")
	}
	switch {
	case given:
		password, _ := cmd.Flags().GetString("password")
		body["password"] = password
	case clear:
		body["password"] = ""
	case fromStdin:
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read password from standard input: %w", err)
		}
		password := strings.TrimRight(line, "\r\n")
		if password == "" {
			return nil, errors.New("--password-stdin read an empty password; use --clear-password to remove it")
		}
		body["password"] = password
	}
	return body, nil
}
