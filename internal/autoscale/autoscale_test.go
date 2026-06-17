package autoscale

import "testing"

func TestLabelsMatch(t *testing.T) {
	pool := []string{"self-hosted", "linux", "x64"}
	cases := []struct {
		job  []string
		want bool
	}{
		{[]string{"self-hosted", "linux"}, true},
		{[]string{"self-hosted", "linux", "x64"}, true},
		{[]string{"self-hosted"}, true},
		{[]string{"self-hosted", "windows"}, false}, // windows not on pool
		{[]string{"gpu"}, false},
		{nil, true}, // no requested labels -> any runner matches
	}
	for _, c := range cases {
		if got := labelsMatch(pool, c.job); got != c.want {
			t.Errorf("labelsMatch(%v, %v) = %v, want %v", pool, c.job, got, c.want)
		}
	}
}
