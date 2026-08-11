package service

import "linkMe/internal/repository"

// ServiceManager implements Service by holding the concrete sub-services
// and wiring them to the shared repositories.
type ServiceManager struct {
	authService AuthService
}

// NewServiceManager builds a Service wired to the given repositories,
// constructing the auth service (and any other sub-services) on top of them.
func NewServiceManager(repos repository.Repository) Service {
	return &ServiceManager{
		authService: NewAuthService(repos),
	}
}

// Auth returns the auth service held by the manager.
func (s *ServiceManager) Auth() AuthService {
	return s.authService
}
