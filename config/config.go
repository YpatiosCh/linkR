package config

import (
	"linkMe/internal/redis"
	"linkMe/pkg/dotenv"
	"strings"
)

// Config holds every runtime configuration value the application needs.
// All values are loaded from environment variables (or the .env file) by
// Load. main validates that required fields are non-empty (and RedisClient
// reachable) before handing the Config off to the layers that need it.
//
// RedisClient is a deliberate exception to "Config holds only primitive
// values": session revocation (middleware), rate limiting (router), and two
// services all need the same shared Redis client, and threading it through
// every constructor individually was worse than holding it here once,
// alongside the other shared config. pgxpool.Pool and the Resend client are
// NOT given the same treatment — they only ever have one consumer each
// (repository.NewRepoManager, service.NewServiceManager), so there's no
// fan-out problem to solve for them.
type Config struct {
	DatabaseURL    string
	JWTSecret      string
	AllowedOrigins []string
	AppEnv         string
	ResendAPIKey   string
	EmailFrom      string
	FrontendURL    string
	RedisClient    *redis.Client

	// Google OAuth credentials. No defaults: GoogleRedirectURL in particular
	// must exactly match what's registered in Google's console, so silently
	// defaulting it would be dangerous. main validates all three are
	// non-empty at startup, same as DatabaseURL/JWTSecret/ResendAPIKey.
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
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
		ResendAPIKey:   dotenv.GetEnv("RESEND_API_KEY", ""),
		EmailFrom:      dotenv.GetEnv("EMAIL_FROM", ""),
		FrontendURL:    dotenv.GetEnv("FRONTEND_URL", "http://localhost:3000"),
		RedisClient:    redis.NewClient(dotenv.GetEnv("REDIS_ADDR", "localhost:6380")),

		GoogleClientID:     dotenv.GetEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: dotenv.GetEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleRedirectURL:  dotenv.GetEnv("GOOGLE_REDIRECT_URL", ""),
	}
}
