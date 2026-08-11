package dotenv

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Load reads the .env file in the current working directory into the
// process environment. It does not fail when no .env file exists; it
// logs a notice and relies on already-set environment variables.
func Load() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, relying on existing environment variables")
	}
}

// GetEnv returns the value of the environment variable key, or fallback
// when key is unset or set to an empty string.
func GetEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
