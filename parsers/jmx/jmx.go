package jmx

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"

	"github.com/ymhhh/go-common/types"
	"github.com/ymhhh/prome_exporters/parsers"

	dto "github.com/prometheus/client_model/go"
)

var (
	metricReg = regexp.MustCompile(`[.\- ]`)
)

type Parser struct {
	logger *slog.Logger
	cfg    parsers.Config
}

func NewParser(logger *slog.Logger, cfg parsers.Config) (parsers.Parser, error) {
	p := &Parser{
		logger: logger,
		cfg:    cfg,
	}

	return p, nil
}

type beans struct {
	Beans []map[string]any `json:"beans"`
}

func (p *Parser) Parse(bs []byte, tags map[string]string, _ string) (map[string]*dto.MetricFamily, error) {

	var kept = beans{}
	if err := json.Unmarshal(bs, &kept); err != nil {
		return nil, err
	}

	metricFamilies := make(map[string]*dto.MetricFamily)

	for _, values := range kept.Beans {
		if len(values) == 0 {
			continue
		}

		nameValue, exist := values["name"]
		if !exist {
			continue
		}

		nameValueStr, ok := nameValue.(string)
		if !ok {
			continue
		}

		names := strings.Split(nameValueStr, ",name=")
		if len(names) <= 1 {
			continue
		}

		var metricPrefixes []string

		if !p.cfg.JMXOptions.IgnorePrefix {
			serviceStr := names[0]
			if ok := strings.Contains(serviceStr, ":service="); ok {
				services := strings.Split(serviceStr, ":service=")
				if len(services) <= 1 {
					continue
				}
				metricPrefixes = append(metricPrefixes, strings.TrimSpace(services[0]), strings.TrimSpace(services[1]))
			}
		}

		namesSubs := strings.Split(names[1], ",sub=")
		beanTags := make(map[string]string, len(tags)+4)
		for k, v := range tags {
			beanTags[k] = v
		}
		beanTags["name"] = strings.TrimSpace(namesSubs[0])
		if len(namesSubs) > 1 {
			beanTags["sub"] = strings.TrimSpace(namesSubs[1])
		}

		for key, value := range values {
			if value == nil || !strings.HasPrefix(key, "tag.") {
				continue
			}
			tagKey := strings.TrimPrefix(key, "tag.")
			switch t := value.(type) {
			case string:
				if t = strings.TrimSpace(t); t != "" {
					beanTags[tagKey] = t
				}
			case int, int64, int32:
				beanTags[tagKey] = fmt.Sprintf("%d", t)
			case float32, float64:
				beanTags[tagKey] = fmt.Sprintf("%f", t)
			}
		}

		for key, value := range values {
			if value == nil {
				continue
			}
			metricNames := append(metricPrefixes, strings.TrimSpace(key))
			metricName := strings.Join(metricNames, "_")

			metricName = metricReg.ReplaceAllString(metricName, "_")

			if !parsers.MatchNameFilter(metricName, p.cfg.Whitelists, p.cfg.Blacklists) {
				p.logger.Debug("filter metric", "metric", metricName)
				continue
			}

			var th = 0.0
			switch x := reflect.TypeOf(value).Kind(); x {
			case reflect.Bool:
				if value == true {
					th = 1
				} else {
					th = 0
				}
			default:
				tValue, err := types.ToFloat64(value)
				if err != nil {
					p.logger.Warn("value is not numeric", "key", key, "value", value)
					continue
				}
				th = tValue
			}

			mf, ok := metricFamilies[metricName]
			if !ok {
				typ := dto.MetricType_UNTYPED
				mf = &dto.MetricFamily{
					Name: &metricName,
					Type: &typ,
				}
			}

			metric := &dto.Metric{
				Untyped: &dto.Untyped{
					Value: &th,
				},
			}

			for k, v := range beanTags {
				key, value := k, v
				metric.Label = append(metric.Label, &dto.LabelPair{Name: &key, Value: &value})
			}

			mf.Metric = append(mf.GetMetric(), metric)
			metricFamilies[metricName] = mf
		}
	}

	return metricFamilies, nil
}
