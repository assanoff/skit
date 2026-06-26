// Package query provides the response envelope for paginated list endpoints:
// Result[T] carries the page's items plus the total count, the page/rows echoed
// back, and the derived TotalPages and Prev/Next page numbers. It implements
// rest.ResponseEncoder, so a handler returns it directly.
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
//	// -> {items,total,page,rowsPerPage,totalPages,prev?,next?}
//
// When the items must also be localized by the translation middleware, embed
// Result in a type that implements translation.TranslatableList over its Items
// (the middleware translates in place before Encode runs).
//
// # Variants
//
//   - Result[T]: plain JSON envelope
//     {items,total,page,rowsPerPage,totalPages,prev?,next?} (offset). prev/next
//     are page numbers, omitted when there is no previous/next page.
//   - CursorResult[T]: cursor (keyset) variant {items,next,prev}, paired with
//     page.Cursor, for stable/efficient paging over large sets.
//
// Both implement rest.ResponseEncoder. For a JSON:API list, tag the item DTO with
// github.com/hashicorp/jsonapi tags and return to.JSONAPI(items) instead — the
// jsonapi package assembles the document.
package query
