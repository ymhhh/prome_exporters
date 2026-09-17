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
	last     atomic.Value // []*dto.MetricFamily
}

func (m *mockOutput) Connect() error       { return nil }
func (m *mockOutput) Close() error         { return nil }
func (m *mockOutput) SampleConfig() string { return "" }
func (m *mockOutput) Description() string  { return "mock" }

func (m *mockOutput) Write(metrics []*dto.MetricFamily) error {
	m.writes.Add(1)
	m.last.Store(metrics)
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

	runOut := a.runningOutputs["default"]
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runOut.metricsBuffer.Length() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if runOut.metricsBuffer.Length() == 0 {
		t.Fatal("expected metrics in buffer after failed write")
	}

	a.Stop()
}

type errWriteFailed struct{}

func (errWriteFailed) Error() string { return "write failed" }

type errConnectFailed struct{}

func (errConnectFailed) Error() string { return "connect failed" }

type failConnectOutput struct {
	mockOutput
}

func (m *failConnectOutput) Connect() error { return errConnectFailed{} }

func TestFlushBatchFailureDoesNotDuplicateMetrics(t *testing.T) {
	name := "same_metric"
	typ := dto.MetricType_GAUGE
	v1, v2 := 1.0, 2.0
	fam1 := &dto.MetricFamily{
		Name: &name,
		Type: &typ,
		Metric: []*dto.Metric{{
			Gauge: &dto.Gauge{Value: &v1},
		}},
	}
	fam2 := &dto.MetricFamily{
		Name: &name,
		Type: &typ,
		Metric: []*dto.Metric{{
			Gauge: &dto.Gauge{Value: &v2},
		}},
	}

	out := &mockOutput{}
	out.setWriteErr(errWriteFailed{})

	runOut := &runningOutput{
		name:   "default",
		output: out,
		logger: slog.Default(),
	}
	a := &Agent{
		Logger: slog.Default(),
		Config: &conf.Config{Exporter: conf.ExporterConfig{
			GlobalTags: map[string]string{"region": "us"},
		}},
		runningOutputs: map[string]*runningOutput{"default": runOut},
	}
	runOut.metricsBuffer.Push(fam1)
	runOut.metricsBuffer.Push(fam2)

	if ok := a.flushBatch(runOut, 2); ok {
		t.Fatal("expected write failure")
	}
	if got := runOut.metricsBuffer.Length(); got != 2 {
		t.Fatalf("expected original 2 families in buffer, got %d", got)
	}

	items, ok := runOut.metricsBuffer.PopMany(2)
	if !ok {
		t.Fatal("expected to pop original families")
	}
	if len(items[0].GetMetric()) != 1 {
		t.Fatalf("first family should still have 1 metric, got %d", len(items[0].GetMetric()))
	}
	if len(items[1].GetMetric()) != 1 {
		t.Fatalf("second family should still have 1 metric, got %d", len(items[1].GetMetric()))
	}
	for i, item := range items {
		for _, metric := range item.GetMetric() {
			for _, lp := range metric.GetLabel() {
				if lp.GetName() == "region" {
					t.Fatalf("buffered family %d should not receive global tags after failed write", i)
				}
			}
		}
	}
}

func TestRunOutputConnectFailureStopsAgent(t *testing.T) {
	inputName := "test_input_connect_fail"
	outputName := "test_output_connect_fail"

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
		return &failConnectOutput{}, nil
	})

	cfg := &conf.Config{
		Exporter: conf.ExporterConfig{
			CommandType:       0,
			FlushInterval:     types.Duration(time.Second),
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
	if err := a.Run(); err == nil {
		t.Fatal("expected connect failure")
	}

	runOut := a.runningOutputs["default"]
	select {
	case _, ok := <-runOut.metricsChan:
		if ok {
			t.Fatal("metricsChan should be closed after failed Run")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for metricsChan to close")
	}

	if err := a.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckConfigDefaultsNegativeLimits(t *testing.T) {
	inputName := "test_input_neg_limits"
	outputName := "test_output_neg_limits"

	inputs.RegisterFactory(inputName, func(opts ...plugins.Option) (plugins.InputMetricsCollector, error) {
		return &mockMetricsCollector{}, nil
	})
	outputs.RegisterFactory(outputName, func(opts ...plugins.Option) (plugins.Output, error) {
		return &mockOutput{}, nil
	})

	cfg := &conf.Config{
		Exporter: conf.ExporterConfig{
			CommandType:       0,
			MetricBufferLimit: -1,
			MetricBatchSize:   -5,
		},
		Inputs: []*conf.InputsConfig{{Name: inputName}},
		Output: &conf.OutputConfig{Name: outputName},
	}

	a, err := NewAgent(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if a.Config.Exporter.MetricBufferLimit != 10000 {
		t.Fatalf("expected buffer limit default 10000, got %d", a.Config.Exporter.MetricBufferLimit)
	}
	if a.Config.Exporter.MetricBatchSize != 10000 {
		t.Fatalf("expected batch size default 10000, got %d", a.Config.Exporter.MetricBatchSize)
	}
}

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

func TestInputsRouteToSeparateOutputs(t *testing.T) {
	inA := "route_input_a"
	inB := "route_input_b"
	outAName := "route_out_a_plugin"
	outBName := "route_out_b_plugin"

	nameA, nameB := "metric_a", "metric_b"
	val := 1.0
	typ := dto.MetricType_GAUGE

	inputs.RegisterFactory(inA, func(opts ...plugins.Option) (plugins.InputMetricsCollector, error) {
		return &mockMetricsCollector{metrics: []*dto.MetricFamily{{
			Name: &nameA, Type: &typ,
			Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: &val}}},
		}}}, nil
	})
	inputs.RegisterFactory(inB, func(opts ...plugins.Option) (plugins.InputMetricsCollector, error) {
		return &mockMetricsCollector{metrics: []*dto.MetricFamily{{
			Name: &nameB, Type: &typ,
			Metric: []*dto.Metric{{Gauge: &dto.Gauge{Value: &val}}},
		}}}, nil
	})

	outA, outB := &mockOutput{}, &mockOutput{}
	outputs.RegisterFactory(outAName, func(opts ...plugins.Option) (plugins.Output, error) { return outA, nil })
	outputs.RegisterFactory(outBName, func(opts ...plugins.Option) (plugins.Output, error) { return outB, nil })

	cfg := &conf.Config{
		Exporter: conf.ExporterConfig{
			CommandType:       0,
			FlushInterval:     types.Duration(50 * time.Millisecond),
			MetricBufferLimit: 1000,
			MetricBatchSize:   100,
		},
		Inputs: []*conf.InputsConfig{
			{Name: inA, Interval: types.Duration(20 * time.Millisecond), Output: "sink_a"},
			{Name: inB, Interval: types.Duration(20 * time.Millisecond), Output: "sink_b"},
		},
		Outputs: map[string]*conf.OutputConfig{
			"sink_a": {Name: outAName},
			"sink_b": {Name: outBName},
		},
	}

	a, err := NewAgent(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Run(); err != nil {
		t.Fatal(err)
	}
	defer a.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if outA.writes.Load() > 0 && outB.writes.Load() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if outA.writes.Load() == 0 || outB.writes.Load() == 0 {
		t.Fatalf("expected both outputs to receive writes, a=%d b=%d", outA.writes.Load(), outB.writes.Load())
	}

	gotA, _ := outA.last.Load().([]*dto.MetricFamily)
	gotB, _ := outB.last.Load().([]*dto.MetricFamily)
	if len(gotA) == 0 || gotA[0].GetName() != "metric_a" {
		t.Fatalf("sink_a should get metric_a, got %#v", gotA)
	}
	if len(gotB) == 0 || gotB[0].GetName() != "metric_b" {
		t.Fatalf("sink_b should get metric_b, got %#v", gotB)
	}
}
