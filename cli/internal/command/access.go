package command

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newAccessCommand groups the installation's access settings, the access audit,
// and the Directory (access.md, decision 082). Every route requires the admin
// role. Pool memberships and Sudo Policy live under `swallow infrastructure
// pools`, because they are addressed by Pool.
func newAccessCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "access",
		Short: "Access settings, the access audit, and the Directory (admin)",
	}

	settings := &cobra.Command{Use: "settings", Short: "Show or change the installation's access settings"}
	settings.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Show the access settings, including the read-only POSIX id start",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return getJSON(cmd, "access/settings", nil)
		},
	})
	set := &cobra.Command{
		Use:   "set",
		Short: "Set whether admins' sudo on every Server asks for a password",
		RunE: func(cmd *cobra.Command, _ []string) error {
			value, _ := cmd.Flags().GetBool("admin-sudo-passwordless")
			return sendJSON(cmd, "PUT", "access/settings", nil, map[string]any{"adminSudoPasswordless": value})
		},
	}
	set.Flags().Bool("admin-sudo-passwordless", false, "admins use sudo without a password")
	_ = set.MarkFlagRequired("admin-sudo-passwordless")
	settings.AddCommand(set)

	audit := &cobra.Command{
		Use:   "audit",
		Short: "List the most recent access changes, newest first",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := newQuery().setInt(cmd, "limit", "limit").build()
			return getJSON(cmd, "access/audit", q)
		},
	}
	audit.Flags().Int("limit", 0, "how many entries to return, 1 to 500 (default: 100)")

	directory := &cobra.Command{Use: "directory", Short: "Show the Directory or apply it now"}
	directory.AddCommand(
		&cobra.Command{
			Use:   "get",
			Short: "Show the Directory's configuration and the Directory controller's last report",
			RunE: func(cmd *cobra.Command, _ []string) error {
				return getJSON(cmd, "access/directory", nil)
			},
		},
		&cobra.Command{
			Use:   "sync",
			Short: "Ask the Directory controller to apply the content now",
			RunE: func(cmd *cobra.Command, _ []string) error {
				return sendNoContent(cmd, "POST", "access/directory/sync", nil, nil)
			},
		},
	)

	cmd.AddCommand(settings, audit, directory)
	return cmd
}

// sudoSettings are the values a membership override takes (glossary Sudo Policy).
var sudoSettings = []string{"inherit", "on", "off"}

// poolMembersCmd builds `swallow infrastructure pools members`: a Pool's
// memberships with their resolved Sudo Policy (pool-access.md). Reads need any
// Pool Role in the Pool (or admin); writes need the Pool's owner role (or
// admin). A Pool the caller may not read is reported as not found.
func poolMembersCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "members", Short: "Manage a Pool's members and their sudo overrides"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list <poolId>",
		Short: "List a Pool's members with their effective sudo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, poolPath(args[0], "members"), nil)
		},
	})

	add := &cobra.Command{
		Use:   "add <poolId>",
		Short: "Add an existing User to a Pool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username, _ := cmd.Flags().GetString("username")
			role, _ := cmd.Flags().GetString("role")
			body := map[string]any{"username": username, "role": role}
			if err := addSudoOverrides(cmd, body); err != nil {
				return err
			}
			return sendJSON(cmd, "POST", poolPath(args[0], "members"), nil, body)
		},
	}
	add.Flags().String("username", "", "the User's username")
	add.Flags().String("role", "member", "Pool Role: member or owner")
	addSudoOverrideFlags(add)
	_ = add.MarkFlagRequired("username")

	update := &cobra.Command{
		Use:   "update <poolId> <userId>",
		Short: "Change a member's Pool Role or sudo overrides",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body := map[string]any{}
			if cmd.Flags().Changed("role") {
				role, _ := cmd.Flags().GetString("role")
				body["role"] = role
			}
			if err := addSudoOverrides(cmd, body); err != nil {
				return err
			}
			if len(body) == 0 {
				return fmt.Errorf("nothing to change: pass --role, --sudo, or --sudo-passwordless")
			}
			return sendJSON(cmd, "PATCH", poolPath(args[0], "members/"+args[1]), nil, body)
		},
	}
	update.Flags().String("role", "", "new Pool Role: member or owner")
	addSudoOverrideFlags(update)

	cmd.AddCommand(add, update, &cobra.Command{
		Use:   "remove <poolId> <userId>",
		Short: "Remove a member; its access to the Pool's Servers ends within a minute",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendNoContent(cmd, "DELETE", poolPath(args[0], "members/"+args[1]), nil, nil)
		},
	})
	return cmd
}

// poolAccessPolicyCmd builds `swallow infrastructure pools access-policy`: a
// Pool's Sudo Policy defaults (pool-access.md). `set` replaces both values, so
// both flags are required; the API has no partial update.
func poolAccessPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "access-policy", Short: "Show or replace a Pool's sudo defaults"}
	cmd.AddCommand(&cobra.Command{
		Use:   "get <poolId>",
		Short: "Show a Pool's sudo defaults",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, poolPath(args[0], "access-policy"), nil)
		},
	})
	set := &cobra.Command{
		Use:   "set <poolId>",
		Short: "Replace a Pool's sudo defaults",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			memberSudo, _ := cmd.Flags().GetBool("member-sudo")
			passwordless, _ := cmd.Flags().GetBool("sudo-passwordless")
			return sendJSON(cmd, "PUT", poolPath(args[0], "access-policy"), nil, map[string]any{
				"memberSudo": memberSudo, "sudoPasswordless": passwordless,
			})
		},
	}
	set.Flags().Bool("member-sudo", false, "members have sudo unless their membership overrides it")
	set.Flags().Bool("sudo-passwordless", false, "sudo asks for no password unless a membership overrides it")
	_ = set.MarkFlagRequired("member-sudo")
	_ = set.MarkFlagRequired("sudo-passwordless")
	cmd.AddCommand(set)
	return cmd
}

// poolPath is the path of a sub-resource of one Pool.
func poolPath(poolID, rest string) string {
	return "infrastructure/pools/" + poolID + "/" + rest
}

// addSudoOverrideFlags adds the --sudo and --sudo-passwordless overrides that
// addSudoOverrides reads.
func addSudoOverrideFlags(cmd *cobra.Command) {
	values := strings.Join(sudoSettings, ", ")
	cmd.Flags().String("sudo", "", "sudo override: "+values+" (ignored for an owner, who always has sudo)")
	cmd.Flags().String("sudo-passwordless", "", "passwordless sudo override: "+values)
}

// addSudoOverrides copies the override flags the user gave into body, checking
// each against the closed set so a typo fails before the request.
func addSudoOverrides(cmd *cobra.Command, body map[string]any) error {
	for _, flag := range []struct{ name, field string }{{"sudo", "sudo"}, {"sudo-passwordless", "sudoPasswordless"}} {
		if !cmd.Flags().Changed(flag.name) {
			continue
		}
		value, _ := cmd.Flags().GetString(flag.name)
		valid := false
		for _, allowed := range sudoSettings {
			valid = valid || value == allowed
		}
		if !valid {
			return fmt.Errorf("invalid --%s %q: use one of %s", flag.name, value, strings.Join(sudoSettings, ", "))
		}
		body[flag.field] = value
	}
	return nil
}
