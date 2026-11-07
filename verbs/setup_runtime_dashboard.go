package verbs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DashboardProbe checks the actual served health and store epoch. Public mode
// additionally requires the cache contract. Transport may be faked in tests.
var errDashboardReach = errors.New("dashboard could not be reached")

// DashboardNotReady is a dashboard that answered but does not serve this
// sprint yet, and why (a status, a cache header, a body that does not parse
// or names another epoch). Verify returns it with false.
type DashboardNotReady struct{ Why string }

func (d *DashboardNotReady) Error() string { return "dashboard not ready: " + d.Why }

type DashboardProbe struct{ Transport http.RoundTripper }

func (p DashboardProbe) Verify(ctx context.Context, address string, epoch int, public bool) (bool, error) {
	base, err := url.Parse(address)
	if err != nil {
		return false, cause("invalid dashboard address", err)
	}
	if base.Host == "" || base.User != nil || (base.Scheme != "http" && base.Scheme != "https") || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return false, fmt.Errorf("invalid dashboard address %q: want http(s)://host[:port] with no user, path, query or fragment", Redact(address))
	}
	client := http.Client{Transport: p.Transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	healthy, err := dashboardGet(ctx, &client, base, "/healthz", nil)
	if err != nil || !healthy {
		return false, err
	}
	return dashboardGet(ctx, &client, base, "/api/sprint", func(resp *http.Response) string { return dashboardBody(resp, epoch, public) })
}

// dashboardGet is true when GET path answers 200 and check finds nothing
// wrong; false with a *DashboardNotReady saying what was wrong.
func dashboardGet(ctx context.Context, client *http.Client, base *url.URL, path string, check func(*http.Response) string) (bool, error) {
	target := *base
	target.Path = path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("%w (%s)", errDashboardReach, errText(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, &DashboardNotReady{Why: fmt.Sprintf("GET %s answered HTTP %d", path, resp.StatusCode)}
	}
	if check != nil {
		if why := check(resp); why != "" {
			return false, &DashboardNotReady{Why: "GET " + path + ": " + why}
		}
	}
	return true, nil
}

// dashboardBody is "" when the body is this sprint's, else what is wrong.
func dashboardBody(resp *http.Response, epoch int, public bool) string {
	if public && resp.Header.Get("ETag") == "" {
		return "a public dashboard needs an ETag"
	}
	if public && !dashboardCache(resp.Header.Values("Cache-Control")) {
		return fmt.Sprintf("a public dashboard needs Cache-Control max-age=1 and no no-store/no-cache/private, got %q", strings.Join(resp.Header.Values("Cache-Control"), ", "))
	}
	var body struct {
		OK    bool `json:"ok"`
		Epoch int  `json:"epoch"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	switch {
	case err != nil:
		return "reading the body: " + errText(err)
	case len(data) > 4<<20:
		return "the body is over 4 MB"
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return ParseFailure(err, data)
	}
	if !body.OK || body.Epoch != epoch {
		return fmt.Sprintf("ok %v epoch %d, want ok true epoch %d", body.OK, body.Epoch, epoch)
	}
	return ""
}
func dashboardCache(headers []string) bool {
	maxAge := false
	for _, line := range headers {
		for _, part := range strings.Split(line, ",") {
			key, value, hasValue := strings.Cut(strings.TrimSpace(part), "=")
			switch strings.ToLower(key) {
			case "no-store", "no-cache", "private":
				return false
			case "max-age":
				n, err := strconv.Atoi(strings.Trim(value, `"`))
				if !hasValue || err != nil || n != 1 || maxAge {
					return false
				}
				maxAge = true
			}
		}
	}
	return maxAge
}
