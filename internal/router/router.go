package router

import (
	"linkMe/config"
	"linkMe/internal/handlers"
	"linkMe/internal/middleware"
	"linkMe/internal/redis"
	"net/http"
	"time"
)

// SetupRoutes registers all application routes on a new ServeMux, applies
// per-route middleware (rate limiting, authentication), wraps the mux in global
// middleware (request logging outermost, then security headers, then CORS),
// and returns the assembled handler ready to pass to http.ListenAndServe.
func SetupRoutes(h handlers.Handler, cfg config.Config) http.Handler {
	mux := http.NewServeMux()

	requireAuth := middleware.RequireAuth(cfg.JWTSecret, redis.NewSessionRevocationStore(cfg.RedisClient))
	rl := func(name string, limit int, window time.Duration) func(http.Handler) http.Handler {
		return redis.NewRateLimiter(cfg.RedisClient, name, limit, window)
	}

	// Health check — no authentication or rate limiting needed.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Public auth routes.
	mux.Handle("POST /api/v1/auth/register", rl("register", 5, time.Hour)(http.HandlerFunc(h.Auth().Register)))
	mux.Handle("POST /api/v1/auth/login", rl("login", 10, 15*time.Minute)(http.HandlerFunc(h.Auth().Login)))
	mux.Handle("POST /api/v1/auth/refresh", rl("refresh", 60, 15*time.Minute)(http.HandlerFunc(h.Auth().Refresh)))
	mux.Handle("GET /api/v1/auth/google", rl("google-start", 10, 15*time.Minute)(http.HandlerFunc(h.Auth().GoogleStart)))
	mux.Handle("GET /api/v1/auth/google/callback", rl("google-callback", 10, 15*time.Minute)(http.HandlerFunc(h.Auth().GoogleCallback)))

	// Authenticated auth routes.
	mux.Handle("POST /api/v1/auth/logout", rl("logout", 10, 15*time.Minute)(requireAuth(http.HandlerFunc(h.Auth().Logout))))
	mux.Handle("POST /api/v1/auth/logout-all", rl("logout-all", 5, 15*time.Minute)(requireAuth(http.HandlerFunc(h.Auth().LogoutAll))))

	// Public email verification and password reset routes — the opaque
	// token in the request body is the credential, not a bearer token.
	mux.Handle("POST /api/v1/auth/email/verification/request", rl("email-verify-request", 5, time.Hour)(http.HandlerFunc(h.Auth().RequestEmailVerification)))
	mux.Handle("POST /api/v1/auth/email/verification/verify", rl("email-verify-verify", 10, 15*time.Minute)(http.HandlerFunc(h.Auth().VerifyEmail)))
	mux.Handle("POST /api/v1/auth/password/reset/request", rl("password-reset-request", 5, time.Hour)(http.HandlerFunc(h.Auth().RequestPasswordReset)))
	mux.Handle("POST /api/v1/auth/password/reset/confirm", rl("password-reset-confirm", 10, 15*time.Minute)(http.HandlerFunc(h.Auth().ResetPassword)))

	// Authenticated current-user routes.
	mux.Handle("GET /api/v1/me", rl("me", 60, 15*time.Minute)(requireAuth(http.HandlerFunc(h.User().GetMe))))
	mux.Handle("POST /api/v1/me/password/change", rl("me-password-change", 5, 15*time.Minute)(requireAuth(http.HandlerFunc(h.User().ChangePassword))))
	mux.Handle("POST /api/v1/me/password/set", rl("me-password-set", 5, 15*time.Minute)(requireAuth(http.HandlerFunc(h.User().SetPassword))))
	mux.Handle("PATCH /api/v1/me/profile", rl("me-profile-update", 20, 15*time.Minute)(requireAuth(http.HandlerFunc(h.User().UpdateProfile))))
	mux.Handle("DELETE /api/v1/me", rl("me-delete", 5, 15*time.Minute)(requireAuth(http.HandlerFunc(h.User().DeleteAccount))))

	// Global middleware: request logging is outermost so it observes every
	// request end-to-end (including CORS preflights and unmatched routes)
	// and generates the request ID before anything else runs; security
	// headers, CORS, and the request-body size cap wrap the mux as before.
	return middleware.RequestLogger(cfg.Logger)(
		middleware.SecurityHeaders(cfg.AppEnv)(
			middleware.CORS(cfg.AllowedOrigins)(
				middleware.MaxBody(middleware.MaxRequestBodyBytes)(mux),
			),
		),
	)
}
