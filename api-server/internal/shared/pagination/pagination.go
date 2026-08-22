package pagination

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

type Page struct {
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

type Result[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

func FromQuery(c *fiber.Ctx) Page {
	page := 1
	pageSize := 20

	if p := c.Query("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := c.Query("pageSize"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 100 {
			pageSize = v
		}
	}

	return Page{Page: page, PageSize: pageSize}
}

func (p Page) Offset() int {
	return (p.Page - 1) * p.PageSize
}

func NewResult[T any](items []T, total int, p Page) Result[T] {
	if items == nil {
		items = []T{}
	}
	return Result[T]{
		Items:    items,
		Total:    total,
		Page:     p.Page,
		PageSize: p.PageSize,
	}
}
