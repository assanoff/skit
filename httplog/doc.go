// Package httplog writes the access log of an HTTP server: one record per
// request, through the application's own *slog.Logger, so access lines share
// its output, level, trace correlation and sinks.
//
// # Usage
//
// Put RequestLogger into the group of business routes, after middleware.Trace,
// so a record carries the trace of the request and its route:
//
//	r.Use(
//	    middleware.Trace(middleware.TraceConfig{TracerProvider: d.TracerProvider}),
//	    httplog.RequestLogger(d.Logger.With("logger", "access"), &httplog.Options{
//	        Level:         slog.LevelInfo,
//	        Schema:        httplog.SchemaOTEL.Concise(true),
//	        RecoverPanics: true,
//	    }),
//	    d.Metrics.Middleware(),
//	)
//
// A handler adds its own attributes to the record with SetAttrs and its error
// with SetError.
//
// # Fields
//
// The record's level follows the status code: 5xx — error, 4xx — warn (429 —
// info), OPTIONS — debug, the rest — info. Its message is the method, the path
// without the query, the status code and the duration:
// "GET /v1/widgets/42 => HTTP 200 (1.2ms)". The query stays out: it may carry
// tokens and personal data.
//
// The fields are named by the Schema; with SchemaOTEL, the default, they are
// the names of the OpenTelemetry semantic conventions, so the access log, the
// server span and the HTTP metrics share one vocabulary and one value:
//
//   - http.request.method — middleware.Method: a standard method, else _OTHER;
//   - http.route — middleware.Route, e.g. /v1/widgets/{id}; absent outside a
//     route;
//   - http.response.status_code;
//   - http.server.request.duration — in seconds;
//   - client.address — the first X-Forwarded-For hop, else the host of the
//     connection's address, without the port.
//
// Concise keeps these, the error and the headers and bodies the Options log;
// the full schema adds the URL, host, scheme, protocol, sizes, user agent and
// referer.
//
// # Errors
//
// An error goes under ErrorKey ("err") as an error value, the key of every log
// record of the service: SetError's, ErrClientAborted when the client went
// away (with error.type ClientAborted), and a recovered panic as
// "panic: <value>" with its stack under exception.stacktrace.
//
// # Options
//
//   - Level: the lowest level recorded (default info).
//   - Schema: SchemaOTEL (default), SchemaECS or SchemaGCP; Concise(true) for
//     the essential fields only.
//   - RecoverPanics: turn a panic into a 500 (default on).
//   - Skip: a predicate to leave a request out.
//   - LogRequestHeaders, LogResponseHeaders: the headers to record (default
//     Content-Type and Origin, and Content-Type).
//   - LogRequestBody, LogResponseBody, LogBodyContentTypes, LogBodyMaxLen: the
//     bodies to record, their content types and length (default 1024 bytes).
//   - LogExtraAttrs: more attributes from the request, its body and status.
package httplog
