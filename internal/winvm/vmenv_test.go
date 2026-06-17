package winvm

import "testing"

func TestVMEnvRewritesCacheHost(t *testing.T) {
	out := vmEnv(map[string]string{
		"ACTIONS_RESULTS_URL": "http://host.docker.internal:3000/",
		"ACTIONS_CACHE_URL":   "http://host.docker.internal:3000/",
		"OTHER":               "keep",
	})
	if got := out["ACTIONS_RESULTS_URL"]; got != "http://10.0.2.2:3000/" {
		t.Errorf("ACTIONS_RESULTS_URL = %q", got)
	}
	if got := out["ACTIONS_CACHE_URL"]; got != "http://10.0.2.2:3000/" {
		t.Errorf("ACTIONS_CACHE_URL = %q", got)
	}
	if out["OTHER"] != "keep" {
		t.Error("non-cache env must be preserved")
	}
}

func TestVMEnvLeavesBareIP(t *testing.T) {
	out := vmEnv(map[string]string{"ACTIONS_RESULTS_URL": "http://192.168.1.5:3000/"})
	if got := out["ACTIONS_RESULTS_URL"]; got != "http://192.168.1.5:3000/" {
		t.Errorf("bare IP rewritten: %q", got)
	}
}
