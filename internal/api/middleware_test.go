package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/egor-kardash/webhook-delivery-service/internal/api"
)

type panicPinger struct{}

func (panicPinger) Ping(_ context.Context) error {
	panic("boom")
}

type logEntry struct {
	Level     string `json:"level"`
	Msg       string `json:"msg"`
	Status    int    `json:"status"`
	Bytes     int    `json:"bytes"`
	RequestID string `json:"request_id"`
}

func findLogEntry(t *testing.T, buf *bytes.Buffer, msg string) logEntry {
	t.Helper()

	raw := strings.TrimSpace(buf.String())
	if raw == "" {
		t.Fatalf("log buffer is empty, want entry with msg %q", msg)
	}

	for line := range strings.SplitSeq(raw, "\n") {
		var entry logEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("unmarshal log line %q: %v", line, err)
		}
		if entry.Msg == msg {
			return entry
		}
	}

	t.Fatalf("log entry with msg %q not found in:\n%s", msg, raw)
	return logEntry{}
}

func TestRequestLogging(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	pinger := &stubPinger{}
	router := api.NewRouter(logger, pinger)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/nope", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	requireStatus(t, rr, http.StatusNotFound)

	wantReqID := rr.Header().Get("X-Request-ID")
	if wantReqID == "" {
		t.Fatal("X-Request-ID header is empty, want non-empty")
	}

	entry := findLogEntry(t, &logBuf, "http request")
	if entry.Status != http.StatusNotFound {
		t.Errorf("log status = %d, want %d", entry.Status, http.StatusNotFound)
	}
	if entry.Bytes <= 0 {
		t.Errorf("log bytes = %d, want > 0", entry.Bytes)
	}
	if entry.RequestID != wantReqID {
		t.Errorf("log request_id = %q, want %q", entry.RequestID, wantReqID)
	}

	if pinger.calls != 0 {
		t.Errorf("Ping() calls = %d, want 0", pinger.calls)
	}
}

func TestRequestID(t *testing.T) {
	t.Run("generates header when missing", func(t *testing.T) {
		pinger := &stubPinger{}
		router := newTestRouter(pinger)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()

		router.ServeHTTP(rr, req)

		requireStatus(t, rr, http.StatusOK)

		if got := rr.Header().Get("X-Request-ID"); got == "" {
			t.Error("X-Request-ID header is empty, want generated ID")
		}
	})

	t.Run("echoes valid client header", func(t *testing.T) {
		cases := []struct {
			name        string
			clientReqID string
		}{
			{name: "typical", clientReqID: "some-req-id.123_asd"},
			{name: "max_length_64", clientReqID: strings.Repeat("a", 64)},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				pinger := &stubPinger{}
				router := newTestRouter(pinger)

				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
				req.Header.Set("X-Request-ID", tc.clientReqID)
				rr := httptest.NewRecorder()

				router.ServeHTTP(rr, req)

				requireStatus(t, rr, http.StatusOK)

				if got := rr.Header().Get("X-Request-ID"); got != tc.clientReqID {
					t.Errorf("X-Request-ID = %q, want %q", got, tc.clientReqID)
				}
			})
		}
	})

	t.Run("replaces invalid client header", func(t *testing.T) {
		cases := []struct {
			name     string
			badReqID string
		}{
			{name: "newline", badReqID: "bad\nlog-injection"},
			{name: "spaces", badReqID: "has spaces"},
			{name: "too_long", badReqID: strings.Repeat("x", 65)},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				pinger := &stubPinger{}
				router := newTestRouter(pinger)

				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
				req.Header.Set("X-Request-ID", tc.badReqID)
				rr := httptest.NewRecorder()

				router.ServeHTTP(rr, req)

				requireStatus(t, rr, http.StatusOK)

				if got := rr.Header().Get("X-Request-ID"); got == "" || got == tc.badReqID {
					t.Errorf("got X-Request-ID = %q, want replaced ID", got)
				}
			})
		}
	})
}

func TestPanicRecovery(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	pinger := panicPinger{}
	router := api.NewRouter(logger, pinger)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	requireError(t, rr, http.StatusInternalServerError, "internal_error")

	wantReqID := rr.Header().Get("X-Request-ID")
	if wantReqID == "" {
		t.Fatal("X-Request-ID header is empty, want non-empty")
	}

	panicEntry := findLogEntry(t, &logBuf, "panic recovered")
	if panicEntry.Level != slog.LevelError.String() {
		t.Errorf("panic log level = %q, want %q", panicEntry.Level, slog.LevelError.String())
	}
	if panicEntry.RequestID != wantReqID {
		t.Errorf("panic log request_id = %q, want %q", panicEntry.RequestID, wantReqID)
	}

	accessEntry := findLogEntry(t, &logBuf, "http request")
	if accessEntry.Level != slog.LevelError.String() {
		t.Errorf("access log level = %q, want %q", accessEntry.Level, slog.LevelError.String())
	}
	if accessEntry.Status != http.StatusInternalServerError {
		t.Errorf("access log status = %d, want %d", accessEntry.Status, http.StatusInternalServerError)
	}
	if accessEntry.RequestID != wantReqID {
		t.Errorf("access log request_id = %q, want %q", accessEntry.RequestID, wantReqID)
	}
}
