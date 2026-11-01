// Package delivery exposes the module list: the Extension Modules compiled into the build and the
// spec schemas they declare (contract: modules.md).
package delivery

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"

	"github.com/maple52046/swallow/internal/core/module/domain"
)

// ModuleLister lists the modules of the build, ordered by kind and then id; the module registry
// implements it.
type ModuleLister interface {
	Modules() []domain.Module
}

// ModuleHandler serves GET /api/v1/modules.
type ModuleHandler struct {
	modules ModuleLister
}

// NewModuleHandler serves the modules of the build.
func NewModuleHandler(modules ModuleLister) *ModuleHandler {
	return &ModuleHandler{modules: modules}
}

// moduleResponse is the wire shape of one module. Spec is the manifest's schema as written, so its
// properties keep the order a generic form shows them in; a nil schema is JSON null.
type moduleResponse struct {
	Kind    string          `json:"kind"`
	ID      string          `json:"id"`
	Version string          `json:"version"`
	Spec    json.RawMessage `json:"spec"`
}

// List returns every module of the build.
func (h *ModuleHandler) List(c *fiber.Ctx) error {
	modules := h.modules.Modules()
	items := make([]moduleResponse, 0, len(modules))
	for _, module := range modules {
		manifest := module.Manifest
		items = append(items, moduleResponse{
			Kind: string(manifest.Kind), ID: manifest.ID, Version: manifest.Version, Spec: manifest.Spec,
		})
	}
	return c.JSON(fiber.Map{"items": items})
}
