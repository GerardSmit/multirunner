// Package metrics exposes Prometheus metrics + a health endpoint and provides
// pool lifecycle hooks that update them.
package metrics

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/GerardSmit/multirunner/internal/pool"
)

// Metrics holds the registry and instruments.
type Metrics struct {
	reg    *prometheus.Registry
	active *prometheus.GaugeVec
	jobs   *prometheus.CounterVec
	reprov *prometheus.CounterVec
}

// New builds the metrics set.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	active := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "multirunner_runners_active", Help: "Currently running ephemeral runners.",
	}, []string{"pool"})
	jobs := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "multirunner_jobs_total", Help: "Ephemeral runners that completed (one job each).",
	}, []string{"pool", "result"})
	reprov := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "multirunner_reprovision_errors_total", Help: "Runner launch/JIT errors.",
	}, []string{"pool"})
	reg.MustRegister(active, jobs, reprov)
	return &Metrics{reg: reg, active: active, jobs: jobs, reprov: reprov}
}

// Hooks returns pool lifecycle hooks that update the metrics.
func (m *Metrics) Hooks() pool.Hooks {
	return pool.Hooks{
		OnStart: func(p string) { m.active.WithLabelValues(p).Inc() },
		OnStop: func(p string, code int, err error) {
			m.active.WithLabelValues(p).Dec()
			result := "success"
			if err != nil {
				result = "error"
				m.reprov.WithLabelValues(p).Inc()
			}
			m.jobs.WithLabelValues(p, result).Inc()
		},
	}
}

// Serve runs the /metrics + /health endpoints until ctx is cancelled.
func (m *Metrics) Serve(ctx context.Context, listen string, logger *slog.Logger) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	srv := &http.Server{Addr: listen, Handler: mux}
	go func() {
		logger.Info("metrics listening", "addr", listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server stopped", "err", err)
		}
	}()
	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutCtx)
}
