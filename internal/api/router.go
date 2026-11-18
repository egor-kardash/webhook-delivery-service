package api

import (
	"log/slog"
	"net/http"
)

func NewRouter(log *slog.Logger, db Pinger) http.Handler {
	router := http.NewServeMux()

	handler := Chain(
		router,
		RequestLog(log),
		Recover(),
		JSONError,
	)

	router.HandleFunc("GET /healthz", healthz)
	router.HandleFunc("GET /readyz", readyz(db))

	return handler
}
