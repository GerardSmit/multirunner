package backend

import "testing"

func TestCacheHost(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"hostname", map[string]string{"ACTIONS_RESULTS_URL": "http://host.docker.internal:3000/"}, "host.docker.internal"},
		{"bare ip skipped", map[string]string{"ACTIONS_RESULTS_URL": "http://10.0.0.5:3000/"}, ""},
		{"no cache", map[string]string{"OTHER": "x"}, ""},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		if got := CacheHost(c.env); got != c.want {
			t.Errorf("%s: CacheHost = %q, want %q", c.name, got, c.want)
		}
	}
}
