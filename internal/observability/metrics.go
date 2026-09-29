package observability

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec

	UploadsTotal   *prometheus.CounterVec
	UploadDuration *prometheus.HistogramVec

	StorageUsage *prometheus.GaugeVec

	DBConnectionsTotal    prometheus.Gauge
	DBConnectionsIdle     prometheus.Gauge
	DBConnectionsAcquired prometheus.Gauge

	RabbitMQQueueDepth *prometheus.GaugeVec
}

type DBPoolStats interface {
	TotalConns() int32
	IdleConns() int32
	AcquiredConns() int32
}

func NewMetrics(registry prometheus.Registerer) *Metrics {
	metrics := &Metrics{
		HTTPRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "avatars_http_requests_total",
				Help: "Total number of HTTP requests.",
			},
			[]string{"method", "route", "status"},
		),

		HTTPRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "avatars_http_request_duration_seconds",
				Help: "HTTP request duration in seconds.",
			},
			[]string{"method", "route", "status"},
		),

		UploadsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "avatars_uploads_total",
				Help: "Total number of avatar uploads.",
			},
			[]string{"status"},
		),

		UploadDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "avatars_upload_duration_seconds",
				Help: "Avatar upload duration in seconds.",
			},
			[]string{"status"},
		),

		StorageUsage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "avatars_storage_bytes",
				Help: "Storage used by avatars per user in bytes.",
			},
			[]string{"user_id"},
		),

		DBConnectionsTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "avatars_db_connections_total",
				Help: "Current total number of PostgreSQL connections in the pool.",
			},
		),

		DBConnectionsIdle: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "avatars_db_connections_idle",
				Help: "Current number of idle PostgreSQL connections in the pool.",
			},
		),

		DBConnectionsAcquired: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "avatars_db_connections_acquired",
				Help: "Current number of acquired PostgreSQL connections.",
			},
		),

		RabbitMQQueueDepth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "avatars_rabbitmq_queue_depth",
				Help: "Current number of messages ready for delivery in a RabbitMQ queue.",
			},
			[]string{"queue"},
		),
	}

	registry.MustRegister(
		metrics.HTTPRequestsTotal,
		metrics.HTTPRequestDuration,
		metrics.UploadsTotal,
		metrics.UploadDuration,
		metrics.StorageUsage,
		metrics.DBConnectionsTotal,
		metrics.DBConnectionsIdle,
		metrics.DBConnectionsAcquired,
		metrics.RabbitMQQueueDepth,
	)

	return metrics
}

func (m *Metrics) SetDBPoolStats(stats DBPoolStats) {
	m.DBConnectionsTotal.Set(float64(stats.TotalConns()))
	m.DBConnectionsIdle.Set(float64(stats.IdleConns()))
	m.DBConnectionsAcquired.Set(float64(stats.AcquiredConns()))
}

func (m *Metrics) SetStorageUsage(usage map[string]int64) {
	m.StorageUsage.Reset()

	for userID, bytes := range usage {
		m.StorageUsage.WithLabelValues(userID).Set(float64(bytes))
	}
}

func (m *Metrics) SetRabbitMQQueueDepth(queue string, depth int) {
	m.RabbitMQQueueDepth.
		WithLabelValues(queue).
		Set(float64(depth))
}
