package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matryer/is"

	"github.com/assanoff/skit/health"
)

func TestLivenessAlwaysOK(t *testing.T) {
	is := is.New(t)

	rec := httptest.NewRecorder()
	health.Liveness()(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	is.Equal(rec.Code, http.StatusOK)
	is.Equal(rec.Header().Get("Content-Type"), "application/json")

	var body map[string]string
	is.NoErr(json.Unmarshal(rec.Body.Bytes(), &body))
	is.Equal(body["status"], "ok")
}

func TestReadinessAllHealthy(t *testing.T) {
	is := is.New(t)

	ok := func(ctx context.Context) error {
		return nil
	}
	h := health.Readiness(
		0,
		health.NamedChecker{Name: "db", Check: ok},
		health.NamedChecker{Name: "cache", Check: ok},
	)

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	is.Equal(rec.Code, http.StatusOK)

	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	is.NoErr(json.Unmarshal(rec.Body.Bytes(), &body))
	is.Equal(body.Status, "ok")
	is.Equal(body.Checks["db"], "ok")
	is.Equal(body.Checks["cache"], "ok")
}

func TestReadinessUnhealthyReports503WithDetail(t *testing.T) {
	is := is.New(t)

	healthy := func(ctx context.Context) error {
		return nil
	}
	failing := func(ctx context.Context) error {
		return errors.New("dial refused")
	}
	h := health.Readiness(
		time.Second,
		health.NamedChecker{Name: "db", Check: healthy},
		health.NamedChecker{Name: "broker", Check: failing},
	)

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	is.Equal(rec.Code, http.StatusServiceUnavailable)

	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	is.NoErr(json.Unmarshal(rec.Body.Bytes(), &body))
	is.Equal(body.Status, "unavailable")
	is.Equal(body.Checks["db"], "ok")
	is.Equal(body.Checks["broker"], "dial refused") // the failing check's error is surfaced
}
