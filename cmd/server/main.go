package main

import (
	"context"
	"linkMe/config"
	"linkMe/internal/handlers"
	"linkMe/internal/repository"
	"linkMe/internal/router"
	"linkMe/internal/service"
	"linkMe/pkg/dotenv"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

// main is the application entry point. It loads environment variables,
// validates the application config, creates and pings the pgx connection
// pool, constructs the repository, service, and handler managers over the
// pool, registers routes on a new HTTP mux, and serves HTTP on :8080.
func main() {
	dotenv.Load()

	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}
	if cfg.JWTSecret == "" {
		log.Fatal("JWT_SECRET is not set")
	}
	if cfg.ResendAPIKey == "" {
		log.Fatal("RESEND_API_KEY is not set")
	}

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal("unable to create connection pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatal("unable to reach database")
	}
	log.Println("connected to database")

	repos    := repository.NewRepoManager(pool)
	services := service.NewServiceManager(repos, cfg)
	h        := handlers.NewHandlerManager(services)

	log.Println("server listening on :8080")
	if err := http.ListenAndServe(":8080", router.SetupRoutes(h, cfg)); err != nil {
		log.Fatal(err)
	}
}
