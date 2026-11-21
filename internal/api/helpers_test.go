package api_test

import (
	"context"
	"encoding/json"
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

type stubPinger struct {
	err   error
	calls int
}

func (p *stubPinger) Ping(ctx context.Context) error {
	p.calls++

	return p.err
}

func requireStatus(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	t.Helper()

	if rr.Code != want {
		t.Fatalf("status = %d, want %d; body = %q", rr.Code, want, rr.Body.String())
	}
}

func requireError(t *testing.T, rr *httptest.ResponseRecorder, status int, code string) errorEnvelope {
	t.Helper()

	requireStatus(t, rr, status)

	body := decodeJSONBody[errorEnvelope](t, rr)
	if body.Error.Code != code {
		t.Errorf("error.code = %q, want %q", body.Error.Code, code)
	}

	return body
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

func newJSONPost(t *testing.T, path, body string) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	return req
}

func serve(t *testing.T, handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	return rr
}

func newTestRouter(p api.Pinger, regs ...api.Registrar) http.Handler {
	return api.NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), p, regs...)
}
