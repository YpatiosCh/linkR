package config

import (
	"errors"
	"linkMe/internal/redis"
	"linkMe/pkg/dotenv"
	"linkMe/pkg/logging"
	"log/slog"
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
//
// Logger follows the same reasoning as RedisClient: main (startup/shutdown
// logging) and router.SetupRoutes (which wires it into the request-logging
// middleware) both need the same *slog.Logger instance, so it's built once
// here rather than threaded through constructors individually. LogLevel,
// unlike Logger, is an ordinary primitive — like every other string on this
// struct, it's populated in Load regardless of how many places consume it
// (here, exactly one: the line below that builds Logger).
type Config struct {
	DatabaseURL    string
	JWTSecret      string
	AllowedOrigins []string
	AppEnv         string
	LogLevel       string
	ResendAPIKey   string
	EmailFrom      string
	FrontendURL    string
	RedisClient    *redis.Client
	Logger         *slog.Logger

	// Google OAuth credentials. No defaults: GoogleRedirectURL in particular
	// must exactly match what's registered in Google's console, so silently
	// defaulting it would be dangerous. main validates all three are
	// non-empty at startup, same as DatabaseURL/JWTSecret/ResendAPIKey.
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
}

// Load reads configuration from environment variables, falling back to the
// .env file loaded by dotenv.Load, into config. It validates that
// DatabaseURL, JWTSecret, ResendAPIKey, and the three Google OAuth
// credentials are non-empty, returning an error naming the first missing
// one; the caller (main) is responsible for reacting to that error (e.g.
// log.Fatal). RedisClient's reachability is not checked here — that
// requires a live network call, which the caller performs separately.
func Load(config *Config) error {
	originsRaw := dotenv.GetEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")
	var origins []string
	for _, o := range strings.Split(originsRaw, ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}

	config.DatabaseURL = dotenv.GetEnv("DATABASE_URL", "")
	config.JWTSecret = dotenv.GetEnv("JWT_SECRET", "")
	config.AllowedOrigins = origins
	config.AppEnv = dotenv.GetEnv("APP_ENV", "development")
	config.LogLevel = dotenv.GetEnv("LOG_LEVEL", "info")
	config.Logger = logging.New(config.AppEnv, config.LogLevel)
	config.ResendAPIKey = dotenv.GetEnv("RESEND_API_KEY", "")
	config.EmailFrom = dotenv.GetEnv("EMAIL_FROM", "")
	config.FrontendURL = dotenv.GetEnv("FRONTEND_URL", "http://localhost:3000")
	config.RedisClient = redis.NewClient(dotenv.GetEnv("REDIS_ADDR", "localhost:6380"))
	config.GoogleClientID = dotenv.GetEnv("GOOGLE_CLIENT_ID", "")
	config.GoogleClientSecret = dotenv.GetEnv("GOOGLE_CLIENT_SECRET", "")
	config.GoogleRedirectURL = dotenv.GetEnv("GOOGLE_REDIRECT_URL", "")

	if config.DatabaseURL == "" {
		return errors.New("DATABASE_URL is not set")
	}
	if config.JWTSecret == "" {
		return errors.New("JWT_SECRET is not set")
	}
	if config.ResendAPIKey == "" {
		return errors.New("RESEND_API_KEY is not set")
	}
	if config.GoogleClientID == "" {
		return errors.New("GOOGLE_CLIENT_ID is not set")
	}
	if config.GoogleClientSecret == "" {
		return errors.New("GOOGLE_CLIENT_SECRET is not set")
	}
	if config.GoogleRedirectURL == "" {
		return errors.New("GOOGLE_REDIRECT_URL is not set")
	}

	return nil
}
