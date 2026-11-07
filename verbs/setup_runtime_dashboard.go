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

type DashboardProbe struct{ Transport http.RoundTripper }

func (p DashboardProbe) Verify(ctx context.Context, address string, epoch int, public bool) (bool, error) {
	base, err := url.Parse(address)
	if err != nil || base.Host == "" || base.User != nil || (base.Scheme != "http" && base.Scheme != "https") || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return false, fmt.Errorf("invalid dashboard address")
	}
	client := http.Client{Transport: p.Transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	healthy, err := dashboardGet(ctx, &client, base, "/healthz", nil)
	if err != nil || !healthy {
		return false, err
	}
	return dashboardGet(ctx, &client, base, "/api/sprint", func(resp *http.Response) bool { return dashboardBody(resp, epoch, public) })
}
func dashboardGet(ctx context.Context, client *http.Client, base *url.URL, path string, check func(*http.Response) bool) (bool, error) {
	target := *base
	target.Path = path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("%w: GET %s: %v", errDashboardReach, target.String(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("GET %s: HTTP %d", target.String(), resp.StatusCode)
	}
	if check != nil {
		if !check(resp) {
			return false, fmt.Errorf("GET %s: body or headers not as the dashboard contract requires (ETag=%q Cache-Control=%q)", target.String(), resp.Header.Get("ETag"), resp.Header.Get("Cache-Control"))
		}
		return true, nil
	}
	return true, nil
}
func dashboardBody(resp *http.Response, epoch int, public bool) bool {
	if public && (resp.Header.Get("ETag") == "" || !dashboardCache(resp.Header.Values("Cache-Control"))) {
		return false
	}
	// LOCAL DIAGNOSTIC: the dashboard's /api/sprint envelope is {build, data:{epoch,...}, attemptAt,...}
	// (dash/server.go payload), not {ok, epoch}; decode the shape the dash really serves.
	var body struct {
		Data struct {
			Epoch int `json:"epoch"`
		} `json:"data"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	return err == nil && len(data) <= 4<<20 && json.Unmarshal(data, &body) == nil && body.Data.Epoch == epoch
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
