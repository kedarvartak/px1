package publicsite

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	MaxStatusComponents = 12
	statusProbeTimeout  = 2 * time.Second
)

type ComponentState string

const (
	Operational ComponentState = "operational"
	Unavailable ComponentState = "unavailable"
)

type Component struct {
	Name     string
	ReadyURL string
}

type ComponentStatus struct {
	Name  string         `json:"name"`
	State ComponentState `json:"state"`
}

// Status deliberately carries no error text, latency, hostnames, or tenant
// information: it is served without authentication.
type Status struct {
	Overall    ComponentState    `json:"overall"`
	Components []ComponentStatus `json:"components"`
	CheckedAt  time.Time         `json:"checkedAt"`
}

// ParseStatusComponents reads PX1_STATUS_COMPONENTS, a comma-separated list of
// name=readiness-URL pairs such as "webhook=https://hook.px1.dev/readyz".
func ParseStatusComponents(value string) ([]Component, error) {
	var components []Component
	seen := map[string]bool{}
	for _, pair := range strings.Split(value, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		name, raw, ok := strings.Cut(pair, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" || seen[name] {
			return nil, fmt.Errorf("status component %q must be a unique name=URL pair", pair)
		}
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopback(parsed.Hostname()))) {
			return nil, fmt.Errorf("status component %q needs an https URL (http only on loopback) without credentials", name)
		}
		seen[name] = true
		components = append(components, Component{Name: name, ReadyURL: parsed.String()})
	}
	if len(components) > MaxStatusComponents {
		return nil, fmt.Errorf("at most %d status components are supported", MaxStatusComponents)
	}
	return components, nil
}

type StatusChecker struct {
	Components []Component
	Client     *http.Client
	Now        func() time.Time
}

// Check probes every component concurrently. A component is operational only
// when its readiness endpoint answers 2xx within the timeout; with no
// configured components the overall state is unavailable rather than a
// misleading "all systems operational".
func (c StatusChecker) Check(ctx context.Context) Status {
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: statusProbeTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	results := make([]ComponentStatus, len(c.Components))
	var wg sync.WaitGroup
	for i, component := range c.Components {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = ComponentStatus{Name: component.Name, State: probe(ctx, client, component.ReadyURL)}
		}()
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	overall := Operational
	if len(results) == 0 {
		overall = Unavailable
	}
	for _, result := range results {
		if result.State != Operational {
			overall = Unavailable
		}
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	return Status{Overall: overall, Components: results, CheckedAt: now().UTC()}
}

func (c StatusChecker) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := c.Check(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(status)
	})
}

func probe(ctx context.Context, client *http.Client, target string) ComponentState {
	ctx, cancel := context.WithTimeout(ctx, statusProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Unavailable
	}
	response, err := client.Do(request)
	if err != nil {
		return Unavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return Unavailable
	}
	return Operational
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
