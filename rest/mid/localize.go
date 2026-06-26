package mid

import (
	"context"
	"net/http"

	"github.com/assanoff/skit/errs"
	"github.com/assanoff/skit/i18n"
	"github.com/assanoff/skit/rest"
)

// LocalizeErrors returns application middleware that translates an *errs.Error
// response into the request language before rest.Respond encodes it. The
// language is resolved per request by lang (e.g. a reqctx accessor), so this
// middleware stays decoupled from how the language got into the context.
//
// Non-error responses pass through unchanged; a nil translator or lang accessor
// makes it a no-op. Place it outermost of the application middleware (e.g. first
// in router.New) so it wraps the deeper auth/validation middleware and localizes
// the errors they return.
func LocalizeErrors(tr *i18n.Translator, lang func(context.Context) string) rest.MidFunc {
	return func(next rest.HandlerFunc) rest.HandlerFunc {
		return func(ctx context.Context, r *http.Request) rest.ResponseEncoder {
			resp := next(ctx, r)
			e, ok := resp.(*errs.Error)
			if !ok {
				return resp
			}
			if tr == nil || lang == nil {
				return e
			}
			return tr.TranslateError(lang(ctx), e)
		}
	}
}
