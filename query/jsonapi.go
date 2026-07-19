package query

import (
	"encoding/json"

	"github.com/hashicorp/jsonapi"

	"github.com/assanoff/skit/page"
)

// PaginationJSONAPI is the pagination metadata carried in a JSON:API document's
// top-level meta object (meta.pagination). Its keys intentionally differ from the
// plain Pagination envelope — this is the JSON:API convention (page/size).
type PaginationJSONAPI struct {
	Page         int `json:"page"`
	Size         int `json:"size"`
	TotalPages   int `json:"total_pages"`
	TotalResults int `json:"total_results"`
}

// ResultJSONAPI is the JSON:API counterpart of Result: it renders the page's
// items as a JSON:API document (application/vnd.api+json) with the pagination in
// the top-level meta. It implements the rest.ResponseEncoder interface (Encode).
//
// T must be a pointer to a struct tagged with github.com/hashicorp/jsonapi tags
// (`jsonapi:"primary,<type>"`, `jsonapi:"attr,<name>"`, ...); the jsonapi package
// assembles the document from those tags. For a single JSON:API resource without
// pagination, use to.JSONAPI instead.
type ResultJSONAPI[T any] struct {
	Items []T
	Meta  *jsonapi.Meta
}

// NewResultJSONAPI builds a JSON:API list result from the page's items, the total
// row count, and the page request that produced them. The pagination lands in
// meta.pagination. When the total spans no pages (empty result), page and size
// report zero.
func NewResultJSONAPI[T any](items []T, total int, pg page.Page) ResultJSONAPI[T] {
	totalPages := numPages(total, pg.RowsPerPage())
	if totalPages == 0 {
		pg = page.Page{}
	}
	return ResultJSONAPI[T]{
		Items: items,
		Meta: &jsonapi.Meta{
			"pagination": PaginationJSONAPI{
				Page:         pg.Number(),
				Size:         pg.RowsPerPage(),
				TotalPages:   totalPages,
				TotalResults: total,
			},
		},
	}
}

// Encode implements the rest.ResponseEncoder interface: it marshals the items
// into a JSON:API many-document and attaches the pagination meta.
func (r ResultJSONAPI[T]) Encode() ([]byte, string, error) {
	payload, err := jsonapi.Marshal(r.Items)
	if err != nil {
		return nil, "", err
	}
	if many, ok := payload.(*jsonapi.ManyPayload); ok && r.Meta != nil {
		many.Meta = r.Meta
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	return data, jsonapi.MediaType, nil
}
