package command

import (
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"github.com/maple52046/swallow/cli/internal/client"
)

// newProvisioningCommand groups the OS provisioning surface: OS image catalog
// and overlay, deployment templates, Server tags, durable deploy/release/recover
// operations, image verification, network inspection, and provisioning tasks
// (provisioning.md, server-tags.md).
//
// The durable operation verbs (`deploy`, `release`, `recover`) map to the
// canonical *-operations endpoints, not the deprecated one-shot routes.
func newProvisioningCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provisioning",
		Short: "Manage OS images, templates, tags, and deployment operations",
	}
	cmd.AddCommand(
		provisioningImagesCmd(),
		provisioningTemplatesCmd(),
		provisioningTagsCmd(),
		provisioningPreflightCmd(),
		provisioningDeployCmd(),
		provisioningReleaseCmd(),
		provisioningRecoverCmd(),
		provisioningVerifyImageCmd(),
		provisioningNetworksCmd(),
		provisioningTasksCmd(),
	)
	return cmd
}

// -------- OS Images --------

func provisioningImagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "images",
		Short: "List, upload, delete OS images and manage their overlay",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List OS images for an integration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			integration, _ := cmd.Flags().GetString("integration")
			q := newQuery().set("integrationId", integration).build()
			return getJSON(cmd, "provisioning/images", q)
		},
	}
	list.Flags().String("integration", "", "provisioner integration id")
	_ = list.MarkFlagRequired("integration")
	cmd.AddCommand(list)

	cmd.AddCommand(provisioningImageUploadCmd())

	del := &cobra.Command{
		Use:   "delete",
		Short: "Delete an uploaded custom OS image",
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := imageIdentityQuery(cmd)
			return sendNoContent(cmd, "DELETE", "provisioning/images", q, nil)
		},
	}
	addImageIdentityFlags(del)
	cmd.AddCommand(del)

	cmd.AddCommand(provisioningImageOverlayCmd())
	return cmd
}

// provisioningImageUploadCmd streams a multipart image upload. It uses a client
// with the request timeout disabled because the artifact can be multi-gigabyte
// and the contract exempts upload from the short catalog read timeout.
func provisioningImageUploadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload a custom OS image (multipart)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			integration, _ := cmd.Flags().GetString("integration")
			name, _ := cmd.Flags().GetString("name")
			architecture, _ := cmd.Flags().GetString("architecture")
			title, _ := cmd.Flags().GetString("title")
			filetype, _ := cmd.Flags().GetString("filetype")
			defaultUser, _ := cmd.Flags().GetString("default-user")
			path, _ := cmd.Flags().GetString("content")

			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("open image file: %w", err)
			}
			defer f.Close()

			c, err := newUploadClient()
			if err != nil {
				return err
			}
			fields := map[string]string{
				"integrationId": integration,
				"name":          name,
				"architecture":  architecture,
			}
			if title != "" {
				fields["title"] = title
			}
			if filetype != "" {
				fields["filetype"] = filetype
			}
			if defaultUser != "" {
				fields["defaultUser"] = defaultUser
			}
			var out any
			if err := c.Upload(ctx(cmd), "provisioning/images", fields, client.UploadFile{
				Field:  "content",
				Name:   nameFromPath(path),
				Reader: f,
			}, &out); err != nil {
				return err
			}
			return printResult(out)
		},
	}
	cmd.Flags().String("integration", "", "target provisioner integration id")
	cmd.Flags().String("name", "", "operator-chosen image name")
	cmd.Flags().String("architecture", "", "CPU architecture, e.g. amd64")
	cmd.Flags().String("title", "", "optional human-readable label")
	cmd.Flags().String("filetype", "", "optional artifact format (provider default when omitted)")
	cmd.Flags().String("default-user", "", "optional default login user the image's cloud-init creates, e.g. cloud-user")
	cmd.Flags().String("content", "", "path to the image artifact file")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("architecture")
	_ = cmd.MarkFlagRequired("content")
	return cmd
}

func provisioningImageOverlayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "overlay",
		Short: "Set or clear the swallow-owned image overlay (display labels, tags, default user)",
	}

	set := &cobra.Command{
		Use:   "set",
		Short: "Replace the overlay (name, osSystem, release, tags, defaultUser) from a JSON/YAML body",
		Long: "Replace the whole overlay from a JSON/YAML body. Omitted fields are cleared, so " +
			"include every value to keep; defaultUser is the login user automation uses on Servers " +
			"deployed with the image.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendNoContent(cmd, "PATCH", "provisioning/images/overlay", imageIdentityQuery(cmd), body)
		},
	}
	addImageIdentityFlags(set)
	addFileFlag(set, "overlay fields (name, osSystem, release, tags, defaultUser)")
	cmd.AddCommand(set)

	clear := &cobra.Command{
		Use:   "clear",
		Short: "Remove the overlay, reverting to provider values",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sendNoContent(cmd, "DELETE", "provisioning/images/overlay", imageIdentityQuery(cmd), nil)
		},
	}
	addImageIdentityFlags(clear)
	cmd.AddCommand(clear)
	return cmd
}

// addImageIdentityFlags registers the query identity every image overlay/delete
// route requires: integration + image + architecture (an imageId may contain a
// slash, so it cannot be a path segment).
func addImageIdentityFlags(cmd *cobra.Command) {
	cmd.Flags().String("integration", "", "provisioner integration id")
	cmd.Flags().String("image", "", "image id")
	cmd.Flags().String("architecture", "", "image architecture")
	_ = cmd.MarkFlagRequired("integration")
	_ = cmd.MarkFlagRequired("image")
	_ = cmd.MarkFlagRequired("architecture")
}

func imageIdentityQuery(cmd *cobra.Command) url.Values {
	integration, _ := cmd.Flags().GetString("integration")
	image, _ := cmd.Flags().GetString("image")
	architecture, _ := cmd.Flags().GetString("architecture")
	return newQuery().
		set("integrationId", integration).
		set("imageId", image).
		set("architecture", architecture).
		build()
}

// -------- Deployment Templates --------

func provisioningTemplatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "templates",
		Short: "Manage deployment templates and their cloud-init user-data",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List templates, optionally filtered by Site and integration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			site, _ := cmd.Flags().GetString("site-id")
			integration, _ := cmd.Flags().GetString("integration")
			q := newQuery().set("siteId", defaultSite(site)).set("integrationId", integration).build()
			return getJSON(cmd, "provisioning/templates", q)
		},
	}
	list.Flags().String("site-id", "", "filter by Site (defaults to the global --site)")
	list.Flags().String("integration", "", "filter by integration id")
	cmd.AddCommand(list)

	cmd.AddCommand(&cobra.Command{
		Use:   "get <templateId>",
		Short: "Get one template",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, "provisioning/templates/"+args[0], nil)
		},
	})

	create := &cobra.Command{
		Use:   "create",
		Short: "Create a template from a JSON/YAML body",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "provisioning/templates", nil, body)
		},
	}
	addFileFlag(create, "template definition (integrationId, name, imageId, network, ...)")
	cmd.AddCommand(create)

	update := &cobra.Command{
		Use:   "update <templateId>",
		Short: "Update a template from a JSON/YAML body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "PATCH", "provisioning/templates/"+args[0], nil, body)
		},
	}
	addFileFlag(update, "changed template fields")
	cmd.AddCommand(update)

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <templateId>",
		Short: "Delete a template",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendNoContent(cmd, "DELETE", "provisioning/templates/"+args[0], nil, nil)
		},
	})

	cmd.AddCommand(provisioningTemplateUserDataCmd())
	return cmd
}

func provisioningTemplateUserDataCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user-data",
		Short: "Set or clear a template's write-only cloud-init user-data",
	}

	set := &cobra.Command{
		Use:   "set <templateId>",
		Short: "Set the user-data from a raw file (--from) or JSON/YAML body (--file)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := userDataBody(cmd)
			if err != nil {
				return err
			}
			return sendNoContent(cmd, "PUT", fmt.Sprintf("provisioning/templates/%s/user-data", args[0]), nil, body)
		},
	}
	addFileFlag(set, "JSON/YAML body carrying userData")
	set.Flags().String("from", "", "path to a raw cloud-init file to send as userData")
	cmd.AddCommand(set)

	cmd.AddCommand(&cobra.Command{
		Use:   "clear <templateId>",
		Short: "Clear the sealed user-data",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendNoContent(cmd, "DELETE", fmt.Sprintf("provisioning/templates/%s/user-data", args[0]), nil, nil)
		},
	})
	return cmd
}

// userDataBody builds the PUT user-data payload. --from reads a raw cloud-init
// file into {"userData": ...}; otherwise --file supplies the JSON/YAML body
// directly. Exactly one source is required.
func userDataBody(cmd *cobra.Command) (any, error) {
	from, _ := cmd.Flags().GetString("from")
	if from != "" {
		data, err := readFileOrStdin(from)
		if err != nil {
			return nil, err
		}
		return map[string]string{"userData": string(data)}, nil
	}
	return requireBodyFile(cmd)
}

// -------- Server Tags --------

func provisioningTagsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tags",
		Short: "List a Site's tags and edit Server tags",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List the tags known for a Site",
		RunE: func(cmd *cobra.Command, _ []string) error {
			site, _ := cmd.Flags().GetString("site-id")
			effective := defaultSite(site)
			if effective == "" {
				return fmt.Errorf("a Site is required: pass --site-id or the global --site")
			}
			q := newQuery().set("siteId", effective).build()
			return getJSON(cmd, "provisioning/tags", q)
		},
	}
	list.Flags().String("site-id", "", "the Site whose tag catalog to list (defaults to the global --site)")
	cmd.AddCommand(list)

	edit := &cobra.Command{
		Use:   "edit",
		Short: "Apply a tri-state tag edit to one or more Servers",
		Long:  "Edit Server tags via a JSON/YAML body, or with --server (repeatable), --add, and --remove.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, ok, err := readBodyFile(cmd)
			if err != nil {
				return err
			}
			if !ok {
				body, err = tagsEditBodyFromFlags(cmd)
				if err != nil {
					return err
				}
			}
			return sendJSON(cmd, "POST", "provisioning/tags", nil, body)
		},
	}
	addFileFlag(edit, "tag edit body ({serverIds, add, remove}); omit to use the flags below")
	edit.Flags().StringSlice("server", nil, "target Server id (repeatable)")
	edit.Flags().StringSlice("add", nil, "tag to add to every listed Server (repeatable)")
	edit.Flags().StringSlice("remove", nil, "tag to remove from every listed Server (repeatable)")
	cmd.AddCommand(edit)
	return cmd
}

func tagsEditBodyFromFlags(cmd *cobra.Command) (map[string]any, error) {
	servers, _ := cmd.Flags().GetStringSlice("server")
	add, _ := cmd.Flags().GetStringSlice("add")
	remove, _ := cmd.Flags().GetStringSlice("remove")
	if len(servers) == 0 {
		return nil, fmt.Errorf("no target Servers: pass --server (repeatable) or --file")
	}
	if len(add) == 0 && len(remove) == 0 {
		return nil, fmt.Errorf("nothing to change: pass at least one --add or --remove")
	}
	body := map[string]any{"serverIds": servers}
	if len(add) > 0 {
		body["add"] = add
	}
	if len(remove) > 0 {
		body["remove"] = remove
	}
	return body, nil
}

// -------- Deploy / Release / Recover operations --------

func provisioningPreflightCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Run deployment target preflight for one or more Servers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := serverIdsBody(cmd, nil)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "provisioning/deployments/preflight", nil, body)
		},
	}
	addFileFlag(cmd, "preflight body ({serverIds}); omit to use --server")
	cmd.Flags().StringSlice("server", nil, "target Server id (repeatable)")
	return cmd
}

func provisioningDeployCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Create a durable OS deployment Operation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "provisioning/deployment-operations", nil, body)
		},
	}
	addFileFlag(cmd, "deployment body (serverIds, templateId/settings, userData, network)")
	return cmd
}

func provisioningReleaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "release",
		Short: "Create a durable release Operation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			extra := map[string]any{}
			collectEraseFlags(cmd, extra)
			body, err := serverIdsBody(cmd, extra)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "provisioning/release-operations", nil, body)
		},
	}
	addFileFlag(cmd, "release body ({serverIds, erase, ...}); omit to use the flags below")
	cmd.Flags().StringSlice("server", nil, "target Server id (repeatable)")
	addEraseFlags(cmd)
	cmd.Flags().String("comment", "", "optional operator comment")
	cmd.Flags().Bool("unbind-static-ips", false, "unbind captured static IPs after release")
	return cmd
}

func provisioningRecoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recover",
		Short: "Create a durable recover (Return to Ready) Operation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			extra := map[string]any{}
			if cmd.Flags().Changed("comment") {
				v, _ := cmd.Flags().GetString("comment")
				extra["comment"] = v
			}
			if cmd.Flags().Changed("unbind-static-ips") {
				v, _ := cmd.Flags().GetBool("unbind-static-ips")
				extra["unbindStaticIPs"] = v
			}
			body, err := serverIdsBody(cmd, extra)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "provisioning/recover-operations", nil, body)
		},
	}
	addFileFlag(cmd, "recover body ({serverIds, ...}); omit to use the flags below")
	cmd.Flags().StringSlice("server", nil, "target Server id (repeatable)")
	cmd.Flags().String("comment", "", "optional operator comment")
	cmd.Flags().Bool("unbind-static-ips", false, "unbind captured static IPs during recovery")
	return cmd
}

func provisioningVerifyImageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-image",
		Short: "Launch a custom-image verification Operation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := requireBodyFile(cmd)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "provisioning/image-verifications", nil, body)
		},
	}
	addFileFlag(cmd, "verification body (integrationId, imageId, architecture, deployTarget, serverId, keepServer)")
	return cmd
}

func provisioningNetworksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "networks",
		Short: "Inspect live Server network configuration",
	}
	inspect := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect the live network of one or more Servers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := serverIdsBody(cmd, nil)
			if err != nil {
				return err
			}
			return sendJSON(cmd, "POST", "provisioning/networks/inspect", nil, body)
		},
	}
	addFileFlag(inspect, "inspection body ({serverIds}); omit to use --server")
	inspect.Flags().StringSlice("server", nil, "target Server id (repeatable)")
	cmd.AddCommand(inspect)
	return cmd
}

func provisioningTasksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tasks",
		Short: "Inspect and retry provisioning tasks",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get <taskId>",
		Short: "Get one provisioning task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return getJSON(cmd, "provisioning/tasks/"+args[0], nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "retry <taskId>",
		Short: "Retry a retryable provisioning task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendJSON(cmd, "POST", fmt.Sprintf("provisioning/tasks/%s/retry", args[0]), nil, nil)
		},
	})
	return cmd
}

// -------- shared body builders --------

// serverIdsBody builds a request body for the batch endpoints that take a
// serverIds list. A --file body wins when provided; otherwise the --server flag
// values form the list and extra fields (for example erase controls) are merged
// in. It errors when neither a file nor any --server is given.
func serverIdsBody(cmd *cobra.Command, extra map[string]any) (any, error) {
	if body, ok, err := readBodyFile(cmd); err != nil {
		return nil, err
	} else if ok {
		return body, nil
	}
	servers, _ := cmd.Flags().GetStringSlice("server")
	if len(servers) == 0 {
		return nil, fmt.Errorf("no target Servers: pass --server (repeatable) or --file")
	}
	body := map[string]any{"serverIds": servers}
	for k, v := range extra {
		body[k] = v
	}
	return body, nil
}

// addEraseFlags registers the disk-erase controls shared by release.
func addEraseFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("erase", false, "wipe disks on release")
	cmd.Flags().Bool("secure-erase", false, "request hardware secure erase (requires --erase)")
	cmd.Flags().Bool("quick-erase", false, "request quick erase (requires --erase)")
}

// collectEraseFlags copies explicitly-set release controls into the extra map so
// an unset flag is omitted rather than sent as a default the server would honor.
func collectEraseFlags(cmd *cobra.Command, extra map[string]any) {
	for flag, field := range map[string]string{
		"erase":             "erase",
		"secure-erase":      "secureErase",
		"quick-erase":       "quickErase",
		"unbind-static-ips": "unbindStaticIPs",
	} {
		if cmd.Flags().Changed(flag) {
			v, _ := cmd.Flags().GetBool(flag)
			extra[field] = v
		}
	}
	if cmd.Flags().Changed("comment") {
		v, _ := cmd.Flags().GetString("comment")
		extra["comment"] = v
	}
}
