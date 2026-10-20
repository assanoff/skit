// Package jsonapi encodes rest responses as JSON:API documents
// (application/vnd.api+json) with github.com/hashicorp/jsonapi. It is a
// separate package so that the library reaches only the services that serve
// JSON:API, not every importer of rest.
//
// The DTO is tagged for github.com/hashicorp/jsonapi, which assembles the
// document (data, attributes, relationships, included) from the tags:
//
//	type WidgetResource struct {
//		ID   string `jsonapi:"primary,widgets"`
//		Name string `jsonapi:"attr,name"`
//	}
//
// # Usage
//
//	// one resource, or a collection without pagination
//	return jsonapi.Payload(&WidgetResource{ID: id, Name: name})
//	return jsonapi.Payload(resources) // []*WidgetResource
//
//	// a page of a list: the pagination goes to meta.pagination
//	return jsonapi.NewResult(resources, total, pg)
//
// # API
//
//   - Payload: one resource or a collection, as rest.ResponseEncoder.
//   - Result / NewResult: a list document with meta.pagination {page, size,
//     total_pages, total_results}; on an empty result page and size are zero.
//
// A file that also imports github.com/hashicorp/jsonapi names one of the two
// packages, e.g. hjsonapi.
package jsonapi
