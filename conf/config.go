package conf

import (
	"os"

	beConfig "github.com/prometheus/blackbox_exporter/config"
	"github.com/ymhhh/go-common/config"
	"github.com/ymhhh/go-common/types"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Exporter ExporterConfig `yaml:"exporter" json:"exporter"`

	Inputs []*InputsConfig `yaml:"inputs" json:"inputs"`
	Output *OutputConfig   `yaml:"output" json:"output"`
}

type ExporterConfig struct {
	// 0 command; 1 server
	CommandType int `yaml:"command_type" json:"command_type"`

	GlobalTags map[string]string `yaml:"global_tags" json:"global_tags"`

	FlushInterval     types.Duration `yaml:"flush_interval" json:"flush_interval"`
	MetricBufferLimit int64          `yaml:"metric_buffer_limit" json:"metric_buffer_limit"`
	MetricBatchSize   int64          `yaml:"metric_batch_size" json:"metric_batch_size"`

	BlackboxProbe BlackboxProbeConfig `yaml:"blackbox_probe" json:"blackbox_probe"`
}

type BlackboxProbeConfig struct {
	Open    bool             `yaml:"open" json:"open"`
	Modules *beConfig.Config `yaml:",inline" json:",inline"`
}

type InputsConfig struct {
	Name     string         `yaml:"name" json:"name"`
	Interval types.Duration `yaml:"interval" json:"interval"`

	Tags map[string]string `yaml:"tags" json:"tags"`

	Options config.Options `json:"options" yaml:"options"`
}

type OutputConfig struct {
	Name    string         `yaml:"name" json:"name"`
	Options config.Options `json:"options" yaml:"options"`
}

func (p *Config) check() error {
	return nil
}

func GetConfigWithFile(filename string) (*Config, error) {
	c, err := config.Load(filename)
	if err != nil {
		return nil, err
	}
	ec := &Config{}
	if err = c.Object(ec); err != nil {
		return nil, err
	}

	if err = ec.check(); err != nil {
		return nil, err
	}
	return ec, nil
}

// OptionsToConfig converts config.Options to config.Config without using
// (*config.Options).ToConfig (which may panic with some go-common versions).
func OptionsToConfig(opts config.Options) (config.Config, error) {
	if opts == nil {
		return nil, nil
	}
	b, err := yaml.Marshal(map[string]any(opts))
	if err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "prome_exporters_opts_*.yaml")
	if err != nil {
		return nil, err
	}
	name := f.Name()
	defer os.Remove(name)
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return nil, err
	}
	return config.Load(name)
}
