package dash

import (
	"fmt"
	"net/url"
	"strings"
)

// SprintDSN requires a passwordless URL and selects the sprint database role.
func SprintDSN(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return "", fmt.Errorf("dashboard: configure a passwordless NOVA_PG_DSN")
	}
	if u.User != nil {
		if _, has := u.User.Password(); has {
			return "", fmt.Errorf("dashboard: configure a passwordless NOVA_PG_DSN")
		}
	}
	for key := range u.Query() {
		if strings.EqualFold(key, "user") || strings.EqualFold(key, "password") {
			return "", fmt.Errorf("dashboard: configure a passwordless NOVA_PG_DSN")
		}
	}
	u.User = url.User("nova_sprint")
	return u.String(), nil
}
