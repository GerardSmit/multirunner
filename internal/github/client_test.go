package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v66/github"

	"github.com/GerardSmit/multirunner/internal/config"
)

// newTestClient points a Client at an httptest server.
func newTestClient(t *testing.T, server *httptest.Server, scope config.Scope, owner, repo string) *Client {
	t.Helper()
	ghc := github.NewClient(nil)
	base, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	ghc.BaseURL = base
	return &Client{gh: ghc, scope: scope, owner: owner, repo: repo}
}

func TestGenerateJITConfig_Scopes(t *testing.T) {
	cases := []struct {
		name     string
		scope    config.Scope
		owner    string
		repo     string
		wantPath string
	}{
		{"repo", config.ScopeRepo, "octo", "hello", "/repos/octo/hello/actions/runners/generate-jitconfig"},
		{"org", config.ScopeOrg, "myorg", "", "/orgs/myorg/actions/runners/generate-jitconfig"},
		{"enterprise", config.ScopeEnterprise, "myent", "", "/enterprises/myent/actions/runners/generate-jitconfig"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if r.URL.Path != tc.wantPath {
					t.Errorf("path = %s, want %s", r.URL.Path, tc.wantPath)
				}
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &gotBody)
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"encoded_jit_config": "BASE64BLOB",
					"runner":             map[string]any{"id": 42, "name": tc.owner + "-runner"},
				})
			}))
			defer srv.Close()

			c := newTestClient(t, srv, tc.scope, tc.owner, tc.repo)
			out, err := c.GenerateJITConfig(context.Background(), JITConfigRequest{
				Name:          "runner-1",
				RunnerGroupID: 1,
				Labels:        []string{"self-hosted", "linux"},
				WorkFolder:    "_work",
			})
			if err != nil {
				t.Fatalf("GenerateJITConfig: %v", err)
			}
			if out.EncodedJITConfig != "BASE64BLOB" {
				t.Errorf("EncodedJITConfig = %q", out.EncodedJITConfig)
			}
			if out.Runner.ID != 42 {
				t.Errorf("Runner.ID = %d, want 42", out.Runner.ID)
			}
			if gotBody["name"] != "runner-1" {
				t.Errorf("body name = %v", gotBody["name"])
			}
			if gotBody["work_folder"] != "_work" {
				t.Errorf("body work_folder = %v", gotBody["work_folder"])
			}
			labels, ok := gotBody["labels"].([]any)
			if !ok || len(labels) != 2 {
				t.Errorf("body labels = %v", gotBody["labels"])
			}
		})
	}
}

func TestGenerateJITConfig_EmptyConfigErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"encoded_jit_config": ""})
	}))
	defer srv.Close()

	c := newTestClient(t, srv, config.ScopeOrg, "myorg", "")
	if _, err := c.GenerateJITConfig(context.Background(), JITConfigRequest{Name: "r", RunnerGroupID: 1}); err == nil {
		t.Fatal("expected error on empty encoded_jit_config")
	}
}

func TestCreateRegistrationToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/myorg/actions/runners/registration-token" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "REGTOKEN"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv, config.ScopeOrg, "myorg", "")
	tok, err := c.CreateRegistrationToken(context.Background())
	if err != nil {
		t.Fatalf("CreateRegistrationToken: %v", err)
	}
	if tok != "REGTOKEN" {
		t.Errorf("token = %q", tok)
	}
}

func TestPATTransportSetsAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := &patTransport{token: "ghp_secret", base: http.DefaultTransport}
	client := &http.Client{Transport: tr}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotAuth != "Bearer ghp_secret" {
		t.Errorf("Authorization = %q, want Bearer ghp_secret", gotAuth)
	}
}

func TestRunnersPath(t *testing.T) {
	c := &Client{scope: config.Scope("bogus")}
	if _, err := c.runnersPath("x"); err == nil {
		t.Fatal("expected error for unsupported scope")
	}
}
