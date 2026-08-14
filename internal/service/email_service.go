package service

import (
	"context"
	"fmt"
	"linkMe/config"
	"linkMe/internal/utils/emailtemplates"
	"net/url"

	"github.com/resend/resend-go/v3"
)

// emailService implements EmailService on top of the Resend API. Sender
// identity and the frontend link base come from the shared cfg, the same way
// authService reads cfg.JWTSecret — never as loose constructor strings.
type emailService struct {
	client *resend.Client
	cfg    config.Config
}

// NewEmailService builds an EmailService backed by client, using cfg.EmailFrom
// and cfg.FrontendURL for sender identity and link construction. client is
// taken as a dependency (not constructed here) so tests can inject one built
// with resend.NewCustomClient wrapping a fake http.RoundTripper.
func NewEmailService(client *resend.Client, cfg config.Config) EmailService {
	return &emailService{client: client, cfg: cfg}
}

// SendVerificationEmail sends an email containing a link to
// cfg.FrontendURL/verify-email carrying token, which the frontend exchanges
// for a verified account via POST /api/v1/auth/email/verification/verify.
func (s *emailService) SendVerificationEmail(ctx context.Context, email string, token string) error {
	link := fmt.Sprintf("%s/verify-email?token=%s", s.cfg.FrontendURL, url.QueryEscape(token))

	_, err := s.client.Emails.SendWithContext(ctx, &resend.SendEmailRequest{
		From:    s.cfg.EmailFrom,
		To:      []string{email},
		Subject: "Verify your email",
		Html:    emailtemplates.VerificationEmailHTML(link),
	})
	if err != nil {
		return fmt.Errorf("sending verification email: %w", err)
	}
	return nil
}

// SendPasswordResetEmail sends an email containing a link to
// cfg.FrontendURL/reset-password carrying token, which the frontend
// exchanges for a new password via POST /api/v1/auth/password/reset/confirm.
func (s *emailService) SendPasswordResetEmail(ctx context.Context, email string, token string) error {
	link := fmt.Sprintf("%s/reset-password?token=%s", s.cfg.FrontendURL, url.QueryEscape(token))

	_, err := s.client.Emails.SendWithContext(ctx, &resend.SendEmailRequest{
		From:    s.cfg.EmailFrom,
		To:      []string{email},
		Subject: "Reset your password",
		Html:    emailtemplates.PasswordResetEmailHTML(link),
	})
	if err != nil {
		return fmt.Errorf("sending password reset email: %w", err)
	}
	return nil
}
