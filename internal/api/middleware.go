package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"time"
)

const (
	maxRequestIDLen = 64
	requestIDHeader = "X-Request-ID"
)

type (
	ctxKeyRequestID struct{}
	ctxKeyLogger    struct{}
)

type Middleware = func(http.Handler) http.Handler

func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for _, mw := range slices.Backward(middlewares) {
		h = mw(h)
	}
	return h
}

func JSONError(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		iw := &statusInterceptWriter{ResponseWriter: w}
		next.ServeHTTP(iw, r)

		if !iw.intercepted {
			return
		}

		w.Header().Del("Content-Type")
		w.Header().Del("X-Content-Type-Options")

		switch iw.status {
		case http.StatusNotFound:
			WriteError(r.Context(), w, http.StatusNotFound, "not_found", "route not found")
		case http.StatusMethodNotAllowed:
			WriteError(r.Context(), w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	})
}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get(requestIDHeader)
		if !isValidRequestID(reqID) {
			reqID = generateID()
		}

		w.Header().Set(requestIDHeader, reqID)

		ctx := context.WithValue(r.Context(), ctxKeyRequestID{}, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if reqID, ok := ctx.Value(ctxKeyRequestID{}).(string); ok {
		return reqID
	}
	return ""
}

func isValidRequestID(id string) bool {
	if len(id) == 0 || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		isAllowedPunct := c == '-' || c == '_' || c == '.'
		if !isAlphaNum && !isAllowedPunct {
			return false
		}
	}
	return true
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func WithLogger(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			l := log.With("request_id", RequestIDFrom(r.Context()))
			ctx := context.WithValue(r.Context(), ctxKeyLogger{}, l)
			next.ServeHTTP(w, r.WithContext(ctx))
		}
		return http.HandlerFunc(fn)
	}
}

func LoggerFrom(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(ctxKeyLogger{}).(*slog.Logger); ok {
		return log
	}

	return slog.Default()
}

func AccessLog() Middleware {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := wrapResponseWriter(w)

			defer func() {
				status := ww.status
				if status == 0 {
					status = http.StatusOK
				}

				lvl := slog.LevelInfo
				if status >= 500 {
					lvl = slog.LevelError
				}

				LoggerFrom(r.Context()).Log(r.Context(), lvl, "http request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", status,
					"bytes", ww.bytesWritten,
					"duration", time.Since(start),
					"remote_addr", r.RemoteAddr,
				)
			}()

			next.ServeHTTP(ww, r)
		}
		return http.HandlerFunc(fn)
	}
}

func Recover() Middleware {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			ww := wrapResponseWriter(w)

			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(rec)
				}

				LoggerFrom(r.Context()).Error("panic recovered",
					"panic", rec,
					"stack", string(debug.Stack()),
				)

				if ww.status != 0 {
					panic(http.ErrAbortHandler)
				}

				WriteError(r.Context(), ww, http.StatusInternalServerError, "internal_error", "internal server error")
			}()

			next.ServeHTTP(ww, r)
		}
		return http.HandlerFunc(fn)
	}
}
