package prometheus

import (
	"bytes"
	"io"
	"log/slog"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/ymhhh/prome_exporters/parsers"
	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/proto"
)

func TestParser_Parse_TextAndTags(t *testing.T) {
	t.Parallel()

	p, err := NewParser(slog.New(slog.NewTextHandler(io.Discard, nil)), parsers.Config{})
	if err != nil {
		t.Fatalf("NewParser err=%v", err)
	}

	body := []byte(`# HELP up 1 if up
# TYPE up gauge
up{job="demo"} 1
`)
	mfs, err := p.Parse(body, map[string]string{"a": "1", "b": "2"}, string(expfmt.FmtText))
	if err != nil {
		t.Fatalf("Parse err=%v", err)
	}
	mf := mfs["up"]
	if mf == nil || len(mf.GetMetric()) != 1 {
		t.Fatalf("expected mf up with 1 metric, got %+v", mf)
	}

	labels := map[string]string{}
	for _, lp := range mf.GetMetric()[0].GetLabel() {
		labels[lp.GetName()] = lp.GetValue()
	}
	if labels["job"] != "demo" {
		t.Fatalf("expected job=demo, got %q", labels["job"])
	}
	if labels["a"] != "1" || labels["b"] != "2" {
		t.Fatalf("expected injected tags a=1 b=2, got a=%q b=%q", labels["a"], labels["b"])
	}
}

func TestParser_Parse_ProtoDelimited(t *testing.T) {
	t.Parallel()

	p, err := NewParser(slog.New(slog.NewTextHandler(io.Discard, nil)), parsers.Config{})
	if err != nil {
		t.Fatalf("NewParser err=%v", err)
	}

	name := "foo_total"
	mf := &dto.MetricFamily{
		Name: &name,
		Type: dto.MetricType_COUNTER.Enum(),
		Metric: []*dto.Metric{
			{
				Counter: &dto.Counter{Value: proto.Float64(3)},
			},
		},
	}

	var buf bytes.Buffer
	if _, err := protodelim.MarshalTo(&buf, mf); err != nil {
		t.Fatalf("protodelim.MarshalTo err=%v", err)
	}

	ct := expfmt.ProtoType + "; proto=" + expfmt.ProtoProtocol + "; encoding=delimited"
	mfs, err := p.Parse(buf.Bytes(), map[string]string{"instance": "x"}, ct)
	if err != nil {
		t.Fatalf("Parse err=%v", err)
	}
	got := mfs[name]
	if got == nil || len(got.GetMetric()) != 1 {
		t.Fatalf("expected mf %s with 1 metric, got %+v", name, got)
	}
	labels := map[string]string{}
	for _, lp := range got.GetMetric()[0].GetLabel() {
		labels[lp.GetName()] = lp.GetValue()
	}
	if labels["instance"] != "x" {
		t.Fatalf("expected injected instance=x, got %q", labels["instance"])
	}
}
