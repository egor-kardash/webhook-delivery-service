package api

import (
	"log/slog"
	"net/http"
	"time"
)

const dbTimeout = 3 * time.Second

type Registrar interface {
	RegisterRoutes(*http.ServeMux)
}

func NewRouter(log *slog.Logger, db Pinger, regs ...Registrar) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(db))

	for _, r := range regs {
		r.RegisterRoutes(mux)
	}

	return Chain(mux, RequestLog(log), Recover(), JSONError)
}
