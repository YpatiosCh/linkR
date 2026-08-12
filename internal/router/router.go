package router

import (
	"linkMe/config"
	"linkMe/internal/handlers"
	"linkMe/internal/middleware"
	"linkMe/internal/middleware/ratelimit"
	"net/http"
	"time"
)

// SetupRoutes registers all application routes on a new ServeMux, applies
// per-route middleware (rate limiting, authentication), wraps the mux in global
// middleware (security headers outermost, then CORS), and returns the assembled
// handler ready to pass to http.ListenAndServe.
func SetupRoutes(h handlers.Handler, cfg config.Config) http.Handler {
	mux := http.NewServeMux()

	requireAuth := middleware.RequireAuth(cfg.JWTSecret)
	rl := ratelimit.New

	// Health check — no authentication or rate limiting needed.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Public auth routes.
	mux.Handle("POST /api/v1/auth/register", rl(5, time.Hour)(http.HandlerFunc(h.Auth().Register)))
	mux.Handle("POST /api/v1/auth/login", rl(10, 15*time.Minute)(http.HandlerFunc(h.Auth().Login)))
	mux.Handle("POST /api/v1/auth/refresh", rl(60, 15*time.Minute)(http.HandlerFunc(h.Auth().Refresh)))

	// Authenticated auth routes.
	mux.Handle("POST /api/v1/auth/logout", rl(10, 15*time.Minute)(requireAuth(http.HandlerFunc(h.Auth().Logout))))
	mux.Handle("POST /api/v1/auth/logout-all", rl(5, 15*time.Minute)(requireAuth(http.HandlerFunc(h.Auth().LogoutAll))))

	// Authenticated current-user routes.
	mux.Handle("GET /api/v1/me", rl(60, 15*time.Minute)(requireAuth(http.HandlerFunc(h.Me().GetMe))))
	mux.Handle("POST /api/v1/me/password/change", rl(5, 15*time.Minute)(requireAuth(http.HandlerFunc(h.Me().ChangePassword))))

	// Global middleware: security headers run first (outermost), then CORS.
	return middleware.SecurityHeaders(cfg.AppEnv)(
		middleware.CORS(cfg.AllowedOrigins)(mux),
	)
}
