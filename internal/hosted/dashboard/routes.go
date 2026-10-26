// Package dashboard defines provider-neutral contracts for the hosted UI.
package dashboard

type Route struct {
	Name       string
	Pattern    string
	Navigation bool
}

var Routes = []Route{
	{Name: "overview", Pattern: "/:organization", Navigation: true},
	{Name: "repositories", Pattern: "/:organization/repositories", Navigation: true},
	{Name: "pull requests", Pattern: "/:organization/pull-requests", Navigation: true},
	{Name: "pull request", Pattern: "/:organization/pull-requests/:number"},
	{Name: "rules", Pattern: "/:organization/rules", Navigation: true},
	{Name: "memories", Pattern: "/:organization/memories", Navigation: true},
	{Name: "analytics", Pattern: "/:organization/analytics", Navigation: true},
	{Name: "billing", Pattern: "/:organization/billing", Navigation: true},
	{Name: "members", Pattern: "/:organization/members", Navigation: true},
	{Name: "settings", Pattern: "/:organization/settings", Navigation: true},
}
