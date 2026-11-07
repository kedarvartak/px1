package eval

import (
	"context"
	"fmt"
)

// Launch thresholds for an agentic tier against the Base pipeline.
const (
	MinTierRecallLift    = 0.20 // relative: Plus recall must be at least 1.2 × Base recall
	MaxTierCostMultiple  = 3.0
	MaxTierPrecisionDrop = 0.03
)

type TierComparison struct {
	Base, Candidate Metrics
	RecallLift      float64
	CostMultiple    float64
	Passed          bool
	Reasons         []string
}

// GateTierLaunch runs Base and a candidate tier on the same v2 seeded-bug
// benchmark and passes only if the candidate improves recall by at least 20%
// relative to Base, costs at most 3× Base, and does not give up more than
// three points of precision. Recall gains bought with noise do not count.
// A comparison that cannot be computed fails rather than passing by default.
func GateTierLaunch(ctx context.Context, dataset Dataset, base, candidate Reviewer) (TierComparison, error) {
	if base == nil || candidate == nil {
		return TierComparison{}, fmt.Errorf("base and candidate reviewers are required")
	}
	if err := dataset.ValidateV2Coverage(); err != nil {
		return TierComparison{}, fmt.Errorf("tier gate needs the full seeded benchmark: %w", err)
	}
	baseMetrics, err := Run(ctx, dataset, base)
	if err != nil {
		return TierComparison{}, fmt.Errorf("run base tier: %w", err)
	}
	candidateMetrics, err := Run(ctx, dataset, candidate)
	if err != nil {
		return TierComparison{}, fmt.Errorf("run candidate tier: %w", err)
	}
	return CompareTiers(baseMetrics, candidateMetrics), nil
}

func CompareTiers(base, candidate Metrics) TierComparison {
	result := TierComparison{Base: base, Candidate: candidate, Passed: true}
	fail := func(format string, args ...any) {
		result.Passed = false
		result.Reasons = append(result.Reasons, fmt.Sprintf(format, args...))
	}
	if base.Cases == 0 || base.Cases != candidate.Cases {
		fail("tiers must be measured on the same non-empty case set (base %d, candidate %d)", base.Cases, candidate.Cases)
		return result
	}
	if base.Recall <= 0 {
		fail("base recall is zero, so a relative recall lift cannot be measured")
	} else {
		result.RecallLift = candidate.Recall/base.Recall - 1
		if result.RecallLift < MinTierRecallLift-1e-9 {
			fail("recall lift %.1f%% is below the required %.0f%% (base %.3f, candidate %.3f)", result.RecallLift*100, MinTierRecallLift*100, base.Recall, candidate.Recall)
		}
	}
	if base.CostMicrousd <= 0 {
		fail("base cost is zero, so a cost multiple cannot be measured")
	} else {
		result.CostMultiple = float64(candidate.CostMicrousd) / float64(base.CostMicrousd)
		if result.CostMultiple > MaxTierCostMultiple+1e-9 {
			fail("cost is %.2f× base; the limit is %.0f×", result.CostMultiple, MaxTierCostMultiple)
		}
	}
	if drop := base.Precision - candidate.Precision; drop > MaxTierPrecisionDrop+1e-9 {
		fail("precision dropped %.1f points (base %.3f, candidate %.3f); the limit is %.0f", drop*100, base.Precision, candidate.Precision, MaxTierPrecisionDrop*100)
	}
	return result
}
