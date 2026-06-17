package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/GerardSmit/multirunner/internal/config"
	"github.com/GerardSmit/multirunner/internal/ghapp"
)

// connectCmd runs the GitHub App manifest flow and writes the resulting App
// credentials into the config file (App auth, no PAT).
func connectCmd(cfgPath, org, repo, name string, port int, keyOut string) error {
	var scope config.Scope
	var owner, repoName string
	switch {
	case org != "":
		scope, owner = config.ScopeOrg, org
	case repo != "":
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("--repo must be owner/repo")
		}
		scope, owner, repoName = config.ScopeRepo, parts[0], parts[1]
	default:
		return fmt.Errorf("specify --org <org> or --repo <owner/repo>")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	creds, err := ghapp.Connect(ctx, ghapp.Options{
		Scope: string(scope), Org: org, Owner: owner, Repo: repoName, Name: name, Port: port,
	})
	if err != nil {
		return err
	}

	if keyOut == "" {
		keyOut = filepath.Join(filepath.Dir(cfgPath), "multirunner-app.private-key.pem")
	}
	if err := os.WriteFile(keyOut, []byte(creds.PEM), 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}
	if err := config.WriteAppAuth(cfgPath, scope, owner, repoName, creds.AppID, creds.InstallationID, keyOut); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Println()
	fmt.Printf("Connected. App %q (id=%d) installed (installation=%d).\n", creds.Slug, creds.AppID, creds.InstallationID)
	fmt.Printf("  private key : %s\n", keyOut)
	fmt.Printf("  config      : %s (auth set to App; pat removed)\n", cfgPath)
	if creds.WebhookSecret != "" {
		fmt.Printf("  webhook secret (save for provisioning: webhook): %s\n", creds.WebhookSecret)
	}
	fmt.Printf("  app settings: %s\n", creds.HTMLURL)
	fmt.Println("\nRun:  multirunner run -config " + cfgPath)
	return nil
}
