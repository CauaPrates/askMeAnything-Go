package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/CauaPrates/askMeAnything-Go-React.git/internal/api"
	"github.com/CauaPrates/askMeAnything-Go-React.git/internal/store/pgstore"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, name := range []string{"WSRS_DATABASE_HOST", "WSRS_DATABASE_NAME", "WSRS_DATABASE_USER", "WSRS_DATABASE_PASSWORD"} {
		if os.Getenv(name) == "" {
			return fmt.Errorf("missing environment variable: %s", name)
		}
	}
	port := os.Getenv("WSRS_DATABASE_PORT")
	if port == "" {
		port = "5432"
	}
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	if err != nil || parsedPort == 0 {
		return fmt.Errorf("invalid WSRS_DATABASE_PORT")
	}
	sslmode := os.Getenv("WSRS_DATABASE_SSLMODE")
	if sslmode == "" {
		sslmode = "prefer"
	}
	connectionURL := &url.URL{Scheme: "postgres", User: url.UserPassword(os.Getenv("WSRS_DATABASE_USER"), os.Getenv("WSRS_DATABASE_PASSWORD")), Host: net.JoinHostPort(os.Getenv("WSRS_DATABASE_HOST"), port), Path: "/" + os.Getenv("WSRS_DATABASE_NAME")}
	query := url.Values{"sslmode": []string{sslmode}}
	connectionURL.RawQuery = query.Encode()
	config, err := pgxpool.ParseConfig(connectionURL.String())
	if err != nil {
		return err
	}
	config.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return err
	}
	defer pool.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = pool.Ping(pingCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	handler := api.NewHandler(pgstore.New(pool))
	defer handler.Close()
	address := os.Getenv("WSRS_HTTP_ADDRESS")
	if address == "" {
		address = ":8080"
	}
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	slog.Info("AMA API started", "address", address)
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		handler.Close()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		return server.Shutdown(shutdownCtx)
	}
	return nil
}
