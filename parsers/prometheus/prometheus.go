package prometheus

import (
	"bytes"
	"errors"
	"io"
	"net/http"

	"log/slog"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/ymhhh/prome_exporters/parsers"
	"google.golang.org/protobuf/proto"
)

type Parser struct {
	cfg    parsers.Config
	logger *slog.Logger
}

func NewParser(logger *slog.Logger, cfg parsers.Config) (parsers.Parser, error) {
	return &Parser{logger: logger, cfg: cfg}, nil
}

func (p *Parser) Parse(body []byte, tags map[string]string, contentType string) (map[string]*dto.MetricFamily, error) {
	metricFamilies := make(map[string]*dto.MetricFamily)

	// Use the same content-type rules as prometheus/common (incl. proto delimited:
	// application/vnd.google.protobuf; proto=...; encoding=delimited).
	h := make(http.Header)
	h.Set("Content-Type", contentType)
	format := expfmt.ResponseFormat(h)
	dec := expfmt.NewDecoder(bytes.NewReader(body), format)

	for {
		mf := &dto.MetricFamily{}
		if err := dec.Decode(mf); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			p.logger.Error("decoding metrics failed", "error", err)
			return nil, err
		}
		if name := mf.GetName(); name != "" {
			metricFamilies[name] = mf
		}
	}

	for k, v := range tags {
		name := proto.String(k)
		value := proto.String(v)
		for _, family := range metricFamilies {
			for _, metric := range family.GetMetric() {
				metric.Label = append(metric.Label, &dto.LabelPair{Name: name, Value: value})
			}
		}
	}

	return metricFamilies, nil
}
