# Plans & Entitlements — Backend Specification

## 1. Overview

This document defines the V1 subscription plans, entitlements, platform fees, authorization architecture, subscription lifecycle, downgrade behavior, and testing requirements.

The plan system is intentionally small:

- **Free**
- **Pro**

The core product workflow remains available on Free. Pro primarily provides higher limits, advanced capabilities, and a lower platform transaction fee.

The plan system must integrate with the existing authentication/session architecture without introducing a database lookup on every authenticated request solely to determine the user's plan.

---

## 2. Plans

### 2.1 Free

| Property | Value |
|---|---:|
| Monthly price | $0 |
| Platform fee | 5% per successful sale |
| Active products | 3 |
| Storage | 1 GB |
| Maximum individual file | 250 MB |
| Product versions | Unlimited |
| Customers | Unlimited |
| Sales | Unlimited |
| Automatic delivery | Yes |
| Automatic product updates | Yes |
| Customer library | Yes |
| Basic analytics | Yes |
| Advanced analytics | No |
| Affiliate program | No |
| Platform branding | Included |

### 2.2 Pro

| Property | Value |
|---|---:|
| Monthly price | $19 |
| Platform fee | 1% per successful sale |
| Active products | Unlimited |
| Storage | 25 GB |
| Maximum individual file | 2 GB |
| Product versions | Unlimited |
| Customers | Unlimited |
| Sales | Unlimited |
| Automatic delivery | Yes |
| Automatic product updates | Yes |
| Customer library | Yes |
| Basic analytics | Yes |
| Advanced analytics | Yes |
| Affiliate program | Yes |
| Platform branding | Removable |

---

## 3. Commercial Model

The platform earns revenue through two mechanisms:

1. Pro subscriptions at **$19/month**.
2. A platform fee applied to each successful sale.

The platform fee depends on the seller's effective plan at the time the transaction is processed.

| Plan | Platform fee |
|---|---:|
| Free | 5% |
| Pro | 1% |

Platform fees are calculated **per transaction**. The system must not depend on an end-of-month reconciliation to determine the platform's primary transaction revenue.

### Example

For a $100 successful sale:

- Free → $5 platform fee
- Pro → $1 platform fee

Payment-provider processing fees are separate from the platform fee and are handled by the payment integration.

---

## 4. Plan Keys

V1 defines two stable internal plan keys:

```text
free
pro
```

Plan keys must be centralized and must not be scattered through business logic.

Avoid:

```go
if plan == "pro" {
    // ...
}
```

throughout the application.

Prefer centralized entitlement resolution:

```go
entitlements.Can(planKey, "affiliates.enabled")
```

or:

```go
entitlements.Max(planKey, "products.max_active")
```

---

## 5. Entitlements

Plan configuration is application-level configuration.

V1 does not require a database table for individual plan entitlements.

### 5.1 Entitlement catalog

```text
products.max_active
storage.max_bytes
file.max_size_bytes

analytics.basic
analytics.advanced

affiliates.enabled

branding.platform_removal
checkout.customization
```

### 5.2 Free configuration

```text
products.max_active       = 3
storage.max_bytes         = 1 GB
file.max_size_bytes       = 250 MB

analytics.basic           = true
analytics.advanced        = false

affiliates.enabled        = false

branding.platform_removal = false
checkout.customization    = false
```

### 5.3 Pro configuration

```text
products.max_active       = unlimited
storage.max_bytes         = 25 GB
file.max_size_bytes       = 2 GB

analytics.basic           = true
analytics.advanced        = true

affiliates.enabled        = true

branding.platform_removal = true
checkout.customization    = true
```

The exact internal representation of unlimited values is an implementation detail, but it must not rely on arbitrary magic numbers.

---

## 6. Core Product Capabilities

The following capabilities are available on both plans:

- Product creation
- Digital product delivery
- Payment links
- Customer library
- Unlimited customers
- Unlimited sales
- Product versioning
- Automatic product updates
- Basic analytics

A creator should not lose the fundamental ability to sell digital products simply because they remain on Free.

---

## 7. Product Limits

### 7.1 Free

Free users may have up to **3 active products**.

### 7.2 Pro

Pro users have **unlimited active products**.

Only active products count toward the limit.

Archived/deleted products do not consume the active-product allowance.

### Enforcement

The plan configuration provides the limit:

```text
Free → 3
Pro  → unlimited
```

The current number of active products is dynamic domain state and must therefore be obtained from the database/repository when creating a product.

The service should conceptually perform:

```text
AuthContext
    ↓
Resolve products.max_active
    ↓
Count active products
    ↓
Compare limit
    ↓
Allow or reject
```

The database is queried because the application needs the actual product count, not because it needs to rediscover the user's plan.

---

## 8. Product Versions & Automatic Updates

Product versioning is available on both plans with no version-count limit.

A creator may publish a new version of an existing product.

When a new version is published:

```text
Current product version
        ↓
Creator publishes new version
        ↓
New version becomes current
        ↓
Existing buyers retain access
        ↓
Existing buyers can receive/download the new version
```

Automatic product updates are a core product capability and are not restricted to Pro.

The exact update notification and delivery workflow belongs to the product/versioning implementation.

---

## 9. Storage

### 9.1 Limits

| Plan | Storage |
|---|---:|
| Free | 1 GB |
| Pro | 25 GB |

Storage must be represented internally using integer byte counts.

The application must use one consistent unit convention across:

- entitlement configuration
- database/storage metadata
- upload validation
- API responses
- tests

### 9.2 Upload validation

An upload must satisfy both:

1. Individual file-size limit.
2. Remaining account storage.

For example:

```text
Free storage limit: 1 GB
Current usage:      900 MB
New file:           200 MB

Result: rejected
```

The service must determine actual current usage from domain/storage state.

---

## 10. Individual File Size

| Plan | Maximum file size |
|---|---:|
| Free | 250 MB |
| Pro | 2 GB |

Boundary behavior must be deterministic.

For example:

```text
Free:
250 MB       → allowed
250 MB + 1B  → rejected

Pro:
2 GB         → allowed
2 GB + 1B    → rejected
```

The exact byte representation used by the implementation must also be used by tests.

---

## 11. Customers & Sales

Both plans provide:

- Unlimited customers
- Unlimited successful sales

There is no customer-count cap and no sales-count cap in V1.

This allows successful creators to remain on Free while the 5% platform fee provides the platform's transaction revenue.

---

## 12. Analytics

### Basic analytics

Available to both plans.

Basic analytics may include:

- Sales count
- Revenue
- Product sales
- Basic time-based sales summaries

### Advanced analytics

Available only to Pro.

Advanced analytics may include:

- Detailed sales trends
- Product performance
- Conversion metrics
- Download metrics
- Customer metrics
- Affiliate performance

The exact analytics metrics belong to the analytics feature specification.

The plan system only controls access to the capability.

---

## 13. Affiliate Program

The affiliate program is Pro-only.

```text
Free → affiliates.enabled = false
Pro  → affiliates.enabled = true
```

The affiliate feature may allow creators to:

1. Enable affiliates.
2. Create affiliate relationships.
3. Generate affiliate links.
4. Attribute sales.
5. Calculate commissions.

The detailed affiliate behavior is outside the scope of this document.

This document only establishes plan access.

---

## 14. Platform Branding

Free includes platform branding.

Pro allows the creator to remove platform branding.

```text
Free → branding.platform_removal = false
Pro  → branding.platform_removal = true
```

The exact presentation of platform branding is a frontend/product concern.

---

## 15. Checkout Customization

Checkout customization is available to Pro.

```text
Free → checkout.customization = false
Pro  → checkout.customization = true
```

The exact customization surface belongs to the checkout implementation.

---

# 16. Authorization Architecture

## 16.1 Principle

The plan must be available to authorization logic without querying the database on every authenticated request.

The access token contains a **minimal plan snapshot**.

The subscription state stored server-side remains the authoritative source of truth.

This gives the application:

- Fast normal request authorization.
- No subscription lookup solely for plan discovery on every request.
- A clear server-side source of truth.
- A bounded stale-plan window through short-lived access tokens.

---

## 16.2 Access Token

The access token should contain the minimum information needed to establish authorization context.

Conceptually:

```text
user_id
session_id
plan_key
issued_at
expires_at
```

> **Decision (cross-ref):** access token is a **JWT** — HS256 via `golang-jwt/jwt`, stateless, carrying exactly the claims above. Verified without a DB lookup on the request hot path (auth spec §9/§22; §16.1/§18.1 here). Not yet implemented.

The token must **not** contain the entire entitlement configuration.

Do not put values such as:

```text
max_products
storage_limit
affiliate_enabled
platform_fee
```

directly into every token.

The token carries:

```text
plan_key = free | pro
```

The application resolves the plan's entitlements from centralized configuration.

---

## 16.3 Source of Truth

The access token is a snapshot.

The authoritative subscription state remains server-side.

Conceptually:

```text
Server-side subscription
        ↓
Login / refresh / plan transition
        ↓
Access token
        ↓
Normal API request
```

The token must never become the permanent source of truth for subscription state.

---

## 16.4 Access Token Lifetime

Access tokens should remain short-lived.

The existing authentication architecture should determine the exact lifetime; approximately **10–15 minutes** is appropriate for the V1 design.

A short lifetime bounds how long an old plan snapshot can remain active.

---

## 16.5 Refresh Behavior

When issuing a new access token during refresh:

1. Validate the refresh/session state.
2. Determine the user's current effective plan.
3. Issue the access token with the current `plan_key`.

Conceptually:

```text
Refresh
   ↓
Current subscription state
   ↓
Current effective plan
   ↓
New access token
```

This is an appropriate point for synchronizing plan state.

Normal API requests should not perform the same subscription lookup solely to determine the plan.

---

## 16.6 Plan Changes

Plan changes must refresh authorization context.

### Upgrade

```text
Free
  ↓
Subscription becomes Pro
  ↓
New authorization context
  ↓
New access token contains pro
```

### Downgrade

```text
Pro
  ↓
Subscription becomes Free
  ↓
New authorization context
  ↓
New access token contains free
```

The user should not have to manually log out and log back in to obtain their new plan.

The subscription/payment implementation is responsible for determining when the plan actually changes.

The authorization layer is responsible for reflecting that change.

---

# 17. AuthContext

The authentication middleware should expose plan context alongside identity.

Conceptually:

```go
type AuthContext struct {
    UserID    uuid.UUID
    SessionID uuid.UUID
    PlanKey   string
}
```

The exact structure may follow the existing authentication implementation.

Handlers and services must not parse JWTs themselves.

---

# 18. Authentication vs Authorization

Authentication establishes:

> Who is this user?

Authorization establishes:

> What is this user allowed to do?

The responsibilities remain separate.

```text
Access token
    ↓
Authentication middleware
    ↓
AuthContext
    ↓
Ownership + entitlement checks
    ↓
Application service
    ↓
Domain operation
```

---

## 18.1 Middleware responsibilities

Authentication middleware is responsible for:

- Reading the access credential.
- Validating the token.
- Checking expiration.
- Validating required token claims.
- Extracting user identity.
- Extracting session identity.
- Extracting `plan_key`.
- Building the request `AuthContext`.

It is **not** responsible for:

- Counting products.
- Calculating storage usage.
- Checking product ownership from arbitrary resources.
- Determining whether a specific upload fits the user's remaining storage.
- Implementing product workflows.
- Querying the database solely to rediscover plan configuration.

---

# 19. Entitlement Service

Introduce a centralized application-level entitlement component.

Conceptually:

```go
type EntitlementService interface {
    Can(planKey string, entitlement string) bool
    Max(planKey string, entitlement string) int64
    PlatformFeeRate(planKey string) int64
}
```

The exact interface can evolve during implementation.

The important requirements are:

- Centralized plan policy.
- No database dependency for ordinary entitlement resolution.
- Stable entitlement names.
- No scattered plan conditionals.

Example:

```go
entitlements.Can(
    auth.PlanKey,
    "affiliates.enabled",
)
```

Example:

```go
maxProducts := entitlements.Max(
    auth.PlanKey,
    "products.max_active",
)
```

Example:

```go
feeBps := entitlements.PlatformFeeRate(auth.PlanKey)
```

---

# 20. Service-Level Authorization

Business authorization belongs in the application/service layer.

For example, creating a product:

```text
Request
  ↓
Authentication middleware
  ↓
AuthContext
  ↓
Product service
  ↓
Resolve product limit from plan
  ↓
Count active products
  ↓
Enforce limit
  ↓
Create product
```

The service combines:

- Authorization context.
- Static plan policy.
- Dynamic domain state.

---

# 21. Ownership Authorization

Plan authorization does not replace resource ownership checks.

For example:

```text
PATCH /api/v1/products/:id
```

must verify that the product belongs to the authenticated user.

A Pro user must not be able to modify another user's product.

Authorization therefore remains conceptually:

```text
Authentication
    +
Resource ownership
    +
Plan entitlement
    +
Business validation
```

---

# 22. When Database Access Is Appropriate

The backend should **not** query the database simply to answer:

> Is this user Free or Pro?

The plan is already available in authorization context.

Database access remains appropriate when the operation requires dynamic domain state.

Examples:

- Counting active products.
- Determining storage usage.
- Verifying product ownership.
- Reading authoritative subscription state during refresh.
- Processing subscription changes.
- Processing billing events.
- Recording transactions.

The architectural rule is:

> Do not query the database merely to rediscover static plan configuration on every authenticated request.

---

# 23. Subscription Lifecycle

## 23.1 New user

Every newly registered user starts on:

```text
free
```

This must also apply to supported OAuth registration flows.

A user must always have an effective plan.

---

## 23.2 Upgrade

When a Free user upgrades:

1. Subscription/payment operation succeeds.
2. Server-side subscription becomes Pro.
3. New authorization context is generated.
4. New access token contains `plan_key = pro`.
5. Pro capabilities become available.

---

## 23.3 Cancellation

If cancellation occurs at the end of the current billing period:

```text
Pro
  ↓
Cancellation requested
  ↓
Pro remains effective until billing period ends
  ↓
Subscription expires
  ↓
Effective plan becomes Free
```

The exact billing lifecycle belongs to the subscription/payment implementation.

---

## 23.4 Expiration

When Pro expires:

```text
pro
 ↓
subscription no longer active
 ↓
effective plan = free
```

The user's existing data remains intact.

---

# 24. Downgrades

Downgrades must **never silently delete existing business data**.

A downgrade changes what the user may do going forward.

It does not automatically remove resources that were validly created under the previous plan.

---

## 24.1 Product downgrade

Example:

```text
Pro user
20 active products
        ↓
Downgrade to Free
        ↓
20 products remain
```

The user is now above the Free limit.

They may:

- Continue accessing their existing products.
- Archive/delete products.
- Upgrade again.

They may not create additional active products until they are within the Free allowance or upgrade.

The exact behavior of individual product operations should be defined by the product specification, but automatic deletion is prohibited.

---

## 24.2 Storage downgrade

Example:

```text
Pro user
8 GB stored
        ↓
Downgrade to Free
        ↓
8 GB remains
```

The application must not automatically delete files.

While above the Free limit, new uploads that would increase usage beyond the allowed limit should be rejected.

The user can:

- Remove files.
- Reduce usage.
- Upgrade again.

Existing data must remain intact.

---

## 24.3 Affiliate downgrade

Existing affiliate-related records must not be silently deleted solely because the user downgraded.

The affiliate feature may restrict creation or activation of new affiliate functionality while the user is on Free.

The exact behavior belongs to the affiliate specification.

---

# 25. API Errors

Plan restrictions must use stable machine-readable error codes.

Recommended V1 codes:

```text
ENTITLEMENT_REQUIRED
STORAGE_LIMIT_REACHED
FILE_SIZE_LIMIT_EXCEEDED
```

Example:

```json
{
  "error": {
    "code": "ENTITLEMENT_REQUIRED",
    "message": "This action is not available on your current plan."
  }
}
```

Where useful, the API may include structured entitlement information:

```json
{
  "error": {
    "code": "ENTITLEMENT_REQUIRED",
    "message": "You have reached the maximum number of active products.",
    "entitlement": "products.max_active",
    "limit": 3
  }
}
```

Clients must rely on the machine-readable error code, not on the human-readable message.

---

# 26. Monetary Representation

Prices and fees must not use floating-point arithmetic.

Use integer minor currency units for monetary values.

Example:

```text
$19.00 → 1900 cents
```

Platform fee rates should use basis points:

```text
5% → 500 bps
1% → 100 bps
```

Fee calculations must be deterministic and have explicitly tested rounding behavior.

---

# 27. Platform Fee Calculation

The transaction flow is:

```text
Customer payment
      ↓
Determine seller's effective plan
      ↓
Resolve platform fee rate
      ↓
Calculate platform fee
      ↓
Process payment
      ↓
Seller receives proceeds
      ↓
Platform receives platform fee
```

The exact payment-provider transfer/application-fee mechanism is outside this document.

The payment implementation must keep separate:

- Payment-provider processing fees.
- Platform fees.
- Seller proceeds.

---

# 28. API Plan Information

The authenticated user endpoint may expose safe plan information.

Example:

```json
{
  "data": {
    "id": "uuid",
    "email": "user@example.com",
    "email_verified": true,
    "name": "Jane Doe",
    "plan": {
      "key": "pro",
      "status": "active"
    }
  }
}
```

Frontend applications may use this for UX.

Frontend plan information is never an authorization mechanism.

All plan enforcement remains server-side.

---

# 29. Testing Strategy

The plan system must have automated tests before dependent feature work is considered complete.

Testing must cover:

- Plan configuration.
- Entitlement resolution.
- Fee calculations.
- Product limits.
- Storage limits.
- File-size limits.
- Feature access.
- Authentication context.
- Token refresh.
- Upgrade/downgrade transitions.
- Stale token behavior.
- API authorization.
- Ownership + entitlement checks.
- No-database-plan-lookup behavior.
- Authentication regressions.

---

# 30. Unit Tests

## 30.1 Plan configuration

Verify:

```text
Free:
products.max_active = 3
storage.max_bytes = 1 GB
file.max_size_bytes = 250 MB
advanced analytics = false
affiliates = false
branding removal = false
checkout customization = false

Pro:
products.max_active = unlimited
storage.max_bytes = 25 GB
file.max_size_bytes = 2 GB
advanced analytics = true
affiliates = true
branding removal = true
checkout customization = true
```

---

## 30.2 Platform fee

Verify:

```text
Free → 5%
Pro  → 1%
```

Test multiple transaction amounts, including boundary and rounding cases.

Examples:

```text
$20
$100
$2,000
```

Do not use floating-point equality for monetary assertions.

---

## 30.3 Product limits

Verify:

```text
Free + 0 products → allowed
Free + 1 product  → allowed
Free + 2 products → allowed
Free + 3 products → rejected

Pro + any number of products → allowed
```

Verify archived/deleted products do not incorrectly consume the active-product limit.

---

## 30.4 Storage limits

Test:

- Upload within limit.
- Upload exactly at limit.
- Upload beyond limit.
- Existing usage + new file at limit.
- Existing usage + new file beyond limit.
- Free boundary.
- Pro boundary.

---

## 30.5 File-size limits

Test:

```text
Free:
250 MB       → allowed
250 MB + 1B  → rejected

Pro:
2 GB         → allowed
2 GB + 1B    → rejected
```

---

## 30.6 Feature entitlements

Verify:

```text
Free:
advanced analytics → denied
affiliate program → denied
branding removal → denied
checkout customization → denied

Pro:
advanced analytics → allowed
affiliate program → allowed
branding removal → allowed
checkout customization → allowed
```

---

# 31. Authentication Integration Tests

Verify that newly registered users receive Free.

Verify that login creates an access token with the correct `plan_key`.

Example:

```text
New user
  ↓
Free subscription
  ↓
Login
  ↓
plan_key = free
```

For Pro:

```text
Pro subscription
  ↓
Login
  ↓
plan_key = pro
```

---

# 32. Refresh Tests

Verify that refresh obtains the current effective plan.

Example:

```text
Free subscription
  ↓
Refresh
  ↓
Access token = free
```

Then:

```text
Subscription changes to Pro
  ↓
Refresh
  ↓
Access token = pro
```

And the reverse:

```text
Pro
  ↓
Downgrade
  ↓
Refresh
  ↓
Access token = free
```

---

# 33. Plan Transition Tests

### Upgrade

Verify:

- Subscription becomes Pro.
- Authorization context becomes Pro.
- Pro-only features become available.
- Product limit becomes unlimited.
- Storage limit becomes 25 GB.
- File-size limit becomes 2 GB.
- Platform fee becomes 1% for applicable subsequent transactions.

### Downgrade

Verify:

- Subscription becomes Free.
- Authorization context becomes Free.
- Pro-only creation actions are restricted.
- Free limits apply to future operations.
- Existing data remains intact.

---

# 34. Stale Token Tests

Because the access token contains a plan snapshot, explicitly test plan changes while an older access token still exists.

Example:

```text
Free token
   ↓
User upgrades to Pro
   ↓
Old token still contains free
```

Verify the defined short-lived-token behavior.

A newly issued/refreshed token must contain Pro.

The implementation must ensure stale authorization cannot persist indefinitely.

---

# 35. No-Database-Plan-Lookup Test

Entitlement resolution must work without a subscription database lookup.

The test should verify that:

```text
AuthContext.plan_key
        ↓
EntitlementService
        ↓
entitlement
```

does not invoke a subscription repository.

Instrumentation, mocks, or fakes may be used to verify this behavior.

This is an explicit architecture requirement.

---

# 36. Domain-State Tests

Verify that business operations still query dynamic domain state where required.

Example:

```text
Free
3 active products
        ↓
Create product
        ↓
Count = 3
        ↓
ENTITLEMENT_REQUIRED
```

This confirms the intended separation:

```text
Plan policy
→ application configuration

Actual resource state
→ database/domain repository
```

---

# 37. Downgrade Tests

Verify that downgrades do not delete:

- Products.
- Files.
- Product versions.
- Customer records.
- Affiliate records where applicable.
- Historical analytics data where applicable.

Verify that future restricted operations are denied correctly.

Example:

```text
Pro
20 products
  ↓
Downgrade
  ↓
Free
  ↓
20 products remain
  ↓
Create another product
  ↓
Rejected
```

---

# 38. API Authorization Tests

Verify:

```text
Unauthenticated
  ↓
Protected endpoint
  ↓
401 Unauthorized
```

Verify:

```text
Authenticated Free user
  ↓
Pro-only action
  ↓
403 Forbidden
ENTITLEMENT_REQUIRED
```

Verify:

```text
Authenticated Pro user
  ↓
Pro-only action
  ↓
Success
```

---

# 39. Ownership + Plan Tests

Verify that plan access never bypasses resource ownership.

Example:

```text
User A = Pro
Product belongs to User B
        ↓
User A attempts modification
        ↓
Rejected
```

A Pro plan does not grant access to resources owned by other users.

---

# 40. Authentication Regression Tests

After adding plan context, rerun the existing authentication suite.

Verify that the plan changes do not break:

- Registration.
- Login.
- Logout.
- Refresh.
- Session revocation.
- Password reset.
- Email verification.
- OAuth authentication.
- Existing authentication middleware behavior.

---

# 41. Implementation Order

## Phase 1 — Plan Foundation

1. Verify existing `plans` and `user_subscriptions` structures.
2. Define `free` and `pro`.
3. Seed plan records.
4. Define centralized plan configuration.
5. Define entitlement keys.
6. Define fee representation.

## Phase 2 — Authorization Context

7. Add `plan_key` to access-token claims.
8. Extend `AuthContext`.
9. Update authentication middleware.
10. Update login token creation.
11. Update refresh token creation.
12. Ensure normal requests do not perform subscription lookup solely for plan resolution.

## Phase 3 — Entitlement Service

13. Implement centralized entitlement resolution.
14. Implement boolean entitlements.
15. Implement numeric limits.
16. Implement unlimited values.
17. Implement platform fee resolution.

## Phase 4 — Business Enforcement

18. Product limit enforcement.
19. Storage enforcement.
20. File-size enforcement.
21. Advanced analytics authorization.
22. Affiliate authorization.
23. Branding authorization.
24. Checkout customization authorization.

## Phase 5 — Subscription Lifecycle

25. Free → Pro.
26. Pro → Free.
27. Cancellation/expiration handling.
28. Authorization-context synchronization.
29. Downgrade behavior.

## Phase 6 — Testing

30. Unit tests.
31. Fee calculation tests.
32. Limit tests.
33. Feature entitlement tests.
34. Authentication integration tests.
35. Refresh tests.
36. Plan transition tests.
37. Stale-token tests.
38. No-database-plan-lookup tests.
39. Downgrade tests.
40. API authorization tests.
41. Ownership + entitlement tests.
42. Authentication regression tests.

---

# 42. Definition of Done

The plan system is complete when:

- [ ] `free` and `pro` plans exist.
- [ ] Every user has an effective plan.
- [ ] Free is $0/month.
- [ ] Pro is $19/month.
- [ ] Free platform fee is 5%.
- [ ] Pro platform fee is 1%.
- [ ] Platform fees are calculated per successful transaction.
- [ ] Free allows 3 active products.
- [ ] Pro allows unlimited active products.
- [ ] Free allows 1 GB storage.
- [ ] Pro allows 25 GB storage.
- [ ] Free allows 250 MB individual files.
- [ ] Pro allows 2 GB individual files.
- [ ] Product versions are unlimited on both plans.
- [ ] Automatic product updates are available on both plans.
- [ ] Customers are unlimited on both plans.
- [ ] Sales are unlimited on both plans.
- [ ] Basic analytics are available on both plans.
- [ ] Advanced analytics are Pro-only.
- [ ] Affiliates are Pro-only.
- [ ] Platform branding removal is Pro-only.
- [ ] Checkout customization is Pro-only.
- [ ] Plan configuration is centralized.
- [ ] Plan conditionals are not scattered throughout business logic.
- [ ] Access tokens contain minimal `plan_key` authorization context.
- [ ] Access tokens do not contain the full entitlement configuration.
- [ ] Normal authenticated requests do not query the database solely to determine the plan.
- [ ] Refresh synchronizes the current plan context.
- [ ] Plan changes synchronize authorization context.
- [ ] Business limits are enforced in application/service logic.
- [ ] Dynamic resource state is still read from repositories where required.
- [ ] Ownership authorization remains independent of plan authorization.
- [ ] Downgrades never silently delete existing business data.
- [ ] Entitlement failures use stable machine-readable error codes.
- [ ] Monetary calculations are deterministic.
- [ ] Unit tests pass.
- [ ] Integration tests pass.
- [ ] Authorization tests pass.
- [ ] Downgrade tests pass.
- [ ] Stale-token behavior is tested.
- [ ] No-database-plan-lookup behavior is tested.
- [ ] Existing authentication tests continue to pass.

---

# 43. Architectural Decisions

The following decisions are considered fixed for V1 unless this specification is explicitly revised.

### AD-01 — Two plans

V1 contains only:

```text
free
pro
```

### AD-02 — Free remains fully usable

The core product workflow is available on Free.

### AD-03 — Monetization

Free uses a 5% platform fee.

Pro costs $19/month and uses a 1% platform fee.

### AD-04 — Plan context in access token

The access token contains a minimal `plan_key`.

### AD-05 — Server-side subscription source of truth

The JWT is a snapshot, not the authoritative subscription record.

### AD-06 — No per-request plan database lookup

Normal authenticated requests must not query the database solely to discover the user's plan.

### AD-07 — Centralized entitlements

Plan capabilities and limits are resolved through centralized application configuration.

### AD-08 — Service-layer enforcement

Business operations enforce actual entitlements and dynamic resource limits.

### AD-09 — Short-lived access tokens

Access tokens remain short-lived so stale plan context has a bounded lifetime.

### AD-10 — Refresh synchronization

Refresh operations obtain current plan state and issue updated authorization context.

### AD-11 — Downgrades preserve data

Downgrading a plan never silently deletes existing business data.

### AD-12 — Deterministic money

Monetary values and platform fees use deterministic integer/decimal-safe representations.

### AD-13 — Automated testing

Plan authorization, limits, transitions, and interactions with authentication are covered by automated tests.
