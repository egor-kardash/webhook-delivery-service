package api

import (
	"net/http"
	"strings"
)

type responseWriter struct {
	http.ResponseWriter
	status       int
	bytesWritten int
	wroteHeader  bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	if rw, ok := w.(*responseWriter); ok {
		return rw
	}
	return &responseWriter{ResponseWriter: w}
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.status = code
		rw.wroteHeader = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

type statusInterceptWriter struct {
	http.ResponseWriter
	status      int
	intercepted bool
}

func (sw *statusInterceptWriter) WriteHeader(code int) {
	isJSON := strings.HasPrefix(sw.Header().Get("Content-Type"), "application/json")
	if !isJSON && (code == http.StatusNotFound || code == http.StatusMethodNotAllowed) {
		sw.status = code
		sw.intercepted = true
		return
	}
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusInterceptWriter) Write(b []byte) (int, error) {
	if sw.intercepted {
		return len(b), nil
	}
	return sw.ResponseWriter.Write(b)
}

func (sw *statusInterceptWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}
