package jsonapi

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/jsonapi"

	"github.com/assanoff/skit/page"
	"github.com/assanoff/skit/rest"
)

// Payload wraps model as a JSON:API rest.ResponseEncoder: model is a pointer to
// a tagged struct (one resource) or a slice of such pointers (a collection). A
// marshal error surfaces from Encode, and rest.Respond maps it to 500.
func Payload(model any) rest.ResponseEncoder {
	return payload{model: model}
}

type payload struct {
	model any
}

// Encode implements rest.ResponseEncoder.
func (p payload) Encode() ([]byte, string, error) {
	var buf bytes.Buffer
	if err := jsonapi.MarshalPayload(&buf, p.model); err != nil {
		return nil, "", fmt.Errorf("jsonapi: marshal payload: %w", err)
	}
	return buf.Bytes(), jsonapi.MediaType, nil
}

// Pagination is the paging metadata of a list document, under meta.pagination.
// Its keys follow the JSON:API convention (page/size), not rest.Pagination.
type Pagination struct {
	Page         int `json:"page"`
	Size         int `json:"size"`
	TotalPages   int `json:"total_pages"`
	TotalResults int `json:"total_results"`
}

// Result is a paginated JSON:API list document: the items as resources and
// the pagination in the top-level meta. It implements rest.ResponseEncoder.
type Result[T any] struct {
	Items []T
	Meta  *jsonapi.Meta
}

// NewResult builds a list document from the page's items, the total row
// count, and the page request that produced them (see rest.NewPagination).
func NewResult[T any](items []T, total int, pg page.Page) Result[T] {
	p := rest.NewPagination(total, pg)
	return Result[T]{
		Items: items,
		Meta: &jsonapi.Meta{
			"pagination": Pagination{
				Page:         p.CurrentPage,
				Size:         p.Limit,
				TotalPages:   p.TotalPages,
				TotalResults: p.TotalItems,
			},
		},
	}
}

// Encode implements rest.ResponseEncoder: it marshals the items into a
// JSON:API many-document and attaches the pagination meta.
func (r Result[T]) Encode() ([]byte, string, error) {
	doc, err := jsonapi.Marshal(r.Items)
	if err != nil {
		return nil, "", fmt.Errorf("jsonapi: marshal result: %w", err)
	}
	if many, ok := doc.(*jsonapi.ManyPayload); ok && r.Meta != nil {
		many.Meta = r.Meta
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return nil, "", fmt.Errorf("jsonapi: encode result: %w", err)
	}
	return data, jsonapi.MediaType, nil
}
