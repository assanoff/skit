// Package query provides the response envelopes for list and single-object
// endpoints. Every envelope shares the house shape — an "error_code" and a
// "data" object — so clients parse one structure across all endpoints:
//
//	list:   {"error_code":"ok","data":{"items":[...],"pagination":{"total_pages":..,"current_page":..,"limit":..,"total_items":..}}}
//	single: {"error_code":"ok","data":{...}}
//
// Result[T] (list) and ResultItem[T]/CursorResult[T] all implement
// rest.ResponseEncoder, so a handler returns them directly.
//
// # Usage
//
//	pg, err := page.Parse(q.Get("page"), q.Get("rows"))
//	if err != nil {
//		return errs.New(errs.InvalidArgument, err)
//	}
//	items, err := core.Query(ctx, pg)   // one page
//	total, err := core.Count(ctx)        // full count
//	return query.NewResult(toDTOs(items), total, pg)
//	// -> {error_code, data:{items, pagination}}
//
//	one, err := core.QueryByID(ctx, id)
//	return query.NewResultItem(toDTO(one)) // -> {error_code, data:{...}}
//
// When the items must also be localized by the translation middleware, embed
// Result in a type that implements translation.TranslatableList over its Items
// (the middleware translates in place before Encode runs).
//
// # Variants
//
//   - Result[T]: offset list envelope {error_code, data:{items, pagination}},
//     where pagination is {total_pages, current_page, limit, total_items}.
//   - ResultItem[T]: single-object envelope {error_code, data:{...}}.
//   - CursorResult[T]: cursor (keyset) list envelope {error_code, data:{items,
//     next?, prev?}}, paired with page.Cursor, for stable/efficient paging over
//     large sets. next/prev are opaque tokens, omitted when there is no such page.
//   - ResultJSONAPI[T]: JSON:API list document (application/vnd.api+json) with the
//     pagination under meta.pagination {page, size, total_pages, total_results}.
//     T is a pointer to a github.com/hashicorp/jsonapi-tagged DTO. Use this for a
//     paginated JSON:API list; for a single JSON:API resource without pagination,
//     use to.JSONAPI instead.
//
// All implement rest.ResponseEncoder.
package query
