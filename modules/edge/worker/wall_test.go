package worker

import "testing"

// The worker's report carries the run's wall for every harness and result:
// taken at 1000, reported at 1612 is wall=612, beside the tokens or alone.
func TestReportCarriesWall(t *testing.T) {
	t.Parallel()
	cases := []struct{ words, want string }{
		{"input=10 output=5 model=deepseek/deepseek-flash", "input=10 output=5 model=deepseek/deepseek-flash wall=612"},
		{"", "wall=612"},
		{"input=lots", "input=lots"}, // words that do not read are the harness's to answer for
	}
	for _, c := range cases {
		if got := withWall(c.words, 1000, 1612); got != c.want {
			t.Fatalf("withWall(%q) = %q, want %q", c.words, got, c.want)
		}
	}
	if got := withWall("output=5", 0, 1612); got != "output=5" {
		t.Fatalf("no start is no wall: %q", got)
	}
}
