package cache

import "strings"

// RunnerEnv returns the environment variables to inject into each runner
// container so actions/cache (v2) targets this self-hosted server instead of
// GitHub's Azure backend. advertiseURL must be reachable from inside the
// container (e.g. http://host.docker.internal:3000).
func RunnerEnv(advertiseURL string) map[string]string {
	base := strings.TrimRight(advertiseURL, "/") + "/"
	return map[string]string{
		"ACTIONS_RESULTS_URL":      base,
		"ACTIONS_CACHE_URL":        base,
		"ACTIONS_CACHE_SERVICE_V2": "true",
	}
}
