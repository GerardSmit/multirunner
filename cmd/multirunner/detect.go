package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/GerardSmit/multirunner/internal/config"
	"github.com/GerardSmit/multirunner/internal/detect"
	"github.com/GerardSmit/multirunner/internal/github"
)

// detectCmd scans a repo and recommends runner image flavors + a pools block.
func detectCmd(cfgPath *string) *cobra.Command {
	var scanPath, repo, osName string
	c := &cobra.Command{
		Use:   "detect",
		Short: "Scan a repo and recommend runner image flavors + a pools config",
		Long: `Inspect a repository's project files (and .github/workflows) and recommend which
prebuilt runner image flavors its CI likely needs, printing a ready-to-paste pools
block. This is a setup-time helper; job routing stays label-based, so add the
suggested flavor label to a job's runs-on to send it to that pool.

--path scans a local checkout (no network). --repo owner/name scans a remote repo
via the GitHub API using the auth from --config.`,
		Example: `  multirunner detect --path .
  multirunner detect --repo octo/hello --config config.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if osName != "linux" && osName != "windows" {
				return fmt.Errorf("--os must be linux|windows, got %q", osName)
			}
			src, err := detectSource(cmd.Context(), scanPath, repo, cfgPath)
			if err != nil {
				return err
			}
			res, err := detect.Scan(src)
			if err != nil {
				return err
			}
			fmt.Print(detect.Render(res, osName))
			return nil
		},
	}
	c.Flags().StringVar(&scanPath, "path", ".", "local checkout to scan")
	c.Flags().StringVar(&repo, "repo", "", "scan a remote repo via the GitHub API (owner/name)")
	c.Flags().StringVar(&osName, "os", "linux", "OS the pools target: linux|windows")
	return c
}

// detectSource builds a path- or repo-backed detect.Source. --repo takes
// precedence when set.
func detectSource(ctx context.Context, scanPath, repo string, cfgPath *string) (detect.Source, error) {
	if repo == "" {
		abs, err := filepath.Abs(scanPath)
		if err != nil {
			return nil, err
		}
		return detect.DirSource{Root: abs}, nil
	}

	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return nil, fmt.Errorf("--repo must be owner/name, got %q", repo)
	}
	abs, err := filepath.Abs(*cfgPath)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(abs)
	if err != nil {
		return nil, fmt.Errorf("load config (needed for GitHub auth): %w", err)
	}
	gh, err := github.New(ctx, config.GitHub{
		URL:   cfg.GitHub.URL,
		Scope: config.ScopeRepo,
		Owner: owner,
		Repo:  name,
	}, cfg.Auth)
	if err != nil {
		return nil, fmt.Errorf("github client: %w", err)
	}
	return ghSource{ctx: ctx, c: gh}, nil
}

// ghSource adapts a repo-scoped GitHub client to detect.Source.
type ghSource struct {
	ctx context.Context
	c   *github.Client
}

func (s ghSource) Paths() ([]string, error)          { return s.c.RepoFilePaths(s.ctx) }
func (s ghSource) ReadFile(p string) ([]byte, error) { return s.c.RepoFile(s.ctx, p) }
