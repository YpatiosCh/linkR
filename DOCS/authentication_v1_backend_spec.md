# Authentication & Authorization Backend Specification

> **Implementation status legend:** ✅ = implemented · 🟡 = partially implemented · ⬜ NEXT = next task · unmarked = not started
> **Current position:** Phase F/G of the build order (§61). Phases A–E complete ✅ · **Next: email infrastructure → email verification + password reset; or Google OAuth.**
> **Status:** Register, Login, Refresh, Logout, Logout-all, GET /me, and Password Change are fully implemented through every layer. Security hardening complete: CORS allowlist, security headers, per-route rate limiting (`internal/middleware/`), `RequireAuth` middleware. All routes wired via `internal/router/router.go`. Status markers maintained as of 2026-08-12.

## 1. Purpose

This document defines the V1 authentication, authorization, session, middleware, and account-security architecture for the digital product delivery platform.

The backend will be implemented in Go and exposed through a versioned HTTP API.

The goal is to establish a secure, predictable authentication foundation before implementing products, payments, files, orders, or analytics.

## 2. V1 Authentication Scope

### Required

- ✅ Email/password registration
- ✅ Email/password login
- ⬜ Email verification
- ⬜ Password reset
- ⬜ Google OAuth 2.0 / OpenID Connect login
- ✅ Logout (single session)
- ✅ Logout-all (all sessions for user)
- ✅ Session management — creation, single revoke, revoke-all-for-user; listing/get-by-id pending
- ✅ Refresh-token rotation with reuse detection — full rotation + family revocation on reuse
- ✅ Current-user endpoint (`GET /api/v1/me`)
- ✅ Protected API routes — `RequireAuth` middleware (Bearer header + cookie fallback)
- ⬜ Role/permission middleware
- 🟡 Plan-aware authorization foundation — free plan auto-assigned on register; plan key embedded in JWT; entitlements/limits pending (see plans spec)
- ⬜ Account deletion
- ⬜ Basic security/audit events — audit_events table exists; nothing writes events yet

### Not in V1

- Magic-link authentication
- Apple Sign-In
- Microsoft Sign-In
- Passkeys
- 2FA/MFA
- Teams/organizations
- SSO/SAML

The data model should nevertheless allow additional authentication providers to be added later without changing the user model.

---

# 3. Core Account Model ✅ DONE — single User model, free plan auto-assigned on registration

Every registered person is a single `User`.

There is no separate "buyer" and "creator" account.

A user can:

- purchase products
- create products
- view purchases
- view sales
- access analytics
- upgrade their plan

Every newly registered user automatically receives the `free` plan.

Conceptually:

```text
User
 ├── Authentication identities
 ├── Sessions
 ├── Purchases
 ├── Products
 ├── Subscription / Plan
 └── Profile
```

The user's plan controls feature limits and entitlements. It does not define their identity.

---

# 4. Recommended Technology Boundaries ✅ DONE — Handler → Service → Repository → DB layering implemented; exact package layout intentionally evolved (handlers/service/repository dirs, see DOCS/ARCHITECTURE_AND_RULES.md §6)

The authentication module should not be tightly coupled to HTTP handlers.

Recommended layers:

```text
HTTP Handler
    ↓
Application Service
    ↓
Repository / External Provider
    ↓
Database / OAuth Provider
```

Suggested packages:

```text
/internal/auth
    handler.go
    service.go
    repository.go
    password.go
    oauth.go
    tokens.go
    errors.go
    models.go

/internal/middleware
    authentication.go
    authorization.go
    rate_limit.go
    request_id.go

/internal/users
    ...

/internal/security
    audit.go
    hashing.go
    random.go
```

Exact package structure can evolve, but authentication logic should remain isolated and testable.

---

# 5. API Conventions 🟡 PARTIAL — error envelope `{"error":{code,message}}` and no-leak rules implemented; `/api/v1` base path applied to all routes; success `"data"` wrapper applied to `GET /api/v1/me` only — register/login/refresh return raw payloads (intentional divergence from spec; no `data` wrapper on auth endpoints)

Base path:

```text
/api/v1
```

Authentication endpoints:

```text
/api/v1/auth/*
```

User endpoints:

```text
/api/v1/me/*
```

All JSON APIs should use consistent response and error formats.

Example success:

```json
{
  "data": {
    "user": {}
  }
}
```

Example error:

```json
{
  "error": {
    "code": "INVALID_CREDENTIALS",
    "message": "Invalid email or password."
  }
}
```

Do not expose internal database errors, stack traces, token values, password-hashing details, or provider-specific secrets.

---

# 6. Authentication Endpoints

## 6.1 Register 🟡 PARTIAL — core flow implemented (normalize → existing-account check → Argon2id → user + free plan + password identity + session issuance) and wired at `POST /api/v1/auth/register`; missing: email-verification challenge (step 7) and verification email (step 8); session created unconditionally (step 9 product-policy not applied). ⚠ Returns 409 `EMAIL_ALREADY_EXISTS` on existing email — spec suggests a generic response to prevent account enumeration (decision needed). Response is `models.AuthResponse` (id, email, expires_at). **⚠ Deliberate divergence from this section as originally written (2026-08-16): registration no longer collects or sets `name`.** Profile fields (name, avatar, company name, description, social links) are set exclusively via `PATCH /api/v1/me/profile` as a second phase, after authentication — for any provider, including Google (Google's `name`/`picture` claims are discarded on signup too, never seeded). See §14 and the new Profile Management endpoint below.

```http
POST /api/v1/auth/register
```

Request:

```json
{
  "email": "user@example.com",
  "password": "Strong-Passw0rd!"
}
```

Validation:

- email must be syntactically valid, ≤254 bytes, and a bare address (the RFC 5322 `"Display Name <addr>"` form is rejected)
- normalize email consistently
- password must be 12–72 bytes and contain at least one uppercase letter, one lowercase letter, one digit, and one special character
- reject obviously malformed input

Behavior:

1. Normalize email.
2. Check whether an account already exists.
3. Hash password using Argon2id.
4. Create the user (`name`/`avatar_url`/`company_name`/`description` left unset).
5. Assign the default `free` plan.
6. Create an email/password authentication identity.
7. Create an email-verification challenge.
8. Send verification email when email infrastructure is available.
9. Create an authenticated session only if the product policy allows unverified sessions.
10. Return the user/session representation without exposing sensitive fields.

Recommended response:

```http
201 Created
```

```json
{
  "id": "uuid",
  "email": "user@example.com",
  "expires_at": "2026-08-12T10:15:00Z"
}
```

`expires_at` is the expiry of the issued access token. Clients should use this to schedule a proactive background refresh timer immediately after registration, so the first silent refresh fires before the token lapses rather than waiting for a 401.

### 6.1.1 Profile Management (added 2026-08-16, not in the original spec)

Registration collects only `email`/`password`. Everything else about a user's public
identity is set afterward, as an authenticated action:

```http
PATCH /api/v1/me/profile
```

Partial patch — every field is optional; an omitted field is left unchanged, an
explicitly-sent field (including an empty string) replaces the current value:

```json
{
  "name": "Jane Doe",
  "avatar_url": "https://example.com/avatar.png",
  "company_name": "Jane's Templates",
  "description": "I make Notion templates.",
  "social_links": {
    "platforms": {"discord": "https://discord.gg/xyz"},
    "other": [{"label": "Slack", "url": "https://joinslack.example/xyz"}]
  }
}
```

`social_links.platforms` keys are validated against a closed enum (website, x, instagram,
youtube, tiktok, discord, github, linkedin) — an unrecognized key (e.g. a typo like
`"diskord"`) is rejected with 400 `INVALID_INPUT`, not silently stored, so the frontend can
always map a known key to a brand icon. `social_links.other` is a bounded (max 5) list of
free-text `{label, url}` pairs for platforms not yet in the enum, rendered with a generic
icon. Responds `200` with the same `{"data": <MeResponse>}` shape `GET /api/v1/me` uses.
Rate limit: 20/15min.

Also added: `POST /api/v1/me/password/set` — sets an *initial* password on an account with
no password identity yet (e.g. an OAuth-only account); 409 `PASSWORD_ALREADY_SET` if one
already exists. Distinct from `POST /api/v1/me/password/change` (§15), which requires and
verifies a *current* password. See §14 for the `has_password` field that tells a client
which of the two endpoints to offer.

Do not reveal whether a particular email is already registered in a way that enables account enumeration. If registration encounters an existing account, use a generic response where appropriate.

---

# 7. Email Verification ⬜ TODO — schema only (`email_verification_tokens` table); no endpoints, queries, service, or model

## 7.1 Request verification email

```http
POST /api/v1/auth/email/verification/request
```

Request:

```json
{
  "email": "user@example.com"
}
```

Response should be intentionally generic.

A verification token must be:

- cryptographically random
- single-use
- short-lived
- stored hashed in the database
- associated with the intended user
- invalidated after successful use

Never store raw verification tokens.

## 7.2 Verify email

```http
POST /api/v1/auth/email/verification/verify
```

Request:

```json
{
  "token": "opaque-token"
}
```

Behavior:

1. Hash/lookup the token.
2. Validate expiration.
3. Validate unused status.
4. Mark user's email as verified.
5. Mark token as consumed.
6. Record an audit event.

Return the authenticated user's public account representation.

---

# 8. Email/Password Login ✅ DONE — full flow implemented: normalize → find password identity → `hash.VerifyPassword` → issue JWT access token + opaque refresh token → set HttpOnly cookies → 200 `AuthResponse` (id, email, expires_at — `name` dropped 2026-08-16, see §6.1). Unknown email, wrong password, a deleted account (fixed 2026-08-16, see §32.2), and OAuth-only accounts all return the same `INVALID_CREDENTIALS` (enumeration defense). Registered at `POST /api/v1/auth/login` with 10/15min rate limit. Still missing: audit event (§31)

```http
POST /api/v1/auth/login
```

Request:

```json
{
  "email": "user@example.com",
  "password": "password"
}
```

Behavior:

1. Normalize email.
2. Locate user/auth identity.
3. Verify password using Argon2id.
4. Apply account security checks.
5. Create a new session.
6. Issue access token/session credentials.
7. Rotate/replace any previous authentication state as required by policy.
8. Record successful authentication event.

Successful response:

```json
{
  "id": "uuid",
  "email": "user@example.com",
  "name": "Jane Doe",
  "expires_at": "2026-08-12T10:15:00Z"
}
```

`expires_at` allows the client to start a background refresh timer immediately after login. See §11 for the same field on `POST /api/v1/auth/refresh`.

Failure behavior:

- Return the same generic authentication error for unknown email and incorrect password.
- Do not reveal whether an email exists.
- Apply rate limiting and abuse controls.

---

# 9. Token and Session Architecture ✅ DONE — JWT access tokens (HS256, 15-min, claims: user_id/session_id/plan_key) via `internal/utils/jwttoken`; opaque refresh tokens (32-byte random, SHA-256 hashed, 30-day expiry, token-family lineage) via `internal/utils/token`; full rotation + reuse detection in `AuthService.Refresh`; access tokens are also checked against a Redis-backed revocation store on every request (`internal/redis`), so logout invalidates them immediately instead of only at natural expiry

Use short-lived access credentials and long-lived refresh/session credentials.

Recommended model:

```text
Access Token
    short lifetime
    ↓
API authorization

Refresh Token
    longer lifetime
    ↓
obtain new access token
```

> **Decision (access token format):** JWT — HS256 via `golang-jwt/jwt/v5` (vetted library, per §26/§46.19). ✅ Implemented in `internal/utils/jwttoken`. Short lifetime 15 min. Claims: `user_id`, `session_id`, `plan_key`. Signature + expiry verification in `RequireAuth` middleware is stateless — no *Postgres* lookup on the request hot path (see §22; plans spec §16.1/§18.1). **Revision:** `RequireAuth` also checks session revocation on every request now, via a Redis `EXISTS` (`internal/redis.SessionRevocationStore`, `internal/middleware/auth.go`) — a logged-out access token is rejected immediately rather than remaining valid until its natural 15-min expiry. This closes the "logout doesn't actually invalidate the access token" gap that the original stateless-only design accepted; the fast-path store is Redis (shared across every instance, self-expiring, no per-instance in-memory state), not the primary database, so the original "no DB lookup" intent — cheap, horizontally-scalable, no query fan-out — still holds. Refresh tokens remain opaque + DB-hashed.

Access tokens should have a short lifetime, for example 10–15 minutes.

Refresh tokens should have a longer lifetime, for example 30 days, subject to product/security requirements.

## Refresh token rules ✅ DONE — random ✅, stored hashed ✅, expiration ✅, session-bound ✅, single-use rotation ✅, revocable ✅, replaced on refresh ✅, reuse detection + family revocation ✅

Refresh tokens must:

- be cryptographically random
- be stored hashed server-side
- be single-use when rotated
- have an expiration time
- belong to a session
- be revocable
- be replaced on refresh

On refresh:

```text
old refresh token
       ↓
validate
       ↓
revoke/consume old token
       ↓
create new refresh token
       ↓
create new access token
```

If a previously consumed refresh token is reused, treat it as potential token theft and revoke the associated session/token family.

---

# 10. Cookie vs Authorization Header ✅ DONE — cookies are HttpOnly/Secure/SameSite=Lax/Path=/; `RequireAuth` checks Authorization: Bearer header first, falls back to `access_token` cookie; authenticated routes enforced

For a browser-first web application, prefer secure, HttpOnly cookies for session/refresh credentials.

Recommended cookie properties:

```text
HttpOnly
Secure
SameSite=Lax (or stricter where compatible)
Path=/
```

Avoid storing long-lived authentication credentials in `localStorage`.

If mobile clients or third-party API clients are added later, the API can support an Authorization Bearer token flow separately.

The final implementation should establish one explicit token transport policy and apply it consistently.

---

# 11. Refresh ✅ DONE — `POST /api/v1/auth/refresh`; reads refresh_token cookie → hash → GetSessionByTokenHash → reuse check (RevokedAt set → RevokeSessionFamily + ErrTokenReuseDetected) → expiry check → GetUser → GetSubscription → WithinTx(MarkSessionConsumed + new session in same family) → JWT + new opaque token set as cookies → 200 ExpiresAt

```http
POST /api/v1/auth/refresh
```

No user password is required.

Behavior:

1. Read refresh credential.
2. Validate signature/opaque-token hash as applicable.
3. Validate expiration.
4. Validate session.
5. Validate token family.
6. Rotate refresh token.
7. Issue new access token.
8. Update session metadata.

Return:

```json
{
  "data": {
    "expires_at": "..."
  }
}
```

Do not return the raw refresh token in JSON if it is being delivered as an HttpOnly cookie.

---

# 12. Logout ✅ DONE — `POST /api/v1/auth/logout`; RequireAuth → reads sessionID from JWT claims → RevokeSession (Postgres) + SessionRevoker.RevokeSession (Redis, immediate access-token invalidation — see §9) → clear cookies → 204. Idempotent.

```http
POST /api/v1/auth/logout
```

Behavior:

1. Identify current session.
2. Revoke current refresh token/session.
3. Clear authentication cookies.
4. Record logout event.

Response:

```http
204 No Content
```

Logout must be safe to call repeatedly.

---

# 13. Logout All Sessions ✅ DONE — `POST /api/v1/auth/logout-all`; RequireAuth → reads userID from JWT claims → RevokeAllSessionsForUser → clear cookies → 204.

```http
POST /api/v1/auth/logout-all
```

Requires authentication.

Behavior:

- revoke every active session for the user
- clear current authentication cookies
- invalidate all refresh-token families

This is important after:

- suspected account compromise
- password reset
- security settings changes

---

# 14. Current User ✅ DONE — `GET /api/v1/me`; RequireAuth → reads userID from JWT claims → GetUser + GetActiveSubscription + has-password-identity check → 200 `{"data":{id,email,email_verified,name,avatar_url,company_name,description,social_links,has_password,plan:{id,status}}}`. Never returns hashes or tokens. `name`/`avatar_url`/`company_name`/`description` are all `omitempty` (nil until set via §6.1.1's `PATCH /api/v1/me/profile`) — `has_password` is deliberately **not** `omitempty`, since the client needs to reliably tell "false" apart from "field absent" to decide whether to offer "set a password" (§6.1.1) or "change password" (§15).

```http
GET /api/v1/me
```

Requires authentication.

Response should include only safe public/account data:

```json
{
  "data": {
    "id": "uuid",
    "email": "user@example.com",
    "email_verified": true,
    "name": "Jane Doe",
    "avatar_url": "...",
    "company_name": "Jane's Templates",
    "description": "I make Notion templates.",
    "social_links": {
      "platforms": {"discord": "https://discord.gg/xyz"},
      "other": [{"label": "Slack", "url": "https://joinslack.example/xyz"}]
    },
    "has_password": true,
    "plan": {
      "id": "free",
      "status": "active"
    }
  }
}
```

Never return:

- password hash
- OAuth access tokens
- refresh tokens
- password-reset tokens
- verification tokens
- internal security fields

---

# 15. Password Change ✅ DONE — `POST /api/v1/me/password/change`; RequireAuth + 5/15min rate limit → decode PasswordChangeRequest → GetAuthIdentityByUserIDAndProvider("password") → ErrPasswordNotSet if OAuth-only or no hash → VerifyPassword (ErrInvalidCredentials on mismatch) → validate new password → Argon2id hash → WithinTx(UpdatePasswordHash + RevokeOtherSessionsForUser keeping current session) → 204

```http
POST /api/v1/me/password/change
```

Requires authentication.

Request:

```json
{
  "current_password": "old-password",
  "new_password": "new-password"
}
```

Behavior:

1. Require current password.
2. Validate new password.
3. Hash new password with Argon2id.
4. Replace password hash.
5. Revoke all other sessions.
6. Keep or recreate the current session according to policy.
7. Record audit event.

If the account was originally created through Google only, the user may not have a password. The endpoint should return an explicit state such as:

```text
PASSWORD_NOT_CONFIGURED
```

rather than treating Google authentication as a password.

---

# 16. Password Reset ⬜ TODO — schema only (`password_reset_tokens` table)

## Request reset

```http
POST /api/v1/auth/password/reset/request
```

Request:

```json
{
  "email": "user@example.com"
}
```

Always return a generic response:

```json
{
  "data": {
    "message": "If an account exists, a password reset email has been sent."
  }
}
```

This prevents account enumeration.

## Confirm reset

```http
POST /api/v1/auth/password/reset/confirm
```

Request:

```json
{
  "token": "opaque-token",
  "new_password": "new-password"
}
```

Behavior:

1. Validate token.
2. Validate expiration.
3. Validate single-use state.
4. Hash new password.
5. Update password.
6. Consume token.
7. Revoke all sessions.
8. Record security event.

Password reset tokens must never be stored in plaintext.

---

# 17. Google Authentication ⬜ TODO — note: `auth_identities` model already supports the `google` provider (no schema change needed)

Use Google's OAuth 2.0 / OpenID Connect authorization flow.

Do not trust arbitrary client-supplied Google profile information.

The backend must validate the OAuth/OIDC response and establish the identity from Google's verified claims.

## Start OAuth

```http
GET /api/v1/auth/google
```

Behavior:

1. Generate cryptographically random `state`.
2. Store state server-side or in a secure short-lived mechanism.
3. Redirect user to Google's authorization endpoint.
4. Request the minimum required scopes.

Recommended identity scope:

```text
openid
email
profile
```

## OAuth callback

```http
GET /api/v1/auth/google/callback
```

Behavior:

1. Validate `state`.
2. Exchange authorization code with Google.
3. Validate ID token/claims.
4. Obtain verified Google subject identifier.
5. Find existing authentication identity.
6. If found, sign in the associated user.
7. If not found:
   - identify the verified email
   - create/link the user according to account-linking policy
   - assign `free` plan
   - create Google authentication identity
8. Create application session.
9. Redirect to the application.

Store the provider's stable subject identifier, not the Google access token, as the primary external identity key.

---

# 18. Google Account Linking ⬜ TODO

A user may eventually have:

```text
User
 ├── Email/password
 └── Google
```

The `auth_identities` model should support multiple identities.

Do not automatically merge accounts solely because two records contain the same email unless the provider's email is verified and the account-linking policy explicitly allows it.

Safer linking flow:

```http
POST /api/v1/me/auth-identities/google/link
```

Requires authentication.

The user must complete a Google OAuth flow before the identity is linked.

Unlinking:

```http
DELETE /api/v1/me/auth-identities/google
```

Do not allow a user to remove their only usable authentication method without first establishing another one.

---

# 19. Authentication Identity Model ✅ DONE — separate `auth_identities` table with `UNIQUE(provider, provider_subject)`, nullable `password_hash`, FK to users; `provider="password"` / `provider_subject=normalized email` used in Register

Do not put provider-specific authentication data directly into `users`.

Use a separate table.

Conceptual model:

```text
users
--------------------------------
id
email
email_verified_at
name
avatar_url
created_at
updated_at
deleted_at


auth_identities
--------------------------------
id
user_id
provider
provider_subject
password_hash (nullable)
created_at
updated_at

UNIQUE(provider, provider_subject)
```

For password authentication:

```text
provider = "password"
provider_subject = normalized email or stable identity ID
password_hash = Argon2id hash
```

For Google:

```text
provider = "google"
provider_subject = Google's stable subject ID
password_hash = NULL
```

A separate credential table may be preferable if the implementation wants even stronger separation:

```text
password_credentials
--------------------------------
user_id
password_hash
created_at
updated_at
```

Either approach is acceptable; keep provider-specific secrets out of the general user record.

---

# 20. Session Model ✅ DONE — schema matches §58 exactly (`refresh_token_hash`, `token_family_id`, `last_used_at`, `revoked_at`, `ip_address`, `user_agent`); note: §20 text says `last_seen_at`, the code and §58 use `last_used_at` (consistent)

Conceptual:

```text
sessions
--------------------------------
id
user_id
token_family_id
created_at
expires_at
revoked_at
last_seen_at
ip_address
user_agent
```

Optional:

```text
device_name
location_hint
```

Avoid storing excessive personal/device information.

Sessions allow the application to:

- list active sessions later
- revoke individual sessions
- revoke all sessions
- detect refresh-token reuse
- implement account-security controls

---

# 21. Authorization Architecture ⬜ TODO — concept only; nothing to authorize yet (no protected endpoints)

Authentication answers:

> Who is this?

Authorization answers:

> What is this user allowed to do?

These must remain separate.

Example:

```text
Authentication middleware
    ↓
sets authenticated UserID
    ↓
Authorization middleware/service
    ↓
checks permission/ownership/plan
    ↓
handler
```

Do not use authentication middleware as the only authorization mechanism.

---

# 22. Authentication Middleware ✅ DONE — `RequireAuth(jwtSecret)` in `internal/middleware/auth.go` (`package middleware`): checks Authorization: Bearer header first, then `access_token` cookie; verifies JWT (HS256); injects `jwttoken.Claims` into context; 401 `UNAUTHORIZED` on missing/invalid token. `AuthClaims(r)` extracts claims from context. Handlers import `internal/middleware` to call `middleware.AuthClaims(r)`. Wired per-route in `internal/router/router.go`.

Every protected request passes through:

```text
Request
  ↓
Request ID
  ↓
Rate limiting
  ↓
Authentication
  ↓
Authorization
  ↓
Handler
```

Authentication middleware should:

1. Read access credential.
2. Validate it.
3. Validate expiration.
4. Validate issuer/audience where applicable.
5. Extract user/session identity.
6. Optionally check session revocation state.
7. Attach authenticated identity to request context.

Example conceptual context:

```go
type AuthContext struct {
    UserID    uuid.UUID
    SessionID uuid.UUID
}
```

Never place the full user record into context unless necessary.

---

# 23. Authorization Middleware ⬜ TODO

Authorization should support multiple checks.

## Authentication required

```go
RequireAuthenticated()
```

Example:

```text
POST /api/v1/products
GET  /api/v1/me
POST /api/v1/me/password/change
```

## Resource ownership

A user may edit only their own product.

```text
User A
  ↓
Product owned by User B
  ↓
403 Forbidden
```

Ownership checks must happen server-side.

Never trust:

```json
{
  "owner_id": "..."
}
```

from the client.

The server should derive ownership from the authenticated user and the resource in the database.

---

# 24. Plan-Based Authorization ⬜ TODO — deferred to DOCS/plans_and_entitlements_v1_backend_spec.md; only the plan model + free-plan assignment exist

Plans should be represented as entitlements rather than hard-coded everywhere.

Bad:

```go
if user.Plan == "pro" {
    ...
}
```

Better:

```go
entitlements.CanCreateProduct(user)
```

or:

```go
authorization.RequireEntitlement("products.unlimited")
```

Example:

```text
FREE
 ├── products.max = 3
 ├── analytics.basic = true
 └── analytics.advanced = false

PRO
 ├── products.max = unlimited
 ├── analytics.basic = true
 └── analytics.advanced = true
```

This allows plans to evolve without rewriting authorization logic.

---

# 25. HTTP Status Rules ✅ DONE — `errorStatusMap` codes conform: 201 register, 200 login/refresh/me, 204 logout/logout-all/password-change, 400 malformed body/invalid input, 401 invalid credentials/unauthorized, 409 email conflict, 429 rate limit exceeded, 500 internal error. 403/422 not yet exercised.

Use predictable status codes.

```text
200 OK
Successful request.

201 Created
Resource created.

204 No Content
Successful request with no response body.

400 Bad Request
Malformed request.

401 Unauthorized
Missing/invalid authentication.

403 Forbidden
Authenticated but not allowed.

404 Not Found
Resource does not exist or should not be disclosed.

409 Conflict
Request conflicts with existing resource.

422 Unprocessable Entity
Validation failure, if this convention is adopted consistently.

429 Too Many Requests
Rate limit exceeded.

500 Internal Server Error
Unexpected server error.
```

Do not use `401` for authorization failures. Use `403` when the user is authenticated but lacks permission.

---

# 26. Security Requirements ✅ DONE — Argon2id ✅ (`pkg/hash`, explicit memory/iterations/parallelism), crypto/rand tokens + hashed storage ✅ (`utils/token`), token expiry + single-use rotation + reuse detection ✅ (in `AuthService.Refresh`), HttpOnly+Secure+SameSite=Lax cookies ✅, CORS allowlist ✅, security headers ✅, per-route rate limiting ✅, password composition policy ✅ (added 2026-08-16), request body size cap + unknown-JSON-field rejection ✅ (added 2026-08-16). SQL injection was audited 2026-08-16 and found already structurally prevented (100% sqlc-generated, pgx-parameterized queries) — no changes needed.

## Password hashing

Use **Argon2id**. `pkg/hash`: `m=64MB, t=3, p=2`, 16-byte `crypto/rand` salt, 32-byte key,
PHC string format, constant-time (`subtle.ConstantTimeCompare`) verification — comfortably
above OWASP minimums, unchanged since first implemented.

Never:

- store plaintext passwords
- log passwords
- encrypt passwords reversibly
- use fast hashes such as SHA-256 directly for passwords

Use a vetted Argon2id implementation with deliberately configured memory, iterations, and parallelism.

### Password policy (added 2026-08-16)

12–72 bytes, and must contain at least one uppercase letter, one lowercase letter, one
digit, and one special character (`internal/utils/validate.Password`). Deliberately
composition-based rather than a common-password/breach-list check — a local embedded
blocklist was considered and could still be added later, but the user chose composition
rules for this pass.

### Input hardening (added 2026-08-16)

- Email: length-capped at 254 bytes (RFC 5321); rejects the RFC 5322
  `"Display Name <addr>"` form (`net/mail.ParseAddress` accepts it, so the parsed address
  is compared back against the full input to ensure only a bare address was given).
- All free-text profile fields (name, company name, description, custom social-link
  labels) reject invalid UTF-8, control characters, and Unicode bidirectional-override
  characters (the "Trojan Source" attack class) — `description` is the one field that
  allows a bare `\n` for paragraph breaks.
- Every JSON request body is decoded via `response.DecodeJSON`, which calls
  `DisallowUnknownFields()` — an unrecognized field is now 400 `INVALID_BODY`, not
  silently ignored.
- Every request body is capped at 1MB via `middleware.MaxBody`, applied globally — scoped
  to today's text-only JSON routes; future file uploads are expected to go through a
  presigned R2 URL directly, never through this server, so this cap doesn't need revisiting
  for that.

## Tokens

All security tokens must use cryptographically secure random generation.

Examples:

- password reset
- email verification
- OAuth state
- refresh tokens

Never use:

- timestamps
- predictable UUID sequences
- `math/rand`
- user IDs as tokens

---

# 27. Rate Limiting ✅ DONE — per-IP fixed-window, **Redis-backed** (shared across every server instance), implemented in `internal/redis/ratelimit.go`. `NewRateLimiter(client, name, limit, window)` returns a middleware; counters live in Redis (`ratelimit:{name}:{ip}`, `INCR` + `EXPIRE`-on-first-hit) instead of the in-process map the first implementation used — a single instance's Go map doesn't limit anything once there's more than one instance. Applied to every route in `internal/router/router.go`:

```text
POST /api/v1/auth/register                       5 / hour
POST /api/v1/auth/login                         10 / 15 min
POST /api/v1/auth/refresh                       60 / 15 min   (machine-initiated, multiple tabs/devices)
POST /api/v1/auth/logout                        10 / 15 min
POST /api/v1/auth/logout-all                     5 / 15 min
POST /api/v1/auth/email/verification/request     5 / hour
POST /api/v1/auth/email/verification/verify     10 / 15 min
POST /api/v1/auth/password/reset/request         5 / hour
POST /api/v1/auth/password/reset/confirm        10 / 15 min
GET  /api/v1/me                                 60 / 15 min
POST /api/v1/me/password/change                  5 / 15 min
```

Rate limit runs before `RequireAuth` on authenticated routes — 429 before JWT verification. Responds immediately with 429 `TOO_MANY_REQUESTS`. IP extracted from `X-Real-IP` (reverse proxy) then `RemoteAddr`. Still missing: account/email-based controls (only per-IP today).

---

# 28. CSRF Protection ✅ DONE — SameSite=Lax on all cookies is the chosen strategy. This is sufficient for a pure JSON API: browsers do not send SameSite=Lax cookies on cross-origin non-GET requests initiated by foreign sites, which blocks CSRF for all state-changing endpoints. No CSRF token is needed because this server never returns HTML — there are no forms, no multipart posts, no browser-initiated navigation requests that a CSRF attack could exploit. OAuth callback `state` validation is mandatory when Google OAuth is implemented (⬜ pending).

---

# 29. CORS ✅ DONE — implemented in `internal/middleware/cors.go`. Explicit allowlist from `config.AllowedOrigins` (env `CORS_ALLOWED_ORIGINS`, comma-separated; default `http://localhost:3000`). Echoes the matching origin back (never `*`), sets `Access-Control-Allow-Credentials: true`, adds `Vary: Origin`. Preflight `OPTIONS` requests receive 204 with `Allow-Methods`, `Allow-Headers`, `Max-Age`. Origins not in the allowlist receive no CORS headers — browser blocks them. Applied globally via `middleware.CORS(cfg.AllowedOrigins)` in `SetupRoutes`.

---

# 30. Security Headers ✅ DONE — implemented in `internal/middleware/securityheaders.go`. Applied globally (outermost middleware) via `middleware.SecurityHeaders(cfg.AppEnv)` in `SetupRoutes`:

```text
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: strict-origin-when-cross-origin
Content-Security-Policy: default-src 'none'   (safe — server returns JSON only, never HTML)
Strict-Transport-Security: max-age=31536000; includeSubDomains   (production only, AppEnv == "production")
```

HSTS is gated on `AppEnv` because it mandates HTTPS and would break local HTTP development.

---

# 31. Audit Events ✅ DONE (2026-08-19) — `internal/models/audit_event.go` (taxonomy),
`internal/repository/audit_event_repository.go`, `internal/service/audit_service.go`
(`AuditRecorder`) write events from every auth/user flow; see
`DOCS/ARCHITECTURE_AND_RULES.md` §9 for the full event list and design (fire-and-forget,
IP/User-Agent/request ID via `internal/utils/reqctx`)

Authentication events should be recorded.

Examples:

```text
USER_REGISTERED
LOGIN_SUCCEEDED
LOGIN_FAILED
LOGOUT
LOGOUT_ALL
PASSWORD_CHANGED
PASSWORD_RESET_REQUESTED
PASSWORD_RESET_COMPLETED
EMAIL_VERIFIED
GOOGLE_LOGIN
GOOGLE_LINKED
GOOGLE_UNLINKED
SESSION_REVOKED
REFRESH_TOKEN_REUSE_DETECTED
ACCOUNT_DELETED
```

Audit records should contain enough metadata for security investigation without storing secrets.

Never log:

- passwords
- access tokens
- refresh tokens
- reset tokens
- verification tokens
- OAuth authorization codes

---

# 32. Account Deletion ✅ DONE (2026-08-16) — `DELETE /api/v1/me`; RequireAuth + 5/15min rate limit → `UserService.DeleteAccount(userID, sessionID, currentPassword)` → soft-delete (`users.deleted_at = now()`) + revoke every session → clear cookies → 204

```http
DELETE /api/v1/me
```

Requires authentication. Request body:

```json
{
  "current_password": "..."
}
```

`current_password` is required and verified only if the account has a password identity
(`ErrInvalidCredentials` on mismatch) — an OAuth-only account has nothing to verify it
against, so the field is ignored and deletion proceeds on being authenticated alone.

Behavior, as implemented (deliberate divergence from the spec draft above — see rationale
below):

1. If the account has a password identity, verify `current_password` first (a confirmation
   step for this irreversible action, mirroring `POST /api/v1/me/password/change`'s shape).
2. Soft-delete: set `users.deleted_at = now()`. Every other query in this codebase already
   filters `WHERE deleted_at IS NULL`, so the account instantly disappears from login,
   `GET /api/v1/me`, and every other lookup.
3. Revoke every active session (`RevokeAllSessionsForUser` + the Redis-backed
   `SessionRevoker`, same as `LogoutAll`).
4. Clear the auth cookies and respond `204`.

**Not implemented from the original draft, deliberately**: "re-authenticate or require
*recent* authentication" (step 1 above uses password re-entry as the confirmation gate
instead — session-recency-based re-auth was considered and skipped); "queue
deletion/anonymization of associated data" (no products/purchases/payment data exist yet
for this to apply to — nothing to anonymize); "record the security event" (now built —
see §31 and `AuditAccountDeleted`).

**Retention decision (2026-08-16, product decision, not in the original draft)**:
`users.email` remains a permanent `UNIQUE` constraint — deliberately never relaxed to a
partial index scoped to non-deleted rows — so a deleted account's email is retained
indefinitely (for record-keeping/possible contact). The consequence: a deleted account's
email can never be claimed by a *different* new account. Instead, `Register` with a
previously-deleted account's email is treated as an intentional **reactivation** of that
same account — see the new §6.1.2 below. This is the mechanism that makes "the email
becomes usable again" true from the user's perspective without actually freeing the
`UNIQUE` constraint or purging the row.

Do not immediately destroy records that are legally required for financial/accounting purposes.

Payment/order retention requirements should be handled separately from authentication.

### 32.1 Register-time reactivation (added 2026-08-16, not in the original spec)

`Register`'s existing-email check now uses a variant that also sees soft-deleted rows
(`GetUserByEmailIncludingDeleted`, the only query in this codebase that doesn't filter
`deleted_at`). Three outcomes instead of two:

1. No row at all → create a brand-new account (unchanged).
2. Row exists, not deleted → attach a password identity to it if none exists yet, or
   reject with `EMAIL_ALREADY_EXISTS` if one does (unchanged, §6.1's existing behavior).
3. **Row exists, deleted** → reactivate: clear `deleted_at`, set the password identity to
   the password just submitted (updating the hash if a password identity already existed
   from before deletion, or creating one if the account had been OAuth-only), and sign the
   caller in. Same account ID, same history — this is a restoration, not a new account.

### 32.2 Bug found and fixed while implementing this (2026-08-16)

`Login` authenticates via `auth_identities` — a table account deletion never touches —
*before* ever reading from `users`. That means a correct password still verifies
successfully for a deleted account; only the final `GetUserByID` call (which does filter
`deleted_at`) fails. That call's error used to propagate raw, which meant a deleted
account's login attempt returned a distinguishable `404 USER_NOT_FOUND` instead of the
generic `401 INVALID_CREDENTIALS` every other login failure returns — breaking §8's
enumeration defense for exactly the accounts where it would matter most (confirming an
email was once registered, then deleted). Fixed: that specific `ErrUserNotFound` is now
translated to `ErrInvalidCredentials`, same as every other Login failure path.
`VerifyEmail` and `ResetPassword` had a related but distinct gap — neither reads `users`
at all today, so a verification/reset token issued before an account was deleted would
still succeed after deletion. Both gained an explicit `GetUserByID` guard right after the
token's expiry/used-at checks, translating `ErrUserNotFound` → `ErrTokenInvalid`.
`GoogleCallback` has the same latent `GetUserByID` gap for a deleted account's existing
`google` identity, left unfixed — it never exposes a distinguishable error to the client
(always a generic `?error=oauth_failed` redirect either way), so there's no enumeration
leak, only a minor "logs at Error level for an expected case" nit.

---

# 34. Email Change ⬜ TODO (future)

Although not essential to the first login flow, the account architecture should reserve:

```http
POST /api/v1/me/email/change
```

Recommended process:

```text
Current user
   ↓
Recent authentication
   ↓
Enter new email
   ↓
Send verification challenge
   ↓
Verify new email
   ↓
Change email
   ↓
Revoke/rotate sensitive sessions if required
```

Do not change the email merely because the user submits it.

---

# 35. API Endpoint Summary

## Public

```text
POST /api/v1/auth/register           ✅ wired; JWT + refresh cookies on success
POST /api/v1/auth/login              ✅ wired; JWT + refresh cookies on success
POST /api/v1/auth/refresh            ✅ wired; rotates refresh token, issues new JWT
POST /api/v1/auth/password/reset/request    ✅ wired; 5/hour rate limit; generic 200 always
POST /api/v1/auth/password/reset/confirm    ✅ wired; 10/15min rate limit; 204
POST /api/v1/auth/email/verification/request ✅ wired; 5/hour rate limit; generic 200 always
POST /api/v1/auth/email/verification/verify  ✅ wired; 10/15min rate limit; 200 {"data":{...}}
GET  /api/v1/auth/google             ✅ wired; 10/15min rate limit; redirects to Google
GET  /api/v1/auth/google/callback    ✅ wired; 10/15min rate limit; redirects to FRONTEND_URL
```

## Authenticated

```text
POST /api/v1/auth/logout             ✅ wired; 10/15min rate limit + RequireAuth; 204
POST /api/v1/auth/logout-all         ✅ wired; 5/15min rate limit + RequireAuth; 204
GET  /api/v1/me                      ✅ wired; 60/15min rate limit + RequireAuth; 200 {"data":{...}}
PATCH /api/v1/me/profile             ✅ wired; 20/15min rate limit + RequireAuth; 200 {"data":{...}} (added 2026-08-16, see §6.1.1)
POST /api/v1/me/password/change      ✅ wired; 5/15min rate limit + RequireAuth; 204
POST /api/v1/me/password/set         ✅ wired; 5/15min rate limit + RequireAuth; 204 (added 2026-08-16, see §6.1.1)
DELETE /api/v1/me                    ✅ wired; 5/15min rate limit + RequireAuth; 204 (added 2026-08-16, see §32)
POST /api/v1/me/auth-identities/google/link   ⬜ deprioritized 2026-08-16
DELETE /api/v1/me/auth-identities/google      ⬜ deprioritized 2026-08-16
```

Potential future endpoints:

```text
GET  /api/v1/me/sessions
DELETE /api/v1/me/sessions/:session_id

POST /api/v1/me/email/change

POST /api/v1/auth/magic-link/request
POST /api/v1/auth/magic-link/verify

POST /api/v1/me/mfa/enable
POST /api/v1/me/mfa/verify
```

---

# 36. Recommended Database Tables ✅ DONE — all 8 core tables exist (`users`, `auth_identities`, `sessions`, `email_verification_tokens`, `password_reset_tokens`, `audit_events`, `plans`, `user_subscriptions`); `oauth_states` correctly omitted per §49

Authentication should start with at least:

```text
users
auth_identities
sessions
refresh_tokens
email_verification_tokens
password_reset_tokens
audit_events
plans
user_subscriptions
```

Potentially:

```text
oauth_states
```

if OAuth state is persisted server-side.

Sensitive token tables should store hashes rather than raw tokens.

---

# 37. Registration Flow 🟡 PARTIAL — matches implemented flow through "Create session"; "Create verification token" step not implemented; session created unconditionally

```text
                    ┌───────────────┐
                    │ POST register │
                    └───────┬───────┘
                            ↓
                    Validate request
                            ↓
                    Normalize email
                            ↓
                    Check account
                            ↓
                    Hash password
                            ↓
                       Create User
                            ↓
                    Assign FREE plan
                            ↓
                  Create auth identity
                            ↓
                Create verification token
                            ↓
                    Create session
                            ↓
                    Return user/session
```

---

# 38. Login Flow 🟡 PARTIAL — implemented through "Create session → Issue refresh credential → Set secure cookies → Return user". Not implemented: "Issue access credential" (§9 JWT) and "Security/rate-limit checks" (§27)

```text
Client
  ↓
POST /auth/login
  ↓
Validate input
  ↓
Find identity
  ↓
Verify Argon2id password
  ↓
Security/rate-limit checks
  ↓
Create session
  ↓
Issue access credential
  ↓
Issue refresh credential
  ↓
Set secure cookies
  ↓
Return user
```

---

# 39. Google Login Flow ⬜ TODO

```text
Browser
  ↓
GET /auth/google
  ↓
Generate state
  ↓
Redirect to Google
  ↓
User authenticates
  ↓
Google callback
  ↓
Validate state
  ↓
Exchange code
  ↓
Validate OIDC identity
  ↓
Find auth_identity
      │
      ├── exists → login
      │
      └── missing → create/link user
                         ↓
                    assign FREE plan
                         ↓
                    create identity
  ↓
Create application session
  ↓
Redirect to application
```

---

# 40. Request Processing Pipeline 🟡 PARTIAL — CORS ✅, security headers ✅, rate limiting ✅, authentication middleware ✅, request ID ✅, structured logging ✅, request body size cap ✅ (all in `internal/middleware/`, wired in `internal/router/router.go` as `RequestLogger → SecurityHeaders → CORS → MaxBody → mux`). Still missing: panic recovery (audited 2026-08-16, no live/reachable panic found — this is defense-in-depth, not a bugfix), authorization middleware (blocked on protected resources existing — no products yet)

Every API request should conceptually pass through:

```text
Incoming Request
       ↓
Request ID
       ↓
Panic Recovery
       ↓
Logging / Observability
       ↓
CORS
       ↓
Security Headers
       ↓
Rate Limiting
       ↓
Authentication Middleware
       ↓
Authorization Middleware
       ↓
Handler
       ↓
Application Service
       ↓
Repository
       ↓
Response
```

Not every endpoint requires authentication.

For example:

```text
/auth/register       → public
/auth/login          → public
/auth/google         → public
/auth/refresh        → public credential required
/me                  → authenticated
/products            → authentication/authorization depending on operation
```

---

# 41. Authentication Context ✅ DONE — `middleware.RequireAuth` injects `jwttoken.Claims{UserID, SessionID, PlanKey}` into request context; `middleware.AuthClaims(r)` retrieves them. Handlers never parse JWTs themselves.

Handlers should not parse JWTs/cookies themselves.

Bad:

```go
func Handler(w http.ResponseWriter, r *http.Request) {
    // parse cookie
    // decode token
    // validate token
    // find user
}
```

Instead:

```go
func Handler(w http.ResponseWriter, r *http.Request) {
    userID, ok := auth.UserIDFromContext(r.Context())
    if !ok {
        // should normally be impossible on protected route
    }

    ...
}
```

Authentication is a middleware concern.

Business authorization remains an application/service concern.

---

# 42. Ownership Authorization ⬜ TODO (future — products not built)

For resources such as products:

```text
GET /api/v1/products/:id
PATCH /api/v1/products/:id
DELETE /api/v1/products/:id
```

The server must check:

```text
product.owner_id == authenticated_user_id
```

Do not rely solely on a generic "creator" role.

The same user model supports both buyers and sellers.

---

# 43. Plan Authorization ⬜ TODO (future — products not built)

Product creation could eventually follow:

```text
Authenticated
      ↓
Check product entitlement
      ↓
Count user's active products
      ↓
Compare with plan limit
      ↓
Allow / deny
```

Example:

```text
FREE → max 3 active products
PRO  → unlimited
```

The plan check belongs in the authorization/application layer, not inside the HTTP handler.

---

# 44. Error Handling 🟡 PARTIAL — stable machine-readable codes + envelope implemented (`Code*` constants in `internal/utils/response/codes.go`, `errorStatusMap`); codes in use: INVALID_BODY, INVALID_CREDENTIALS, EMAIL_ALREADY_EXISTS, USER_NOT_FOUND, PASSWORD_NOT_SET, INTERNAL_ERROR (spec list is "examples"; EMAIL_ALREADY_EXISTS/PASSWORD_NOT_SET names differ slightly from EMAIL_ALREADY_REGISTERED/PASSWORD_NOT_CONFIGURED)

Define stable machine-readable error codes.

Examples:

```text
INVALID_REQUEST
INVALID_CREDENTIALS
ACCOUNT_NOT_VERIFIED
ACCOUNT_DISABLED
EMAIL_ALREADY_REGISTERED
TOKEN_INVALID
TOKEN_EXPIRED
SESSION_REVOKED
PASSWORD_TOO_WEAK
PASSWORD_NOT_CONFIGURED
OAUTH_STATE_INVALID
OAUTH_AUTHENTICATION_FAILED
FORBIDDEN
ENTITLEMENT_REQUIRED
RATE_LIMITED
```

Frontend applications should use error codes rather than parsing human-readable messages.

---

# 45. Testing Requirements 🟡 PARTIAL — first tests landed with login: unit tests for password hashing/verification, token generation/hashing, and email normalization/validation, plus a login unit test (success + all invalid-credential paths) via a fake `repository.Repository`. All tests live under the root `test/` directory, mirroring the package under test (see DOCS/ARCHITECTURE_AND_RULES.md §3.9/Q6) — never colocated beside source. Integration/security suites (real-DB registration/login, refresh rotation, etc.) still ⬜ (see plans spec §29–30 for the testing strategy)

Authentication should have extensive automated tests before dependent features are built.

## Unit tests

Test:

- password hashing
- password verification
- token generation
- token hashing
- token expiration
- token reuse
- authorization rules
- entitlement rules
- email normalization
- OAuth state validation

## Integration tests

Test:

- registration
- login
- logout
- refresh
- refresh-token rotation
- password reset
- email verification
- Google identity creation
- Google identity linking
- account deletion
- protected endpoints

## Security tests

Test:

- invalid credentials
- brute-force/rate-limit behavior
- expired tokens
- revoked tokens
- reused refresh tokens
- CSRF
- CORS
- account enumeration
- unauthorized resource access
- cross-user product access
- malformed JWT/token input
- OAuth state attacks
- callback replay
- session fixation

---

# 46. Non-Negotiable Security Rules

1. ✅ Never store plaintext passwords. (Argon2id in `pkg/hash`)
2. ✅ Never store raw long-lived security tokens. (refresh tokens stored as SHA-256 hashes)
3. ✅ Never log credentials or tokens.
4. ✅ Never trust client-supplied user IDs for authorization. (nothing trusts client IDs; ownership checks pending with products)
5. ⬜ Never expose whether an email exists during password-reset requests. (feature pending)
6. ⬜ Always validate OAuth `state`. (feature pending)
7. ⬜ Always validate OAuth/OIDC identity claims. (feature pending)
8. ✅ Rotate refresh tokens. (single-use rotation in `AuthService.Refresh`)
9. ✅ Detect refresh-token reuse. (RevokedAt check → RevokeSessionFamily + ErrTokenReuseDetected)
10. ✅ Revoke sessions after password change. (RevokeOtherSessionsForUser in ChangePassword; password reset pending)
11. ✅ Use secure cookies for browser sessions where applicable. (HttpOnly, Secure, SameSite=Lax, Path=/)
12. ✅ Protect cookie-authenticated state-changing requests against CSRF. (SameSite=Lax is the complete strategy for this JSON-only API — see §28)
13. ⬜ Use HTTPS in production. (no production deployment yet)
14. ✅ Keep authentication and authorization separate. (distinct layers/concerns in the architecture)
15. ✅ Keep plan entitlements separate from authentication. (plans are a separate domain/spec)
16. ✅ Do not allow a user to access another user's resources. (no multi-user resource endpoints yet; ownership checks will enforce)
17. ✅ Do not return secrets in API responses. (DTOs never include hashes/tokens)
18. ✅ Do not implement custom cryptography.
19. ✅ Use vetted libraries for Argon2id, OAuth/OIDC, JWT/token handling, and cryptographic randomness. (x/crypto argon2id, stdlib crypto/rand + sha256)
20. ✅ Authentication behavior is covered by automated tests. (test/service, test/handlers, test/middleware, test/jwttoken, test/token, test/hash, test/validate)

---

# 47. Implementation Order

Build authentication in this order:

### Phase A — Foundation

1. ✅ Go HTTP server (`net/http`, `:8080`, pattern routing)
2. ✅ Configuration/environment handling (`pkg/dotenv`)
3. ✅ Database connection (pgxpool + ping)
4. 🟡 Migration system — migration files exist (goose syntax) for all 8 tables, but **no migration runner is wired** (goose is not in `go.mod`; migrations are not applied by any tool)
5. ✅ `users` table
6. ✅ `auth_identities` table
7. ✅ `plans` / default free plan (plans table + seeded `free` + auto-assignment on register)
8. ✅ `sessions` and refresh-token tables (refresh token stored as hash on `sessions` per §49)
9. ✅ Error/response framework (`internal/msgs` + `internal/utils/response`)
10. ⬜ Request ID and structured logging

### Phase B — Email Authentication

11. ✅ Argon2id password hashing (`pkg/hash`)
12. ✅ Registration (service + handler implemented; route not yet registered)
13. ⬜ NEXT Login
14. ⬜ Access-token/session validation
15. ⬜ Refresh-token rotation
16. ⬜ Logout
17. ⬜ Logout-all
18. ⬜ Current-user endpoint

### Phase C — Account Security

19. ⬜ Email verification
20. ⬜ Password reset
21. ✅ Password change
22. ⬜ Account deletion
24. ✅ Audit events
25. ✅ Rate limiting

### Phase D — Google

26. ⬜ Google OAuth configuration
27. ⬜ OAuth state handling
28. ⬜ Google callback
29. ⬜ OIDC claim validation
30. ⬜ Identity creation
31. ⬜ Existing-account linking policy
32. ⬜ Google link/unlink

### Phase E — Middleware & Authorization

33. ✅ Authentication middleware (`RequireAuth` + `AuthClaims` in `internal/middleware/auth.go`)
34. ⬜ Resource ownership authorization
35. ⬜ Plan entitlement service
36. ⬜ Authorization middleware/helpers
37. ✅ Protected route test suite (`test/middleware/`, `test/handlers/`)

### Phase F — Hardening

38. ✅ CSRF strategy (SameSite=Lax — complete for JSON-only API, see §28)
39. ✅ CORS configuration (`internal/middleware/cors.go`, allowlist from env)
40. ✅ Security headers (`internal/middleware/securityheaders.go`)
41. ✅ Rate limiting (`internal/middleware/ratelimit/`, per-route in `internal/router/router.go`)
42. ✅ Token-reuse detection (RevokedAt check → RevokeSessionFamily in `AuthService.Refresh`)
43. ⬜ Security integration tests (CORS headers, rate limit 429, header values)
44. ⬜ Production configuration review

Only after these phases are stable should the implementation move into products, file storage, checkout, orders, and delivery.

---

# 48. Definition of Done

Authentication V1 is considered complete when:

- ✅ A new user can register with email/password.
- ✅ A new user automatically receives the Free plan.
- ⬜ A new user can sign in with Google.
- ⬜ A Google user is represented by the same `users` model as an email user.
- ✅ A user can log in/out safely.
- ✅ Sessions survive normal browser use without exposing long-lived credentials to JavaScript. (HttpOnly+Secure+SameSite=Lax cookies on all auth endpoints)
- ✅ Access credentials expire. (JWT 15-min lifetime enforced by golang-jwt/v5)
- ✅ Refresh credentials rotate. (full single-use rotation in `AuthService.Refresh`)
- ✅ Reused refresh credentials trigger session-family revocation. (RevokedAt check → RevokeSessionFamily)
- ⬜ Password reset works securely.
- ⬜ Email verification works securely.
- ✅ Password changes invalidate appropriate sessions. (RevokeOtherSessionsForUser in ChangePassword)
- ✅ Protected endpoints reject unauthenticated requests. (RequireAuth on all authenticated routes)
- ⬜ Protected resources enforce ownership.
- ⬜ Plan limits can be enforced independently of authentication.
- ⬜ Account deletion revokes authentication.
- ✅ Security-sensitive events are auditable. (see §31)
- ✅ Authentication endpoints are rate-limited. (per-route fixed-window in `internal/middleware/ratelimit/`)
- ⬜ OAuth state and identity claims are validated.
- 🟡 Automated unit, integration, and security tests pass. (unit + handler tests ✅; integration/security tests ⬜)
- ✅ No authentication secret appears in logs, API responses, or database plaintext storage.

This authentication foundation should be treated as infrastructure: once it passes the Definition of Done, product features can build on it without implementing their own authentication logic.


---

# 49. Final V1 Architecture Decision ✅ DONE — exactly the 8 V1 tables; no `oauth_states`/`plan_entitlements`/`password_credentials`/`refresh_tokens` tables; refresh token stored as hash on `sessions`

The authentication module will intentionally remain small and strongly structured.

We will **not** create a table, service, repository, or abstraction merely because a future feature might need it.

The V1 goal is:

> Build the smallest authentication foundation that is secure, testable, extensible, and appropriate for the rest of the platform.

## Final PostgreSQL tables

V1 will use exactly these core tables:

```text
users
auth_identities
sessions
email_verification_tokens
password_reset_tokens
plans
user_subscriptions
audit_events
```

No additional authentication tables should be introduced unless a concrete requirement requires them.

### Explicitly excluded from V1

The following are intentionally not separate database tables:

```text
oauth_states
plan_entitlements
password_credentials
refresh_tokens
```

Rationale:

- OAuth state can be stored using a secure short-lived mechanism rather than requiring a database table.
- Plan entitlements are simple enough for V1 to be represented by application-level plan configuration.
- A separate password credential table is unnecessary while the only password authentication method is email/password.
- Refresh tokens belong to the session model in V1 and should be stored as a hash associated with the session.

If these requirements become sufficiently complex later, they can be extracted without changing the external API contracts.

---

# 50. Final Backend Architecture ✅ DONE — Handler → Service Interface → Repository Interface → PostgreSQL matches exactly; ⚠ external/security deps (hasher, token generator) are concrete packages (`pkg/hash`, `utils/token`) called directly, not injected interfaces (see §51/§54 note)

The backend will follow:

```text
HTTP Request
     ↓
Handler
     ↓
Service Interface
     ↓
Repository Interface
     ↓
PostgreSQL
```

External/security dependencies are injected into services:

```text
                         ┌── PasswordHasher
                         │
                         ├── TokenGenerator
                         │
                         ├── OAuthProvider
                         │
AuthService ─────────────┼── EmailSender
                         │
                         ├── UserRepository
                         ├── AuthIdentityRepository
                         ├── SessionRepository
                         ├── TokenRepository
                         ├── PlanRepository
                         └── AuditRepository
```

The exact repository interfaces should be organized around business capabilities rather than blindly creating one interface per database table.

---

# 51. Dependency Injection Rules 🟡 PARTIAL — constructors take interfaces ✅ (handlers ← service; services ← repository); composition root ✅ wired in `main.go` (repos → services → handlers → router.SetupRoutes); ⚠ services receive the aggregate `repository.Repository` (not per-entity repos, unlike the §51 example); ⚠ security deps (hasher/token) called directly, not injected — spec rule 10 (replace with mocks in tests) is mitigated by test fakes at the repository interface level

Dependency injection is a core architectural requirement.

## Rules

1. Handlers receive service interfaces through constructors.
2. Services receive repository and infrastructure interfaces through constructors.
3. Repositories receive database dependencies through constructors.
4. External providers are injected.
5. No business service creates its own repository.
6. No handler creates a service.
7. No repository creates another repository.
8. No global mutable service/database singleton should be hidden inside application logic.
9. The composition root is responsible for constructing the dependency graph.
10. Tests must be able to replace infrastructure dependencies with mocks/fakes.

Conceptually:

```go
func NewAuthService(
    users UserRepository,
    identities AuthIdentityRepository,
    sessions SessionRepository,
    passwordHasher PasswordHasher,
    tokenGenerator TokenGenerator,
    google OAuthProvider,
    emailSender EmailSender,
    audit AuditRepository,
) AuthService {
    // ...
}
```

The constructor should receive interfaces, not concrete PostgreSQL implementations.

---

# 52. Repository Layer 🟡 PARTIAL — responsibilities conform (persistence + row→domain mapping + error translation only) ✅; interface shapes evolved (`CreateUser`/`GetUserByEmail`/`GetAuthIdentityByProviderAndSubject`/`CreateSession`); missing methods pending features: `Update`/`SoftDelete` (users), `GetByUserID`/`Delete` (identities), `GetByID`/`Revoke`/`RevokeAllForUser`/`RotateRefreshToken` (sessions)

Repositories are responsible for persistence only.

They may:

- execute SQL
- map database rows to domain models
- perform database transactions
- enforce persistence-level constraints

They must not:

- hash passwords
- decide whether a user is authorized
- decide whether a plan allows an operation
- send emails
- perform OAuth
- generate security tokens
- contain HTTP concerns
- contain business workflows

## UserRepository

```go
type UserRepository interface {
    Create(ctx context.Context, user *User) error
    GetByID(ctx context.Context, id uuid.UUID) (*User, error)
    GetByEmail(ctx context.Context, email string) (*User, error)
    Update(ctx context.Context, user *User) error
    SoftDelete(ctx context.Context, id uuid.UUID) error
}
```

## AuthIdentityRepository

```go
type AuthIdentityRepository interface {
    Create(ctx context.Context, identity *AuthIdentity) error

    GetByProviderSubject(
        ctx context.Context,
        provider string,
        subject string,
    ) (*AuthIdentity, error)

    GetByUserID(
        ctx context.Context,
        userID uuid.UUID,
    ) ([]AuthIdentity, error)

    Delete(
        ctx context.Context,
        userID uuid.UUID,
        provider string,
    ) error
}
```

## SessionRepository

```go
type SessionRepository interface {
    Create(ctx context.Context, session *Session) error

    GetByID(
        ctx context.Context,
        id uuid.UUID,
    ) (*Session, error)

    Revoke(
        ctx context.Context,
        id uuid.UUID,
    ) error

    RevokeAllForUser(
        ctx context.Context,
        userID uuid.UUID,
    ) error

    RotateRefreshToken(
        ctx context.Context,
        sessionID uuid.UUID,
        oldHash []byte,
        newHash []byte,
    ) error
}
```

Additional repositories should be introduced only when their domain responsibility becomes meaningful.

---

# 53. Service Layer 🟡 PARTIAL — single `AuthService` (no per-table services ✅); implemented: `Register` ✅, `Login` ✅, `Refresh` ✅, `Logout` ✅, `LogoutAll` ✅, `GetMe` ✅, `ChangePassword` ✅; pending: `VerifyEmail`, `RequestPasswordReset`, `ResetPassword`, `GoogleLogin`. Note: actual signatures differ slightly from the conceptual spec below (e.g. `ChangePassword` takes `userID, sessionID, currentPassword, newPassword` individually; `GetMe` returns `(models.User, models.Subscription, error)`).

The service layer owns business workflows.

For V1, authentication should have one primary service:

```go
type AuthService interface {
    Register(
        ctx context.Context,
        input RegisterInput,
    ) (*AuthResult, error)

    Login(
        ctx context.Context,
        input LoginInput,
    ) (*AuthResult, error)

    Refresh(
        ctx context.Context,
        input RefreshInput,
    ) (*AuthResult, error)

    Logout(
        ctx context.Context,
        sessionID uuid.UUID,
    ) error

    LogoutAll(
        ctx context.Context,
        userID uuid.UUID,
    ) error

    VerifyEmail(
        ctx context.Context,
        token string,
    ) error

    RequestPasswordReset(
        ctx context.Context,
        email string,
    ) error

    ResetPassword(
        ctx context.Context,
        token string,
        password string,
    ) error

    ChangePassword(
        ctx context.Context,
        userID uuid.UUID,
        input ChangePasswordInput,
    ) error

    GoogleLogin(
        ctx context.Context,
        input GoogleLoginInput,
    ) (*AuthResult, error)
}
```

Do **not** create a separate service for every authentication table.

For example, a `SessionService` is not required simply because a `sessions` table exists.

The authentication service can coordinate session persistence through `SessionRepository`.

---

# 54. Security Interfaces 🟡 PARTIAL — hasher + token generator exist as concrete packages (`pkg/hash`, `utils/token`) but are NOT defined as interfaces or injected (blocks mock-based testing per §51 rule 10); `OAuthProvider` and `EmailSender` do not exist yet

Security-sensitive and externally replaceable components should be interfaces.

## PasswordHasher

```go
type PasswordHasher interface {
    Hash(password string) (string, error)
    Compare(hash string, password string) error
}
```

Production implementation:

```text
Argon2idHasher
```

Tests can use a fake implementation.

---

## TokenGenerator

```go
type TokenGenerator interface {
    Generate() (string, error)
    Hash(token string) []byte
}
```

Used for:

- email verification
- password reset
- refresh tokens
- other opaque security tokens

All generated values must use cryptographically secure randomness.

---

## OAuthProvider

```go
type OAuthProvider interface {
    GetAuthorizationURL(state string) string

    Exchange(
        ctx context.Context,
        code string,
    ) (*OAuthIdentity, error)
}
```

Production:

```text
GoogleOAuthProvider
```

Tests:

```text
MockOAuthProvider
```

The service should not contain Google-specific HTTP implementation details.

---

## EmailSender

```go
type EmailSender interface {
    SendVerificationEmail(
        ctx context.Context,
        email string,
        token string,
    ) error

    SendPasswordResetEmail(
        ctx context.Context,
        email string,
        token string,
    ) error
}
```

During development this can be:

```text
NoopEmailSender
```

or a local development implementation.

Later it can be replaced with a real provider without changing the authentication service.

This also means the authentication implementation does not have to wait for the final email provider integration.

---

# 55. Handler Layer ✅ DONE — responsibilities match exactly: thin adapters (decode JSON → call service → translate errors → set cookies → write response); no SQL, hashing, token generation, or business logic in handlers

Handlers are HTTP adapters.

Handlers are responsible for:

- reading HTTP requests
- decoding JSON
- validating basic request shape
- extracting route/query parameters
- calling services
- translating service errors to HTTP responses
- setting cookies/headers
- writing HTTP responses

Handlers must not:

- execute SQL
- hash passwords
- validate OAuth identity claims
- generate authentication tokens
- enforce product ownership
- calculate plan limits
- implement authentication workflows

Example:

```go
func (h *AuthHandler) Register(
    w http.ResponseWriter,
    r *http.Request,
) {
    var input RegisterInput

    if err := decodeJSON(r, &input); err != nil {
        writeError(w, err)
        return
    }

    result, err := h.authService.Register(
        r.Context(),
        input,
    )
    if err != nil {
        writeError(w, err)
        return
    }

    writeJSON(
        w,
        http.StatusCreated,
        result,
    )
}
```

The handler should remain intentionally thin.

---

# 56. Middleware Architecture 🟡 PARTIAL — Authentication ✅ (`internal/middleware/auth.go`), Rate limiting ✅ (`internal/middleware/ratelimit/`), CORS ✅ (`internal/middleware/cors.go`), Security headers ✅ (`internal/middleware/securityheaders.go`); all wired in `internal/router/router.go`. Authorization ⬜ (pending protected resources)

V1 should have three primary middleware concerns.

## Authentication

```text
RequireAuth()
```

Responsibilities:

1. Read authentication credential.
2. Validate credential.
3. Validate expiration.
4. Validate session where applicable.
5. Extract user/session identity.
6. Place minimal identity information into request context.

Example:

```go
type AuthContext struct {
    UserID    uuid.UUID
    SessionID uuid.UUID
}
```

Handlers should retrieve the authenticated identity from context rather than parsing authentication credentials themselves.

---

## Authorization

Authentication and authorization remain separate.

Authentication answers:

```text
Who are you?
```

Authorization answers:

```text
Are you allowed to perform this operation?
```

Examples:

```text
RequireAuthenticated()
RequireOwnership()
RequireEntitlement()
```

Resource ownership and plan-limit checks generally belong in the service/application layer because they require domain/database state.

---

## Rate limiting

Rate limiting should protect:

```text
POST /auth/register
POST /auth/login
POST /auth/password/reset/request
POST /auth/password/reset/confirm
POST /auth/email/verification/request
POST /auth/email/verification/verify
POST /auth/refresh
```

It should be introduced as part of the authentication foundation, not after the rest of the platform is complete.

---

# 57. Plan Authorization ⬜ TODO — deferred to DOCS/plans_and_entitlements_v1_backend_spec.md; only `models.Plan` + `CreatePlan` exist

Do not hard-code plan checks throughout handlers.

Avoid:

```go
if user.Plan == "pro" {
    ...
}
```

Prefer an application-level entitlement/plan component:

```go
entitlements.CanCreateProduct(user)
```

or:

```go
authorization.RequireEntitlement(
    "products.unlimited",
)
```

However, V1 does **not** require a `plan_entitlements` database table.

Plan configuration can initially be represented in application code/configuration.

Example:

```text
FREE
    max_products = 3
    basic_analytics = true
    advanced_analytics = false

PRO
    max_products = unlimited
    basic_analytics = true
    advanced_analytics = true
```

If plan rules become dynamic or significantly more complex, introduce a dedicated entitlement model later.

---

# 58. Final Database Responsibility ✅ DONE — all 8 tables match the specified columns: `users` (incl. `deleted_at`), `auth_identities`, `sessions` (incl. `refresh_token_hash` + `token_family_id`), `email_verification_tokens`, `password_reset_tokens` (hashed single-use tokens), `plans` (seeded `free`), `user_subscriptions`, `audit_events` (metadata JSONB)

## users

Represents the account itself.

```text
id
email
name
avatar_url
email_verified_at
created_at
updated_at
deleted_at
```

## auth_identities

Represents external/login identities.

```text
id
user_id
provider
provider_subject
created_at
updated_at
```

Examples:

```text
google / Google subject ID
password / application identity
```

The implementation may represent email/password credentials directly on `users` if that proves simpler, but the preferred extensible V1 design keeps authentication identity information separate.

## sessions

Represents authenticated application sessions.

```text
id
user_id
refresh_token_hash
token_family_id
created_at
expires_at
last_used_at
revoked_at
ip_address
user_agent
```

## email_verification_tokens

Stores hashed, short-lived, single-use email verification credentials.

```text
id
user_id
token_hash
expires_at
used_at
created_at
```

## password_reset_tokens

Stores hashed, short-lived, single-use password reset credentials.

```text
id
user_id
token_hash
expires_at
used_at
created_at
```

## plans

Represents available subscription tiers.

```text
id
name
price_monthly
created_at
updated_at
```

## user_subscriptions

Represents the user's current/historical plan relationship.

```text
id
user_id
plan_id
status
started_at
ends_at
created_at
updated_at
```

## audit_events

Security and account events.

```text
id
user_id
event_type
ip_address
user_agent
metadata
created_at
```

Never store passwords, refresh tokens, reset tokens, verification tokens, OAuth authorization codes, or other secrets in audit metadata.

---

# 59. Composition Root / DI ✅ DONE — `cmd/server/main.go` constructs the full manager graph (pool → `repository.NewRepoManager` → `service.NewServiceManager` → `handlers.NewHandlerManager`) and delegates route/middleware wiring to `router.SetupRoutes(h, cfg)`. `main.go` contains no business logic.

All concrete implementations should be created in one composition root.

Conceptually:

```go
func BuildApplication(cfg Config) *Application {
    db := postgres.New(cfg.Database)

    userRepo := postgres.NewUserRepository(db)
    identityRepo := postgres.NewAuthIdentityRepository(db)
    sessionRepo := postgres.NewSessionRepository(db)

    passwordHasher := security.NewArgon2idHasher()
    tokenGenerator := security.NewTokenGenerator()

    googleProvider := oauth.NewGoogleProvider(cfg.Google)
    emailSender := email.NewSender(cfg.Email)

    auditRepo := postgres.NewAuditRepository(db)

    authService := auth.NewService(
        userRepo,
        identityRepo,
        sessionRepo,
        passwordHasher,
        tokenGenerator,
        googleProvider,
        emailSender,
        auditRepo,
    )

    authHandler := auth.NewHandler(authService)

    return NewApplication(authHandler)
}
```

The exact constructors may differ, but the architectural principle is fixed:

```text
Concrete infrastructure
        ↓
Injected into interfaces
        ↓
Services
        ↓
Handlers
        ↓
HTTP router
```

---

# 60. Anti-Patterns to Avoid ✅ DONE — no invented abstractions (no UserValidator/TokenFactory/etc.), no per-table services, interfaces at meaningful boundaries only

Do not introduce interfaces solely for the sake of having more interfaces.

Avoid creating abstractions such as:

```text
UserValidator
EmailNormalizer
SessionFactory
TokenFactory
UserFactory
AuthContextBuilder
PlanResolver
```

unless a real requirement makes the abstraction useful.

Interfaces should represent meaningful boundaries:

- persistence
- external providers
- security primitives
- application services

Likewise, do not create one service per database table.

A table is a storage implementation detail; it is not automatically a business domain.

---

# 61. Final V1 Build Order

## Phase A — Database

1. ✅ PostgreSQL setup (docker-compose, postgres:16, port 5433)
2. 🟡 Migration system — migration files exist (goose syntax); **no runner wired** (goose not in `go.mod`)
3. ✅ `users`
4. ✅ `auth_identities`
5. ✅ `sessions`
6. ✅ `email_verification_tokens`
7. ✅ `password_reset_tokens`
8. ✅ `plans`
9. ✅ `user_subscriptions`
10. ✅ `audit_events`
11. ✅ Foreign keys (REFERENCES clauses in all migrations)
12. ✅ Unique constraints (email, `UNIQUE(provider, provider_subject)`, token hashes, active subscription per user, refresh_token_hash)
13. ✅ Required indexes (all migrations create their indexes)
14. ✅ Seed Free plan (`INSERT INTO plans ... 'free'`)

## Phase B — Domain

15. ✅ Domain models (`internal/models`: User, AuthIdentity, Session, Subscription, Plan)
16. ✅ Domain errors (`internal/msgs` sentinels)
17. ✅ Input/output DTOs — all auth DTOs done (`models.RegisterRequest/LoginRequest` in `request.go`; `models.AuthResponse/RefreshResponse/MeResponse/MePlanResponse` in `response.go`; `models.RegisterInput/LoginInput` in `user.go`). `AuthResponse` (id, email, name, expires_at) is returned by both Register and Login so clients can schedule a proactive background refresh immediately.
18. ✅ Repository interfaces (`internal/repository/interface.go`)

## Phase C — Infrastructure

19. ✅ PostgreSQL repository implementations (4 entity repositories)
20. ✅ Argon2id password hasher (`pkg/hash`)
21. ✅ Cryptographic token generator (`utils/token`)
22. ⬜ Development email sender
23. ⬜ Google OAuth provider

## Phase D — Services

24. ✅ Registration
25. ✅ Login
26. ✅ Session creation/revocation/revoke-all-for-user
27. ✅ Refresh-token rotation + reuse detection
28. ✅ Logout
29. ✅ Logout-all
30. ⬜ Email verification
31. ⬜ Password reset
32. ⬜ Password change
33. ⬜ Google login
34. ⬜ Google identity linking
35. ✅ Audit events

## Phase E — Middleware

36. ✅ Authentication middleware (`RequireAuth` + `AuthClaims` in `internal/middleware/auth.go`)
37. ⬜ Authorization helpers
38. ✅ Rate limiting (`internal/middleware/ratelimit/`, per-route via `internal/router/router.go`)
39. ✅ CORS (`internal/middleware/cors.go`)
40. ✅ CSRF strategy (SameSite=Lax — sufficient for JSON-only API)
41. ✅ Security headers (`internal/middleware/securityheaders.go`)

## Phase F — HTTP

42. ✅ Authentication handlers — Register, Login, Refresh, Logout, LogoutAll in `auth_handler.go`
43. ✅ `/me` handler — `me_handler.go`, `MeHandler` interface (GetMe + ChangePassword), wired in `HandlerManager`
44. ✅ Route registration — all endpoints wired in `internal/router/router.go` via `SetupRoutes`; `main.go` is composition-only
45. ✅ Error mapping (`response.HandleError` + `errorStatusMap`); `CodeTooManyRequests` added
46. ✅ Secure cookie handling (HttpOnly/Secure/SameSite=Lax on all auth endpoints)

## Phase G — Testing

47. ⬜ Repository integration tests (need real DB — deferred)
48. ✅ Service unit tests — Register (success/email-exists/invalid-input), Login (success/all-invalid-credential paths), Refresh (success/reuse/expired), Logout, LogoutAll, GetMe (success/user-not-found), ChangePassword (success/invalid-creds/oauth-only/weak-password)
49. ✅ Handler tests — Register, Login, Refresh, Logout, LogoutAll, GetMe, ChangePassword (all happy paths + key error paths)
50. ✅ Middleware tests (`test/middleware/`) — RequireAuth (Bearer, cookie, precedence, missing, invalid, wrong key, empty Bearer), AuthClaims outside protected route
51. ✅ JWT utility tests — Issue+Verify roundtrip, wrong secret, tampered token, malformed input, alg:none attack, duration constant, distinct tokens
52. ✅ Session rotation tests — covered in service Refresh tests (old session consumed, new session created, family preserved)
53. ⬜ Security tests (rate-limit 429 response, CORS header values, security header values, account enumeration)
54. ⬜ End-to-end authentication tests (real DB)

---

# 62. Final Definition of Done

Authentication V1 is not complete merely because registration and login work.

It is complete when:

- ✅ Email/password registration works.
- ⬜ Google authentication works.
- ✅ Every new account receives the Free plan.
- ⬜ A single user can both buy and sell.
- ✅ Authentication identities can be extended later. (auth_identities model is provider-agnostic)
- ✅ Sessions are server-controlled and revocable. (single revoke + revoke-all-for-user implemented)
- ✅ Access credentials expire. (JWT 15-min lifetime enforced by golang-jwt/v5)
- ✅ Refresh credentials rotate. (full rotation in AuthService.Refresh)
- ✅ Refresh-token reuse is detected. (RevokedAt check + RevokeSessionFamily on detection)
- ✅ Logout works.
- ✅ Logout-all works.
- ⬜ Email verification works.
- ⬜ Password reset works.
- ✅ Password changes invalidate appropriate sessions. (RevokeOtherSessionsForUser in ChangePassword)
- ✅ Protected routes require authentication. (RequireAuth on all authenticated routes)
- ✅ Authorization is separate from authentication. (distinct layers in the architecture)
- ⬜ Resource ownership is enforced server-side.
- ⬜ Plan limits are enforced in application/service logic.
- ✅ Authentication endpoints are rate-limited. (per-route fixed-window, `internal/middleware/ratelimit/`)
- ⬜ OAuth state is validated.
- ⬜ Google identity claims are validated.
- ✅ CORS behavior is explicitly implemented. (`internal/middleware/cors.go`, allowlist from env)
- ✅ CSRF protection is in place. (SameSite=Lax — complete for JSON-only API)
- ⬜ CORS/security behavior covered by automated tests.
- ✅ Sensitive tokens are never stored plaintext. (hashed refresh tokens, Argon2id passwords)
- ✅ Passwords use Argon2id.
- ✅ Secrets never appear in logs.
- ✅ Security events are auditable. (see §31)
- ✅ Dependency injection is used throughout the application boundary. (constructor DI; composition root in main.go)
- ✅ Handlers do not contain business logic.
- ✅ Repositories do not contain business logic.
- ✅ Services are independently unit-testable. (fake repository interface used in all service tests)
- 🟡 The full authentication flow is covered by automated tests. (unit + handler tests ✅; integration/security ⬜)

Only after this Definition of Done is satisfied should the project move on to products, file storage, checkout, orders, and digital delivery.
