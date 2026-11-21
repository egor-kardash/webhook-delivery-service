//go:build integration

package testdb

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Database string
	User     string
	Password string
	Host     string
	Port     int
	SSLMode  string
}

func (c Config) Validate() error {
	switch {
	case c.Database == "":
		return errors.New("TEST_POSTGRES_DB: required")
	case !strings.HasSuffix(c.Database, "_test"):
		return fmt.Errorf("TEST_POSTGRES_DB %q: must end with _test", c.Database)
	case c.User == "":
		return errors.New("TEST_POSTGRES_USER: required")
	case c.Password == "":
		return errors.New("TEST_POSTGRES_PASSWORD: required")
	case c.Host == "":
		return errors.New("TEST_POSTGRES_HOST: required")
	case c.Port < 1 || c.Port > 65535:
		return fmt.Errorf("TEST_POSTGRES_PORT %d: must be in range 1-65535", c.Port)
	}

	switch c.SSLMode {
	case "disable", "require", "verify-ca", "verify-full", "allow", "prefer":
	default:
		return fmt.Errorf("TEST_POSTGRES_SSLMODE %q: unsupported value", c.SSLMode)
	}

	return nil
}

func (c Config) DSN() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Password),
		Host:   net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Path:   "/" + c.Database,
	}

	q := u.Query()
	q.Set("sslmode", c.SSLMode)
	u.RawQuery = q.Encode()

	return u.String()
}

func Load() (*Config, error) {
	port, err := envInt("TEST_POSTGRES_PORT", 5433)
	if err != nil {
		return nil, err
	}

	cfg := Config{
		Database: os.Getenv("TEST_POSTGRES_DB"),
		User:     os.Getenv("TEST_POSTGRES_USER"),
		Password: os.Getenv("TEST_POSTGRES_PASSWORD"),
		Host:     envString("TEST_POSTGRES_HOST", "localhost"),
		Port:     port,
		SSLMode:  envString("TEST_POSTGRES_SSLMODE", "disable"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func RequireDatabase(ctx context.Context, pool *pgxpool.Pool) error {
	var name string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil {
		return fmt.Errorf("read current_database: %w", err)
	}
	if !strings.HasSuffix(name, "_test") {
		return fmt.Errorf("refusing to run against database %q: name must end with _test", name)
	}

	return nil
}

func RequireSchema(ctx context.Context, pool *pgxpool.Pool, table string) error {
	var migrated bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&migrated); err != nil {
		return fmt.Errorf("check schema: %w", err)
	}
	if !migrated {
		return fmt.Errorf("schema is not migrated: %q is missing; run make test-integration", table)
	}

	return nil
}

func envString(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}

	return fallback
}

func envInt(key string, fallback int) (int, error) {
	val, ok := os.LookupEnv(key)
	if !ok || val == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("%s %q: %w", key, val, err)
	}

	return parsed, nil
}
