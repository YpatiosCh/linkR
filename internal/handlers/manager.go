package handlers

import "linkMe/internal/service"

// HandlerManager is the concrete Handler that wires together all
// handler groups (currently only the auth handler) with their shared
// dependencies, starting from a single service.Service.
type HandlerManager struct {
	authHandler AuthHandler
}

// NewHandlerManager builds a Handler from a service.Service, constructing
// the auth handler with the service and returning the assembled manager.
func NewHandlerManager(service service.Service) Handler {
	return &HandlerManager{authHandler: NewAuthHandler(service)}
}

// Auth returns the authHandler managed by this HandlerManager.
func (h *HandlerManager) Auth() AuthHandler {
	return h.authHandler
}
