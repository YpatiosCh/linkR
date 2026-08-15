# Testing linkMe's Auth API with Postman

This is a walkthrough for `DOCS/postman/linkMe.postman_collection.json`, which
covers every endpoint currently implemented (see
`DOCS/authentication_v1_backend_spec.md` and `DOCS/ARCHITECTURE_AND_RULES.md`
§9 for the full behavior spec). It's especially focused on answering: **did an
email actually get sent?**

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
   `RESEND_API_KEY` is required — the server calls `log.Fatal` at startup if
   it's unset (`cmd/server/main.go`). Use a real key and set
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
   You should see `connected to database` and `server listening on :8080`.

## 2. Import the collection

Postman → **Import** → select `DOCS/postman/linkMe.postman_collection.json`.
Everything needed is baked into the collection's own variables (Collection →
**Variables** tab) — no separate environment file to import:

| Variable | Default | Purpose |
|---|---|---|
| `base_url` | `http://localhost:8080` | Change if your server runs elsewhere |
| `test_email` / `test_password` | `ada@example.com` / `correct-horse-battery` | Used by Register/Login |
| `new_password` | `new-correct-horse-battery` | Used by Change Password / Confirm Reset |
| `access_token` / `refresh_token` | empty | Auto-filled by Register/Login/Refresh's test scripts |
| `verification_token` / `reset_token` | empty | **You** fill these in manually — see §3 |

Set `test_email` to the email address on your Resend account — Resend's
sandbox restriction (using `onboarding@resend.dev` as the sender, no verified
domain) only delivers to that address.

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
2. **Register** — creates the account; its test script captures `access_token`
   and `refresh_token` from the `Set-Cookie` headers automatically.
3. **Get Me** — confirms the Bearer token works; `email_verified` should be
   `false`.
4. **Change Password** — then either update `test_password`/reuse
   `new_password` on subsequent Logins.
5. **Refresh** — sends `Cookie: refresh_token={{refresh_token}}` explicitly
   (see the note in the collection request's description for why — Postman's
   cookie jar isn't reliably used here). Rotates both tokens.
6. **Request Verification Email** → grab the token per §3 → **Verify Email**
   → **Get Me** again to confirm `email_verified` is now `true`.
7. **Request Password Reset** → grab the token per §3 → **Confirm Password
   Reset**. This revokes every session, so `access_token` stops working —
   run **Login** again with `new_password` to get a fresh one.
8. **Logout** / **Logout All** — whenever you're done with a session.

## 5. Google OAuth login/signup

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
   a password Login.

The two entries in the collection (`Google: Start` and `Google: Callback`)
are included for reference/documentation only — Postman will follow the
redirects and typically report a JSON-parse failure on the final HTML page
Google or your frontend serves, which is expected.

## 6. Rate limits while testing

Defined per-route in `internal/router/router.go`. The two you're most likely
to hit while manually testing email flows:

- `POST /api/v1/auth/email/verification/request` — 5/hour
- `POST /api/v1/auth/password/reset/request` — 5/hour

Everything else (register 5/hour, login 10/15min, refresh 60/15min, the
`/verify` and `/confirm` endpoints 10/15min) is generous enough for normal
manual testing. A `429` means `CodeTooManyRequests` — wait out the window.

Token lifetimes: verification tokens last 24h, reset tokens 1h
(`internal/service/auth_service.go`) — a token you grabbed earlier in a
session is still valid for a while if you didn't get to use it immediately.

## 7. Troubleshooting

- **401 on a protected route** — the access token is a 15-minute JWT
  (`jwttoken.AccessTokenDuration`). Run **Login** again to refresh
  `access_token`, or use **Refresh**.
- **401 `TOKEN_REUSE_DETECTED` on Refresh** — you ran Refresh twice with the
  same stale `refresh_token` (each refresh rotates it and invalidates the
  old one). This revokes the whole session family — Login again.
- **Generic message from the two "request" endpoints even for a real
  account** — expected, that's the enumeration defense described in §3; check
  the Resend dashboard, not the API response.
- **429 `TOO_MANY_REQUESTS`** — see §5.
