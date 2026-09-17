package conf

import (
	"fmt"
	"os"

	beConfig "github.com/prometheus/blackbox_exporter/config"
	"github.com/ymhhh/go-common/config"
	"github.com/ymhhh/go-common/errcode"
	"github.com/ymhhh/go-common/types"
	"gopkg.in/yaml.v3"
)

const defaultOutputName = "default"

type Config struct {
	Exporter ExporterConfig `yaml:"exporter" json:"exporter"`

	Inputs  []*InputsConfig          `yaml:"inputs" json:"inputs"`
	Output  *OutputConfig            `yaml:"output" json:"output"`
	Outputs map[string]*OutputConfig `yaml:"outputs" json:"outputs"`
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

	// Output is the key of an entry in Outputs.
	Output string `yaml:"output" json:"output"`

	Tags map[string]string `yaml:"tags" json:"tags"`

	Options config.Options `json:"options" yaml:"options"`
}

type OutputConfig struct {
	Name          string         `yaml:"name" json:"name"`
	FlushInterval types.Duration `yaml:"flush_interval" json:"flush_interval"`
	Options       config.Options `json:"options" yaml:"options"`
}

// normalizeOutputs merges legacy top-level output into Outputs as "default".
func (p *Config) normalizeOutputs() error {
	if p.Outputs == nil {
		p.Outputs = make(map[string]*OutputConfig)
	}

	if p.Output != nil {
		if _, exists := p.Outputs[defaultOutputName]; exists {
			return errcode.Newf("outputs.%s conflicts with legacy top-level output", defaultOutputName)
		}
		p.Outputs[defaultOutputName] = p.Output
		p.Output = nil
	}

	if len(p.Outputs) == 0 {
		return errcode.Newf("at least one output is required (output or outputs)")
	}

	for key, out := range p.Outputs {
		if key == "" {
			return errcode.Newf("outputs key must not be empty")
		}
		if out == nil {
			return errcode.Newf("outputs.%s is nil", key)
		}
		if out.Name == "" {
			return errcode.Newf("outputs.%s.name is required", key)
		}
	}

	onlyDefault := len(p.Outputs) == 1
	if _, ok := p.Outputs[defaultOutputName]; onlyDefault && ok {
		for _, input := range p.Inputs {
			if input.Output == "" {
				input.Output = defaultOutputName
			}
		}
		return nil
	}

	for i, input := range p.Inputs {
		if input.Output == "" {
			return errcode.Newf("inputs[%d].output is required when multiple named outputs are configured", i)
		}
		if _, ok := p.Outputs[input.Output]; !ok {
			return errcode.Newf("inputs[%d].output %q not found in outputs", i, input.Output)
		}
	}
	return nil
}

func (p *Config) Check() error {
	switch p.Exporter.CommandType {
	case 0, 1:
	default:
		return errcode.Newf("invalid exporter.command_type %d: must be 0 (command) or 1 (server)", p.Exporter.CommandType)
	}

	if len(p.Inputs) == 0 {
		return errcode.Newf("at least one input is required")
	}

	for i, input := range p.Inputs {
		if input.Name == "" {
			return errcode.Newf("inputs[%d].name is required", i)
		}
	}

	if err := p.normalizeOutputs(); err != nil {
		return err
	}

	for i, input := range p.Inputs {
		if input.Output == "" {
			return errcode.Newf("inputs[%d].output is required", i)
		}
		if _, ok := p.Outputs[input.Output]; !ok {
			return errcode.Newf("inputs[%d].output %q not found in outputs", i, input.Output)
		}
	}

	if p.Exporter.BlackboxProbe.Open {
		if p.Exporter.CommandType != 1 {
			return errcode.Newf("blackbox_probe.open requires command_type 1 (server mode)")
		}
		if p.Exporter.BlackboxProbe.Modules == nil || len(p.Exporter.BlackboxProbe.Modules.Modules) == 0 {
			return errcode.Newf("blackbox_probe.modules is required when blackbox_probe.open is true")
		}
	}

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

	if err = ec.Check(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
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
