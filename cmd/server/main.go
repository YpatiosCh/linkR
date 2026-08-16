package main

import (
	"context"
	"linkMe/config"
	"linkMe/internal/handlers"
	"linkMe/internal/repository"
	"linkMe/internal/router"
	"linkMe/internal/service"
	"linkMe/pkg/dotenv"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// main is the application entry point. It loads environment variables,
// validates the application config (which also builds the structured
// logger), creates and pings the pgx connection pool, constructs the
// repository, service, and handler managers over the pool, registers
// routes on a new HTTP mux, and serves HTTP on :8080.
func main() {
	dotenv.Load()

	cfg := config.Config{}
	if err := config.Load(&cfg); err != nil {
		cfg.Logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logger := cfg.Logger

	if err := cfg.RedisClient.Ping(context.Background()).Err(); err != nil {
		logger.Error("unable to reach redis", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to redis")

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("unable to create connection pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		logger.Error("unable to reach database", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to database")

	repos := repository.NewRepoManager(pool)
	services := service.NewServiceManager(repos, cfg)
	h := handlers.NewHandlerManager(services, cfg)

	logger.Info("server listening", "addr", ":8080")
	if err := http.ListenAndServe(":8080", router.SetupRoutes(h, cfg)); err != nil {
		logger.Error("server stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}
