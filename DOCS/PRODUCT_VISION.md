# linkMe — Product Vision

> Last updated: 2026-08-13
> **Status:** living document. This is the single source of *product intent*. When
> we learn, decide, or imagine something, it goes here — rough is fine (see
> [§11 Idea Log](#11-idea-log)).
>
> **Scope boundary:** this document explains *what linkMe is and why*. It does **not**
> define behavior. Behavior lives in the per-feature specs:
> - `DOCS/authentication_v1_backend_spec.md`
> - `DOCS/plans_and_entitlements_v1_backend_spec.md`
> - future product specs (products, checkout, delivery, versioning, affiliates)
>
> Where this document and a feature spec disagree, **the feature spec wins** and
> this document should be corrected.

---

## 1. One-liner

> **linkMe turns any digital file into a shareable payment link that delivers
> itself the moment someone pays.**

Upload a product → share a link → the buyer pays through the link → the file is
delivered automatically → the buyer keeps lifetime access, including every future
version.

**The link is the product.** A creator never has to build a store, wire up a
checkout, host files, or manually email downloads. They paste one link anywhere
they already have an audience.

---

## 2. The problem we're solving

Creators who sell digital goods — PDFs, templates, presets, fonts, code, ebooks,
art packs, courses — are stuck between two bad options:

- **Heavy storefront platforms** (full e-commerce suites): powerful but slow to
  set up, overkill for "I just want to sell this one file," and often priced for
  businesses rather than individual creators.
- **DIY duct tape** (a payment button + a cloud drive link + manual email): cheap
  but fragile. Files leak, delivery breaks, there's no record of who owns what,
  and updating a product means re-emailing every past buyer by hand.

Neither handles the *full* sales loop well: **host the file, take the payment,
deliver the goods, and keep the buyer relationship alive after the sale.**

linkMe owns that entire loop behind a single link.

---

## 3. Who it's for

Primary users are **individual digital creators** who already have an audience and
a file to sell:

- Template sellers (Notion, design, spreadsheets, docs)
- Digital artists (art packs, brushes, textures)
- Music/audio creators (sample packs, presets, sound kits)
- Course creators and educators
- Developers selling code, boilerplates, or assets
- Writers selling ebooks and guides

The unifying trait is not the file *type* — linkMe is deliberately
**format-agnostic**. The unifying trait is the *motion*: "I have something
digital, I want a link that sells and delivers it, and I don't want to run a
store to do it."

**One account, both roles.** There is no separate "buyer" and "seller" account. A
single user can sell their own products and buy other creators' products with the
same login. (See auth spec §3.)

---

## 4. What linkMe is — and isn't

**It is:**
- A per-product payment link that doubles as the delivery mechanism.
- Automatic, secure file delivery on successful payment.
- A durable record of ownership (a buyer's "library" of everything they've bought).
- A versioning system where updating a product upgrades every existing buyer for free.

**It is not (in V1):**
- A storefront/marketplace with discovery, browsing, or search across creators.
- A subscription/membership billing tool for creators' *own* customers.
- A general file host or cloud-drive competitor.
- A course LMS (video hosting, lessons, quizzes) — a course is just a file here.

Staying narrow — *link in, delivery out* — is the point. Every feature is judged
against whether it strengthens that loop.

---

## 5. How linkMe makes money

Two revenue streams, both monetizing the same core value (being the middleman for
anything digital that can be sold). **These figures are owned by the plans spec —
this section must match it exactly.**

### 5.1 Subscription (creator-facing)

| Plan | Price | Who it's for |
|---|---:|---|
| **Free** | **$0 / month** | Any creator. The full core selling loop works here. |
| **Pro** | **$19 / month** | Creators who want higher limits, advanced analytics, affiliates, and a lower per-sale fee. |

Free is **fully usable**, not a crippled trial — a creator can run a real business
on Free indefinitely. Pro is an *upgrade*, not an *unlock* of the basics.

### 5.2 Platform fee (transaction-facing)

A percentage of each successful sale, taken at charge time via **Stripe Connect
application fees**.

| Plan | Platform fee per sale |
|---|---:|
| Free | **5%** |
| Pro | **1%** |

This is why Free can stay free forever: successful Free creators generate
transaction revenue through the 5% fee. The fee is calculated **per transaction**,
never via end-of-month reconciliation.

> **Important (see §5.4 of the vision and the plans-spec review):** the fee rate is
> resolved from the **authoritative server-side subscription record at charge time**,
> *not* from the plan snapshot in the access token. A stale or tampered token must
> never be able to lower a seller's fee.

### 5.3 Why two streams instead of one

They capture different creators at different stages. A hobbyist making a few sales
a month costs us little and pays via the 5% fee. A creator doing real volume is
better off on Pro's 1% + $19 — and Pro is where the growth features (affiliates,
advanced analytics) live, so volume creators self-select into it. The affiliate
program (see §7.2) is the primary engine pushing Free → Pro.

---

## 6. External dependencies (the whole external surface)

We rely on as few outside businesses as possible. Everything else is ours.

| Service | Role | Why this one |
|---|---|---|
| **Cloudflare R2** | Object storage for product files | S3-compatible with **zero egress fees** — critical when your whole product is delivering downloads. Format-agnostic. |
| **Stripe** | Payments + Connect | Checkout powers the payment links; **Connect application fees** implement the platform %; Connect also powers **affiliate payouts**. |

**Storage mental model:**
- **R2 holds the binaries** — the actual product files.
- **Postgres holds everything relational** — products, versions, purchases, links,
  sellers, buyers, entitlements, subscriptions.
- R2 is object storage, not a database; the two complement each other.

### 6.1 Open external dependency: transactional email

Email is not a *business partner* but it is an external dependency we haven't
chosen yet. We already need it for:
- email verification and password reset (auth spec — `NoopEmailSender` seam),
- **buyer notifications when a product ships a new version** (§7.1).

Candidate providers: Resend / AWS SES / Mailgun / Postmark. **Open decision — see §12.**

---

## 7. The two features that make linkMe worth choosing

Anyone can put a file behind a paywall. These are the reasons to pick linkMe
specifically.

### 7.1 Lifetime auto-versioning (available on every plan)

A purchase binds the buyer to the **product**, not to a specific version.

```text
product           → the seller's item (e.g. "Notion CRM template")
product_versions  → v1, v2, v3 … each with its own R2 storage key
purchase          → binds buyer ↔ PRODUCT (not a version)
```

When a seller publishes `v2`, **every existing buyer immediately owns it, for
free** — no repurchase, no migration, no "you bought v1 so you're stuck." The
buyer gets an email, comes back to the platform, and downloads the new version
from their library.

Why it matters: it converts a one-time sale into an ongoing relationship, gives
buyers a reason to trust the purchase, and gives sellers a reason to keep
improving products on the platform. The version-notification email is the funnel
that pulls buyers back.

### 7.2 Affiliate program (Pro-only)

Pro creators can let others promote their products for a commission:

- affiliate links per product,
- configurable commission %,
- attribution captured at checkout,
- payouts via Stripe Connect.

Rides the `affiliates.enabled` entitlement from the plans spec. This is the
**primary upsell from Free → Pro**: a growing Free creator who wants a promotion
network has a concrete, revenue-driven reason to upgrade.

---

## 8. What a buyer gets (the after-the-sale relationship)

The sale is the *start*, not the end. A buyer's account gives them:

- A **library** of everything they've purchased, across all creators.
- **Lifetime access** to each product, including every future version.
- **Re-download** any time (no "the link expired" dead ends).
- **New-version notifications** by email.

> **Open decision (see §12):** do buyers need a full account, or is
> *email + a signed download token* enough for a first purchase? This shapes the
> whole delivery UX and is deferred to the product spec.

---

## 9. V1 MVP scope

Built strictly in this order; nothing past this list is committed.

```text
1. Auth + plans                         ← built, not yet wired to products
2. Products: upload to R2               ← plan-gated by storage + max_products
3. Links: one payment link per product  → Stripe Checkout
4. Auto-delivery                        → Stripe webhook (checkout.completed)
                                          → grant access → presigned R2 URL
5. Versioning + buyer email notifications
6. Ship → gather feedback → let feedback drive what's next
```

Post-MVP, **everything is customer-feedback-driven.** We do not pre-commit roadmap
beyond this loop.

---

## 10. Guardrails / principles

Short list of things we've decided *not* to compromise on, so future sessions
don't have to relitigate them:

- **Free stays genuinely usable.** The core loop is never paywalled. (plans AD-02)
- **Format-agnostic.** We never assume a file type; "any digital good" is the promise.
- **Minimal external surface.** R2 + Stripe (+ an email provider, TBD). Resist adding more.
- **Server-side is the source of truth.** Plan, ownership, and fees are always
  enforced server-side; the frontend and the token are UX hints, never authority.
- **Money is never floating-point.** Integer minor units + basis points. (plans §26)
- **Narrow beats broad.** A feature earns its place only by strengthening
  *link-in → delivery-out*.

---

## 11. Idea Log

> Append-only scratchpad. Anything — features, risks, pricing experiments,
> half-thoughts. Date each entry. We triage later.

- (2026-08-11) Initial vision captured in §1–§9.
- (2026-08-13) Vision rewritten for clarity and aligned to the two backend specs.
  Corrected the pricing statement (Free = $0, Pro = $19; earlier "$19–$49 Free/Pro"
  was wrong per the plans spec). Added explicit buyer-side section (§8) and
  guardrails (§10). Flagged fee-resolution-from-token as a security decision (§5.2).
- (2026-08-16) Considered, deferred: new-device/new-location login alert emails
  (triggered on `Login`/`GoogleCallback` when the session's IP/User-Agent hasn't
  been seen before for that user), with a call-to-action that differs by account
  type — password accounts get a "reset your password" link (which already
  revokes every other session in one action); OAuth-only accounts get a
  "set a password" + "log out everywhere" link, since they have no password to
  rotate and stolen-email account takeover for an OAuth-only account is really a
  compromised-Google-account problem outside this app's control. Deferred because
  a naive "seen this (IP, User-Agent) pair before" check would false-positive
  constantly for mobile/VPN/rotating-IP users — needs real design (device
  fingerprinting, ASN/geo-tolerant IP comparison, or an accepted-false-positive
  approach) before implementation, not a bolted-on heuristic. The
  `POST /api/v1/me/password/set` endpoint (added in the same pass that logged
  this idea) is exactly what the OAuth-only side of that future CTA would use.

---

## 12. Open questions / decisions

| # | Question | Status | Owner spec |
|---|---|---|---|
| 1 | Transactional email provider (Resend / SES / Mailgun / Postmark) | **OPEN** — blocks email verification, password reset, and version notifications | auth + product |
| 2 | Delivery mechanics: on-platform download page vs. emailed link; download-link expiry rules | **OPEN** | product |
| 3 | Buyer identity: full account vs. email + signed download token for first purchase | **OPEN** | product |
| 4 | Per-file-type storage/size nuances (any-digital ⇒ arbitrary formats) | **OPEN** | product |
| 5 | Register response: adopt generic `201` + inbox-differentiated email to close the account-enumeration gap (auth spec §6.1 flags this) | **OPEN — recommend closing** | auth |
| 6 | Fee resolution + privileged actions during a stale downgrade window: confirm these read the subscription record, not the JWT `plan_key` | **OPEN — recommend confirming** | plans |

---

## 13. Glossary

| Term | Meaning |
|---|---|
| **Product** | A seller's item. Has one or more versions. |
| **Product version** | A specific uploaded revision of a product, each with its own R2 key. |
| **Payment link** | The shareable URL for a product; drives Stripe Checkout. |
| **Purchase** | The binding of a buyer to a *product* (not a version) — grants lifetime access. |
| **Library** | A buyer's collection of all purchased products. |
| **Platform fee** | The % linkMe takes per sale via Stripe Connect (5% Free / 1% Pro). |
| **Entitlement** | A capability or limit granted by a plan (e.g. `products.max_active`). |
| **Effective plan** | The authoritative server-side plan for a user right now (source of truth over any token snapshot). |
