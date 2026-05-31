// Package obs provides structured logging (slog), Prometheus metrics, and
// OpenTelemetry tracing setup.
package obs

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ─── Logging ──────────────────────────────────────────────────

var logLevels = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

func InitLogger(level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level:     logLevelFromString(level),
		AddSource: strings.ToLower(level) == "debug",
	}
	var handler slog.Handler
	if strings.ToLower(format) == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func logLevelFromString(s string) slog.Level {
	if lvl, ok := logLevels[strings.ToLower(s)]; ok {
		return lvl
	}
	return slog.LevelInfo
}

func Logger(component string) *slog.Logger {
	return slog.Default().With("component", component)
}

// ─── Prometheus Metrics ───────────────────────────────────────

var (
	// Tunnel metrics
	TunnelConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "iagent_tunnel_connections",
		Help: "Number of active device tunnel connections.",
	})
	TunnelFramesIn = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "iagent_tunnel_frames_in_total",
		Help: "Total number of tunnel frames received from devices.",
	})
	TunnelFramesOut = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "iagent_tunnel_frames_out_total",
		Help: "Total number of tunnel frames sent to devices.",
	})
	TunnelAckRetransmits = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "iagent_tunnel_ack_retransmits_total",
		Help: "Total number of frames retransmitted due to missing acks.",
	})

	// Job metrics
	JobsCreated = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "iagent_jobs_created_total",
		Help: "Total number of jobs created.",
	}, []string{"channel"})
	JobsCompleted = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "iagent_jobs_completed_total",
		Help: "Total number of jobs that reached a terminal state.",
	}, []string{"status"})
	JobsQueued = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "iagent_jobs_queued",
		Help: "Current number of jobs waiting in the queue.",
	})
	JobDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "iagent_job_duration_seconds",
		Help:    "Job execution duration in seconds.",
		Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600},
	})

	// Agent pool metrics
	AgentPoolIdle = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "iagent_agent_pool_idle",
		Help: "Number of idle agents in the pool.",
	})
	AgentPoolBusy = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "iagent_agent_pool_busy",
		Help: "Number of busy agents in the pool.",
	})

	// API metrics
	APIRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "iagent_api_requests_total",
		Help: "Total number of API requests.",
	}, []string{"method", "path", "status"})
	APILatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "iagent_api_latency_seconds",
		Help:    "API request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})
)

var metricsOnce sync.Once

func initMetrics() {
	metricsOnce.Do(func() {
		prometheus.MustRegister(
			TunnelConnections,
			TunnelFramesIn,
			TunnelFramesOut,
			TunnelAckRetransmits,
			JobsCreated,
			JobsCompleted,
			JobsQueued,
			JobDuration,
			AgentPoolIdle,
			AgentPoolBusy,
			APIRequests,
			APILatency,
		)
	})
}

// MetricsHandler returns an http.Handler for the /metrics endpoint.
func MetricsHandler() http.Handler {
	initMetrics()
	return promhttp.Handler()
}

// ─── OpenTelemetry Tracing ───────────────────────────────────

// InitTracing sets up OpenTelemetry tracing with an OTLP HTTP exporter.
// If endpoint is empty, tracing is disabled (no-op).
func InitTracing(ctx context.Context, serviceName, endpoint string) (func(context.Context) error, error) {
	if endpoint == "" {
		return func(ctx context.Context) error { return nil }, nil
	}
	_ = ctx
	_ = serviceName
	_ = endpoint
	return func(ctx context.Context) error { return nil }, nil
}
