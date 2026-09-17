package conf

import (
	"testing"

	beConfig "github.com/prometheus/blackbox_exporter/config"
)

func TestConfigCheckValid(t *testing.T) {
	cfg := &Config{
		Exporter: ExporterConfig{CommandType: 0},
		Inputs:   []*InputsConfig{{Name: "http"}},
		Output:   &OutputConfig{Name: "http"},
	}
	if err := cfg.Check(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
	if cfg.Inputs[0].Output != defaultOutputName {
		t.Fatalf("expected input output %q, got %q", defaultOutputName, cfg.Inputs[0].Output)
	}
	if _, ok := cfg.Outputs[defaultOutputName]; !ok {
		t.Fatal("expected legacy output normalized into outputs.default")
	}
}

func TestConfigCheckInvalidCommandType(t *testing.T) {
	cfg := &Config{
		Exporter: ExporterConfig{CommandType: 99},
		Inputs:   []*InputsConfig{{Name: "http"}},
		Output:   &OutputConfig{Name: "http"},
	}
	if err := cfg.Check(); err == nil {
		t.Fatal("expected error for invalid command_type")
	}
}

func TestConfigCheckBlackboxRequiresModules(t *testing.T) {
	cfg := &Config{
		Exporter: ExporterConfig{
			CommandType: 1,
			BlackboxProbe: BlackboxProbeConfig{
				Open: true,
			},
		},
		Inputs: []*InputsConfig{{Name: "http"}},
		Output: &OutputConfig{Name: "http"},
	}
	if err := cfg.Check(); err == nil {
		t.Fatal("expected error when blackbox open without modules")
	}

	cfg.Exporter.BlackboxProbe.Modules = &beConfig.Config{
		Modules: map[string]beConfig.Module{
			"http_2xx": {Prober: "http"},
		},
	}
	if err := cfg.Check(); err != nil {
		t.Fatalf("expected valid blackbox config, got %v", err)
	}
}

func TestNamedOutputsRequireInputRef(t *testing.T) {
	cfg := &Config{
		Exporter: ExporterConfig{CommandType: 0},
		Inputs:   []*InputsConfig{{Name: "http"}},
		Outputs: map[string]*OutputConfig{
			"a": {Name: "http"},
			"b": {Name: "http"},
		},
	}
	if err := cfg.Check(); err == nil {
		t.Fatal("expected error when input.output missing with multiple outputs")
	}

	cfg.Inputs[0].Output = "missing"
	if err := cfg.Check(); err == nil {
		t.Fatal("expected error for missing output ref")
	}

	cfg.Inputs[0].Output = "a"
	if err := cfg.Check(); err != nil {
		t.Fatalf("expected valid named outputs, got %v", err)
	}
}

func TestLegacyOutputConflictsWithDefaultKey(t *testing.T) {
	cfg := &Config{
		Exporter: ExporterConfig{CommandType: 0},
		Inputs:   []*InputsConfig{{Name: "http", Output: "default"}},
		Output:   &OutputConfig{Name: "http"},
		Outputs: map[string]*OutputConfig{
			"default": {Name: "http"},
		},
	}
	if err := cfg.Check(); err == nil {
		t.Fatal("expected conflict between legacy output and outputs.default")
	}
}

func TestSharedOutputBinding(t *testing.T) {
	cfg := &Config{
		Exporter: ExporterConfig{CommandType: 0},
		Inputs: []*InputsConfig{
			{Name: "http", Output: "push"},
			{Name: "zookeeper", Output: "push"},
		},
		Outputs: map[string]*OutputConfig{
			"push": {Name: "http"},
		},
	}
	if err := cfg.Check(); err != nil {
		t.Fatalf("expected shared output binding to be valid, got %v", err)
	}
}
