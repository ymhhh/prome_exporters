package simple

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"log/slog"

	dto "github.com/prometheus/client_model/go"
	"github.com/ymhhh/prome_exporters/parsers"
)

const metricPrefix = "METRIC"

var (
	metricNameSanitizer = regexp.MustCompile(`[.\-]`)
	metricNameValid     = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
)

type Parser struct {
	logger *slog.Logger
	cfg    parsers.Config
}

func NewParser(logger *slog.Logger, cfg parsers.Config) (parsers.Parser, error) {
	return &Parser{logger: logger, cfg: cfg}, nil
}

func (p *Parser) Parse(bs []byte, tags map[string]string, _ string) (map[string]*dto.MetricFamily, error) {
	metricFamilies := make(map[string]*dto.MetricFamily)
	scanner := bufio.NewScanner(bytes.NewReader(bs))
	// Allow long METRIC lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lineNo := 0
	parsed := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields, err := splitFields(line)
		if err != nil {
			p.logger.Warn("simple_parse_skip_line", "line", lineNo, "error", err)
			continue
		}
		if len(fields) < 1 || fields[0] != metricPrefix {
			p.logger.Debug("simple_ignore_non_metric_line", "line", lineNo)
			continue
		}
		if len(fields) < 3 {
			p.logger.Warn("simple_parse_skip_line", "line", lineNo, "error", "METRIC requires name and value")
			continue
		}

		metricName := metricNameSanitizer.ReplaceAllString(fields[1], "_")
		if !metricNameValid.MatchString(metricName) {
			p.logger.Warn("simple_parse_skip_line", "line", lineNo, "error", "invalid metric name", "name", metricName)
			continue
		}
		if !parsers.MatchNameFilter(metricName, p.cfg.Whitelists, p.cfg.Blacklists) {
			continue
		}

		value, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			p.logger.Warn("simple_parse_skip_line", "line", lineNo, "error", "invalid value", "value", fields[2])
			continue
		}

		labels := map[string]string{}
		var ts *int64
		for _, field := range fields[3:] {
			k, v, ok := strings.Cut(field, "=")
			if !ok || k == "" {
				p.logger.Warn("simple_parse_skip_line", "line", lineNo, "error", "invalid label", "field", field)
				labels = nil
				break
			}
			if k == "ts" {
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					p.logger.Warn("simple_parse_skip_line", "line", lineNo, "error", "invalid ts", "value", v)
					labels = nil
					break
				}
				ms := timestampMs(n)
				ts = &ms
				continue
			}
			labels[k] = v
		}
		if labels == nil {
			continue
		}

		mf, ok := metricFamilies[metricName]
		if !ok {
			name := metricName
			typ := dto.MetricType_UNTYPED
			mf = &dto.MetricFamily{Name: &name, Type: &typ}
			metricFamilies[metricName] = mf
		}

		metric := &dto.Metric{
			Untyped: &dto.Untyped{Value: &value},
		}
		if ts != nil {
			metric.TimestampMs = ts
		}
		for k, v := range labels {
			key, value := k, v
			metric.Label = append(metric.Label, &dto.LabelPair{Name: &key, Value: &value})
		}
		for k, v := range tags {
			if labelExists(metric.Label, k) {
				continue
			}
			key, value := k, v
			metric.Label = append(metric.Label, &dto.LabelPair{Name: &key, Value: &value})
		}
		mf.Metric = append(mf.Metric, metric)
		parsed++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if parsed == 0 {
		return nil, fmt.Errorf("no METRIC lines parsed")
	}
	return metricFamilies, nil
}

func labelExists(labels []*dto.LabelPair, name string) bool {
	for _, lp := range labels {
		if lp.GetName() == name {
			return true
		}
	}
	return false
}

func timestampMs(t int64) int64 {
	switch len(strconv.FormatInt(t, 10)) {
	case 10:
		return t * 1000
	case 13:
		return t
	case 16:
		return t / 1000
	default:
		return t
	}
}

// splitFields splits on whitespace but keeps double-quoted values intact.
func splitFields(line string) ([]string, error) {
	var fields []string
	var b strings.Builder
	inQuote := false

	flush := func() {
		if b.Len() == 0 {
			return
		}
		fields = append(fields, b.String())
		b.Reset()
	}

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case ch == '"':
			inQuote = !inQuote
		case unicode.IsSpace(rune(ch)) && !inQuote:
			flush()
		default:
			b.WriteByte(ch)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unclosed quote")
	}
	flush()
	return fields, nil
}
