package main

import (
	"context"
	"linkMe/pkg/dotenv"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

// main is the application entry point. It loads environment variables,
// resolves the DATABASE_URL, creates and pings the pgx connection pool,
// registers a health-check handler on a new HTTP mux, and serves HTTP
// on :8080 until the process terminates.
func main() {
	dotenv.Load()

	dbUrl := dotenv.GetEnv("DATABASE_URL", "")
	if dbUrl == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(context.Background(), dbUrl)
	if err != nil {
		log.Fatal("unable to create connection pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatal("unable to reach database")
	}
	log.Println("connected to database")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	log.Println("server listening on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
