package api

import (
	"log/slog"
	"net/http"
	"time"
)

type ServerOptions struct {
	Address           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

func NewServer(opts ServerOptions, handler http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              opts.Address,
		Handler:           handler,
		ReadHeaderTimeout: opts.ReadHeaderTimeout,
		ReadTimeout:       opts.ReadTimeout,
		WriteTimeout:      opts.WriteTimeout,
		IdleTimeout:       opts.IdleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}
}
