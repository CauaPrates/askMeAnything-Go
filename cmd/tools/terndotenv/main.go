package main

import (
	"errors"
	"os"
	"os/exec"

	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		panic(err)
	}

	cmd := exec.Command(
		"tern",
		"migrate",
		"--migrations",
		"./internal/store/pgstore/migrations",
		"--config",
		"./internal/store/pgstore/migrations/tern.conf",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	port := os.Getenv("WSRS_DATABASE_PORT")
	if port == "" {
		port = "5432"
	}
	sslmode := os.Getenv("WSRS_DATABASE_SSLMODE")
	if sslmode == "" {
		sslmode = "prefer"
	}
	// Initialize pgx fallback addresses with the same host and port as tern.conf.
	cmd.Env = append(os.Environ(), "PGHOST="+os.Getenv("WSRS_DATABASE_HOST"), "PGPORT="+port, "PGSSLMODE="+sslmode)
	if err := cmd.Run(); err != nil {
		panic(err)
	}
}
