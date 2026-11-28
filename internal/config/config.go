package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPServer HTTPServer
	Database   Database
	Delivery   Delivery
	Log        Log
}

type HTTPServer struct {
	Address           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

func (h HTTPServer) Validate() error {
	_, port, err := net.SplitHostPort(h.Address)
	if err != nil {
		return fmt.Errorf("http_server.address %q: %w", h.Address, err)
	}

	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("http_server.port %q: %w", port, err)
	}
	if portNum < 1 || portNum > 65535 {
		return errors.New("http_server.port: must be in range 1-65535")
	}

	if h.ReadHeaderTimeout <= 0 {
		return errors.New("http_server.read_header_timeout: must be greater than 0")
	}
	if h.ReadTimeout <= 0 {
		return errors.New("http_server.read_timeout: must be greater than 0")
	}
	if h.WriteTimeout <= 0 {
		return errors.New("http_server.write_timeout: must be greater than 0")
	}
	if h.IdleTimeout <= 0 {
		return errors.New("http_server.idle_timeout: must be greater than 0")
	}
	if h.ShutdownTimeout <= 0 {
		return errors.New("http_server.shutdown_timeout: must be greater than 0")
	}

	return nil
}

type Database struct {
	Name     string
	User     string
	Password string
	Host     string
	Port     int
	SSLMode  string
}

func (db Database) Validate() error {
	if db.Name == "" {
		return errors.New("database.name: required")
	}
	if db.User == "" {
		return errors.New("database.user: required")
	}
	if db.Password == "" {
		return errors.New("database.password: required")
	}
	if db.Host == "" {
		return errors.New("database.host: required")
	}
	if db.Port < 1 || db.Port > 65535 {
		return errors.New("database.port: must be in range 1-65535")
	}

	switch db.SSLMode {
	case "disable", "require", "verify-ca", "verify-full", "allow", "prefer":
	default:
		return fmt.Errorf("database.sslmode: unsupported value %q", db.SSLMode)
	}

	return nil
}

func (db Database) DSN() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(db.User, db.Password),
		Host:   net.JoinHostPort(db.Host, strconv.Itoa(db.Port)),
		Path:   "/" + db.Name,
	}

	q := u.Query()
	q.Set("sslmode", db.SSLMode)
	u.RawQuery = q.Encode()

	return u.String()
}

type Delivery struct {
	PollInterval time.Duration
	MaxPerTick   int
	HTTPTimeout  time.Duration
	Lease        time.Duration
	Retry        Retry
}

type Retry struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func (d Delivery) Validate() error {
	if d.PollInterval <= 0 {
		return errors.New("delivery.poll_interval: must be greater than 0")
	}
	if d.MaxPerTick <= 0 {
		return errors.New("delivery.max_per_tick: must be greater than 0")
	}
	if d.HTTPTimeout <= 0 {
		return errors.New("delivery.http_timeout: must be greater than 0")
	}
	if d.Lease <= 0 {
		return errors.New("delivery.lease: must be greater than 0")
	}
	if d.Lease <= d.HTTPTimeout {
		return errors.New("delivery.lease: must be greater than delivery.http_timeout")
	}
	if d.Retry.MaxAttempts < 1 {
		return errors.New("delivery.retry.max_attempts: must be at least 1")
	}
	if d.Retry.BaseDelay <= 0 {
		return errors.New("delivery.retry.base_delay: must be greater than 0")
	}
	if d.Retry.MaxDelay <= 0 {
		return errors.New("delivery.retry.max_delay: must be greater than 0")
	}
	if d.Retry.MaxDelay < d.Retry.BaseDelay {
		return errors.New("delivery.retry.max_delay: must be greater than or equal to base_delay")
	}

	return nil
}

type Log struct {
	Level  string
	Format string
}

func (c *Config) Validate() error {
	if err := c.HTTPServer.Validate(); err != nil {
		return err
	}
	if err := c.Database.Validate(); err != nil {
		return err
	}
	if err := c.Delivery.Validate(); err != nil {
		return err
	}

	return nil
}

func Load() (*Config, error) {
	var errs []error

	cfg := Config{
		HTTPServer: HTTPServer{
			Address:           envString("HTTP_ADDR", ":8080"),
			ReadHeaderTimeout: envDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second, &errs),
			ReadTimeout:       envDuration("HTTP_READ_TIMEOUT", 5*time.Second, &errs),
			WriteTimeout:      envDuration("HTTP_WRITE_TIMEOUT", 10*time.Second, &errs),
			IdleTimeout:       envDuration("HTTP_IDLE_TIMEOUT", 60*time.Second, &errs),
			ShutdownTimeout:   envDuration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second, &errs),
		},
		Database: Database{
			Name:     os.Getenv("POSTGRES_DB"),
			User:     os.Getenv("POSTGRES_USER"),
			Password: os.Getenv("POSTGRES_PASSWORD"),
			Host:     envString("POSTGRES_HOST", "localhost"),
			Port:     envInt("POSTGRES_PORT", 5433, &errs),
			SSLMode:  envString("POSTGRES_SSLMODE", "disable"),
		},
		Delivery: Delivery{
			PollInterval: envDuration("DELIVERY_POLL_INTERVAL", time.Second, &errs),
			MaxPerTick:   envInt("DELIVERY_MAX_PER_TICK", 5, &errs),
			HTTPTimeout:  envDuration("DELIVERY_HTTP_TIMEOUT", 5*time.Second, &errs),
			Lease:        envDuration("DELIVERY_LEASE", 30*time.Second, &errs),
			Retry: Retry{
				MaxAttempts: envInt("DELIVERY_MAX_ATTEMPTS", 5, &errs),
				BaseDelay:   envDuration("DELIVERY_RETRY_BASE_DELAY", time.Second, &errs),
				MaxDelay:    envDuration("DELIVERY_RETRY_MAX_DELAY", time.Minute, &errs),
			},
		},
		Log: Log{
			Level:  envString("LOG_LEVEL", "info"),
			Format: envString("LOG_FORMAT", "text"),
		},
	}

	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func envString(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}

	return fallback
}

func envInt(key string, fallback int, errs *[]error) int {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(val)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s %q: %w", key, val, err))
		return 0
	}

	return parsed
}

func envDuration(key string, fallback time.Duration, errs *[]error) time.Duration {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(val)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s %q: %w", key, val, err))
		return 0
	}

	return parsed
}
