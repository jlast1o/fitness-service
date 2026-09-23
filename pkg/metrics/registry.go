package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

func NewServiceRegistry() (*prometheus.Registry, *HTTPMetrics) {
	registry := prometheus.NewRegistry()

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(
			collectors.ProcessCollectorOpts{},
		),
	)

	httpMetrics := NewHTTPMetrics(registry)

	return registry, httpMetrics
}
