# Testing linkMe's Auth API with Postman

This is a walkthrough for `DOCS/postman/linkMe.postman_collection.json`, which
covers every endpoint currently implemented — all 17 routes wired in
`internal/router/router.go` (see `DOCS/authentication_v1_backend_spec.md` and
`DOCS/ARCHITECTURE_AND_RULES.md` §9 for the full behavior spec). It's
especially focused on answering: **did an email actually get sent?**

## 1. Prerequisites

1. Start Postgres and Redis (session revocation + rate limiting need Redis too):
   ```bash
   docker-compose up -d
   ```
2. Apply migrations (goose is installed locally but not wired into `go.mod` —
   run it directly):
   ```bash
   goose -dir internal/db/migrations postgres \
     "postgres://app:app@localhost:5433/digital_delivery?sslmode=disable" up
   ```
3. Configure `.env` (copy from `.env.example` if you haven't already).
   `RESEND_API_KEY` is required — the server fails to start if it's unset
   (`cmd/server/main.go`). Use a real key and set
   `EMAIL_FROM=onboarding@resend.dev` (Resend's built-in sandbox sender — no
   domain verification needed) to send through Resend for real.
   `GOOGLE_CLIENT_ID`/`GOOGLE_CLIENT_SECRET`/`GOOGLE_REDIRECT_URL` are also
   required at startup (see the `.env.example` comments) — from a Google
   Cloud Console OAuth client with `http://localhost:8080/api/v1/auth/google/callback`
   registered as an authorized redirect URI.
4. Run the server:
   ```bash
   go run ./cmd/server
   ```
   You should see `connected to database` and `server listening` in the logs
   (JSON in production, human-readable text otherwise — controlled by
   `APP_ENV`/`LOG_LEVEL`).

## 2. Import the collection

Postman → **Import** → select `DOCS/postman/linkMe.postman_collection.json`.
Everything needed is baked into the collection's own variables (Collection →
**Variables** tab) — no separate environment file to import:

| Variable | Default | Purpose |
|---|---|---|
| `base_url` | `http://localhost:8080` | Change if your server runs elsewhere |
| `test_email` | `ada@example.com` | Used by Register/Login/the two "request" endpoints |
| `test_password` | `Correct-Horse1!` | Used by Register/Login/Change Password/Delete Account |
| `new_password` | `New-Horse-Battery2!` | Used by Change Password/Set Password/Confirm Reset |
| `access_token` / `refresh_token` | empty | Auto-filled by Register/Login/Refresh's test scripts |
| `verification_token` / `reset_token` | empty | **You** fill these in manually — see §3 |

Set `test_email` to the email address on your Resend account — Resend's
sandbox restriction (using `onboarding@resend.dev` as the sender, no verified
domain) only delivers to that address.

### Password policy

Every endpoint that accepts a password (Register, Login, Change Password, Set
Password, Confirm Password Reset) enforces the same rule:
**12–72 bytes, at least one uppercase letter, one lowercase letter, one
digit, and one special character.** The collection's default `test_password`/
`new_password` values already satisfy this — if you change them, keep the
rule in mind, or you'll get `401 INVALID_CREDENTIALS` back with no further
detail about which part failed (deliberate — the API never explains
*why* a password was rejected beyond "invalid").

## 3. "Did we actually send an email?"

Both `Request Verification Email` and `Request Password Reset` always return
`200` with a fixed generic message, on purpose (enumeration defense — the spec
is explicit that the response must never reveal whether the email exists).
**The API response tells you nothing about whether an email went out.** Two
ways to confirm it, independent of whether the email lands in an inbox:

1. **Resend dashboard → Emails.** Every send attempt shows up here in near
   real time with its delivery status (Delivered/Bounced/etc.) and a preview
   of the exact rendered HTML body (including the verification/reset link) —
   this works even if you can't check the recipient's inbox.
2. **The actual inbox.** With `EMAIL_FROM=onboarding@resend.dev`, Resend only
   delivers to **the email address on your own Resend account**. If
   `test_email` in the collection isn't that address, the dashboard log is
   your only confirmation — the email will show as sent from Resend's side
   but won't reach an inbox you can check. To receive it for real, set
   `test_email` to your Resend account's email.

Either way, open the link/preview from the inbox or dashboard, extract the
`token` query parameter, and paste it into `verification_token` /
`reset_token`.

## 4. Suggested run order

1. **Health Check** — confirms the server is up.
2. **Register** — creates the account with just `email`/`password` (no name —
   see §5); its test script captures `access_token` and `refresh_token` from
   the `Set-Cookie` headers automatically.
3. **Get Me** — confirms the Bearer token works; `email_verified` should be
   `false`, `has_password` should be `true` (Register always creates a
   password identity), and `name`/`avatar_url`/`company_name`/`description`
   should all be absent (unset until you run Update Profile).
4. **Update Profile** — set `name`, `avatar_url`, `company_name`,
   `description`, and `social_links`; run **Get Me** again to confirm they
   stuck. Try sending an unrecognized `social_links.platforms` key (e.g.
   `"diskord"` instead of `"discord"`) to see the `400 INVALID_INPUT` guard
   that exists specifically to catch that kind of typo.
5. **Change Password** — then either update `test_password`/reuse
   `new_password` on subsequent Logins.
6. **Refresh** — sends `Cookie: refresh_token={{refresh_token}}` explicitly
   (see the note in the collection request's description for why — Postman's
   cookie jar isn't reliably used here). Rotates both tokens.
7. **Request Verification Email** → grab the token per §3 → **Verify Email**
   → **Get Me** again to confirm `email_verified` is now `true`.
8. **Request Password Reset** → grab the token per §3 → **Confirm Password
   Reset**. This revokes every session, so `access_token` stops working —
   run **Login** again with `new_password` to get a fresh one.
9. **Logout** / **Logout All** — whenever you're done with a session.
10. **Delete Account** — run this **last, deliberately**, not as part of a
    normal pass; it's destructive. See §6 for what it does and how to undo
    it (reactivation via Register).

`Set Password` isn't part of this main flow — it only makes sense against an
OAuth-only account. See §7.

## 5. Registration collects only email + password

`Register` intentionally never asks for a name or any other profile detail —
not for password sign-up, and not for Google sign-up either (Google's
`name`/`picture` claims are deliberately discarded, never seeded onto the new
account). Everything about a user's public identity — `name`, `avatar_url`,
`company_name`, `description`, `social_links` — is set afterward, as a
separate authenticated action, via **Update Profile**
(`PATCH /api/v1/me/profile`). This is a genuine two-phase design, not a
missing feature: check `GET /api/v1/me` right after registering and every one
of those fields will be absent from the response.

## 6. Account deletion and reactivation

`Delete Account` (`DELETE /api/v1/me`) soft-deletes the account — it's hidden
from login, `Get Me`, and everywhere else — but **the account and its email
are retained**, not purged. `users.email` stays permanently unique by design
(so a deleted account's email can be traced back for contact/record-keeping
purposes), which means nobody — including the original owner — can ever
create a *new* account with that email again.

Instead, running **Register** again with a deleted account's email
**reactivates it**: same account, same ID, same history, just restored, with
the password set to whatever you send in that Register call. Try this
sequence to see it end to end:

1. Run `Register` with `test_email`, capture the account.
2. Run `Delete Account` (needs the current password if the account has one).
3. Run `Login` with the same email/password → `401 INVALID_CREDENTIALS`
   (the account is hidden, and this is indistinguishable from any other
   login failure — no enumeration leak).
4. Run `Register` again with the same email but a *different* password →
   `201 Created`, same account ID as step 1, and you're signed back in.
5. Run `Login` with the *old* (pre-deletion) password → still
   `401 INVALID_CREDENTIALS` (it was overwritten in step 4). Run it again
   with the new password → succeeds.

## 7. Set Password — for OAuth-only accounts

`Set Password` (`POST /api/v1/me/password/set`) sets an *initial* password on
an account that doesn't have one yet — meaningfully testable only against an
account created via Google sign-in, which has no password identity at all
(`has_password: false` on `Get Me`). Unlike Change Password, it never asks
for or verifies a current password, since there isn't one.

To test it: complete a real Google sign-in via §8 below, grab the
`access_token` cookie your browser received (dev tools → Application/Storage
→ Cookies), paste it in place of `{{access_token}}` for this one request, and
run **Set Password**. Run **Get Me** afterward — `has_password` flips from
`false` to `true`, and **Change Password** now works on that account too.

## 8. Google OAuth login/signup

`GET /api/v1/auth/google` and `GET /api/v1/auth/google/callback` are **not**
JSON endpoints — both always respond with an HTTP redirect (302), so
Postman's request runner and test scripts aren't useful here. Test these two
in a real browser instead:

1. Open `{{base_url}}/api/v1/auth/google` directly in a browser tab.
2. You'll land on Google's consent screen; sign in and approve.
3. Google redirects back to the callback endpoint, which redirects again to
   `FRONTEND_URL` with the `access_token`/`refresh_token` cookies set (or to
   `FRONTEND_URL?error=oauth_failed` / `?error=oauth_state_invalid` on
   failure — check the server log for the underlying error).
4. From there, cookie-based requests (e.g. **Get Me**) work the same as after
   a password Login. This account has `has_password: false` until you run
   **Set Password** (§7) on it.

The two entries in the collection (`Google: Start` and `Google: Callback`)
are included for reference/documentation only — Postman will follow the
redirects and typically report a JSON-parse failure on the final HTML page
Google or your frontend serves, which is expected.

## 9. Rate limits while testing

Defined per-route in `internal/router/router.go`. The two you're most likely
to hit while manually testing email flows:

- `POST /api/v1/auth/email/verification/request` — 5/hour
- `POST /api/v1/auth/password/reset/request` — 5/hour

Everything else is generous enough for normal manual testing:

- `register` 5/hour, `login` 10/15min, `refresh` 60/15min
- `logout` 10/15min, `logout-all` 5/15min
- `me` (Get Me) 60/15min
- `me-password-change` 5/15min, `me-password-set` 5/15min, `me-delete` 5/15min
- `me-profile-update` 20/15min
- `email-verify-verify` / `password-reset-confirm` 10/15min
- `google-start` / `google-callback` 10/15min

A `429` means `CodeTooManyRequests` — wait out the window.

Token lifetimes: verification tokens last 24h, reset tokens 1h
(`internal/service/auth_service.go`) — a token you grabbed earlier in a
session is still valid for a while if you didn't get to use it immediately.

## 10. Troubleshooting

- **401 on a protected route** — the access token is a 15-minute JWT
  (`jwttoken.AccessTokenDuration`). Run **Login** again to refresh
  `access_token`, or use **Refresh**.
- **401 `TOKEN_REUSE_DETECTED` on Refresh** — you ran Refresh twice with the
  same stale `refresh_token` (each refresh rotates it and invalidates the
  old one). This revokes the whole session family — Login again.
- **401 `INVALID_CREDENTIALS` on Register/Login/Change Password/Set
  Password/Confirm Password Reset** — the password doesn't satisfy the
  policy in §2 (12–72 bytes, upper+lower+digit+special), or (Login only) it's
  genuinely wrong, unknown, or belongs to a deleted/OAuth-only account — the
  response deliberately doesn't say which.
- **400 `INVALID_BODY` on any request** — either malformed JSON, or the body
  contains a field the endpoint doesn't recognize (e.g. a stray `"name"` on
  Register) — unrecognized fields are rejected outright, not silently
  ignored. Also returned if the body exceeds the 1MB request size cap.
- **400 `INVALID_INPUT` on Update Profile** — a validation failure on one of
  the profile fields: an unrecognized `social_links.platforms` key, an
  invalid URL, more than 5 `social_links.other` entries, or a control
  character/invalid UTF-8 in a text field.
- **409 `PASSWORD_ALREADY_SET` on Set Password** — the account already has a
  password identity; use Change Password instead.
- **401 `INVALID_CREDENTIALS` on Delete Account** — `current_password`
  doesn't match the account's *current* password, which may have changed
  since you started testing (Change Password/Confirm Password Reset both
  rotate it) — update the field to match, or re-run Login to confirm which
  password currently works.
- **Generic message from the two "request" endpoints even for a real
  account** — expected, that's the enumeration defense described in §3; check
  the Resend dashboard, not the API response.
- **429 `TOO_MANY_REQUESTS`** — see §9.
