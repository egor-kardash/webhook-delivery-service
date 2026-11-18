package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouterErrors(t *testing.T) {
	t.Run("unknown route returns json 404", func(t *testing.T) {
		pinger := &stubPinger{}
		router := newTestRouter(pinger)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		requireError(t, rr, http.StatusNotFound, "not_found")

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

		requireError(t, rr, http.StatusMethodNotAllowed, "method_not_allowed")

		if allow := rr.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
			t.Errorf("Allow = %q, want to contain %q", allow, http.MethodGet)
		}

		if pinger.calls != 0 {
			t.Errorf("Ping() calls = %d, want 0", pinger.calls)
		}
	})
}
