package handlers

import "linkMe/internal/service"

// HandlerManager is the concrete Handler that wires together all handler
// groups with their shared dependencies, starting from a single service.Service.
type HandlerManager struct {
	authHandler AuthHandler
	meHandler   MeHandler
}

// NewHandlerManager builds a Handler from a service.Service, constructing
// all handler groups and returning the assembled manager.
func NewHandlerManager(service service.Service) Handler {
	return &HandlerManager{
		authHandler: NewAuthHandler(service),
		meHandler:   NewMeHandler(service),
	}
}

// Auth returns the authHandler managed by this HandlerManager.
func (h *HandlerManager) Auth() AuthHandler {
	return h.authHandler
}

// Me returns the meHandler managed by this HandlerManager.
func (h *HandlerManager) Me() MeHandler {
	return h.meHandler
}
