// Package github wraps the GitHub REST API calls multirunner needs:
// JIT runner config generation and registration tokens, across repo / org /
// enterprise scopes, authenticated by either a PAT or a GitHub App.
package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v66/github"

	"github.com/GerardSmit/multirunner/internal/config"
)

// Client talks to GitHub for a single configured scope.
type Client struct {
	gh    *github.Client
	scope config.Scope
	owner string // org name, repo owner, or enterprise slug
	repo  string // only for repo scope
}

// JITConfigRequest is the input for generate-jitconfig.
type JITConfigRequest struct {
	Name          string
	RunnerGroupID int64
	Labels        []string
	WorkFolder    string
}

// JITConfig is the relevant part of the generate-jitconfig response.
type JITConfig struct {
	EncodedJITConfig string `json:"encoded_jit_config"`
	Runner           struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"runner"`
}

// New builds a Client from config, selecting PAT or App auth and honoring a
// GHES base URL when github.url is not github.com.
func New(ctx context.Context, gh config.GitHub, auth config.Auth) (*Client, error) {
	httpClient, err := authHTTPClient(ctx, gh, auth)
	if err != nil {
		return nil, err
	}

	var ghc *github.Client
	if isDotCom(gh.URL) {
		ghc = github.NewClient(httpClient)
	} else {
		// GHES: REST API lives under <url>/api/v3/.
		ghc, err = github.NewClient(httpClient).WithEnterpriseURLs(gh.URL, gh.URL)
		if err != nil {
			return nil, fmt.Errorf("enterprise urls: %w", err)
		}
	}

	return &Client{gh: ghc, scope: gh.Scope, owner: gh.Owner, repo: gh.Repo}, nil
}

func authHTTPClient(ctx context.Context, gh config.GitHub, auth config.Auth) (*http.Client, error) {
	if auth.PAT != "" {
		return &http.Client{
			Timeout:   30 * time.Second,
			Transport: &patTransport{token: auth.PAT, base: http.DefaultTransport},
		}, nil
	}

	apiBase := "https://api.github.com/"
	if !isDotCom(gh.URL) {
		apiBase = strings.TrimRight(gh.URL, "/") + "/api/v3/"
	}
	itr, err := ghinstallation.NewKeyFromFile(http.DefaultTransport, auth.AppID, auth.InstallationID, auth.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("github app key: %w", err)
	}
	itr.BaseURL = strings.TrimRight(apiBase, "/")
	return &http.Client{Timeout: 30 * time.Second, Transport: itr}, nil
}

// patTransport injects a bearer token on every request.
type patTransport struct {
	token string
	base  http.RoundTripper
}

func (t *patTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(r)
}

// GenerateJITConfig requests a single-use JIT config for the configured scope.
func (c *Client) GenerateJITConfig(ctx context.Context, in JITConfigRequest) (*JITConfig, error) {
	body := map[string]any{
		"name":            in.Name,
		"runner_group_id": in.RunnerGroupID,
		"labels":          in.Labels,
	}
	if in.WorkFolder != "" {
		body["work_folder"] = in.WorkFolder
	}

	path, err := c.runnersPath("generate-jitconfig")
	if err != nil {
		return nil, err
	}
	req, err := c.gh.NewRequest(http.MethodPost, path, body)
	if err != nil {
		return nil, fmt.Errorf("build jitconfig request: %w", err)
	}
	var out JITConfig
	resp, err := c.gh.Do(ctx, req, &out)
	if err != nil {
		return nil, fmt.Errorf("generate-jitconfig (%s): %w", c.scope, err)
	}
	if out.EncodedJITConfig == "" {
		return nil, fmt.Errorf("generate-jitconfig returned empty config (status %d)", resp.StatusCode)
	}
	return &out, nil
}

// CreateRegistrationToken returns a short-lived registration token (config.sh
// fallback path when JIT is unavailable).
func (c *Client) CreateRegistrationToken(ctx context.Context) (string, error) {
	path, err := c.runnersPath("registration-token")
	if err != nil {
		return "", err
	}
	req, err := c.gh.NewRequest(http.MethodPost, path, nil)
	if err != nil {
		return "", fmt.Errorf("build registration-token request: %w", err)
	}
	var out struct {
		Token string `json:"token"`
	}
	if _, err := c.gh.Do(ctx, req, &out); err != nil {
		return "", fmt.Errorf("registration-token (%s): %w", c.scope, err)
	}
	return out.Token, nil
}

// CountQueuedRuns returns the number of queued workflow runs (a proxy for queued
// jobs) for repo scope. Org/enterprise scope returns 0 (no cheap REST endpoint;
// use webhook mode there).
func (c *Client) CountQueuedRuns(ctx context.Context) (int, error) {
	if c.scope != config.ScopeRepo {
		return 0, nil
	}
	path := fmt.Sprintf("repos/%s/%s/actions/runs?status=queued&per_page=1",
		url.PathEscape(c.owner), url.PathEscape(c.repo))
	req, err := c.gh.NewRequest(http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}
	var out struct {
		TotalCount int `json:"total_count"`
	}
	if _, err := c.gh.Do(ctx, req, &out); err != nil {
		return 0, fmt.Errorf("list queued runs: %w", err)
	}
	return out.TotalCount, nil
}

// Scope reports the configured scope.
func (c *Client) Scope() config.Scope { return c.scope }

// runnersPath builds the actions/runners sub-path for the configured scope.
func (c *Client) runnersPath(action string) (string, error) {
	switch c.scope {
	case config.ScopeRepo:
		return fmt.Sprintf("repos/%s/%s/actions/runners/%s",
			url.PathEscape(c.owner), url.PathEscape(c.repo), action), nil
	case config.ScopeOrg:
		return fmt.Sprintf("orgs/%s/actions/runners/%s", url.PathEscape(c.owner), action), nil
	case config.ScopeEnterprise:
		return fmt.Sprintf("enterprises/%s/actions/runners/%s", url.PathEscape(c.owner), action), nil
	default:
		return "", fmt.Errorf("unsupported scope %q", c.scope)
	}
}

func isDotCom(rawURL string) bool {
	if rawURL == "" {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	return host == "github.com" || host == "www.github.com" || host == ""
}
