# linkMe — Product Vision

> Last updated: 2026-08-11
> This is the **living vision** document. Anything we learn, decide, or ideate
> about the product goes here so future sessions (and humans) build against the
> same picture. **If a good idea comes up, write it down** (see [§8 Idea Log](#8-idea-log)).

This document is *not* a feature spec. Behavior lives in the per-feature specs
(`DOCS/authentication_v1_backend_spec.md`, `DOCS/plans_and_entitlements_v1_backend_spec.md`,
and future product specs). This document explains **what linkMe is and why** —
the product intent behind the architecture.

---

## 1. Problem

Creators selling PDFs, templates, digital art, and other digital goods need more
than a simple delivery system. They need a platform that handles the full sales
loop: host the file, take the payment, deliver the goods, and keep the buyer
relationship alive after the sale.

## 2. Solution (one line)

> Upload products → get payment links → auto-deliver on purchase.

The **link is the product**: creators get a shareable payment link per product;
buyers pay through the link; the digital product delivers automatically on
purchase.

## 3. Revenue Model

- **Subscription:** $19–$49/month (Free / Pro plans) for creators.
- **Platform cut:** a % of sales via `platform_fee` (Stripe Connect
  application fee at charge time).
- Both monetize the same value: we are the middleman for **anything digital**
  that can be sold — not just templates or PDFs.

## 4. Target Audience

- Course creators
- Template sellers
- Digital artists
- (Anything digital — audio presets, fonts, code, ebooks, art packs…)

## 5. External Integrations (the only businesses we rely on)

| Service | Role | Notes |
|---|---|---|
| **Cloudflare R2** | Object storage for product files | S3-compatible; **zero egress fees** (critical for a delivery platform). Format-agnostic — any digital file. |
| **Stripe** | Payments + Connect | Checkout for payment links; **Connect application fees** implement the platform %; also powers **affiliate payouts**. |

**That is the complete external business surface.** Everything else is ours.

### 5.1 Open gap: transactional email

Email delivery is *not* a business partner but it is an external dependency we
haven't chosen yet. We already need it for the auth spec (email verification,
password reset — see the `NoopEmailSender` seam) and we will need it for
**buyer notifications on new product versions**. Candidate providers: Resend /
AWS SES / Mailgun / Postmark. **Open decision.**

### 5.2 Storage mental model (corrected)

- **R2 holds the binaries** (the product files).
- **Postgres holds everything relational**: products, versions, purchases,
  links, sellers, buyers, entitlements.
- R2 is object storage, not a database. The two complement each other.

---

## 6. Killer Features

### 6.1 Auto product versioning

Sellers can update/upgrade any of their products (e.g. `template_v1` →
`template_v2`). **Every buyer who already purchased the product immediately
gets the new version for free** — they come to the platform and download the
new version, combined with email notifications.

Architectural consequence (for the future product spec):

```text
product           → seller's item (template, art pack, …)
product_versions  → v1, v2, … each with its R2 storage key
purchase          → binds buyer to PRODUCT, not to a version
```

Because the purchase binds to the **product**, every existing buyer
automatically owns v2 the moment it is published. No repurchase, no migration,
no "you bought v1 so you're stuck". The email notification is the funnel that
brings buyers back to the platform.

### 6.2 Affiliate program (Pro plan)

Creators on the **Pro** plan can let others promote their products for a cut:
affiliate links per product, commission % configuration, attribution on
checkout, payouts via Stripe Connect. Rides the existing `affiliate_enabled`
entitlement in the plans spec. This is the primary upsell engine from Free → Pro.

---

## 7. V1 MVP Scope

```text
Auth + plans (built, not yet wired)
    ↓
Products: upload to R2, plan-gated (storage_limit, max_products)
    ↓
Links: shareable payment link per product → Stripe Checkout
    ↓
Auto-delivery: Stripe webhook (checkout.completed) → grant access → presigned R2 URL
    ↓
Versioning + buyer email notifications
    ↓
Ship → get feedback from potential customers → iterate on what to work on next
```

Post-MVP, everything is driven by customer feedback. Nothing past this list is
committed.

---

## 8. Idea Log

> Unstructured capture area. Add anything — features, product ideas, risks,
> pricing experiments — no matter how rough. Date each entry. We triage later.

- (2026-08-11) First entries live in §1–§7 above; this section is the
  append-only scratchpad going forward.

---

## 9. Open Questions / Decisions

| # | Question | Status |
|---|---|---|
| 1 | Transactional email provider (Resend / SES / Mailgun / Postmark) | OPEN — required by auth spec + version notifications |
| 2 | Delivery mechanics: download page vs emailed link; download-link expiry rules | OPEN — for product spec |
| 3 | Buyer identity model: do buyers need accounts, or is email + signed download token enough? | OPEN — for product spec |
| 4 | Plan limits for storage/size per file type (any-digital means arbitrary formats) | OPEN — for product spec |
