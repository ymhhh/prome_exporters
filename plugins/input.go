package plugins

import (
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

type InputType int32

const (
	InputTypePrometheusCollector InputType = iota
	InputTypeMetricsCollector
)

type Input interface {
	InputType() InputType

	NewMetricsCollector(opts ...Option) (InputMetricsCollector, error)
	NewPrometheusCollector(opts ...Option) (InputPrometheusCollector, error)
}

type InputPrometheusCollector interface {
	prometheus.Collector
	Tags() map[string]string
}

type InputMetricsCollector interface {
	PluginDescriber
	Gather() ([]*dto.MetricFamily, error)
}
