# linkMe Backend — Architecture & Hard Rules

> Last updated: 2026-08-12
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
│   auth.go — RequireAuth(jwtSecret), AuthClaims(r)                       │
│   cors.go — CORS(allowedOrigins): allowlist, echo origin, preflight 204 │
│   securityheaders.go — SecurityHeaders(appEnv): nosniff/DENY/CSP/HSTS  │
│   ratelimit/ — New(limit, window): per-IP fixed-window middleware        │
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
   internal/utils   internal helpers (response, validate, token, jwttoken)
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

Files: `user.go` (User, AuthIdentity), `session.go`, `subscription.go`,
`plan.go` (PLAN enum + Plan + `CreatePlan`).

> ⚠ Known smell: `models.RegisterInput` (in `models/user.go`) is dead code —
> the live input type is `service.RegisterInput`. Do not replicate this.
> When a type is needed at a layer boundary, define it once where it is
> consumed and delete the unused copy (Rule E4).

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

- `interface.go` — `Service` (aggregate exposing `Auth() AuthService`) and
  `AuthService` (e.g. `Register(ctx, RegisterInput) (models.User, string, error)`).
- `manager.go` — `ServiceManager` holds the concrete sub-services;
  `NewServiceManager(repos repository.Repository) Service`.
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
  6. return domain values + raw token; errors are sentinels from `msgs` or wrapped.

Service input DTOs live here (`RegisterInput`). Validation uses
`internal/utils/validate` — the service layer owns all validation rules, never
the handlers.

### 3.5 `internal/handlers` — HTTP layer

Same pattern: `interface.go` + `manager.go` + one file per handler group.

- `interface.go` — `Handler` (aggregate exposing `Auth() AuthHandler` and `Me() MeHandler`),
  `AuthHandler` (Register/Login/Refresh/Logout/LogoutAll), `MeHandler` (GetMe/ChangePassword).
- `manager.go` — `HandlerManager` holds concrete handler groups;
  `NewHandlerManager(service service.Service) Handler`.
- `auth_handler.go` — `authHandler` **embeds** `service.Service`. Canonical shape:

  1. decode the JSON body into a request DTO; on failure → `response.Error(w, 400, CodeInvalidBody, …)`;
  2. call the service with `r.Context()`;
  3. on service error → `response.HandleError(w, err)` (centralized mapping, §3.8);
  4. on success → set cookies if needed and write `response.JSON(...)`.

- `me_handler.go` — `meHandler` handles authenticated current-user routes. Reads
  JWT claims via `middleware.AuthClaims(r)` (imported from `internal/middleware`).

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

### Step 7 — Tests + verification (Q1, Q2)

- Write the tests the spec mandates (plans spec §29–30 defines the strategy).
- Run `gofmt`, `go vet ./...`, `go build ./...`, and the test suite.

### Step 8 — Ask when in doubt (G1)

Anything ambiguous at any step — DTO shapes, status codes, transaction
boundaries, spec vs. code conflicts — stop and ask before proceeding.

---

## 9. Current State of the Codebase

Snapshot as of 2026-08-12. Update this section whenever a milestone lands.

### Implemented

- **Entry point**: `cmd/server/main.go` — dotenv load, config validation, pgx pool + ping,
  composition root (repository → service → handler managers), delegates all route/middleware
  wiring to `router.SetupRoutes(h, cfg)`, listen on `:8080`.
- **Config**: `config/config.go` — `Config{DatabaseURL, JWTSecret, AllowedOrigins []string, AppEnv string}`.
  `CORS_ALLOWED_ORIGINS` (comma-separated, default `http://localhost:3000`); `APP_ENV`
  (default `"development"`). Passed to constructors via struct — never as individual args.
- **Schema (migrations, all 8 tables)**: users, auth_identities, email_verification_tokens,
  password_reset_tokens, plans, user_subscriptions, audit_events, sessions.
- **Queries + generated code**: users (create/get-by-email/get-by-id), auth_identities
  (create/get-by-provider+subject/get-by-user+provider/update-password-hash), sessions
  (create/get-by-token-hash/mark-consumed/revoke-session/revoke-family/revoke-all-for-user/
  revoke-other-for-user), user_subscriptions (create/get-active-by-user-id).
- **Domain models**: User, AuthIdentity, Session, Subscription, Plan/PLAN + `CreatePlan`.
  Request DTOs: `models/request.go` (RegisterRequest, LoginRequest, PasswordChangeRequest).
  Response DTOs: `models/response.go` (UserResponse, RefreshResponse, MeResponse, MePlanResponse).
- **Repository layer**: `RepoManager` + 4 entity repositories, `WithinTx` + context-injected
  transactions, `dbXToDomain` mappers. `AuthIdentityRepository` includes
  `GetAuthIdentityByUserIDAndProvider` + `UpdatePasswordHash`. `SessionRepository` includes
  `RevokeSession`, `RevokeAllSessionsForUser`, `RevokeOtherSessionsForUser`.
- **Service layer**: `ServiceManager` + full `AuthService`:
  - `Register` — normalize/validate → email-exists check → Argon2id → transactional user+identity+free-plan → issue JWT + refresh token
  - `Login` — normalize/validate → password identity → VerifyPassword → issue JWT + refresh token; every failure → same `ErrInvalidCredentials` (enumeration defense)
  - `Refresh` — hash lookup → reuse detection (RevokedAt → RevokeFamily + ErrTokenReuseDetected) → expiry check → WithinTx(MarkConsumed + new session in same family) → new JWT + refresh token
  - `Logout` — RevokeSession (idempotent)
  - `LogoutAll` — RevokeAllSessionsForUser
  - `GetMe` — GetUserByID + GetActiveSubscriptionByUserID
  - `ChangePassword` — validate new password → get password identity (ErrPasswordNotSet if OAuth-only) → verify current password → Argon2id hash → WithinTx(UpdatePasswordHash + RevokeOtherSessionsForUser keeping current session)
- **Handler layer**: `HandlerManager` + `AuthHandler` (Register/Login/Refresh/Logout/LogoutAll)
  + `MeHandler` (GetMe/ChangePassword). Logout/LogoutAll clear cookies (`MaxAge=-1`).
  GetMe wraps in `{"data":{...}}` envelope. Both MeHandler methods read claims via
  `middleware.AuthClaims(r)`.
- **Middleware** (`internal/middleware/`):
  - `auth.go` — `RequireAuth(jwtSecret)` (Bearer header → cookie fallback, JWT verify, claims injection) + `AuthClaims(r)`
  - `cors.go` — `CORS(allowedOrigins)`: explicit allowlist, echoes origin (never `*`), `Vary: Origin`, preflight 204
  - `securityheaders.go` — `SecurityHeaders(appEnv)`: X-Content-Type-Options, X-Frame-Options, Referrer-Policy, `default-src 'none'` CSP, HSTS (production only)
  - `ratelimit/ratelimit.go` — `New(limit, window)`: returns per-IP fixed-window middleware; each call creates its own isolated counter store
- **Router** (`internal/router/router.go`): `SetupRoutes(h handlers.Handler, cfg config.Config) http.Handler` — creates all rate limiters, wires per-route middleware chains, wraps mux in global security headers + CORS middleware, returns the assembled handler.
- **JWT tokens**: `internal/utils/jwttoken` — HS256, 15-min lifetime, claims:
  UserID/SessionID/PlanKey. `Issue` + `Verify` (rejects expired, wrong key, alg:none).
- **Routes** (all wired in `internal/router/router.go`, global middleware: security headers + CORS):
  - `GET /health` — public, no rate limit
  - `POST /api/v1/auth/register` — 5/hour rate limit
  - `POST /api/v1/auth/login` — 10/15min rate limit
  - `POST /api/v1/auth/refresh` — 60/15min rate limit
  - `POST /api/v1/auth/logout` — 10/15min rate limit + RequireAuth
  - `POST /api/v1/auth/logout-all` — 5/15min rate limit + RequireAuth
  - `GET /api/v1/me` — 60/15min rate limit + RequireAuth
  - `POST /api/v1/me/password/change` — 5/15min rate limit + RequireAuth
- **Tests** (under root `test/`, per §3.9/Q6):
  - `test/service` — Register (success/email-exists/invalid-input), Login (success/4 invalid paths), Refresh (success/reuse/expired), Logout, LogoutAll, GetMe (success/not-found), ChangePassword (success/invalid-creds/oauth-only/weak-password)
  - `test/handlers` — Register/Login/Refresh/Logout/LogoutAll/GetMe/ChangePassword (happy paths + key error paths)
  - `test/middleware` — RequireAuth (Bearer/cookie/precedence/missing/invalid/wrong-key/empty-Bearer), AuthClaims outside protected route
  - `test/jwttoken` — Issue/Verify roundtrip, wrong key, tampered, malformed, alg:none, duration constant, distinct tokens
  - `test/hash`, `test/token`, `test/validate`
- **Helpers**: `pkg/dotenv`, `pkg/hash` (Argon2id PHC), `utils/token` (opaque token + SHA-256),
  `utils/validate`, `utils/response` (envelope, codes, `errorStatusMap`, `HandleError`),
  `utils/jwttoken`, `msgs` sentinel errors.

### Not yet implemented (from the specs)

- Email verification, password reset, email change
- Google OAuth / account linking
- Audit events (`audit_events` table exists; nothing writes to it yet)
- Account deletion
- Everything in `plans_and_entitlements_v1_backend_spec.md` beyond the plan
  model and free-subscription-on-register behavior

### Suggested next steps (in order of priority)

1. **Email infrastructure** — `EmailSender` interface + dev implementation (noop or local SMTP); required before email verification and password reset can be completed
2. **Email verification** + **Password reset** — both need the email sender; tackle together since they share the same token-generation + email-sending infrastructure
3. **Google OAuth** — large feature; requires OAuth provider abstraction and new routes
4. **Plans & entitlements** — `plans_and_entitlements_v1_backend_spec.md`

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
- [ ] No unanswered questions left open — anything uncertain was asked (G1)
