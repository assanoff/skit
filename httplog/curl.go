package httplog

import (
	"fmt"
	"net/http"
	"strings"
)

// CURL returns a curl command for the given request and body. A body of any
// method goes in --data-raw; -X names the method when curl would send another
// one: GET without a body, POST with one.
func CURL(req *http.Request, reqBody string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "curl")
	if req.Method != curlMethod(reqBody) {
		fmt.Fprintf(&b, " -X %s", req.Method)
	}

	fmt.Fprintf(&b, " %s", singleQuoted(requestURL(req)))

	if reqBody != "" {
		fmt.Fprintf(&b, " --data-raw %s", singleQuoted(reqBody))
	}

	for name, vals := range req.Header {
		for _, val := range vals {
			fmt.Fprintf(&b, " -H %s", singleQuoted(fmt.Sprintf("%s: %s", name, val)))
		}
	}

	return b.String()
}

// curlMethod is the method curl sends without -X.
func curlMethod(body string) string {
	if body != "" {
		return http.MethodPost
	}
	return http.MethodGet
}

func singleQuoted(v string) string {
	return fmt.Sprintf("'%s'", strings.ReplaceAll(v, "'", `'\''`))
}

func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func requestURL(r *http.Request) string {
	return fmt.Sprintf("%s://%s%s", scheme(r), r.Host, r.URL)
}
