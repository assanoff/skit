// Package to hides the repeated work of turning a value into a rest.ResponseEncoder for
// a given content type, so handlers stay terse: return to.JSON(v),
// to.JSONStatus(v, 201) or to.JSONAPI(model) instead of constructing a ResponseEncoder
// by hand.
//
// It is a convenience, not a requirement: rest.Respond accepts ANY rest.ResponseEncoder,
// so a handler whose response needs a bespoke shape can define its own ResponseEncoder
// on its model (in its api package) and return that directly — the SDK responds
// to it the same way.
//
// # Usage
//
//	// plain JSON
//	return to.JSON(widgetDTO)
//	return to.JSONStatus(widgetDTO, http.StatusCreated)
//
//	// JSON:API — the DTO is tagged for github.com/hashicorp/jsonapi:
//	type WidgetDTO struct {
//		ID   string `jsonapi:"primary,widgets"`
//		Name string `jsonapi:"attr,name"`
//	}
//	return to.JSONAPI(&WidgetDTO{ID: id, Name: name}) // single
//	return to.JSONAPI(dtos)                            // collection ([]*WidgetDTO)
//
// JSONAPI emits the application/vnd.api+json content type; the document
// structure (data/attributes/relationships/included) is assembled by the
// jsonapi package from the struct tags.
package to
