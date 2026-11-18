package api_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	t.Run("healthz reports ok", func(t *testing.T) {
		pinger := &stubPinger{}
		router := newTestRouter(pinger)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		requireStatus(t, rr, http.StatusOK)

		type healthzResponse struct {
			Status string `json:"status"`
		}

		body := decodeJSONBody[healthzResponse](t, rr)
		if body.Status != "ok" {
			t.Errorf("status = %q, want %q", body.Status, "ok")
		}

		if pinger.calls != 0 {
			t.Errorf("Ping() calls = %d, want 0", pinger.calls)
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

		requireStatus(t, rr, http.StatusOK)

		type readyzResponse struct {
			Status string `json:"status"`
		}

		body := decodeJSONBody[readyzResponse](t, rr)
		if body.Status != "ready" {
			t.Errorf("status = %q, want %q", body.Status, "ready")
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

		requireError(t, rr, http.StatusServiceUnavailable, "database_unavailable")

		if pinger.calls != 1 {
			t.Errorf("Ping() calls = %d, want 1", pinger.calls)
		}
	})
}
