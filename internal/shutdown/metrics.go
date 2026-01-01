package shutdown

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics for shutdown monitoring.
var (
	// shutdownDuration tracks the total shutdown duration.
	shutdownDuration = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nebulaio_shutdown_duration_seconds",
		Help: "Total duration of the shutdown process in seconds",
	})

	// shutdownPhase tracks the current shutdown phase.
	shutdownPhase = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nebulaio_shutdown_phase",
		Help: "Current shutdown phase (1 = active, 0 = inactive)",
	}, []string{"phase"})

	// inFlightRequests tracks the number of in-flight requests during shutdown.
	inFlightRequests = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nebulaio_shutdown_in_flight_requests",
		Help: "Number of in-flight requests during shutdown",
	})

	// workersStopped tracks the number of workers stopped during shutdown.
	workersStopped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nebulaio_shutdown_workers_stopped_total",
		Help: "Total number of workers stopped during shutdown",
	})

	// shutdownErrors tracks errors during shutdown.
	shutdownErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nebulaio_shutdown_errors_total",
		Help: "Total number of errors during shutdown",
	})

	// shutdownStartTime records when shutdown started.
	shutdownStartTime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nebulaio_shutdown_start_timestamp_seconds",
		Help: "Unix timestamp when shutdown started",
	})
)

// All phases for tracking.
var allPhases = []Phase{
	PhaseNone,
	PhaseDraining,
	PhaseWorkers,
	PhaseHTTPServers,
	PhaseCluster,
	PhaseMetadata,
	PhaseStorage,
	PhaseTiering,
	PhaseAudit,
	PhaseComplete,
	PhaseForcedShutdown,
}

// SetShutdownDuration sets the shutdown duration metric.
func SetShutdownDuration(d time.Duration) {
	shutdownDuration.Set(d.Seconds())
}

// SetShutdownPhase sets the current shutdown phase metric.
func SetShutdownPhase(phase Phase) {
	// Reset all phases to 0
	for _, p := range allPhases {
		shutdownPhase.WithLabelValues(string(p)).Set(0)
	}
	// Set current phase to 1
	shutdownPhase.WithLabelValues(string(phase)).Set(1)
}

// SetInFlightRequests sets the in-flight requests metric.
func SetInFlightRequests(count int64) {
	inFlightRequests.Set(float64(count))
}

// IncrementWorkersStopped increments the workers stopped counter.
func IncrementWorkersStopped() {
	workersStopped.Inc()
}

// IncrementShutdownErrors increments the shutdown errors counter.
func IncrementShutdownErrors() {
	shutdownErrors.Inc()
}

// SetShutdownStartTime sets the shutdown start timestamp.
func SetShutdownStartTime(t time.Time) {
	shutdownStartTime.Set(float64(t.Unix()))
}
