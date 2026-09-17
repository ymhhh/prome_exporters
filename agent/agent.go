package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ymhhh/prome_exporters/conf"
	"github.com/ymhhh/prome_exporters/plugins"
	"github.com/ymhhh/prome_exporters/plugins/inputs"
	"github.com/ymhhh/prome_exporters/plugins/outputs"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/ymhhh/go-common/errcode"
	"google.golang.org/protobuf/proto"
)

const minInterval = time.Second * 1

// Agent runs a set of plugins.
type Agent struct {
	Config *conf.Config

	Logger *slog.Logger

	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once

	inputWG   sync.WaitGroup
	metricsWG sync.WaitGroup
	outputWG  sync.WaitGroup

	runningInputs  []*runningInput
	runningOutputs map[string]*runningOutput
}

type metricsBuffer struct {
	mu    sync.Mutex
	items []*dto.MetricFamily
}

func (b *metricsBuffer) Length() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return int64(len(b.items))
}

func (b *metricsBuffer) Push(v *dto.MetricFamily) {
	if v == nil {
		return
	}
	b.mu.Lock()
	b.items = append(b.items, v)
	b.mu.Unlock()
}

func (b *metricsBuffer) PopMany(n int64) ([]*dto.MetricFamily, bool) {
	if n <= 0 {
		return nil, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if int64(len(b.items)) < n {
		return nil, false
	}
	out := make([]*dto.MetricFamily, n)
	copy(out, b.items[:n])
	b.items = b.items[n:]
	return out, true
}

func (b *metricsBuffer) PrependMany(items []*dto.MetricFamily) {
	if len(items) == 0 {
		return
	}
	b.mu.Lock()
	b.items = append(items, b.items...)
	b.mu.Unlock()
}

type runningInput struct {
	input    *inputs.Input
	logger   *slog.Logger
	interval time.Duration

	promeRegisterer prometheus.Registerer
	promeGatherer   prometheus.Gatherer

	promeCollector   plugins.InputPrometheusCollector
	metricsCollector plugins.InputMetricsCollector

	output   *runningOutput
	stopChan chan struct{}
}

type runningOutput struct {
	name          string
	output        plugins.Output
	logger        *slog.Logger
	flushInterval time.Duration
	metricsChan   chan []*dto.MetricFamily
	metricsBuffer metricsBuffer
	stopChan      chan struct{}
}

// NewAgent returns an Agent for the given Config.
func NewAgent(cfg *conf.Config, logger *slog.Logger) (*Agent, error) {
	a := &Agent{
		Config:         cfg,
		Logger:         logger,
		runningOutputs: make(map[string]*runningOutput),
	}
	if err := a.checkConfig(); err != nil {
		return nil, err
	}
	return a, nil
}

func (p *Agent) checkConfig() error {
	if err := p.Config.Check(); err != nil {
		return err
	}

	if p.Config.Exporter.MetricBufferLimit <= 0 {
		p.Config.Exporter.MetricBufferLimit = 10000
	}
	if p.Config.Exporter.MetricBatchSize <= 0 {
		p.Config.Exporter.MetricBatchSize = 10000
	}

	defaultFlush := time.Duration(p.Config.Exporter.FlushInterval)
	if defaultFlush < minInterval {
		defaultFlush = minInterval
	}

	for key, outCfg := range p.Config.Outputs {
		outFun, err := outputs.GetFactory(outCfg.Name)
		if err != nil {
			return err
		}

		logger := p.Logger.With("output", key, "plugin", outCfg.Name)
		opts := []plugins.Option{plugins.Logger(logger)}
		if outCfg.Options != nil {
			c, err := conf.OptionsToConfig(outCfg.Options)
			if err != nil {
				return err
			}
			opts = append(opts, plugins.Config(c))
		}

		output, err := outFun(opts...)
		if err != nil {
			return err
		}

		flush := time.Duration(outCfg.FlushInterval)
		if flush < minInterval {
			flush = defaultFlush
		}

		bufSize := len(p.Config.Inputs)
		if bufSize < 1 {
			bufSize = 1
		}
		p.runningOutputs[key] = &runningOutput{
			name:          key,
			output:        output,
			logger:        logger,
			flushInterval: flush,
			metricsChan:   make(chan []*dto.MetricFamily, bufSize),
			stopChan:      make(chan struct{}, 1),
		}
	}

	for _, inputConfig := range p.Config.Inputs {
		interval := time.Duration(inputConfig.Interval)
		if interval < minInterval {
			interval = minInterval
		}
		input, err := inputs.GetFactory(inputConfig.Name)
		if err != nil {
			return err
		}

		runOut, ok := p.runningOutputs[inputConfig.Output]
		if !ok {
			return errcode.Newf("output %q not initialized for input %s", inputConfig.Output, inputConfig.Name)
		}

		logger := p.Logger.With("input", inputConfig.Name, "output", inputConfig.Output)
		opts := []plugins.Option{plugins.Logger(logger)}
		if inputConfig.Options != nil {
			c, err := conf.OptionsToConfig(inputConfig.Options)
			if err != nil {
				return err
			}
			opts = append(opts, plugins.Config(c))
		}

		logger.Info("init_input", "interval", interval)

		runningIn := &runningInput{
			input:    input,
			interval: interval,
			logger:   logger,
			output:   runOut,
			stopChan: make(chan struct{}, 1),
		}
		switch input.InputType() {
		case plugins.InputTypePrometheusCollector:
			runningIn.promeCollector, err = input.NewPrometheusCollector(opts...)
			if err != nil {
				return err
			}
			reg := prometheus.NewRegistry()
			runningIn.promeRegisterer = reg
			runningIn.promeGatherer = reg
			if err = runningIn.promeRegisterer.Register(runningIn.promeCollector); err != nil {
				return err
			}
		case plugins.InputTypeMetricsCollector:
			runningIn.metricsCollector, err = input.NewMetricsCollector(opts...)
			if err != nil {
				return err
			}
		}

		p.runningInputs = append(p.runningInputs, runningIn)
	}
	return nil
}

func applyGlobalTags(mf *dto.MetricFamily, tags map[string]string) {
	if len(tags) == 0 || mf == nil {
		return
	}
	for _, metric := range mf.Metric {
		existing := make(map[string]struct{}, len(metric.Label))
		for _, lp := range metric.Label {
			existing[lp.GetName()] = struct{}{}
		}
		for k, v := range tags {
			if _, ok := existing[k]; ok {
				continue
			}
			key := proto.String(k)
			value := proto.String(v)
			metric.Label = append(metric.Label, &dto.LabelPair{Name: key, Value: value})
		}
	}
}

func (p *Agent) runInputs() error {
	gather := func(input *runningInput) ([]*dto.MetricFamily, error) {
		switch input.input.InputType() {
		case plugins.InputTypePrometheusCollector:
			metricFamilies, err := input.promeGatherer.Gather()
			if err != nil {
				input.logger.Error("gather metrics failed", "err", err)
				return nil, err
			}

			for k, v := range input.promeCollector.Tags() {
				key := proto.String(k)
				value := proto.String(v)
				for _, mf := range metricFamilies {
					for i, metric := range mf.GetMetric() {
						metric.Label = append(metric.Label, &dto.LabelPair{Name: key, Value: value})
						mf.GetMetric()[i] = metric
					}
				}
			}
			return metricFamilies, nil
		case plugins.InputTypeMetricsCollector:
			metrics, err := input.metricsCollector.Gather()
			if err != nil {
				input.logger.Error("error", "error", err.Error())
				return nil, err
			}
			return metrics, nil
		}
		return nil, nil
	}

	for _, input := range p.runningInputs {
		if _, err := gather(input); err != nil {
			return err
		}
		p.inputWG.Add(1)
		go func(in *runningInput) {
			defer p.inputWG.Done()
			ticker := time.NewTicker(in.interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					in.logger.Debug("start_gather")
					metrics, err := gather(in)
					if err != nil {
						continue
					}
					in.logger.Debug("input_gather_metrics", "length", len(metrics))
					if len(metrics) == 0 {
						continue
					}
					select {
					case in.output.metricsChan <- metrics:
					case <-p.ctx.Done():
						return
					case <-in.stopChan:
						return
					}
				case <-in.stopChan:
					return
				case <-p.ctx.Done():
					return
				}
			}
		}(input)
	}
	return nil
}

func (p *Agent) flushBatch(runOut *runningOutput, batch int64) bool {
	lenBuffer := runOut.metricsBuffer.Length()
	if lenBuffer <= 0 {
		return true
	}
	if batch > lenBuffer {
		batch = lenBuffer
	}

	runOut.logger.Debug("write_output_size", "buffer_length", lenBuffer, "batch_size", batch)

	metricBuffers, ok := runOut.metricsBuffer.PopMany(batch)
	if !ok {
		runOut.logger.Warn("pop_metrics_not_correct", "batch_size", batch)
		return false
	}

	var (
		mapMetrics = make(map[string]*dto.MetricFamily)
		names      []string
	)
	for _, metricFamily := range metricBuffers {
		if metricFamily == nil {
			continue
		}
		// Clone so merge/global tags never mutate buffered originals.
		cloned := proto.Clone(metricFamily).(*dto.MetricFamily)
		mf, ok := mapMetrics[cloned.GetName()]
		if ok {
			mf.Metric = append(mf.GetMetric(), cloned.GetMetric()...)
		} else {
			mf = cloned
			names = append(names, cloned.GetName())
		}
		mapMetrics[mf.GetName()] = mf
	}
	if len(names) == 0 {
		runOut.logger.Warn("write_output_failed", "buffer_length", lenBuffer, "error", "at least one metric")
		return true
	}

	for _, name := range names {
		applyGlobalTags(mapMetrics[name], p.Config.Exporter.GlobalTags)
	}

	var metrics []*dto.MetricFamily
	for _, name := range names {
		metrics = append(metrics, mapMetrics[name])
	}

	if err := runOut.output.Write(metrics); err != nil {
		runOut.logger.Error("write_output_failed", "buffer_length", lenBuffer, "error", err)
		runOut.metricsBuffer.PrependMany(metricBuffers)
		return false
	}
	return true
}

func (p *Agent) runOutputs() error {
	for _, output := range p.runningOutputs {
		if err := output.output.Connect(); err != nil {
			return err
		}
		p.runMetricsChan(output)

		p.outputWG.Add(1)
		go func(runOut *runningOutput) {
			defer p.outputWG.Done()
			runOut.logger.Info("run_output", "interval", runOut.flushInterval)
			ticker := time.NewTicker(runOut.flushInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					for runOut.metricsBuffer.Length() > 0 {
						batch := p.Config.Exporter.MetricBatchSize
						if !p.flushBatch(runOut, batch) {
							break
						}
					}
				case <-runOut.stopChan:
					for runOut.metricsBuffer.Length() > 0 {
						batch := runOut.metricsBuffer.Length()
						if batch > p.Config.Exporter.MetricBatchSize {
							batch = p.Config.Exporter.MetricBatchSize
						}
						if !p.flushBatch(runOut, batch) {
							break
						}
					}
					if err := runOut.output.Close(); err != nil {
						runOut.logger.Error("failed_stop_output", "error", err)
					}
					return
				}
			}
		}(output)
	}
	return nil
}

func (p *Agent) runMetricsChan(runOut *runningOutput) {
	p.metricsWG.Add(1)
	go func() {
		defer p.metricsWG.Done()
		for {
			select {
			case metrics, ok := <-runOut.metricsChan:
				if !ok {
					return
				}
				runOut.logger.Debug("read_buffer_from_metric_chan", "length", len(metrics))
				for _, metric := range metrics {
					lenBuffer := runOut.metricsBuffer.Length()
					if lenBuffer >= p.Config.Exporter.MetricBufferLimit {
						runOut.logger.Warn("out_of_the_limit_of_buffer",
							"buffer_length", lenBuffer,
							"limit", p.Config.Exporter.MetricBufferLimit,
							"ignore_metric", metric.GetName())
						continue
					}
					runOut.metricsBuffer.Push(metric)
				}
			}
		}
	}()
}

func (p *Agent) stopRunningInputs() {
	for _, input := range p.runningInputs {
		if input.stopChan != nil {
			select {
			case <-input.stopChan:
			default:
				input.stopChan <- struct{}{}
			}
			close(input.stopChan)
		}
		if closer, ok := input.metricsCollector.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil && input.logger != nil {
				input.logger.Error("failed_close_input", "error", err)
			}
		}
	}
}

func (p *Agent) unregisterPrometheusInputs() {
	for _, input := range p.runningInputs {
		if input.input != nil && input.input.InputType() == plugins.InputTypePrometheusCollector && input.promeRegisterer != nil && input.promeCollector != nil {
			input.promeRegisterer.Unregister(input.promeCollector)
		}
	}
}

func (p *Agent) stopRunningOutputs() {
	for _, runOut := range p.runningOutputs {
		if runOut.stopChan != nil {
			select {
			case runOut.stopChan <- struct{}{}:
			default:
			}
		}
	}
}

func (p *Agent) Run() error {
	p.ctx, p.cancel = context.WithCancel(context.Background())

	if err := p.runInputs(); err != nil {
		_ = p.Stop()
		return err
	}
	if err := p.runOutputs(); err != nil {
		_ = p.Stop()
		return err
	}
	return nil
}

func (p *Agent) Stop() error {
	p.stopOnce.Do(func() {
		if p.cancel != nil {
			p.cancel()
		}
		p.stopRunningInputs()
		p.inputWG.Wait()
		p.unregisterPrometheusInputs()

		for _, runOut := range p.runningOutputs {
			if runOut.metricsChan != nil {
				close(runOut.metricsChan)
			}
		}
		p.metricsWG.Wait()

		p.stopRunningOutputs()
		p.outputWG.Wait()
	})
	return nil
}
