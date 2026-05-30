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
	if err := cfg.check(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestConfigCheckInvalidCommandType(t *testing.T) {
	cfg := &Config{
		Exporter: ExporterConfig{CommandType: 99},
		Inputs:   []*InputsConfig{{Name: "http"}},
		Output:   &OutputConfig{Name: "http"},
	}
	if err := cfg.check(); err == nil {
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
	if err := cfg.check(); err == nil {
		t.Fatal("expected error when blackbox open without modules")
	}

	cfg.Exporter.BlackboxProbe.Modules = &beConfig.Config{
		Modules: map[string]beConfig.Module{
			"http_2xx": {Prober: "http"},
		},
	}
	if err := cfg.check(); err != nil {
		t.Fatalf("expected valid blackbox config, got %v", err)
	}
}
