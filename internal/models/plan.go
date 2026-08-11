package models

// PLAN is the numeric identifier of a subscription tier. Higher values
// represent progressively more expensive tiers.
type PLAN int

// The available plan tiers, in increasing order of price.
const (
	FreePlan PLAN = iota
	CreatorPlan
	ProPlan
)

// Plan describes a subscription tier with its display Name, Tier level,
// monthly CostPerMonth, and the PlatformFee charged by the platform.
type Plan struct {
	Name         string
	Tier         PLAN
	CostPerMonth int
	PlatformFee  int
}

// String returns the lowercase display name of the tier. It returns
// "unknown plan" when p does not match any known tier.
func (p PLAN) String() string {
	switch p {
	case FreePlan:
		return "free"
	case CreatorPlan:
		return "creator"
	case ProPlan:
		return "pro"
	default:
		return "unknown plan"
	}
}

// CreatePlan builds a Plan for the given tier using that tier's fixed pricing.
// For an unknown tier it returns an empty Plan.
func CreatePlan(plan PLAN) *Plan {
	switch plan {
	case FreePlan:
		return &Plan{Name: plan.String(), Tier: plan, CostPerMonth: 0, PlatformFee: 10}
	case CreatorPlan:
		return &Plan{Name: plan.String(), Tier: plan, CostPerMonth: 10, PlatformFee: 3}
	case ProPlan:
		return &Plan{Name: plan.String(), Tier: plan, CostPerMonth: 30, PlatformFee: 0}
	default:
		return &Plan{}
	}
}
