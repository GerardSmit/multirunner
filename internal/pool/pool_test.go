package pool

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/GerardSmit/multirunner/internal/backend"
	"github.com/GerardSmit/multirunner/internal/config"
)

type failImageBackend struct{}

func (failImageBackend) Name() string                              { return "fail-image" }
func (failImageBackend) Ping(context.Context) error                { return nil }
func (failImageBackend) OSType(context.Context) (string, error)    { return "linux", nil }
func (failImageBackend) EnsureImage(context.Context, string) error { return errors.New("pull failed") }
func (failImageBackend) Launch(context.Context, backend.LaunchRequest) (backend.RunnerHandle, error) {
	return nil, errors.New("not reached")
}
func (failImageBackend) Close() error { return nil }

func TestManagerReturnsPoolStartupError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	l := NewLauncher(
		config.Pool{Name: "linux", OS: "linux", Size: 1, MaxConsecutiveFailures: 1},
		"missing:image",
		failImageBackend{},
		nil,
		nil,
		nil,
		logger,
		Hooks{},
	)
	err := NewManager([]*Pool{NewPool(l, logger)}, logger).Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "pull failed") {
		t.Fatalf("Run error = %v, want pull failure", err)
	}
}
