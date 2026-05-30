package opentsdb

import (
	"encoding/json"
	"regexp"
	"strconv"
	"time"

	"log/slog"

	dto "github.com/prometheus/client_model/go"
	"github.com/ymhhh/prome_exporters/parsers"
)

var (
	metricReg = regexp.MustCompile("[.-]")
)

type Parser struct {
	logger *slog.Logger
	cfg    parsers.Config
}

type metrics []struct {
	Metric    string            `json:"metric"`
	Timestamp int64             `json:"timestamp"`
	Value     json.Number       `json:"value"`
	Tags      map[string]string `json:"tags"`
}

func NewParser(logger *slog.Logger, cfg parsers.Config) (parsers.Parser, error) {
	return &Parser{logger: logger, cfg: cfg}, nil
}

func (p *Parser) Parse(bs []byte, tags map[string]string, _ string) (map[string]*dto.MetricFamily, error) {

	ms := metrics{}

	if err := json.Unmarshal(bs, &ms); err != nil {
		return nil, err
	}

	metricFamilies := make(map[string]*dto.MetricFamily)

	for _, m := range ms {
		metricName := metricReg.ReplaceAllString(m.Metric, "_")
		mf, ok := metricFamilies[metricName]
		if !ok {
			typ := dto.MetricType_UNTYPED
			mf = &dto.MetricFamily{
				Name: &metricName,
				Type: &typ,
			}
		}
		th, _ := m.Value.Float64()

		metric := &dto.Metric{
			Untyped: &dto.Untyped{
				Value: &th,
			},
		}
		if !p.cfg.JMXOptions.IgnoreTimestamp {
			timestampMs := timestampMsFunc(m.Timestamp)
			metric.TimestampMs = &timestampMs
		}

		for k, v := range m.Tags {
			key, value := k, v
			metric.Label = append(metric.Label, &dto.LabelPair{Name: &key, Value: &value})
		}

		for k, v := range tags {
			key, value := k, v //
			metric.Label = append(metric.Label, &dto.LabelPair{Name: &key, Value: &value})
		}

		mf.Metric = append(mf.Metric, metric)

		metricFamilies[metricName] = mf
	}

	return metricFamilies, nil
}

func timestampMsFunc(t int64) int64 {
	switch len(strconv.Itoa(int(t))) {
	case 10:
		return t * 1000
	case 13:
		return t
	case 16:
		return t / 1000
	default:
		return time.Now().Unix() / 1e6
	}
}
