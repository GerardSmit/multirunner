// Package autoscale scales ephemeral runners on demand: it launches runners when
// jobs are queued (via webhook events and/or API polling), up to each pool's max.
// Polling is outbound-only, so it works behind NAT where webhooks cannot reach.
package autoscale

import (
	"context"
	"log/slog"
	"time"

	"github.com/GerardSmit/multirunner/internal/config"
	"github.com/GerardSmit/multirunner/internal/github"
	"github.com/GerardSmit/multirunner/internal/pool"
)

// Scaler launches runners on demand across a set of pool launchers.
type Scaler struct {
	states    []*state
	gh        *github.Client
	scope     config.Scope
	pollEvery time.Duration
	logger    *slog.Logger
	baseCtx   context.Context // long-lived launch context (set in Run)
}

type state struct {
	l   *pool.Launcher
	sem chan struct{} // capacity = launcher max
}

// New builds a Scaler. pollSec <= 0 disables API polling (webhook-only).
func New(launchers []*pool.Launcher, gh *github.Client, scope config.Scope, pollSec int, logger *slog.Logger) *Scaler {
	states := make([]*state, len(launchers))
	for i, l := range launchers {
		states[i] = &state{l: l, sem: make(chan struct{}, l.Max())}
	}
	every := time.Duration(pollSec) * time.Second
	return &Scaler{states: states, gh: gh, scope: scope, pollEvery: every,
		logger: logger.With("component", "autoscale"), baseCtx: context.Background()}
}

// Run ensures images are present, starts the poller (if enabled), and blocks
// until ctx is cancelled.
func (s *Scaler) Run(ctx context.Context) error {
	s.baseCtx = ctx // launched runners use this long-lived context, not a request ctx
	for _, st := range s.states {
		if err := st.l.EnsureImage(ctx); err != nil {
			return err
		}
	}
	s.logger.Info("autoscaler running", "pools", len(s.states), "poll", s.pollEvery.String())
	if s.pollEvery > 0 {
		s.reconcile() // initial top-up
		go s.pollLoop(ctx)
	}
	<-ctx.Done()
	return nil
}

// OnQueued launches one runner for the first matching pool with spare capacity.
// Launches use the scaler's long-lived context (NOT the caller's), so a webhook
// handler returning does not cancel the runner.
func (s *Scaler) OnQueued(labels []string) {
	for _, st := range s.states {
		if labelsMatch(st.l.Labels(), labels) {
			if s.tryLaunch(st) {
				return
			}
		}
	}
	s.logger.Debug("queued job: no matching pool with spare capacity", "labels", labels)
}

func (s *Scaler) tryLaunch(st *state) bool {
	select {
	case st.sem <- struct{}{}:
		s.logger.Info("scaling up", "pool", st.l.Name())
		go func() {
			defer func() { <-st.sem }()
			if _, err := st.l.RunOne(s.baseCtx); err != nil && s.baseCtx.Err() == nil {
				s.logger.Error("runner failed", "pool", st.l.Name(), "err", err)
			}
		}()
		return true
	default:
		return false
	}
}

func (s *Scaler) pollLoop(ctx context.Context) {
	t := time.NewTicker(s.pollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.reconcile()
		}
	}
}

// reconcile queries queued work (repo scope) and tops up runners to capacity.
func (s *Scaler) reconcile() {
	if s.scope != config.ScopeRepo {
		return // org/enterprise: rely on webhook (no cheap queued-jobs endpoint)
	}
	jobs, err := s.gh.QueuedJobLabels(s.baseCtx)
	if err != nil {
		s.logger.Warn("poll queued jobs failed", "err", err)
		return
	}
	for _, labels := range jobs {
		s.OnQueued(labels)
	}
}

// labelsMatch reports whether a pool with poolLabels can serve a job requesting
// jobLabels (the pool must carry every requested label).
func labelsMatch(poolLabels, jobLabels []string) bool {
	for _, jl := range jobLabels {
		found := false
		for _, pl := range poolLabels {
			if pl == jl {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
