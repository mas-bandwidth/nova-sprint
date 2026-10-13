package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// checkProvider is the provider kind's Check, the same function as nova-tools
// pkg/config/provider.go at 41430ca9f0713318a3e1353eb8c9406455bc7a0a: a balance
// names its date, the date parses, and a console URL is https without credentials.
func checkProvider(r Row) error {
	var problems []string
	date := r.Fields["balance_as_of"]
	if r.Fields["balance_usd"] != "" && date == "" {
		problems = append(problems, "--balance_usd needs --balance_as_of YYYY-MM-DD")
	}
	if date != "" {
		if _, err := time.Parse(time.DateOnly, date); err != nil {
			problems = append(problems, fmt.Sprintf("--balance_as_of %q wants YYYY-MM-DD", date))
		}
	}
	if raw := r.Fields["console_url"]; raw != "" {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			problems = append(problems, "--console_url wants an https:// console URL without credentials, or empty")
		}
	}
	if len(problems) != 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}
