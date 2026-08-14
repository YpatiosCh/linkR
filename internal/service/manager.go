package service

import (
	"linkMe/config"
	"linkMe/internal/repository"

	"github.com/resend/resend-go/v3"
)

// ServiceManager implements Service by holding the concrete sub-services
// and wiring them to the shared repositories and config.
type ServiceManager struct {
	authService  AuthService
	userService  UserService
	emailService EmailService
}

// NewServiceManager builds a Service wired to the given repositories and
// application config, constructing the auth service, the user service, the
// email service (and any other sub-services) on top of them.
func NewServiceManager(repos repository.Repository, cfg config.Config) Service {
	emailService := NewEmailService(resend.NewClient(cfg.ResendAPIKey), cfg)
	return &ServiceManager{
		authService:  NewAuthService(repos, cfg, emailService),
		userService:  NewUserService(repos),
		emailService: emailService,
	}
}

// Auth returns the auth service held by the manager.
func (s *ServiceManager) Auth() AuthService {
	return s.authService
}

// User returns the user service held by the manager.
func (s *ServiceManager) User() UserService {
	return s.userService
}

// Email returns the email service held by the manager.
func (s *ServiceManager) Email() EmailService {
	return s.emailService
}
