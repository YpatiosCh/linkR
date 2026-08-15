package handlers

import (
	"linkMe/config"
	"linkMe/internal/service"
)

// HandlerManager is the concrete Handler that wires together all handler
// groups with their shared dependencies, starting from a single service.Service.
type HandlerManager struct {
	authHandler AuthHandler
	userHandler UserHandler
}

// NewHandlerManager builds a Handler from a service.Service and the
// application config, constructing all handler groups and returning the
// assembled manager. cfg is needed by AuthHandler's Google OAuth redirect
// endpoints (frontend redirect target, state-signing secret).
func NewHandlerManager(service service.Service, cfg config.Config) Handler {
	return &HandlerManager{
		authHandler: NewAuthHandler(service, cfg),
		userHandler: NewUserHandler(service),
	}
}

// Auth returns the authHandler managed by this HandlerManager.
func (h *HandlerManager) Auth() AuthHandler {
	return h.authHandler
}

// User returns the userHandler managed by this HandlerManager.
func (h *HandlerManager) User() UserHandler {
	return h.userHandler
}
