package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"kumparan-assessment/internal/article"
	"kumparan-assessment/internal/author"
	"kumparan-assessment/internal/config"
	"kumparan-assessment/internal/httpx"
	"kumparan-assessment/internal/idgen"
	"kumparan-assessment/internal/postgres"
)

const (
	shutdownTimeout = 10 * time.Second
	requestTimeout  = 5 * time.Second
	maxBodyBytes    = 1 << 20
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DB.DSN(), cfg.DB.MaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	slog.Info("connected to postgres", "host", cfg.DB.Host, "port", cfg.DB.Port, "db", cfg.DB.Name, "max_conns", cfg.DB.MaxConns)

	gin.SetMode(cfg.Server.Mode)

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           newRouter(pool),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func newRouter(pool *pgxpool.Pool) *gin.Engine {
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(
		httpx.RequestID(),
		gin.Logger(),
		httpx.Recovery(),
		httpx.Timeout(requestTimeout),
		httpx.MaxBodyBytes(maxBodyBytes),
	)
	r.NoRoute(httpx.NotFound)
	r.NoMethod(httpx.MethodNotAllowed)

	article.NewHandler(article.NewService(article.NewRepository(pool), idgen.UUIDv7)).Register(r)
	author.NewHandler(author.NewService(author.NewRepository(pool), idgen.UUIDv7)).Register(r)

	r.GET("/healthz", func(c *gin.Context) {
		if err := pool.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok kok gas aja"})
	})

	return r
}
