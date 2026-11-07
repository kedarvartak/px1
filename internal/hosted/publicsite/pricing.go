// Package publicsite defines the public, tenant-free surfaces of the hosted product:
// pricing, the changelog, and the status page. Nothing here may read or expose
// installation data.
package publicsite

import (
	"errors"
	"fmt"

	"px1/internal/hosted/billing"
)

// StandardReviewCredits is the price of one standard (Base) review. Review
// tiers arrive with the agentic engine and must be added here explicitly.
const StandardReviewCredits = 1

type Plan struct {
	Slug                   string   `json:"slug"`
	Name                   string   `json:"name"`
	Audience               string   `json:"audience"`
	MonthlyIncludedCredits int64    `json:"monthlyIncludedCredits"`
	MicrousdPerCredit      int64    `json:"microusdPerCredit"`
	SeatFees               bool     `json:"seatFees"`
	ContactSales           bool     `json:"contactSales"`
	Includes               []string `json:"includes"`
}

// Plans is the single source of truth for public pricing. The dashboard reads
// web/dashboard/lib/pricing.json, which a test keeps identical to this table.
var Plans = []Plan{
	{Slug: "starter", Name: "Starter", Audience: "One developer", MonthlyIncludedCredits: 50, Includes: []string{
		"50 review credits each month", "Commit-pinned review reports", "Repository rules and memory controls",
	}},
	{Slug: "pro", Name: "Pro", Audience: "Teams", MicrousdPerCredit: 800_000, Includes: []string{
		"Pay as you go; credits are shared across the organization", "No seat fees", "Dashboard, analytics, Slack, Linear, and Jira",
	}},
	{Slug: "oss", Name: "Open source", Audience: "Qualified public repositories", Includes: []string{
		"Free credits after an approved application", "The same review pipeline as Pro",
	}},
	{Slug: "enterprise", Name: "Enterprise", Audience: "Regulated organizations", ContactSales: true, Includes: []string{
		"Annual contract", "Self-hosting, SSO, audit logs, and an SLA as they ship",
	}},
}

// MaxStandardReviewCostMicrousd is the largest model cost a paid standard
// review may incur while staying at or above the hosted margin guardrail.
func MaxStandardReviewCostMicrousd(plan Plan) int64 {
	revenue := plan.MicrousdPerCredit * StandardReviewCredits
	return int64(float64(revenue) * (1 - billing.DefaultMinimumMargin))
}

func ValidatePlans(plans []Plan) error {
	if len(plans) == 0 {
		return errors.New("at least one plan is required")
	}
	seen := map[string]bool{}
	paid := 0
	for _, plan := range plans {
		if plan.Slug == "" || plan.Name == "" || plan.Audience == "" || len(plan.Includes) == 0 || seen[plan.Slug] {
			return fmt.Errorf("plan %q must have a unique slug, name, audience, and inclusions", plan.Slug)
		}
		seen[plan.Slug] = true
		if plan.MicrousdPerCredit < 0 || plan.MonthlyIncludedCredits < 0 {
			return fmt.Errorf("plan %q has a negative price or allowance", plan.Slug)
		}
		if plan.ContactSales && plan.MicrousdPerCredit > 0 {
			return fmt.Errorf("plan %q cannot publish a price and require contacting sales", plan.Slug)
		}
		if plan.MicrousdPerCredit > 0 {
			paid++
			if plan.SeatFees {
				return fmt.Errorf("plan %q is credit priced and must not also charge seats", plan.Slug)
			}
		}
	}
	if paid == 0 {
		return errors.New("at least one self-serve paid plan is required")
	}
	return nil
}
