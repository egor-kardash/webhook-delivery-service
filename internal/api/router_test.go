package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/egor-kardash/webhook-delivery-service/internal/api"
)

type errorBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

func decodeJSONBody[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()

	if got, want := rr.Header().Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}

	var v T
	if err := json.NewDecoder(rr.Body).Decode(&v); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return v
}

type stubPinger struct {
	err   error
	calls int
}

func (p *stubPinger) Ping(ctx context.Context) error {
	p.calls++

	return p.err
}

func newTestRouter(p api.Pinger) http.Handler {
	return api.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), p)
}

func TestHealth(t *testing.T) {
	t.Run("healthz reports ok", func(t *testing.T) {
		pinger := &stubPinger{}
		router := newTestRouter(pinger)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want = %d; body = %q", rr.Code, http.StatusOK, rr.Body.String())
		}

		type healthzResponse struct {
			Status string `json:"status"`
		}

		body := decodeJSONBody[healthzResponse](t, rr)
		if body.Status != "ok" {
			t.Errorf("status = %q, want %q", body.Status, "ok")
		}
	})
}

func TestReadiness(t *testing.T) {
	t.Run("readyz reports ready", func(t *testing.T) {
		pinger := &stubPinger{}
		router := newTestRouter(pinger)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want = %d; body = %q", rr.Code, http.StatusOK, rr.Body.String())
		}

		type readyzResponse struct {
			Status string `json:"status"`
		}

		body := decodeJSONBody[readyzResponse](t, rr)
		if body.Status != "ready" {
			t.Errorf("status = %q, want = %q", body.Status, "ready")
		}

		if pinger.calls != 1 {
			t.Errorf("Ping() calls = %d, want 1", pinger.calls)
		}
	})

	t.Run("readyz returns 503 when database is down", func(t *testing.T) {
		pinger := &stubPinger{err: errors.New("connection refused")}
		router := newTestRouter(pinger)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want = %d; body = %q", rr.Code, http.StatusServiceUnavailable, rr.Body.String())
		}

		body := decodeJSONBody[errorEnvelope](t, rr)
		if body.Error.Code != "database_unavailable" {
			t.Errorf("error.code = %q, want %q", body.Error.Code, "database_unavailable")
		}

		if pinger.calls != 1 {
			t.Errorf("Ping() calls = %d, want 1", pinger.calls)
		}
	})
}

func TestRouterErrors(t *testing.T) {
	t.Run("unknown route returns json 404", func(t *testing.T) {
		pinger := &stubPinger{}
		router := newTestRouter(pinger)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want = %d; body = %q", rr.Code, http.StatusNotFound, rr.Body.String())
		}

		body := decodeJSONBody[errorEnvelope](t, rr)
		if body.Error.Code != "not_found" {
			t.Errorf("error.code = %q, want %q", body.Error.Code, "not_found")
		}

		if pinger.calls != 0 {
			t.Errorf("Ping() calls = %d, want 0", pinger.calls)
		}
	})

	t.Run("wrong method returns 405 with Allow", func(t *testing.T) {
		pinger := &stubPinger{}
		router := newTestRouter(pinger)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/healthz", nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want = %d; body = %q", rr.Code, http.StatusMethodNotAllowed, rr.Body.String())
		}

		if allow := rr.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
			t.Errorf("Allow = %q, want to contain %q", allow, http.MethodGet)
		}

		body := decodeJSONBody[errorEnvelope](t, rr)
		if body.Error.Code != "method_not_allowed" {
			t.Errorf("error.code = %q, want %q", body.Error.Code, "method_not_allowed")
		}

		if pinger.calls != 0 {
			t.Errorf("Ping() calls = %d, want 0", pinger.calls)
		}
	})
}
