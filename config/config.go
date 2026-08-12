package config

import (
	"linkMe/pkg/dotenv"
	"strings"
)

// Config holds every runtime configuration value the application needs.
// All values are loaded from environment variables (or the .env file) by
// Load. main validates that required fields are non-empty before handing
// the Config off to the layers that need it.
type Config struct {
	DatabaseURL    string
	JWTSecret      string
	AllowedOrigins []string
	AppEnv         string
}

// Load reads configuration from environment variables, falling back to the
// .env file loaded by dotenv.Load. It does not validate — that is the
// caller's responsibility so errors can be reported with context.
func Load() Config {
	originsRaw := dotenv.GetEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")
	var origins []string
	for _, o := range strings.Split(originsRaw, ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}

	return Config{
		DatabaseURL:    dotenv.GetEnv("DATABASE_URL", ""),
		JWTSecret:      dotenv.GetEnv("JWT_SECRET", ""),
		AllowedOrigins: origins,
		AppEnv:         dotenv.GetEnv("APP_ENV", "development"),
	}
}
