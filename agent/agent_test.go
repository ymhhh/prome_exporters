package agent

import (
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ymhhh/go-common/types"
	"github.com/ymhhh/prome_exporters/conf"
	"github.com/ymhhh/prome_exporters/plugins"
	"github.com/ymhhh/prome_exporters/plugins/inputs"
	"github.com/ymhhh/prome_exporters/plugins/outputs"

	dto "github.com/prometheus/client_model/go"
)

type mockMetricsCollector struct {
	metrics []*dto.MetricFamily
	err     error
}

func (m *mockMetricsCollector) Gather() ([]*dto.MetricFamily, error) {
	return m.metrics, m.err
}

func (m *mockMetricsCollector) SampleConfig() string { return "" }
func (m *mockMetricsCollector) Description() string  { return "mock" }

type mockOutput struct {
	writeErr atomic.Value
	writes   atomic.Int32
}

func (m *mockOutput) Connect() error  { return nil }
func (m *mockOutput) Close() error    { return nil }
func (m *mockOutput) SampleConfig() string { return "" }
func (m *mockOutput) Description() string  { return "mock" }

func (m *mockOutput) Write(metrics []*dto.MetricFamily) error {
	m.writes.Add(1)
	if err, ok := m.writeErr.Load().(error); ok && err != nil {
		return err
	}
	return nil
}

func (m *mockOutput) setWriteErr(err error) {
	m.writeErr.Store(err)
}

func TestAgentStopDrainsGoroutines(t *testing.T) {
	inputName := "test_input_stop"
	outputName := "test_output_stop"

	inputs.RegisterFactory(inputName, func(opts ...plugins.Option) (plugins.InputMetricsCollector, error) {
		name := "test_metric"
		val := 1.0
		typ := dto.MetricType_GAUGE
		return &mockMetricsCollector{
			metrics: []*dto.MetricFamily{{
				Name: &name,
				Type: &typ,
				Metric: []*dto.Metric{{
					Gauge: &dto.Gauge{Value: &val},
				}},
			}},
		}, nil
	})
	outputs.RegisterFactory(outputName, func(opts ...plugins.Option) (plugins.Output, error) {
		return &mockOutput{}, nil
	})

	cfg := &conf.Config{
		Exporter: conf.ExporterConfig{
			CommandType:       0,
			FlushInterval:     types.Duration(100 * time.Millisecond),
			MetricBufferLimit: 1000,
			MetricBatchSize:   100,
		},
		Inputs: []*conf.InputsConfig{{
			Name:     inputName,
			Interval: types.Duration(time.Second),
		}},
		Output: &conf.OutputConfig{Name: outputName},
	}

	a, err := NewAgent(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	if err := a.Run(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(150 * time.Millisecond)

	if err := a.Stop(); err != nil {
		t.Fatal(err)
	}

	// Stop is synchronous; calling again should be safe.
	if err := a.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteFailurePreservesBuffer(t *testing.T) {
	inputName := "test_input_write_fail"
	outputName := "test_output_write_fail"

	inputs.RegisterFactory(inputName, func(opts ...plugins.Option) (plugins.InputMetricsCollector, error) {
		name := "buffer_metric"
		val := 42.0
		typ := dto.MetricType_GAUGE
		return &mockMetricsCollector{
			metrics: []*dto.MetricFamily{{
				Name: &name,
				Type: &typ,
				Metric: []*dto.Metric{{
					Gauge: &dto.Gauge{Value: &val},
				}},
			}},
		}, nil
	})

	out := &mockOutput{}
	out.setWriteErr(errWriteFailed{})
	outputs.RegisterFactory(outputName, func(opts ...plugins.Option) (plugins.Output, error) {
		return out, nil
	})

	cfg := &conf.Config{
		Exporter: conf.ExporterConfig{
			CommandType:       0,
			FlushInterval:     types.Duration(50 * time.Millisecond),
			MetricBufferLimit: 1000,
			MetricBatchSize:   100,
		},
		Inputs: []*conf.InputsConfig{{
			Name:     inputName,
			Interval: types.Duration(20 * time.Millisecond),
		}},
		Output: &conf.OutputConfig{Name: outputName},
	}

	a, err := NewAgent(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Run(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.metricsBuffer.Length() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if a.metricsBuffer.Length() == 0 {
		t.Fatal("expected metrics in buffer after failed write")
	}

	a.Stop()
}

type errWriteFailed struct{}

func (errWriteFailed) Error() string { return "write failed" }

func TestApplyGlobalTagsNoDuplicates(t *testing.T) {
	name := "metric"
	typ := dto.MetricType_GAUGE
	val := 1.0
	key := "env"
	value := "prod"
	mf := &dto.MetricFamily{
		Name: &name,
		Type: &typ,
		Metric: []*dto.Metric{{
			Gauge: &dto.Gauge{Value: &val},
			Label: []*dto.LabelPair{{Name: &key, Value: &value}},
		}},
	}

	applyGlobalTags(mf, map[string]string{"env": "staging", "region": "us"})

	labels := mf.Metric[0].Label
	counts := map[string]int{}
	for _, lp := range labels {
		counts[lp.GetName()]++
	}
	if counts["env"] != 1 {
		t.Fatalf("expected env label once, got %d", counts["env"])
	}
	if counts["region"] != 1 {
		t.Fatalf("expected region label once, got %d", counts["region"])
	}
}
