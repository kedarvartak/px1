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
	{Name: "repository settings", Pattern: "/:organization/repositories/:repository/settings"},
	{Name: "pull requests", Pattern: "/:organization/pull-requests", Navigation: true},
	{Name: "pull request", Pattern: "/:organization/repositories/:repository/pull-requests/:number"},
	{Name: "rules", Pattern: "/:organization/rules", Navigation: true},
	{Name: "memories", Pattern: "/:organization/memories", Navigation: true},
	{Name: "analytics", Pattern: "/:organization/analytics", Navigation: true},
	{Name: "billing", Pattern: "/:organization/billing", Navigation: true},
	{Name: "members", Pattern: "/:organization/members", Navigation: true},
	{Name: "settings", Pattern: "/:organization/settings", Navigation: true},
}

// PublicPrefix holds pages served without a session. Organization routes use
// the GitHub or GitLab login as their first segment, and neither provider
// allows a login that starts with "-", so "/-/" can never shadow a tenant.
const PublicPrefix = "/-/"

var PublicRoutes = []Route{
	{Name: "pricing", Pattern: "/-/pricing"},
	{Name: "docs", Pattern: "/-/docs"},
	{Name: "changelog", Pattern: "/-/changelog"},
	{Name: "status", Pattern: "/-/status"},
}
