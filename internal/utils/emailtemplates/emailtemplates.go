// Package emailtemplates renders the static HTML bodies for the
// transactional emails the auth flows send.
package emailtemplates

import (
	"fmt"
	"html"
)

// VerificationEmailHTML renders the body of the email-verification email.
// link already contains the token as a query parameter.
func VerificationEmailHTML(link string) string {
	escaped := html.EscapeString(link)
	return fmt.Sprintf(`<p>Welcome to linkMe! Confirm your email address to finish setting up your account.</p>
<p><a href="%s">Verify your email</a></p>
<p>If you didn't create this account, you can safely ignore this email.</p>`, escaped)
}

// PasswordResetEmailHTML renders the body of the password-reset email.
// link already contains the token as a query parameter.
func PasswordResetEmailHTML(link string) string {
	escaped := html.EscapeString(link)
	return fmt.Sprintf(`<p>We received a request to reset your linkMe password.</p>
<p><a href="%s">Reset your password</a></p>
<p>If you didn't request this, you can safely ignore this email — your password will not change.</p>`, escaped)
}
