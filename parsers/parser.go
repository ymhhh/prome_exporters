package parsers

import (
	"regexp"

	dto "github.com/prometheus/client_model/go"
)

type Option func(*Config)

type Config struct {
	Name string `yaml:"name" json:"name"`

	PrefixWhitelist []string `yaml:"prefix_whitelist" json:"prefix_whitelist"`
	PrefixBlacklist []string `yaml:"prefix_blacklist" json:"prefix_blacklist"`

	Whitelists []*regexp.Regexp `yaml:"-" json:"-"`
	Blacklists []*regexp.Regexp `yaml:"-" json:"-"`

	// Prometheus
	PrometheusOptions `yaml:",inline" json:",inline"`

	// JMX
	JMXOptions `yaml:",inline" json:",inline"`
}

type PrometheusOptions struct {
}

type JMXOptions struct {
	IgnorePrefix    bool `yaml:"jmx_ignore_prefix" json:"jmx_ignore_prefix"`
	IgnoreTimestamp bool `yaml:"jmx_ignore_timestamp" json:"jmx_ignore_timestamp"`
}

type Parser interface {
	Parse(bs []byte, tags map[string]string, ct string) (map[string]*dto.MetricFamily, error)
}
