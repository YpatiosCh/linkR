# linkMe Backend — Architecture & Hard Rules

> Last updated: 2026-08-16
> Applies to: the entire `linkMe` Go backend (module `linkMe`).

This document is the single source of truth for **how this project is structured**
and **how every agent must work on it**. Read it in full before touching any code.

Companion documents (per-feature sources of truth, see [§6](#6-feature-specifications-source-of-truth)):

- `DOCS/authentication_v1_backend_spec.md`
- `DOCS/plans_and_entitlements_v1_backend_spec.md`
- `DOCS/PRODUCT_VISION.md` (living product vision — what linkMe is and why; not a spec)

---

## Table of Contents

1. [Tech Stack](#1-tech-stack)
2. [High-Level Architecture](#2-high-level-architecture)
3. [Layer-by-Layer Reference](#3-layer-by-layer-reference)
4. [Cross-Cutting Conventions](#4-cross-cutting-conventions)
5. [Package Placement: pkg/ vs utils/ vs internal/](#5-package-placement-pkg-vs-utils-vs-internal)
6. [Feature Specifications (Source of Truth)](#6-feature-specifications-source-of-truth)
7. [Hard Rules](#7-hard-rules)
8. [End-to-End: How to Add a Feature](#8-end-to-end-how-to-add-a-feature)
9. [Current State of the Codebase](#9-current-state-of-the-codebase)
10. [Pre-Submission Verification Checklist](#10-pre-submission-verification-checklist)

---

## 1. Tech Stack

| Concern | Choice |
|---|---|
| Language | Go 1.25 |
| HTTP | stdlib `net/http` with Go 1.22+ pattern routing (e.g. `GET /health`) — **no framework** |
| Database | PostgreSQL 16 (via `docker-compose.yml`, port `5433`, db `digital_delivery`) |
| DB driver | `github.com/jackc/pgx/v5` (`pgxpool` for pooling) |
| Query layer | `sqlc` v1.31.1 (`sqlc.yaml`) — generates code into `internal/db/generated` |
| IDs | `github.com/google/uuid` (UUID PKs; sqlc override maps `uuid` → `uuid.UUID`) |
| Passwords | Argon2id via `golang.org/x/crypto` (`pkg/hash`) |
| Env loading | `github.com/joho/godotenv` (`pkg/dotenv`) |
| Shared fast-path state | Redis 7 (via `docker-compose.yml`, port `6380`), `github.com/redis/go-redis/v9` (`internal/redis`) — session revocation + rate limiting, shared across every server instance |
| Email | `github.com/resend/resend-go/v3` (`internal/service/email_service.go`) |

The dependency set is intentionally minimal. **Adding a dependency is an
architecture decision — ask before adding.** (Rule G1)

---

## 2. High-Level Architecture

The project follows a strict **db → repository → service → handlers** layering,
with a thin `cmd/server` entry point. Each layer depends only on the layers
below it. Nothing ever depends upward or skips a layer.

```text
                        ┌──────────────────────────────┐
                        │       cmd/server/main.go     │  env, config, pool, compose managers,
                        │                              │  call router.SetupRoutes, listen :8080
                        └──────────────┬───────────────┘
                                       │  constructs
                                       ▼
                        ┌──────────────────────────────┐
                        │    internal/router/router.go  │  SetupRoutes — all route registrations,
                        │                              │  per-route rate limiters, middleware chains
                        └──────────────┬───────────────┘
                                       │  uses
                                       ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                     internal/middleware  (HTTP middleware)               │
│   auth.go — RequireAuth(jwtSecret, sessions), AuthClaims(r)             │
│   cors.go — CORS(allowedOrigins): allowlist, echo origin, preflight 204 │
│   securityheaders.go — SecurityHeaders(appEnv): nosniff/DENY/CSP/HSTS  │
└──────────────────────────────────────┬──────────────────────────────────┘
                                       │  wraps
                                       ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                        internal/handlers  (HTTP layer)                  │
│   HandlerManager ──► AuthHandler ──► auth_handler.go                    │
│                   ──► MeHandler  ──► me_handler.go                      │
│   decode JSON → call service → set cookies → write standardized response│
└──────────────────────────────────────┬──────────────────────────────────┘
                                       │  depends on
                                       ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                        internal/service  (business logic)               │
│   ServiceManager ──► AuthService ──► auth_service.go                    │
│   validate, orchestrate, enforce rules, run transactions, issue tokens  │
└──────────────────────────────────────┬──────────────────────────────────┘
                                       │  depends on
                                       ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                        internal/repository  (data access)               │
│   RepoManager ──► UserRepository / AuthIdentityRepository /             │
│                   SubscriptionRepository / SessionRepository            │
│   wrap sqlc queries, map rows → domain models, translate db errors      │
└──────────────────────────────────────┬──────────────────────────────────┘
                                       │  depends on
                                       ▼
┌─────────────────────────────────────────────────────────────────────────┐
│   internal/db (sqlc generated)   │   internal/models  (domain types)     │
│   migrations/  queries/  generated/   │   User, AuthIdentity, Session,   │
│   ⚠ generated/ is NEVER hand-edited  │   Subscription, Plan             │
└─────────────────────────────────────────────────────────────────────────┘

Shared support packages (imported by any of the above, never importing them back):
   internal/msgs    sentinel errors (the contract between layers)
   internal/utils   internal helpers (response, validate, token, jwttoken, cookies)
   internal/redis   the only package touching github.com/redis/go-redis/v9 — session
                    revocation + rate limiting, shared across every server instance;
                    consumers (middleware, router, service) depend on the *redis.Client
                    living on config.Config, never on internal/redis's callers
   pkg              self-contained helpers (dotenv, hash)
```

**The golden dependency rule (A1):** `handlers → service → repository → db/models`.
A layer may only import the layer directly below it plus the shared support
packages (`msgs`, `utils`, `pkg`, `models`). Violations (e.g. a handler touching
the repository, a service importing handlers) are architecture bugs.

---

## 3. Layer-by-Layer Reference

### 3.1 `internal/db` — the database boundary

Three subdirectories, one workflow:

- `migrations/` — timestamped SQL schema migrations (`20260808142214_create_users_table.sql`, …).
  One migration per table/schema change. Add new ones, never edit applied ones.
- `queries/` — named SQL queries consumed by sqlc:
  `-- name: CreateUser :one` followed by the SQL statement.
- `generated/` — **sqlc output. DO NOT EDIT BY HAND.** Regenerate with `sqlc generate`.

sqlc config (`sqlc.yaml`): engine `postgresql`, package `db`, output
`internal/db/generated`, `sql_package: pgx/v5`, `emit_json_tags: true`,
`emit_interface: true`, override `uuid` → `github.com/google/uuid.UUID`.

**Workflow for any data change:** edit `queries/*.sql` (and add a migration if
the schema changes) → run `sqlc generate` → use the generated functions from the
repository layer. Never write raw SQL inside repository methods. (Rule C1)

Current queries (all in `generated/querier.go`): `CreateUser`, `GetUserByEmail`,
`GetUserByID`, `CreateAuthIdentity`, `GetAuthIdentityByProviderAndSubject`,
`CreateUserSubscription`, `CreateSession`.

### 3.2 `internal/models` — domain types

Layer-agnostic structs shared by repository, service, and handlers. Pure data,
no sqlc/pgx types, no HTTP concerns. Optional fields are pointers
(`AvatarURL *string`, `EmailVerifiedAt *time.Time`, `PasswordHash *string`).

Files: `user.go` (User, AuthIdentity, RegisterInput, LoginInput), `session.go`,
`subscription.go`, `plan.go` (PLAN enum + Plan + `CreatePlan`), `token.go`
(`TokenPair`), `request.go`/`response.go` (HTTP DTOs, see §4).

Any struct that crosses a layer boundary — service input/output, HTTP
request/response — belongs here, defined exactly once (Rule E4/G4). Don't
declare a boundary-crossing type inside `service/` or `handlers/` "for now";
audit new sub-services/handlers against this when they're added.

### 3.3 `internal/repository` — data access layer

Pattern per layer: **interface.go (contracts) + manager.go (aggregate) +
one file per entity repository**.

- `interface.go` — top-level `Repository` interface (aggregate: exposes
  `WithinTx` + one accessor per entity repository: `User()`, `AuthIdentity()`,
  `Subscription()`, `Session()`) and per-entity interfaces (`UserRepository`,
  `AuthIdentityRepository`, `SubscriptionRepository`, `SessionRepository`).
- `manager.go` — `RepoManager` holds the `*pgxpool.Pool` and the concrete
  entity repositories; `NewRepoManager(pool *pgxpool.Pool) Repository` wires
  them. Also implements `WithinTx` (see transactions below).
- `txcontext.go` — context-based transaction propagation: unexported `txKey`,
  `injectTx(ctx, tx)`, `extractTx(ctx)`.
- Entity files (e.g. `user_repository.go`) — unexported concrete types
  (`userRepository`) holding a `*db.Queries`, created via
  `NewUserRepository(dbtx db.DBTX)`. Every method:

  1. calls `r.querier(ctx)` to bind to an active transaction if one is in the
     context, otherwise falls back to the base queries;
  2. calls the sqlc-generated query;
  3. translates `pgx.ErrNoRows` into the matching sentinel error from
     `internal/msgs` (e.g. `msgs.ErrUserNotFound`);
  4. wraps unexpected errors with context: `fmt.Errorf("error getting user by email: %w", err)`;
  5. maps the sqlc row to the domain model via a `dbXToDomain(row)` mapper
     (e.g. `dbUserToDomain`), converting nullable `pgtype` fields to pointers.

**Repository responsibilities:** persistence + row↔domain mapping + DB error
translation. **Repository non-responsibilities:** no business rules, no
validation, no HTTP. Returns only `models.*` types — never sqlc rows or
`pgtype` types.

### 3.4 `internal/service` — business logic layer

Same pattern: `interface.go` + `manager.go` + one file per feature service.

- `interface.go` — `Service` (aggregate exposing `Auth() AuthService`,
  `User() UserService`, and `Email() EmailService`), `AuthService`
  (Register/Login/Refresh/Logout/LogoutAll), `UserService` (GetMe/ChangePassword
  — authenticated current-user profile operations, kept separate from
  `AuthService` since they aren't authentication flows), `EmailService`
  (SendVerificationEmail/SendPasswordResetEmail, per auth spec §54).
- `manager.go` — `ServiceManager` holds the concrete sub-services;
  `NewServiceManager(repos repository.Repository, cfg config.Config) Service`.
- `auth_service.go` — `authService` **embeds** `repository.Repository` so it can
  reach every repository and `WithinTx` directly (`s.User()`, `s.AuthIdentity()`,
  `s.Session()`, `s.WithinTx(...)`). The `Register` flow shows the canonical
  service shape:

  1. normalize + validate input (`validate.NormalizeEmail`, `validate.Email`, …);
  2. pre-check business constraints (email already taken → `msgs.ErrEmailAlreadyExists`);
  3. derive secrets/values (`hash.HashPassword`);
  4. run all writes inside a single `WithinTx` (user + auth identity + free plan subscription);
  5. perform post-commit work (`issueSession` → generate opaque token, store its
     SHA-256 hash in a session row, return the raw token to the caller);
  6. return domain values + a `models.TokenPair`; errors are sentinels from
     `msgs` or wrapped.
- `user_service.go` — `userService` embeds `repository.Repository` (no `config.Config`
  needed — it never issues tokens). Implements `GetMe` and `ChangePassword`.
- `email_service.go` — `emailService` (no repository — holds a `*resend.Client`
  and `cfg config.Config`, reading `cfg.EmailFrom`/`cfg.FrontendURL` directly
  rather than taking them as loose constructor strings, the same way
  `authService` reads `cfg.JWTSecret`) backed by `github.com/resend/resend-go/v3`.
  `client *resend.Client` is still taken as a constructor parameter (not built
  inside `NewEmailService`) so tests can inject one wrapping a fake
  `http.RoundTripper` — no network access needed (see
  `test/service/email_service_test.go`). `manager.go` always constructs the
  Resend-backed implementation from `cfg.ResendAPIKey`; `cmd/server/main.go`
  fails fast at startup (`log.Fatal`) if it's unset, same as
  `DatabaseURL`/`JWTSecret` — there is no dev/noop fallback, Resend is
  required. The two static inline HTML bodies live in
  `internal/utils/emailtemplates` (`VerificationEmailHTML`/
  `PasswordResetEmailHTML`) rather than inside the service package — pure
  string-rendering with no repository/service state, so it belongs in
  `utils/` per Rule A4, not in `service/`. No templating dependency (Rule G3).

Service input/output DTOs that cross the service↔handler boundary live in
`internal/models` (`RegisterInput`, `LoginInput`, `TokenPair`), never declared
inside the service package. Validation uses `internal/utils/validate` — the
service layer owns all validation rules, never the handlers.

### 3.5 `internal/handlers` — HTTP layer

Same pattern: `interface.go` + `manager.go` + one file per handler group.

- `interface.go` — `Handler` (aggregate exposing `Auth() AuthHandler` and `User() UserHandler`),
  `AuthHandler` (Register/Login/Refresh/Logout/LogoutAll), `UserHandler` (GetMe/ChangePassword,
  and the future home of other profile-related endpoints).
- `manager.go` — `HandlerManager` holds concrete handler groups;
  `NewHandlerManager(service service.Service) Handler`.
- `auth_handler.go` — `authHandler` **embeds** `service.Service`. Canonical shape:

  1. decode the JSON body into a request DTO; on failure → `response.Error(w, 400, CodeInvalidBody, …)`;
  2. call the service with `r.Context()`;
  3. on service error → `response.HandleError(w, err)` (centralized mapping, §3.8);
  4. on success → set cookies via `utils/cookies` if needed and write `response.JSON(...)`.

  Cookie helpers (`cookies.SetTokenCookies`, `cookies.ClearTokenCookies`) live in
  `internal/utils/cookies`, not in the handler file — handlers stay transport-only
  and any handler needing to set auth cookies reuses the same helper (G4).

- `user_handler.go` — `userHandler` handles authenticated current-user routes
  (GetMe/ChangePassword). Reads JWT claims via `middleware.AuthClaims(r)`
  (imported from `internal/middleware`).

⚠ **Auth middleware is not in this package.** `RequireAuth`, `AuthClaims`, and
`bearerToken` live in `internal/middleware/auth.go` (`package middleware`). Handlers
import `internal/middleware` to call `middleware.AuthClaims(r)`.

Request/response DTOs are declared in `internal/models/request.go` and
`internal/models/response.go` as exported types. Handlers contain **no business
logic** — only transport (decode → delegate → encode).

### 3.6 `internal/msgs` — sentinel errors

The **contract between layers**. All business errors are declared here as
package-level sentinels (`errors.New`), e.g. `ErrInvalidCredentials`,
`ErrEmailAlreadyExists`, `ErrUserNotFound`, `ErrTokenInvalid`, `ErrTokenReuseDetected`.

- Repository translates DB errors into these.
- Service returns them (wrapped or bare).
- Handlers never string-match them; they pass them to `response.HandleError`.

New business error → declare it here, then map it in `response/handle.go`
(Rule D1).

### 3.7 `internal/utils` and `pkg` — helpers

| Package | Role | Imports internal? |
|---|---|---|
| `pkg/dotenv` | `Load()`, `GetEnv(key, fallback)` — godotenv wrapper | no |
| `pkg/hash` | `HashPassword`, `VerifyPassword` — Argon2id, PHC format | no |
| `internal/utils/response` | `JSON`, `Error`, `HandleError`, `codes.go` (`Code*` constants), `errorStatusMap` | yes (`msgs`) |
| `internal/utils/validate` | `NormalizeEmail`, `Email`, `Password`, `Name` + limits | no |
| `internal/utils/token` | `Generate()` (32-byte opaque base64url token), `Hash()` (SHA-256 hex) | no |
| `internal/utils/jwttoken` | `Issue`, `Verify`, `AccessTokenDuration` — HS256 access tokens | no |
| `internal/utils/cookies` | `SetTokenCookies`, `ClearTokenCookies` — HttpOnly auth cookie read/write, shared by any handler | yes (`models`, `utils/jwttoken`) |
| `internal/utils/emailtemplates` | `VerificationEmailHTML(link)`, `PasswordResetEmailHTML(link)` — static inline HTML bodies for the two transactional emails | no |

The boundary rule (Rule B4): `pkg/` packages are self-contained and must never
import `linkMe/internal/*`. `utils/` packages may import other internal packages.
New helpers go in `utils/` unless they are genuinely self-contained and reusable
outside the project — then `pkg/`.

### 3.8 The error → HTTP pipeline (read this carefully)

```text
db error (pgx.ErrNoRows, …)
   │  repository translates to sentinel          internal/msgs
   ▼
msgs.ErrUserNotFound
   │  service returns (bare or wrapped)
   ▼
handler: response.HandleError(w, err)
   │  errorStatusMap: sentinel → (HTTP status, Code*)
   ▼
client: 401/404/409/…  { "error": { "code": "USER_NOT_FOUND", "message": "user not found" } }
```

`response.HandleError` matches with `errors.Is`; **unmapped/unexpected errors
are logged and turned into a generic `500 INTERNAL_ERROR`** — internal details
never leak to clients. Never bypass this pipeline with ad-hoc error shapes.

### 3.9 Testing layout — tests live in root `test/`

All tests live under a top-level `test/` directory, **kept separate from the
code under test** — there are no colocated `_test.go` files inside `internal/`
or `pkg/`. `test/` mirrors the package it exercises, one directory (and one
external test package) per package under test:

```text
test/
  service/    auth_service_test.go   package service_test    → linkMe/internal/service
  handlers/   auth_handler_test.go   package handlers_test   → linkMe/internal/handlers
  middleware/ middleware_test.go     package middleware_test  → linkMe/internal/middleware
  jwttoken/   jwttoken_test.go       package jwttoken_test   → linkMe/internal/utils/jwttoken
  validate/   validate_test.go       package validate_test   → linkMe/internal/utils/validate
  token/      token_test.go          package token_test      → linkMe/internal/utils/token
  hash/       hash_test.go           package hash_test       → linkMe/pkg/hash
```

Consequences of this layout (all intentional):

- **Black-box only.** A `test/` package imports `linkMe/...` and can therefore
  reach **only exported symbols**. Unexported functions (row mappers like
  `dbUserToDomain`, helpers like `issueSession`) are not directly testable;
  they are covered through the exported API that calls them. Design exported
  seams accordingly — e.g. services take the `repository.Repository`
  **interface**, so tests substitute a fake repository (see
  `test/service/auth_service_test.go`).
- **Package naming.** Each test file declares `package <pkg>_test` (not `<pkg>`)
  so it can import the real package of the same name without a collision.
- **Internal-import rule holds.** `test/` sits at the module root, so it is
  permitted to import `linkMe/internal/*` packages.
- Go still discovers and runs these under `go test ./...`; no extra wiring.

---

## 4. Cross-Cutting Conventions

These conventions are what make the codebase predictable. Every new file must
conform. When in doubt, read the closest existing sibling file and mirror it.

| Convention | Description |
|---|---|
| Interface-first | Each layer defines its contracts in `interface.go`; concrete types are unexported |
| Manager pattern | `XManager` (unexported fields, `NewXManager(deps) X`) exposes sub-components via accessor methods (`User()`, `Auth()`, …) |
| Constructor naming | `NewRepoManager`, `NewUserRepository`, `NewServiceManager`, `NewAuthService`, `NewHandlerManager`, `NewAuthHandler` |
| Embedding for DI | Services embed `repository.Repository`; handlers embed `service.Service` |
| Error naming | Sentinels `Err*` in `msgs`; HTTP codes `Code*` in `response/codes.go` |
| Row mapping | `dbXToDomain(row)` per entity in the repository layer |
| Transactions | Only via `WithinTx` + context-injected tx + `querier(ctx)` (Rule C3) |
| Error wrapping | `fmt.Errorf("context: %w", err)`; never `errors.New` for wrapped errors; never swallow errors |
| Response envelope | Success = raw JSON payload; error = `{"error":{"code","message"}}` |
| DTOs at boundaries | Request/response structs live in `internal/models/request.go` and `response.go` as exported types; handlers decode into and encode from these — never define DTOs inside handler files |
| Context | `ctx context.Context` is always the first parameter; handlers pass `r.Context()`; never `context.Background()` inside a request path |
| Docs | Every exported symbol has a Go doc comment stating its purpose and behavior |
| Security | Argon2id passwords, SHA-256-hashed opaque tokens, HttpOnly+Secure cookies, `crypto/subtle` constant-time compares |
| Test placement | All tests live under the root `test/` directory, **never** as colocated `_test.go` files beside source. `test/` mirrors the package under test (`test/service/`, `test/validate/`, …), one external test package per directory (`package service_test`). Tests are black-box — they import `linkMe/...` and exercise **exported** APIs only (see [§3.9](#39-testing-layout-tests-live-in-root-test)) |

---

## 5. Package Placement: pkg/ vs utils/ vs internal/

- `internal/` — everything that must not be imported outside the module.
- `pkg/` — **external-facing, self-contained helpers** (thin wrappers around
  external libraries: `godotenv` → `pkg/dotenv`, `argon2` → `pkg/hash`).
  Zero imports from `linkMe/internal/*`.
- `utils/` — **internal helpers** shared between layers (`response`, `validate`,
  `token`). May import other `internal` packages (e.g. `response` → `msgs`).

New helper placement test: does it need an internal package? → `utils/`.
Is it a standalone wrapper with no internal deps? → `pkg/`. (Rule B4)

---

## 6. Feature Specifications (Source of Truth)

Per-feature behavior is defined in the spec files in `DOCS/`:

- `authentication_v1_backend_spec.md` — registration, login, email verification,
  password reset, OAuth, sessions, refresh-token rotation, middleware,
  authorization, security requirements, API conventions, endpoint summary.
- `plans_and_entitlements_v1_backend_spec.md` — plans (Free/Pro), entitlements,
  platform fees, subscription lifecycle, authorization architecture, and the
  **testing strategy** (sections 29–30 mandate unit tests).
- `PRODUCT_VISION.md` — **not a spec**: the product intent, revenue model,
  external integrations (R2, Stripe), killer features, MVP scope, and idea log.

**Rules governing spec usage (Rule F1):**

1. When starting work on a feature, read its spec first. The spec defines
   endpoints, models, flows, security requirements, and tests.
2. The spec is the **source of truth for the feature**; this document is the
   source of truth for the **architecture**. Where they interact, the spec may
   suggest package layouts (e.g. `/internal/auth`) — **those are suggestions**.
   The real architecture is the db/repository/service/handlers structure
   documented here. Implement spec *behavior* in the *real* architecture.
3. Spec sections may describe future state (e.g. tables, endpoints not yet
   implemented). Do not implement everything at once — implement only what the
   task asks for.
4. If the spec contradicts existing code, or two spec files contradict each
   other — **ask**, do not guess (Golden Rule G1).

---

## 7. Hard Rules

Numbered for reference. **Every rule is mandatory.** When asked to do something
that conflicts with a rule, raise the conflict instead of silently breaking it.

### 7.1 Golden Rules (the four you can never break)

- **G1 — When unsure, ASK.** Ambiguous requirement, missing context, two valid
  interpretations with different effort/behavior, spec conflict, or anything
  that would make you guess: ask the user first. Do not silently pick a
  direction. Guessing wrong is more expensive than asking once.
- **G2 — Never invent what already exists.** Before writing anything, search the
  codebase for the existing function, helper, pattern, or query. Use it.
  There is already: `response.JSON/Error/HandleError`, `validate.*`,
  `token.Generate/Hash`, `hash.HashPassword/VerifyPassword`, `msgs.Err*`,
  `dbXToDomain` mappers, `WithinTx`, and the manager/interface pattern.
  Adding a second way to do something that exists is a bug.
- **G3 — The simplest way is the best.** Prefer the smallest change that
  satisfies the requirement. No speculative abstractions, no "future-proofing",
  no extra layers, no premature optimization. If a plain function suffices,
  don't build a framework around it.
- **G4 — No duplicate code.** No two functions/helpers/types that do the same
  work. If you need similar behavior, extend or refactor the existing
  implementation — never copy it. Dead duplicates must be removed, not left
  behind. (The rule applies to queries, mappers, error handling, validation,
  DTOs, and response writing alike.)

### 7.2 Architecture Integrity

- **A1 — Strict one-way dependencies.** `handlers → service → repository → db/models`.
  No layer imports a layer above it; no layer skips the one below it. Handlers
  never touch repository or db; services never touch handlers or db directly;
  repositories never import service or handlers.
- **A2 — Conform to the existing pattern.** New feature = interface +
  manager + sub-component in each affected layer, mirroring the existing files
  (`interface.go`, `manager.go`, `X_service.go`/`X_repository.go`/`X_handler.go`).
  Do not introduce a parallel structure.
- **A3 — `internal/db/generated` is generated, never edited.** Hand-edits are
  overwritten by the next `sqlc generate`.
- **A4 — Placement rules.** Helpers that need internal packages → `internal/utils`.
  Self-contained wrappers → `pkg`. Domain types → `internal/models`. Layer
  contracts → the owning layer's `interface.go`. Nothing anywhere else.
- **A5 — Don't leak layer internals.** No `pgtype`/sqlc types above the
  repository; no `models.*` leaked as raw response bodies when a DTO is the
  established shape; no HTTP concerns in services; no business rules in handlers.

### 7.3 Data Access

- **C1 — Data changes go through sqlc.** Write SQL in `internal/db/queries/`
  (add a migration in `internal/db/migrations/` for schema changes), run
  `sqlc generate`. Never write raw SQL inside repository code.
- **C2 — Everything above the repository talks to `models.*` only.** Repository
  methods accept and return domain models, never generated rows.
- **C3 — Transactions only via `WithinTx`.** Multi-step writes use
  `s.WithinTx(ctx, fn)`. Repositories must use the `querier(ctx)` helper so
  calls participate in the active transaction. No raw `Begin/Commit` in
  services or handlers; no nested/independent transactions.
- **C4 — Translate DB errors at the repository boundary.** `pgx.ErrNoRows` →
  the matching `msgs` sentinel; other DB errors wrapped with context via `%w`.

### 7.4 Errors & Responses

- **D1 — Centralize error handling.** New business error → add sentinel to
  `internal/msgs` and a mapping in `errorStatusMap` (`internal/utils/response/handle.go`).
  Never return ad-hoc status codes or error shapes from a handler.
- **D2 — Never leak internals to clients.** Unexpected errors go through
  `HandleError`'s fallback: logged server-side, `500 INTERNAL_ERROR` to the
  client. Don't return raw DB/driver error strings.
- **D3 — Wrap, don't swallow.** `fmt.Errorf("context: %w", err)` for errors you
  propagate; empty `catch`/ignored-error branches are forbidden. Log where a
  failure is handled, not everywhere.
- **D4 — Use the response helpers.** All JSON output goes through
  `response.JSON`/`response.Error`. No hand-rolled `w.Write` JSON (the `/health`
  plain-text response is the sanctioned exception).

### 7.5 HTTP & API

- **H1 — Handlers are transport only.** Decode → delegate to service → encode.
  No business logic, no validation rules, no direct data access.
- **H2 — Use `r.Context()`.** Never `context.Background()`/`context.TODO()` in
  request paths.
- **H3 — Match the API conventions of the spec.** Status codes, envelope shape,
  and error codes follow `DOCS/*_spec.md` § API Conventions / § HTTP Status Rules.

### 7.6 Security

- **S1 — Never weaken existing security posture.** Argon2id for passwords,
  SHA-256 hashed opaque tokens, HttpOnly+Secure+SameSite cookies, constant-time
  compares. "Simplifying" any of these to a weaker scheme is forbidden.
- **S2 — Never log or store secrets.** No passwords, raw tokens, or hashes in
  logs, error messages, or commit history. `.env` is gitignored — never commit
  real credentials.
- **S3 — Follow the spec's security requirements** (sections 26–34 of the auth
  spec, §25 of the plans spec) for anything security-adjacent.

### 7.7 Code Quality & Verification

- **Q1 — Verify before declaring done.** `gofmt`, `go vet ./...`, and
  `go build ./...` must pass on changed code. Fix anything you break.
- **Q2 — Tests are mandatory per the feature spec.** The spec files define the
  testing strategy and required unit tests for each feature. Write the tests
  the spec mandates, using the spec's prescribed style. Never delete or weaken
  existing tests.
- **Q3 — Keep functions small and focused** — mirror the existing code, which
  is one responsibility per function, richly documented.
- **Q4 — Doc comments on exported symbols.** Match the existing style: state
  what the symbol does and any notable behavior.
- **Q5 — Bugfix = minimal change.** Fix the bug, don't refactor the surrounding
  code while you're in there. Refactors are separate, explicitly requested work.
- **Q6 — Tests live in root `test/`, never beside the code.** Place every test
  under `test/`, in a directory mirroring the package under test, in an external
  `package <pkg>_test` (see [§3.9](#39-testing-layout-tests-live-in-root-test)).
  Never add a colocated `_test.go` inside `internal/` or `pkg/`. Tests are
  black-box against exported APIs; use interface seams (e.g. a fake
  `repository.Repository`) rather than reaching into unexported internals.

### 7.8 Process

- **F1 — Spec files are the feature source of truth.** Read the relevant
  `DOCS/*_spec.md` before starting a feature; implement what the spec defines,
  in the architecture this document defines. Conflicts → ask.
- **F2 — No new dependencies without asking.** Adding a router, ORM, JWT lib,
  or any new module changes the architecture. Ask first (Rule G1).
- **F3 — Commit only when asked.** Never commit, push, or open PRs unprompted.
- **F4 — Don't implement beyond the request.** No gold-plating, no "while I'm
  here" refactors, no implementing future spec sections not asked for.
- **F5 — Every new or changed route ships with its Postman documentation.**
  `DOCS/postman/linkMe.postman_collection.json` and `internal/router/router.go`
  must stay in exact 1:1 sync — the set of (method, path) pairs in the
  collection must equal the set wired in `SetupRoutes`, no more, no less
  (verify with a quick diff of the two lists before declaring a route-adding
  task done, the way it was verified when this rule was written). For every
  new route:
  1. Add a request to the collection, in the right folder, with a real
     example body/headers and the correct rate limit noted.
  2. Write its `description` field to cover every status code and error
     code it can return, not just the happy path — mirror the density of
     detail already in the collection's existing requests.
  3. Update `DOCS/postman.md`: add or extend a walkthrough section
     explaining what the route does, how to exercise it manually, and any
     ordering dependency on other requests (e.g. "run Login first").
  4. Add at least 2–3 QA testing combinations to `postman.md` for that
     route — the happy path plus the meaningful failure modes (invalid
     input, wrong auth state, the specific sentinel errors it can return) —
     not just "it returns 200."
  A route is not done until both files reflect it, in the same PR/session
  that adds it — not as a follow-up.

### 7.9 Observability

- **O1 — Log at the edges, using the existing pipeline, not ad-hoc calls.**
  This codebase has a real structured-logging pipeline
  (`pkg/logging` + `internal/utils/logctx` + `internal/middleware/requestlog.go`
  — see §9's Logging entry for the full design). New code must use it, not
  `fmt.Println`/`log.Printf`/a fresh ad-hoc logger. In practice, most new
  request-handling code needs **zero** additional logging calls:
  - `middleware.RequestLogger` already logs one structured access-log line
    (method/path/status/duration_ms/request_id) for every request, automatically.
  - `response.HandleError` already logs every sentinel-mapped error (`Warn`
    for a 4xx, `Error` for a 500-mapped sentinel) and every unmapped error
    (`Error`) exactly once, automatically, whenever a handler calls it. A new
    endpoint that reports failures through `response.HandleError` (the
    default — see Rule D1/H1) gets this for free.
  Only add an explicit log call when:
  1. A handler responds through a mechanism *other than* `response.HandleError`
     and still needs failure visibility — e.g. a redirect-based handler like
     `GoogleStart`/`GoogleCallback`. Use `middleware.LoggerFromContext(r)` (or
     `logctx.FromContext(ctx)` from a context without direct `*http.Request`
     access) so the log line still carries the request's correlation ID.
  2. A lifecycle event happens outside a request entirely — startup,
     shutdown, a future background job. Use `cfg.Logger` directly, the same
     way `cmd/server/main.go` does.
  **Never add logging calls inside `internal/service` or `internal/repository`.**
  Errors already propagate as sentinels up to the HTTP boundary and get
  logged exactly once there; a logger call inside a service/repository method
  produces a second, duplicate log line for the same failure and violates
  Rule D3 ("log where a failure is handled, not everywhere"). If a new
  service-layer error needs a status/log-level mapping it doesn't have yet,
  add the sentinel to `msgs` and map it in `errorStatusMap` (Rule D1) rather
  than logging it manually at the point it's returned.
  Never log secrets, passwords, raw tokens, or full request bodies/query
  strings (Rule S2) — the existing access-log fields (method/path/status/
  duration) are deliberately narrow for this reason; don't widen them to
  include headers, cookies, or the query string without a specific reason.

---

## 8. End-to-End: How to Add a Feature

Template for adding a new endpoint/feature. The running example is **email/password
login** (the next auth feature per `authentication_v1_backend_spec.md` §8).

### Step 1 — Read the spec (F1)

Read the feature's section in the spec (`§8 Email/Password Login`, plus
`§9 Token and Session Architecture`). Note the exact request/response shape,
status codes, and security requirements (rate limiting, generic error messages,
session rules).

### Step 2 — SQL (only if a query is missing) (C1, G4)

Check `internal/db/queries/` and `generated/querier.go` for what exists.
**For login, everything needed already exists** (`GetAuthIdentityByProviderAndSubject`,
`GetUserByEmail`) — use it, add nothing (G4). Only when a query is genuinely
missing, add it to `internal/db/queries/`:

```sql
-- name: GetSessionByID :one
SELECT * FROM sessions WHERE id = $1;
```

Add a migration only if the schema changes, then run `sqlc generate`.

### Step 3 — Repository (C2, C4, G4)

- Check the entity interface first — if the method exists (for login,
  `AuthIdentityRepository.GetAuthIdentityByProviderAndSubject` already does),
  call it from the service and stop here.
- If it is genuinely missing: add it to the entity interface in
  `internal/repository/interface.go`, then implement it in the entity file:
  `querier(ctx)` → generated query → map `pgx.ErrNoRows` to a `msgs` sentinel →
  wrap other errors → map row to domain via `dbXToDomain`. Example (the
  `GetSessionByID` query from step 2):

```go
func (r *sessionRepository) GetSessionByID(ctx context.Context, id uuid.UUID) (models.Session, error) {
    q := r.querier(ctx)
    row, err := q.GetSessionByID(ctx, id)
    if errors.Is(err, pgx.ErrNoRows) {
        return models.Session{}, msgs.ErrTokenInvalid
    }
    if err != nil {
        return models.Session{}, fmt.Errorf("getting session: %w", err)
    }
    return dbSessionToDomain(row), nil
}
```

### Step 4 — Service (business logic)

- Add the method to the feature interface in `internal/service/interface.go`
  (`Login(ctx, LoginInput) (models.User, string, error)`).
- Implement in `auth_service.go` using the canonical shape (§3.4): normalize →
  validate → fetch identity → `hash.VerifyPassword` → reject with
  `msgs.ErrInvalidCredentials` on any mismatch (per spec, don't leak which part
  was wrong) → `WithinTx` for anything multi-write (e.g. session rotation) →
  `issueSession`.
- Define the input DTO once, in the service layer (`LoginInput`). (G4)

### Step 5 — Handler (transport only, H1)

- Add the method to `internal/handlers/interface.go` (`Login(w, r)`).
- Implement in `auth_handler.go`: decode `loginRequest` → 400
  `CodeInvalidBody` on failure → call `h.Auth().Login(r.Context(), …)` →
  `response.HandleError` on error → set the `refresh_token` cookie → `response.JSON`.
- Add `LoginRequest`/`UserResponse` to `internal/models/request.go` and `response.go` if not already present.

### Step 6 — Wire the route (in `internal/router/router.go`)

`main.go` only constructs the managers and calls `router.SetupRoutes`. All route
registration, rate limiter creation, and middleware chaining live in `SetupRoutes`:

```go
// internal/router/router.go
mux.Handle("POST /api/v1/auth/login", rl(10, 15*time.Minute)(http.HandlerFunc(h.Auth().Login)))
```

`main.go` reduces to:

```go
h := handlers.NewHandlerManager(services)
log.Fatal(http.ListenAndServe(":8080", router.SetupRoutes(h, cfg)))
```

### Step 7 — Document in Postman (F5)

Add the route to `DOCS/postman/linkMe.postman_collection.json` (real example
body, correct rate limit, a `description` covering every status/error code it
can return) and to `DOCS/postman.md` (a walkthrough section: what it does,
how to exercise it, any ordering dependency on other requests, plus 2–3 QA
combinations — happy path and the meaningful failure modes). Don't defer this
to a follow-up — it's part of the route being done.

### Step 8 — Tests + verification (Q1, Q2)

- Write the tests the spec mandates (plans spec §29–30 defines the strategy).
- Run `gofmt`, `go vet ./...`, `go build ./...`, and the test suite.

### Step 9 — Ask when in doubt (G1)

Anything ambiguous at any step — DTO shapes, status codes, transaction
boundaries, spec vs. code conflicts — stop and ask before proceeding.

---

## 9. Current State of the Codebase

Snapshot as of 2026-08-16 (account deletion + reactivation, on top of: structured logging pipeline; two-phase registration + seller profile system; password/input validation hardening + self-service `SetPassword`; Google OAuth login/signup + email verification + password reset + Redis-backed session revocation + rate limiting). Update this section whenever a milestone lands.

### Implemented

- **Entry point**: `cmd/server/main.go` — dotenv load, config validation, pgx pool + ping,
  composition root (repository → service → handler managers), delegates all route/middleware
  wiring to `router.SetupRoutes(h, cfg)`, listen on `:8080`.
- **Config**: `config/config.go` — `Config{DatabaseURL, JWTSecret, AllowedOrigins []string,
  AppEnv, LogLevel, ResendAPIKey, EmailFrom, FrontendURL string; RedisClient *redis.Client;
  Logger *slog.Logger; GoogleClientID, GoogleClientSecret, GoogleRedirectURL string}`.
  `CORS_ALLOWED_ORIGINS` (comma-separated, default `http://localhost:3000`); `APP_ENV`
  (default `"development"`); `LOG_LEVEL` (default `"info"` — `debug`/`info`/`warn`/`error`);
  `FRONTEND_URL` (default `http://localhost:3000`);
  `REDIS_ADDR` (default `localhost:6380`); `GOOGLE_CLIENT_ID`/`GOOGLE_CLIENT_SECRET`/
  `GOOGLE_REDIRECT_URL` (no default — security-sensitive, must match Google's
  console registration exactly). Passed to constructors via struct —
  never as individual args. `config.Load(cfg *Config) error` populates config
  from the environment (building `Logger` via `pkg/logging.New(AppEnv, LogLevel)`
  immediately after `AppEnv`/`LogLevel` are set, before any validation `return`,
  so `cfg.Logger` is non-nil on every return path) and validates that `DatabaseURL`,
  `JWTSecret`, `ResendAPIKey`, and the three Google credentials are non-empty, returning
  an error naming the first missing one — `EmailFrom`/`FrontendURL` are not
  validated. `cmd/server/main.go` calls `Load`, logs via `cfg.Logger` (not `log.Fatal`
  — see Logging below) and `os.Exit(1)`s on its error, then separately `Ping`s
  `RedisClient` (a live network call `Load` doesn't perform) and exits if unreachable —
  both checks happen at startup before the server binds its listener.
  `RedisClient` and `Logger` are **deliberate, documented exceptions** to "Config holds only
  primitive values": session revocation (middleware), rate limiting (router), request
  logging (router), and every service/handler that logs all need the *same* instances,
  and threading them through every constructor individually was worse than holding
  them once, alongside the rest of the shared config. `pgxpool.Pool` and the Resend
  client are **not** given the same treatment — each has exactly one consumer
  (`repository.NewRepoManager`, `service.NewServiceManager`), so there's no
  fan-out problem to solve for them; don't move them into `Config` without a
  reason that actually applies to them.
- **Schema (migrations, 8 tables + 1 alter)**: users, auth_identities, email_verification_tokens,
  password_reset_tokens, plans, user_subscriptions, audit_events, sessions, plus
  `20260816170000_alter_users_add_profile_fields.sql` — `users.name` dropped to nullable
  (registration no longer collects it) and `company_name TEXT`, `description TEXT`,
  `social_links JSONB NOT NULL DEFAULT '{}'` added.
  Account deletion needed no schema change at all — `users.deleted_at` already existed —
  only three new queries (below).
- **Queries + generated code**: users (create/get-by-email/get-by-id/update-email-verified-at/
  **update-user-profile** — `sqlc.narg` + `COALESCE` per column for true partial-update
  semantics, no hand-rolled dynamic SQL/**get-by-email-including-deleted** (no `deleted_at`
  filter — the one query in this codebase that deliberately sees soft-deleted rows, used
  only by `Register`'s reactivation branch)/**soft-delete-user**/**reactivate-user**),
  auth_identities (create/get-by-provider+subject/get-by-user+provider/update-password-hash),
  sessions (create/get-by-token-hash/mark-consumed/revoke-session/revoke-family/revoke-all-for-user/
  revoke-other-for-user), user_subscriptions (create/get-active-by-user-id),
  email_verification_tokens (create/get-by-hash/mark-consumed), password_reset_tokens
  (create/get-by-hash/mark-consumed).
- **Domain models**: User (`Name`, `AvatarURL`, `CompanyName`, `Description` are all `*string` —
  every profile field is optional and nil until set via `UpdateProfile`; `SocialLinks` is a
  non-pointer `SocialLinks` struct, never nil, zero value means "no links set"; `DeletedAt
  *time.Time` is nil on every normal lookup — all of them filter `WHERE deleted_at IS NULL`
  — and is only ever non-nil on the row `GetUserByEmailIncludingDeleted` returns), AuthIdentity,
  Session, Subscription, Plan/PLAN + `CreatePlan`,
  `TokenPair` (`models/token.go` — moved out of the service package since it crosses the
  service↔handler boundary), `EmailVerificationToken`, `PasswordResetToken`,
  `UpdateProfileInput` (partial-patch input DTO — every field a pointer; nil = leave
  unchanged). `models/social.go` — `SocialPlatform` string enum (website/x/instagram/
  youtube/tiktok/discord/github/linkedin) with `Valid()`, mirroring the existing `PLAN`
  enum style; `SocialLinks{Platforms map[SocialPlatform]string, Other []CustomSocialLink}` —
  `Platforms` keys are validated against the closed enum (rejects typos like "diskord"
  outright) while `Other` is a bounded (`MaxOtherSocialLinks = 5`) list of free-text
  `{Label, URL}` pairs for platforms not yet in the enum, stored together as one JSONB
  column, marshaled/unmarshaled in the repository layer.
  `RegisterInput`/`RegisterRequest` now carry only `{Email, Password}` — registration
  never collects or sets profile fields, for any provider (Google's `info.Name`/
  `info.Picture` are deliberately discarded on signup too, never seeded onto the new
  user — profile data is set exactly once, via `UpdateProfile`, regardless of how the
  account was created).
  Request DTOs: `models/request.go` (RegisterRequest, LoginRequest, PasswordChangeRequest,
  **SetPasswordRequest**, RequestEmailVerificationRequest, VerifyEmailRequest,
  RequestPasswordResetRequest, ResetPasswordRequest, **UpdateProfileRequest** — mirrors
  `UpdateProfileInput`, pointer fields for partial-patch semantics).
  Response DTOs: `models/response.go` (AuthResponse — `Name *string,omitempty` now, was a
  plain string, RefreshResponse, MeResponse — gained `CompanyName`, `Description`,
  `SocialLinks`, and **`HasPassword bool`** (not omitempty — the frontend needs to reliably
  tell "false" from "field absent" to decide whether to show "set a password" or "change
  password"), MePlanResponse, MessageResponse).
- **Repository layer**: `RepoManager` + 6 entity repositories, `WithinTx` + context-injected
  transactions, `dbXToDomain` mappers (now return `(models.User, error)` — an
  unmarshal failure on the `SocialLinks` JSONB column is a real error, not silently
  swallowed). `AuthIdentityRepository` includes
  `GetAuthIdentityByUserIDAndProvider` + `UpdatePasswordHash`. `SessionRepository` includes
  `RevokeSession`, `RevokeAllSessionsForUser`, `RevokeOtherSessionsForUser`. `UserRepository`
  includes `UpdateEmailVerifiedAt` and **`UpdateProfile`** (converts each optional
  `*string` to `pgtype.Text` via the shared `textOrNull` helper — nil means "leave
  unchanged" under the query's `COALESCE`; marshals `SocialLinks` to JSON only when
  non-nil), plus **`GetUserByEmailIncludingDeleted`**, **`SoftDelete`**, and
  **`Reactivate`** for account deletion/reactivation. `EmailVerificationTokenRepository` and
  `PasswordResetTokenRepository` (`Create`/`GetByHash`/`MarkConsumed` each) mirror
  `SessionRepository`'s "return any row including used ones, let the service decide"
  pattern for `GetByHash`.
- **Service layer**: `ServiceManager` + `AuthService` + `UserService` + `EmailService`
  (`AuthService`/`UserService` split — profile operations don't live on `AuthService`;
  `VerifyEmail`/`RequestPasswordReset`/`ResetPassword` live on `AuthService`, not
  `UserService`, since — like `Register`/`Login`/`Refresh` — they're public/token-authenticated
  and never go through `RequireAuth`):
  - `AuthService.Register` — normalize/validate → email-exists check → Argon2id → transactional user+identity+free-plan → issue JWT + refresh token
  - `AuthService.Login` — normalize/validate → password identity → VerifyPassword → issue JWT + refresh token; every failure → same `ErrInvalidCredentials` (enumeration defense)
  - `AuthService.Refresh` — hash lookup → reuse detection (RevokedAt → RevokeFamily + ErrTokenReuseDetected) → expiry check → WithinTx(MarkConsumed + new session in same family) → new JWT + refresh token
  - `AuthService.Logout` — RevokeSession (idempotent)
  - `AuthService.LogoutAll` — RevokeAllSessionsForUser
  - `AuthService.RequestEmailVerification` — normalize/validate email → silent no-op if unknown or already verified → generate opaque token (24h expiry) → store hash → `EmailService.SendVerificationEmail`
  - `AuthService.VerifyEmail` — hash lookup (unknown/expired → `ErrTokenInvalid`, used → `ErrTokenAlreadyUsed`) → WithinTx(UpdateEmailVerifiedAt + MarkConsumed) → return user + active subscription + `hasPasswordIdentity` (same shape as `GetMe`, including `HasPassword`)
  - `AuthService.RequestPasswordReset` — normalize/validate email → silent no-op if unknown → generate opaque token (1h expiry) → store hash → `EmailService.SendPasswordResetEmail`
  - `AuthService.ResetPassword` — validate new password → hash lookup (same invalid/used checks as above) → get password identity (`ErrPasswordNotSet` if OAuth-only) → Argon2id hash → WithinTx(UpdatePasswordHash + MarkConsumed + RevokeAllSessionsForUser)
  - `AuthService.GoogleAuthURL` — generates a CSRF state value (`utils/token.Generate`) and returns it plus `GoogleOAuthClient.AuthURL(state)`
  - `AuthService.GoogleCallback` — exchange code → `FetchUserInfo` → reject `ErrOAuthEmailNotVerified` if unverified → existing `google` identity signs in as-is → else a matching verified email attaches a new `google` identity to that existing account (no `WithinTx`, single write) → else `WithinTx` creates a new user + `google` identity + free subscription → issue JWT + refresh token. `Register` gained a matching branch: an email match with no existing `password` identity attaches one instead of rejecting (`attachPasswordIdentity`) — the two flows are symmetric, so an account can carry both a `password` and a `google` identity (the `auth_identities` schema already allows multiple rows per `user_id`; no migration needed). **Google's `info.Name`/`info.Picture` are never seeded onto the created user** — the brand-new-user branch sets only `ID`/`Email`/`EmailVerifiedAt`, exactly like the password `Register` path; profile fields are set exactly once, via `UpdateProfile`, regardless of provider (this was a deliberate correction mid-session — an earlier version of this branch did seed `Name`/`AvatarURL` from Google, which was wrong per the two-phase-registration design)
  - `UserService.GetMe` — GetUserByID + GetActiveSubscriptionByUserID + `hasPasswordIdentity`
    (a package-level helper shared with `AuthService.VerifyEmail`, since both feed the
    same `MeResponse` shape: `GetAuthIdentityByUserIDAndProvider(ctx, userID, "password")`,
    `err == nil` → true, `ErrUserNotFound` → false, otherwise propagate) — returns
    `(models.User, models.Subscription, bool, error)`, the bool being `HasPassword`
  - `UserService.ChangePassword` — validate new password → get password identity (ErrPasswordNotSet if OAuth-only) → verify current password → Argon2id hash → WithinTx(UpdatePasswordHash + RevokeOtherSessionsForUser keeping current session)
  - `UserService.SetPassword(ctx, userID, sessionID, newPassword)` — sets an *initial*
    password (e.g. for an OAuth-only account), never verifies a current one (there isn't
    one): validate new password → reject with `ErrPasswordAlreadySet` if a password
    identity already exists → hash → `WithinTx`(`CreateAuthIdentity` with
    `Provider:"password", ProviderSubject:user.Email` + `RevokeOtherSessionsForUser`
    keeping the current session) → `SessionRevoker.RevokeSessions`. Mirrors
    `ChangePassword`'s session-revocation safety property: if a hijacked session silently
    added a password credential, the legitimate user's other sessions die and they'll notice.
  - `UserService.UpdateProfile(ctx, userID, input)` — partial patch: each non-nil field in
    `UpdateProfileInput` is validated (`validate.Name`/`URL`/`CompanyName`/`Description`;
    `SocialLinks.Platforms` keys checked against `SocialPlatform.Valid()`, `Other` capped
    at `MaxOtherSocialLinks` and each entry's label/URL validated) then passed to
    `UserRepository.UpdateProfile` as-is; any failure returns `msgs.ErrInvalidInput` (new
    sentinel — see Validation below). No transaction needed (single-row, single-table).
  - `UserService.DeleteAccount(ctx, userID, sessionID, currentPassword)` — soft-deletes
    (`users.deleted_at = now()`, hiding the row from every other lookup) and revokes every
    active session. If the account has a password identity, `currentPassword` must match
    first (`ErrInvalidCredentials` otherwise) — mirrors `ChangePassword`'s confirmation
    step; skipped entirely for an OAuth-only account. **The account and its email are
    retained, not purged** — `users.email` stays a permanent `UNIQUE` constraint
    (deliberately never relaxed to a partial index) so a deleted account's email is
    preserved for record-keeping/contact purposes. See `AuthService.reactivateAccount`
    below for the other half of this design.
  - `AuthService.Register`'s existing-email branch now uses
    `UserRepository.GetUserByEmailIncludingDeleted` (the only caller of this
    non-deleted-filtered query) instead of `GetUserByEmail`, and branches three ways:
    no row → create a new account (unchanged); row exists, not deleted →
    `attachPasswordIdentity` (unchanged); **row exists, deleted →
    `reactivateAccount`** — clears `deleted_at`, sets the password identity to the
    newly supplied password (updates the existing hash if one exists, creates a new
    identity if the account had been OAuth-only), and signs the caller in as the
    *same* account/ID/history. This is the intentional consequence of keeping
    `users.email` permanently unique: a second `Register` with a deleted account's
    email can never create a new row, so it's treated as a deliberate restoration
    instead of failing.
  - **Bug found and fixed while building this**: `AuthService.Login` authenticates via
    `auth_identities` (untouched by soft-delete) *before* ever touching `users`, so a
    correct password still verifies successfully for a deleted account; the final
    `GetUserByID` call (deleted-filtered) then used to leak a raw `ErrUserNotFound`
    (404) instead of the generic `ErrInvalidCredentials` every other Login failure
    returns — breaking the account-enumeration defense for exactly the accounts where
    it matters. Fixed by translating that specific `ErrUserNotFound` into
    `ErrInvalidCredentials`. `AuthService.VerifyEmail` and `AuthService.ResetPassword`
    gained an equivalent guard — an explicit `GetUserByID` check (translating
    `ErrUserNotFound` → `ErrTokenInvalid`) right after the token's expiry/used-at
    checks and before any mutation — closing the same class of gap: neither flow
    otherwise reads `users` at all, so a token issued before deletion would otherwise
    still work after it. `GoogleCallback` has the same latent `GetUserByID` call for a
    deleted account's existing `google` identity, but wasn't fixed — it never exposes
    a distinguishable error to the client either way (always a generic
    `?error=oauth_failed` redirect), so there's no enumeration leak, only a
    slightly-misleading `Error`-level log line for what is actually an expected case;
    left as a minor follow-up rather than in scope here.
  - `EmailService` (`SendVerificationEmail`/`SendPasswordResetEmail`) — always
    Resend-backed (`emailService`); `RESEND_API_KEY` is required at startup
    (`cmd/server/main.go` fails fast if unset), no dev/noop fallback; consumed
    by `AuthService` via a constructor dependency
    (`NewAuthService(repos, cfg, emailSvc, sessions, googleClient)`).
  - `GoogleOAuthClient` (`AuthURL`/`Exchange`/`FetchUserInfo`) — declared in
    `internal/service` (mirrors `SessionRevoker`) so `AuthService` stays
    decoupled/fakeable; real implementation `googleOAuthClient`
    (`internal/service/google_oauth_client.go`) hand-rolls the two outbound
    HTTP calls (token exchange, userinfo fetch) with stdlib `net/http` — no
    OAuth2/OIDC dependency added. Verifies the caller's identity by calling
    Google's userinfo endpoint with the exchanged access token rather than
    verifying the ID token JWT locally (avoids building JWKS fetch/cache/RS256
    verification that doesn't exist anywhere else in this codebase). One
    consumer (`AuthService`), so per the `RedisClient` exception's own logic
    it's constructed once in `service.NewServiceManager` from the three raw
    `cfg.Google*` strings, not held on `Config` itself.
  - `SessionRevoker` (`RevokeSession`/`RevokeSessions`) — the write side of
    session revocation (see Middleware below for the read side). Both
    `AuthService` and `UserService` take one as a constructor dependency and
    call it immediately after the corresponding
    `repository.SessionRepository` revoke call succeeds, at all 5 points a
    session can be invalidated: `Logout`, `LogoutAll`, `Refresh`'s
    reuse-detection branch, `ResetPassword` (all of these on `AuthService`),
    and `ChangePassword` (on `UserService`). `RevokeSessionFamily`/
    `RevokeAllSessionsForUser`/`RevokeOtherSessionsForUser` on
    `repository.SessionRepository` are `:many` queries with `RETURNING id`
    specifically so the service layer knows which session IDs to also mark
    in the revocation store — a plain `:exec` UPDATE wouldn't report them.
    Satisfied by `*redis.SessionRevocationStore` (`internal/redis`),
    constructed once in `ServiceManager.NewServiceManager` from
    `cfg.RedisClient` and passed to both services.
- **Handler layer**: `HandlerManager` + `AuthHandler` (Register/Login/Refresh/Logout/LogoutAll/
  RequestEmailVerification/VerifyEmail/RequestPasswordReset/ResetPassword/GoogleStart/
  GoogleCallback) + `UserHandler`
  (GetMe/ChangePassword/**SetPassword**/**UpdateProfile**/**DeleteAccount** — renamed from
  `MeHandler` since it grew to cover profile operations beyond `/me`). Logout/LogoutAll/
  DeleteAccount clear cookies via `utils/cookies.ClearTokenCookies` (DeleteAccount too — the
  session it was called with no longer exists once the account is deleted).
  GetMe, UpdateProfile, and VerifyEmail all wrap in `{"data": <MeResponse>}`, built by the
  shared `toMeResponse(user, sub, hasPassword)` helper in `user_handler.go` — VerifyEmail
  reuses the exact same response shape (auth spec §7.2 calls for "the authenticated user's
  public account representation", which is what `GET /api/v1/me` already returns).
  UpdateProfile calls `UserService.UpdateProfile` then re-fetches via `GetMe` to build the
  full response (accepts one extra cheap single-row lookup rather than growing
  `UpdateProfile`'s return shape to also carry the subscription/has-password data).
  RequestEmailVerification
  and RequestPasswordReset always respond 200 with a fixed generic `MessageResponse` message
  regardless of whether the target email exists (enumeration defense) — the handler doesn't
  branch on the service's outcome. ResetPassword and SetPassword respond 204, consistent with
  `UserHandler.ChangePassword`. All four `UserHandler` methods read claims via
  `middleware.AuthClaims(r)`.
  Every JSON body decode across both handler files (11 call sites) goes through the new
  `response.DecodeJSON(w, r, &req) bool` helper instead of a hand-rolled
  `json.NewDecoder(r.Body).Decode(&req)` block — it calls `DisallowUnknownFields()` before
  decoding (an unrecognized field is now a 400 `INVALID_BODY`, not silently ignored) and
  writes the standard error response itself on failure, so call sites are just
  `if !response.DecodeJSON(w, r, &req) { return }`.
  `GoogleStart`/`GoogleCallback` are the first handlers in this codebase to
  respond with an HTTP redirect (302) rather than JSON, on both success and
  failure — the browser reaches them via top-level navigation, not `fetch`.
  `AuthHandler` is the first handler needing app config directly, so
  `NewAuthHandler(service, cfg)`/`NewHandlerManager(service, cfg)` grew a
  `config.Config` parameter (`FrontendURL` for redirect targets, `JWTSecret`
  to sign the OAuth state cookie via the new `internal/utils/oauthstate`
  package — `Sign`/`Verify`/`SetCookie`/`ClearCookie`, HMAC-SHA256 over a
  `utils/token.Generate()` value, ~10min TTL cookie, unrelated to the actual
  login session). Failures redirect to `cfg.FrontendURL` with
  `?error=oauth_state_invalid` (bad/missing/mismatched state, caught before
  the service is even called) or `?error=oauth_failed` (any service-layer
  error); the underlying error is logged server-side only. No new
  `msgs`→`codes`/`errorStatusMap` entries were added for OAuth failures since
  these handlers never call `response.HandleError` — the one new sentinel,
  `msgs.ErrOAuthEmailNotVerified`, exists for classification/logging only.
- **Middleware** (`internal/middleware/`):
  - `auth.go` — `RequireAuth(jwtSecret, sessions)` (Bearer header → cookie fallback, JWT verify,
    **session-revocation check**, claims injection) + `AuthClaims(r)`. After JWT verification
    succeeds, it calls `sessions.IsSessionRevoked(ctx, claims.SessionID)` (the
    `SessionRevocationChecker` interface, declared here and satisfied structurally by
    `*redis.SessionRevocationStore`) — a revoked session gets `401 SESSION_REVOKED`
    immediately, rather than waiting out the JWT's remaining lifetime. This overrides the
    "no DB lookup on the request hot path" decision from auth spec §9: the check still
    isn't a *Postgres* lookup (that tradeoff holds), it's a Redis `EXISTS` — see
    `internal/redis` above for why that's the fix instead of an in-process cache
    (multi-instance safe) or a Postgres query (adds real DB load to every request).
  - `cors.go` — `CORS(allowedOrigins)`: explicit allowlist, echoes origin (never `*`), `Vary: Origin`, preflight 204
  - `securityheaders.go` — `SecurityHeaders(appEnv)`: X-Content-Type-Options, X-Frame-Options, Referrer-Policy, `default-src 'none'` CSP, HSTS (production only)
  - `requestlog.go` — `RequestLogger(base *slog.Logger)`: assigns each request a `uuid.NewString()` request ID, sets it on the `X-Request-ID` response header, injects a logger enriched with it (`base.With("request_id", id)`) into the request context via `internal/utils/logctx`, wraps the response writer in an unexported `statusRecorder` to capture the status code, and logs one `Info` "http request" line per request (method/path/status/duration_ms) after it completes. `LoggerFromContext(r) *slog.Logger` is the handler-facing accessor (mirrors `AuthClaims(r)`), never nil (falls back to `slog.Default()` outside the middleware chain). Must be the outermost global middleware so it observes every request, including CORS preflights and unmatched routes, and the request ID exists before anything else runs.
  - `maxbody.go` — `MaxBody(limit int64)`: wraps `r.Body` in `http.MaxBytesReader(w, r.Body, limit)`. `MaxRequestBodyBytes = 1MB` — generous for today's text-only JSON API; scoped only to today's routes, not a blanket rule — product file uploads (once built) will go through a presigned R2 PUT URL directly, never through this server's handlers, so this cap never needs revisiting for that. A body over the limit surfaces as a normal JSON decode error (400 `INVALID_BODY` via `response.DecodeJSON`), not a distinct status — deliberate simplicity, not an oversight.
- **Rate limiting** (`internal/redis/ratelimit.go`, not `internal/middleware/` — see
  `internal/redis` above): `NewRateLimiter(client, name, limit, window)` — per-IP fixed-window
  middleware, `INCR` + `EXPIRE`-on-first-hit against Redis, shared across every instance.
  `name` namespaces the counter per route (`ratelimit:{name}:{ip}`) since the counter store
  is shared now rather than one isolated Go map per call site.
- **Router** (`internal/router/router.go`): `SetupRoutes(h handlers.Handler, cfg config.Config) http.Handler` —
  builds `requireAuth`/the rate-limiter factory from `cfg.RedisClient` (unchanged signature —
  the client travels on `cfg`, which `SetupRoutes` already receives), wires per-route
  middleware chains, wraps mux in global middleware: `RequestLogger` (outermost) →
  `SecurityHeaders` → `CORS` → `MaxBody` → mux, returns the assembled handler.
- **Logging** (`pkg/logging`, `internal/utils/logctx`, `internal/middleware/requestlog.go`):
  stdlib `log/slog` only — no new dependency. `pkg/logging.New(appEnv, levelName) *slog.Logger`
  builds JSON output in production, human-readable text otherwise, level parsed from
  `LOG_LEVEL` (defaults to `info` on empty/unrecognized so a typo never blocks startup).
  `internal/utils/logctx` is the context-propagation seam — `WithLogger`/`FromContext` — kept
  as its own tiny package specifically to avoid an import cycle: `internal/middleware` already
  imports `internal/utils/response` (for `RequireAuth`'s error responses), so `response`
  cannot import `middleware` back; `logctx` has no further internal deps and both sides import
  it instead. `response.HandleError` now takes `(w, r, err)` (was `(w, err)`) and logs via
  `logctx.FromContext(r.Context())` — every *matched* sentinel is logged now, not just
  unmapped errors: `Warn` for client errors (status &lt; 500, e.g. a bad login), `Error` for
  500-mapped sentinels (currently just `ErrSubscriptionNotFound`, previously invisible
  server-side despite representing a real internal-consistency violation) and genuinely
  unmapped errors. Design principle: logging stays at the edges (`main`, `RequestLogger`,
  `HandleError`'s fallback, the two OAuth handler-level failure logs) — `internal/service`
  and `internal/repository` get zero logging calls, since errors already propagate as
  sentinels to the HTTP boundary and get logged exactly once there.
- **JWT tokens**: `internal/utils/jwttoken` — HS256, 15-min lifetime, claims:
  UserID/SessionID/PlanKey. `Issue` + `Verify` (rejects expired, wrong key, alg:none).
- **Input validation & password policy** (`internal/utils/validate/validate.go`): `Password` —
  length 12–72 bytes **and** requires at least one uppercase, one lowercase, one digit, and
  one special character (anything not a letter/digit/whitespace); a length-only check was
  the old behavior, tightened this session. `Email` — length-capped (`MaxEmailLength = 254`,
  RFC 5321) and rejects the RFC 5322 `"Display Name <addr>"` form (`net/mail.ParseAddress`
  accepts it; the parsed address is compared back against the full input to ensure only a
  bare address was given). `Name`/`CompanyName`/`Description`/`CustomLinkLabel`/`URL` all
  reject invalid UTF-8, control characters, and Unicode bidi-override characters (the
  "Trojan Source" class) via a shared unexported `hasDangerousChars(s, allowNewline)` —
  `Description` is the one field that allows a bare `\n` (paragraph breaks in a bio); `\r`
  is never allowed anywhere. Argon2id hashing itself (`pkg/hash`, `m=64MB, t=3, p=2`,
  `crypto/rand` salt, `subtle.ConstantTimeCompare` verify) was already well above OWASP
  minimums and is unchanged. SQL injection was audited and found already structurally
  prevented — every DB access goes through sqlc-generated, pgx-parameterized queries
  (Rule C1 forbids anything else) — no code changes were needed there.
- **Routes** (all wired in `internal/router/router.go`, global middleware: request logging →
  security headers → CORS → body-size cap):
  - `GET /health` — public, no rate limit
  - `POST /api/v1/auth/register` — 5/hour rate limit
  - `POST /api/v1/auth/login` — 10/15min rate limit
  - `POST /api/v1/auth/refresh` — 60/15min rate limit
  - `GET /api/v1/auth/google` — 10/15min rate limit, public (redirects to Google)
  - `GET /api/v1/auth/google/callback` — 10/15min rate limit, public (redirects to `FRONTEND_URL`)
  - `POST /api/v1/auth/logout` — 10/15min rate limit + RequireAuth
  - `POST /api/v1/auth/logout-all` — 5/15min rate limit + RequireAuth
  - `GET /api/v1/me` — 60/15min rate limit + RequireAuth
  - `POST /api/v1/me/password/change` — 5/15min rate limit + RequireAuth
  - `POST /api/v1/me/password/set` — 5/15min rate limit + RequireAuth (sets an initial
    password on an account with none yet; 409 `PASSWORD_ALREADY_SET` otherwise)
  - `PATCH /api/v1/me/profile` — 20/15min rate limit + RequireAuth (partial profile update)
  - `DELETE /api/v1/me` — 5/15min rate limit + RequireAuth (soft-deletes the account,
    requires `current_password` when one is set, clears cookies)
  - `POST /api/v1/auth/email/verification/request` — 5/hour rate limit, public
  - `POST /api/v1/auth/email/verification/verify` — 10/15min rate limit, public
  - `POST /api/v1/auth/password/reset/request` — 5/hour rate limit, public
  - `POST /api/v1/auth/password/reset/confirm` — 10/15min rate limit, public
  (Rate limits for these are not spec-mandated numbers — the spec only says
  they "should" be protected — chosen to match the existing scheme: email-sending
  endpoints get `register`'s 5/hour, token-consuming endpoints get `login`'s 10/15min,
  `me-profile-update` sits between `me`'s 60/15min read rate and `me-password-change`'s
  5/15min since it's a routine write but still a write; `me-delete` matches
  `me-password-change`/`me-password-set` since it's the same class of sensitive,
  confirmation-gated action.)
- **Tests** (under root `test/`, per §3.9/Q6):
  - `test/service/auth_service_test.go` — Register (success/email-exists/invalid-input/attach-password-to-existing-google-account/**reactivates-deleted-account-with-existing-password/reactivates-deleted-account-was-oauth-only**), Login (success/4 invalid paths/**deleted-account → generic ErrInvalidCredentials, not a leaked 404**), Refresh (success/reuse/expired), Logout, LogoutAll, RequestEmailVerification (success/unknown-email/already-verified), VerifyEmail (success/not-found/expired/already-used/**deleted-account → ErrTokenInvalid**), RequestPasswordReset (success/unknown-email), ResetPassword (success/token-invalid/token-already-used/oauth-only/weak-password/**deleted-account → ErrTokenInvalid**), GoogleAuthURL (success), GoogleCallback (existing-google-user/attach-to-existing-password-account/new-user/email-not-verified/exchange-failure/userinfo-fetch-failure) — plus the shared `fakeRepo`/`fakeUserRepo`/`fakeIdentityRepo`/`fakeSessionRepo`/`fakeSubscriptionRepo`/`fakeEmailVerificationTokenRepo`/`fakePasswordResetTokenRepo`/`fakeEmailService`/`fakeGoogleOAuthClient`/`newTestAuthService` fixtures used across all service test files (`fakeUserRepo.GetUserByID` now defaults to "found, not deleted" when its `getByID` field is unset, so pre-existing tests that never configured it don't panic against the new deleted-account guards)
  - `test/service/user_service_test.go` — GetMe (success/not-found/has-password reflects identity state), ChangePassword (success/invalid-creds/oauth-only/weak-password), SetPassword (success/already-set/weak-password), UpdateProfile (success per field/each validation failure/partial-patch leaves other fields untouched), **DeleteAccount (success-with-password/success-oauth-only/wrong-password)**
  - `test/service/email_service_test.go` — SendVerificationEmail/SendPasswordResetEmail (success + non-2xx) against a fake `http.RoundTripper`, no network access
  - `test/handlers/auth_handler_test.go` — Register/Login/Refresh/Logout/LogoutAll/RequestEmailVerification/VerifyEmail/RequestPasswordReset/ResetPassword/GoogleStart/GoogleCallback (happy paths + key error paths, incl. unknown-JSON-field rejection); `redirectLocation` is the helper for asserting `Location` headers on the two redirect-based endpoints
  - `test/handlers/user_handler_test.go` — GetMe/ChangePassword/SetPassword/UpdateProfile (happy paths + key error paths, incl. the Discord-typo/too-many-custom-links 400s)
  - `test/middleware` — RequireAuth (Bearer/cookie/precedence/missing/invalid/wrong-key/empty-Bearer/revoked-session/revocation-check-error), AuthClaims outside protected route, `RequestLogger` (request-ID header/access-log line/context propagation/status default), `MaxBody` (under/over limit)
  - `test/redis` — `SessionRevocationStore` (not-revoked-by-default, single/bulk revoke), `NewRateLimiter` (allow-under-limit, reject-over-limit, per-name isolation, window reset) — all against `miniredis`, no live Redis needed for the suite
  - `test/jwttoken` — Issue/Verify roundtrip, wrong key, tampered, malformed, alg:none, duration constant, distinct tokens
  - `test/oauthstate` — Sign/Verify roundtrip, wrong secret, mismatched query state, tampered signature, malformed cookie value, SetCookie/ClearCookie attributes
  - `test/response` — `HandleError` (mapped/unmapped, Warn-vs-Error log level split), `DecodeJSON` (valid/unknown-field/malformed)
  - `test/hash`, `test/token`, `test/validate` (Password composition, Email length-cap +
    display-name rejection, control-char/bidi-override/invalid-UTF-8 cases across every
    free-text validator)
- **Helpers**: `pkg/dotenv`, `pkg/hash` (Argon2id PHC), `pkg/logging` (slog construction),
  `utils/token` (opaque token + SHA-256), `utils/validate`, `utils/response` (envelope, codes,
  `errorStatusMap`, `HandleError`, `DecodeJSON`), `utils/jwttoken`, `utils/cookies` (auth
  cookie set/clear), `utils/oauthstate` (signed OAuth CSRF state cookie), `utils/logctx`
  (request-scoped logger context propagation), `msgs` sentinel errors — verification/reset
  needed no new sentinels (`ErrTokenInvalid`/`ErrTokenAlreadyUsed`/`ErrPasswordNotSet`/
  `ErrInvalidCredentials` already covered every failure mode); Google OAuth added
  `ErrOAuthEmailNotVerified`; profile updates added `ErrInvalidInput` (400, for validation
  failures outside a credentials context — the first real use of the previously-unused
  `CodeInvalidInput`); `SetPassword` added `ErrPasswordAlreadySet` (409).

### Not yet implemented (from the specs)

- Panic recovery middleware — Go's stdlib `net/http` already recovers a panic per-connection
  so one handler panicking doesn't crash the process, but today that recovery just drops the
  connection with no HTTP response and logs an unstructured stack trace to stderr, bypassing
  `RequestLogger`'s structured logger and request-ID correlation entirely. Audited the
  codebase for live, reachable panic sources (unchecked type assertions, unguarded pointer
  dereferences, unchecked-length slice indexing) as of 2026-08-16 and found none — every
  pointer dereference and type assertion in `internal/` is already guarded — so this is
  defense-in-depth against third-party library panics and future code, not a fix for a known bug.
- Google account linking/unlinking as explicit, authenticated self-service endpoints
  (`POST /api/v1/me/auth-identities/google/link`, `DELETE /api/v1/me/auth-identities/google`,
  auth spec §18) — login/signup (§17) is done, and it already links a `google` identity to an
  existing account by verified-email match, but there's no way for an authenticated user to
  add/remove a Google identity outside of that automatic path. Deprioritized by the user
  (2026-08-16) as not urgent right now.
- Audit events (`audit_events` table exists; nothing writes to it yet — not even
  Register/Login/the verification/reset/Google/DeleteAccount flows; deliberately deferred as
  one cross-cutting pass across all endpoints rather than partial per-endpoint coverage)
- Email change (account deletion — with reactivation on re-registration — landed 2026-08-16;
  see `DeleteAccount`/`reactivateAccount` above)
- New-device/new-location login alert emails — considered and deliberately deferred; see
  `DOCS/PRODUCT_VISION.md` §11 idea log (2026-08-16 entry) for the reasoning (naive IP/UA
  matching would false-positive for mobile/VPN users) and the intended shape once designed
  properly. `POST /api/v1/me/password/set` (this session) is exactly what its OAuth-only-account
  call-to-action would use.
- Full MFA/step-up authentication (requiring both an OAuth login AND the account's password
  together) — deliberately out of scope; `SetPassword` gives an OAuth-only account a *fallback*
  credential, not an added factor. Would be its own dedicated design if ever wanted.
- Automatic verification email on `Register` (verification is opt-in via
  `POST /api/v1/auth/email/verification/request` today — `Register` is unchanged)
- Everything in `plans_and_entitlements_v1_backend_spec.md` beyond the plan
  model and free-subscription-on-register behavior

### Suggested next steps (in order of priority)

1. **Audit events** — one cross-cutting pass wiring `audit_events` writes into every
   existing endpoint (Register, Login, Logout, LogoutAll, ChangePassword, SetPassword,
   UpdateProfile, DeleteAccount, the verification/reset/Google flows, …), not bolted onto
   one feature at a time
2. **Panic recovery middleware** — cheap, no design decisions, no known live bug; do
   whenever convenient
3. **Sensitive operations / recent authentication** (auth spec §33) — needs a definition of
   what counts as "sensitive" and how "recent" a login must be before this is actionable
4. **Plans & entitlements** — `plans_and_entitlements_v1_backend_spec.md`
5. **Google account linking/unlinking** — the explicit self-service endpoints from §18;
   deprioritized by the user as not urgent

---

## 10. Pre-Submission Verification Checklist

Before declaring any task done, verify **all** of:

- [ ] `gofmt` clean on changed files
- [ ] `go vet ./...` passes
- [ ] `go build ./...` passes
- [ ] Tests written per the feature spec, placed under root `test/` mirroring
      the package under test (Q6, §3.9) — not colocated beside source; suite passes
- [ ] Dependencies follow the layer rules (A1); no upward/skipping imports
- [ ] Pattern conforms to the existing manager/interface structure (A2)
- [ ] No new dependency added without asking (F2)
- [ ] No duplicate code introduced; existing helpers reused (G2, G4)
- [ ] Errors centralized: sentinel in `msgs`, mapped in `errorStatusMap` (D1)
- [ ] No secrets/raw tokens in logs, errors, or commits (S2)
- [ ] `internal/db/generated` untouched by hand (A3)
- [ ] No spec section implemented beyond the task scope (F4)
- [ ] Every new/changed route added to the Postman collection and documented in
      `postman.md` with QA testing combinations, in the same pass (F5)
- [ ] Any new logging calls use the existing pipeline and live at the edges
      (handlers/middleware/main), not inside `internal/service`/`internal/repository` (O1)
- [ ] No unanswered questions left open — anything uncertain was asked (G1)
