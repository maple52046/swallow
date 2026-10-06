package app

import (
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"

	provisioningapp "github.com/maple52046/swallow/internal/provisioning/application"
)

// The unauthenticated Server Enrollment downloads (decision 053, contract server-enrollment.md).
// A host that keeps its OS has no swallow credential and often no internet access, so it fetches
// both files from the installation it enrolls into. Neither holds a secret: the provisioner
// credential travels only in the command an admin copies from the enroll-bundle route.

// serveEnrollmentScript returns the GET handler for the enrollment script. The CLI download
// address built into the script is the one the host used to fetch it, read from the request
// (scheme from X-Forwarded-Proto, host and port from Host as the reverse proxy passes them), so
// it is right for whatever address the host reaches swallow at.
func serveEnrollmentScript() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, "text/x-shellscript; charset=utf-8")
		c.Set(fiber.HeaderCacheControl, "no-cache")
		return c.SendString(provisioningapp.HostEnrollmentScript(c.BaseURL()))
	}
}

// serveCLIBinary returns the GET/HEAD handler for the swallow CLI at binaryPath (config
// api.cliBinary). An unset path or a missing file answers 404 with a plain-text reason, which the
// enrollment script's curl shows the operator. The file is checked per request, so a bundle
// upgrade that replaces it is served without a restart, and it is streamed rather than buffered.
func serveCLIBinary(binaryPath string) fiber.Handler {
	path := strings.TrimSpace(binaryPath)
	return func(c *fiber.Ctx) error {
		if path == "" {
			return c.Status(fiber.StatusNotFound).SendString("This swallow installation does not serve its CLI (api.cliBinary is not set).\n")
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return c.Status(fiber.StatusNotFound).SendString("The swallow CLI is missing from this installation.\n")
		}
		c.Attachment("swallow")
		c.Set(fiber.HeaderContentType, "application/octet-stream")
		return c.SendFile(path)
	}
}
