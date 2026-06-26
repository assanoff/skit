package to

import (
	"bytes"

	"github.com/hashicorp/jsonapi"

	"github.com/assanoff/skit/rest"
)

// JSON wraps v as a JSON rest.ResponseEncoder with status 200.
func JSON(v any) rest.ResponseEncoder {
	return rest.JSON(v)
}

// JSONStatus wraps v as a JSON rest.ResponseEncoder with an explicit status.
func JSONStatus(v any, status int) rest.ResponseEncoder {
	return rest.JSONStatus(v, status)
}

// JSONAPI wraps model as a JSON:API rest.ResponseEncoder. model is a pointer to a struct
// (single resource) or a slice of such pointers (collection) whose fields are
// marked with github.com/hashicorp/jsonapi struct tags
// (`jsonapi:"primary,<type>"`, `jsonapi:"attr,<name>"`, ...) — the package builds
// the document, so callers do not hand-assemble it. A marshal error surfaces
// from Encode (Respond maps it to 500).
func JSONAPI(model any) rest.ResponseEncoder {
	return jsonAPIEncoder{model: model}
}

type jsonAPIEncoder struct {
	model any
}

// Encode implements rest.ResponseEncoder, producing a JSON:API document with the
// application/vnd.api+json content type.
func (e jsonAPIEncoder) Encode() ([]byte, string, error) {
	var buf bytes.Buffer
	if err := jsonapi.MarshalPayload(&buf, e.model); err != nil {
		return nil, "", err
	}

	return buf.Bytes(), jsonapi.MediaType, nil
}
