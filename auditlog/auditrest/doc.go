// Package auditrest is the read side of the audit log over REST: the history
// of a model, the diff between two of its versions and the fields each version
// changed, mounted on any skit router in one call.
//
// # Usage
//
// Routes registers the endpoints through the handle seam, so the package does
// not depend on the router type; pass middleware, such as an admin-only check,
// to protect each endpoint:
//
//	auditrest.NewHandlers(core).Routes(r.HandleApp, adminOnly)
//
// # Endpoints
//
//   - GET /auditlog/{model_type}/{model_id}: every version of the model,
//     oldest first.
//   - GET /auditlog/{model_type}/{model_id}/diff?current=N&target=M: the
//     textual diff between two versions; base64=true encodes it.
//   - GET /auditlog/{model_type}/{model_id}/changes: each version with the
//     fields changed since the previous one.
//
// A version that does not exist is 404, a missing or bad version number 400.
//
// Recording is not done here: audit where the change happens, with
// auditlog.Core.Create or an auditbus Event, so every transport — REST, gRPC,
// workers, consumers — is covered by one wiring.
package auditrest
