package run

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// StreamSettings is the policy-data projection passed to the pure planner.
type StreamSettings struct {
	Repo string `json:"repo"`
	Base string `json:"base"`
	PR   bool   `json:"pr"`
	Mode string `json:"mode"`
}
type streamPolicy struct {
	version int
	body    string
}

func collectStreamPolicies(dst map[string]streamPolicy, events []store.Event) {
	for _, event := range events {
		if !event.Accepted || event.After == nil {
			continue
		}
		for _, policy := range event.After.Policies {
			if policy.Kind != "data" || !strings.HasPrefix(policy.Name, "stream:") {
				continue
			}
			name := strings.TrimPrefix(policy.Name, "stream:")
			if prior, ok := dst[name]; !ok || policy.Version > prior.version {
				dst[name] = streamPolicy{version: policy.Version, body: policy.Body}
			}
		}
	}
}

// The snapshot omits policy data, so recover its bounded prefix once at startup.
func loadPolicyHistory(ctx context.Context, s store.Store, target int64) (map[int]store.PolicyVersion, error) {
	out := map[int]store.PolicyVersion{}
	for seq := int64(0); seq < target; {
		events, err := s.Events(ctx, seq, store.MaxEvents)
		if err != nil {
			return nil, fmt.Errorf("run: stream policy history: %w", err)
		}
		if len(events) == 0 {
			return nil, fmt.Errorf("run: stream policy history ended before seq %d", target)
		}
		for _, event := range events {
			if event.Seq != seq+1 {
				return nil, fmt.Errorf("run: stream policy history sequence gap")
			}
			if event.Seq > target {
				break
			}
			if event.Accepted && event.After != nil {
				for version, policy := range event.After.Policies {
					out[version] = policy
				}
			}
			seq = event.Seq
		}
	}
	return out, nil
}
func loadStreamPolicies(ctx context.Context, s store.Store, target int64) (map[string]streamPolicy, error) {
	policies, err := loadPolicyHistory(ctx, s, target)
	if err != nil {
		return nil, err
	}
	out := map[string]streamPolicy{}
	collectStreamPolicies(out, []store.Event{{Accepted: true, After: &store.State{Policies: policies}}})
	return out, nil
}
func streamSettingsFor(s store.Snapshot, data map[string]streamPolicy) (map[string]StreamSettings, error) {
	result := map[string]StreamSettings{}
	for name := range s.Streams {
		p, ok := data[name]
		if !ok {
			continue
		}
		var settings StreamSettings
		if err := json.Unmarshal([]byte(p.body), &settings); err != nil {
			return nil, fmt.Errorf("run: invalid stream settings for %s", name)
		}
		result[name] = settings
	}
	return result, nil
}
