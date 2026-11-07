package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Server Server
	DB     DB
}

type Server struct {
	Addr string
	Mode string
}

type DB struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
	MaxConns int32
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Config{}, fmt.Errorf("config: load .env: %w", err)
	}

	env := &envReader{}
	cfg := Config{
		Server: Server{
			Addr: normalizeAddr(env.required("SERVER_PORT")),
			Mode: env.oneOf("GIN_MODE", "debug", "release", "test"),
		},
		DB: DB{
			Host:     env.required("DB_HOST"),
			Port:     env.required("DB_PORT"),
			User:     env.required("DB_USER"),
			Password: env.required("DB_PASSWORD"),
			Name:     env.required("DB_NAME"),
			SSLMode:  env.oneOf("DB_SSLMODE", "disable", "allow", "prefer", "require", "verify-ca", "verify-full"),
			MaxConns: env.positiveInt32("DB_MAX_CONNS"),
		},
	}

	if err := env.err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (d DB) DSN() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(d.User, d.Password),
		Host:     net.JoinHostPort(d.Host, d.Port),
		Path:     "/" + d.Name,
		RawQuery: url.Values{"sslmode": {d.SSLMode}}.Encode(),
	}
	return u.String()
}

type envReader struct {
	missing []string
	invalid []string
}

func (r *envReader) required(key string) string {
	v := os.Getenv(key)
	if strings.TrimSpace(v) == "" {
		r.missing = append(r.missing, key)
	}
	return v
}

func (r *envReader) oneOf(key string, allowed ...string) string {
	v := strings.TrimSpace(r.required(key))
	if v != "" && !slices.Contains(allowed, v) {
		r.invalid = append(r.invalid, fmt.Sprintf("%s must be one of %s, got %q", key, strings.Join(allowed, "|"), v))
	}
	return v
}

func (r *envReader) positiveInt32(key string) int32 {
	v := strings.TrimSpace(r.required(key))
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil || n < 1 {
		r.invalid = append(r.invalid, fmt.Sprintf("%s must be a positive integer, got %q", key, v))
		return 0
	}
	return int32(n)
}

func (r *envReader) err() error {
	var problems []string
	if len(r.missing) > 0 {
		problems = append(problems, "missing required env: "+strings.Join(r.missing, ", "))
	}
	problems = append(problems, r.invalid...)
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("config: %s", strings.Join(problems, "; "))
}

func normalizeAddr(port string) string {
	if strings.Contains(port, ":") {
		return port
	}
	return ":" + port
}
