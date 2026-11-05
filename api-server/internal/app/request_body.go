package app

import (
	"regexp"

	"github.com/gofiber/fiber/v2"
)

// requestBodyLimit bounds the request body of every route except the OS image upload content
// (conventions.md, Request Bodies). Every JSON body the contract defines is far below it — the
// largest is a Deployment Template's cloud-init user data — so it only refuses bodies no route
// can use, before anything buffers them. It is also the Fiber body limit, which in stream mode
// only sizes the prefetch.
const requestBodyLimit = 4 << 20

// imageUploadContentPath is the one route whose body is a multi-gigabyte stream the handler bounds
// itself (provisioning.md, OS Image Upload).
var imageUploadContentPath = regexp.MustCompile(`^/api/v1/provisioning/image-uploads/[^/]+/content$`)

// isImageUploadContent reports whether a request is the OS image upload content, the only request
// middleware.BodyLimit leaves unbounded. The match is exact and case-sensitive: a spelling Fiber
// would still route to the handler (it matches paths case-insensitively) is bounded like any other
// route, which can only refuse an upload, never let an unbounded body through.
func isImageUploadContent(c *fiber.Ctx) bool {
	return c.Method() == fiber.MethodPut && imageUploadContentPath.MatchString(c.Path())
}
