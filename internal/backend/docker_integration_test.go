package backend

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestDockerPing connects to a real daemon when MULTIRUNNER_TEST_DOCKER_HOST is
// set (Docker or Podman docker-compat). Skipped otherwise.
func TestDockerPing(t *testing.T) {
	host := os.Getenv("MULTIRUNNER_TEST_DOCKER_HOST")
	if host == "" {
		t.Skip("set MULTIRUNNER_TEST_DOCKER_HOST to run")
	}
	be, err := NewDockerLinux(host)
	if err != nil {
		t.Fatalf("NewDockerLinux: %v", err)
	}
	defer be.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := be.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	t.Logf("daemon reachable via %s", host)
}
