package eval

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

type Finding struct {
	Path, BugClass     string
	Severity           string
	StartLine, EndLine int
}
type ReviewResult struct {
	Findings     []Finding
	CostMicrousd int64
	Latency      time.Duration
}
type Reviewer interface {
	Review(context.Context, Case) (ReviewResult, error)
}
type Metrics struct {
	Precision, Recall float64
	CostMicrousd      int64
	MedianLatency     time.Duration
	Cases             int
}

func Run(ctx context.Context, dataset Dataset, reviewer Reviewer) (Metrics, error) {
	if reviewer == nil {
		return Metrics{}, errors.New("reviewer is required")
	}
	if err := dataset.Validate(); err != nil {
		return Metrics{}, err
	}
	var matchedFindings, totalFindings, matchedLabels, totalLabels int
	var totalCost int64
	latencies := make([]time.Duration, 0, len(dataset.Cases))
	for _, benchmark := range dataset.Cases {
		result, err := reviewer.Review(ctx, benchmark)
		if err != nil {
			return Metrics{}, fmt.Errorf("review case %s: %w", benchmark.ID, err)
		}
		totalCost += result.CostMicrousd
		latencies = append(latencies, result.Latency)
		totalFindings += len(result.Findings)
		totalLabels += len(benchmark.Labels)
		for _, finding := range result.Findings {
			if matchesAny(finding, benchmark.Labels) {
				matchedFindings++
			}
		}
		for _, label := range benchmark.Labels {
			if labelMatched(label, result.Findings) {
				matchedLabels++
			}
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	metrics := Metrics{CostMicrousd: totalCost, Cases: len(dataset.Cases)}
	if totalFindings > 0 {
		metrics.Precision = float64(matchedFindings) / float64(totalFindings)
	}
	if totalLabels > 0 {
		metrics.Recall = float64(matchedLabels) / float64(totalLabels)
	}
	if len(latencies) > 0 {
		metrics.MedianLatency = latencies[len(latencies)/2]
		if len(latencies)%2 == 0 {
			metrics.MedianLatency = (latencies[len(latencies)/2-1] + metrics.MedianLatency) / 2
		}
	}
	return metrics, nil
}

func GateRegression(current, baseline Metrics, maxDrop float64) error {
	if maxDrop <= 0 {
		maxDrop = .03
	}
	if baseline.Precision-current.Precision > maxDrop {
		return fmt.Errorf("precision regressed %.1f points", (baseline.Precision-current.Precision)*100)
	}
	if baseline.Recall-current.Recall > maxDrop {
		return fmt.Errorf("recall regressed %.1f points", (baseline.Recall-current.Recall)*100)
	}
	return nil
}

func matchesAny(f Finding, labels []Label) bool {
	for _, l := range labels {
		if overlap(f, l) {
			return true
		}
	}
	return false
}
func labelMatched(l Label, findings []Finding) bool {
	for _, f := range findings {
		if overlap(f, l) {
			return true
		}
	}
	return false
}
func overlap(f Finding, l Label) bool {
	return f.Path == l.Path && f.BugClass == l.BugClass && f.StartLine <= l.EndLine && l.StartLine <= f.EndLine
}
