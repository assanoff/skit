// Package apitest provides helpers for end-to-end HTTP tests that drive a real
// application handler through an httptest.Server.
//
// It removes the boilerplate of building requests, sending JSON, asserting
// status codes, and decoding bodies. It depends only on the standard library,
// so it is safe to use from any module's tests without pulling heavy
// dependencies. Every helper takes the *testing.T given to New and fails the
// test directly (t.Fatalf) on a transport error or unmet expectation.
//
// # Usage
//
//	srv := apitest.New(t, handler)
//	created := srv.PostJSON("/widgets", `{"name":"gadget"}`).
//	    ExpectStatus(http.StatusCreated).JSON()
//	id := created["id"].(string)
//	srv.Get("/widgets/"+id, apitest.WithBearer(token)).
//	    ExpectStatus(http.StatusOK)
//
// New starts an httptest.Server for the handler and registers its shutdown with
// t.Cleanup. The request helpers (Get, Delete, PostJSON, PutJSON, and the
// underlying Do) return a *Response with the status, headers, and a buffered
// body for repeated assertions. ExpectStatus asserts the status code and returns
// the receiver for chaining; Decode, JSON, and JSONArray unmarshal the body.
//
// # Options
//
// Request Options customize the outbound request:
//
//   - WithHeader(key, value): sets a request header.
//   - WithBearer(token): sets "Authorization: Bearer <token>".
package apitest
