package simple

import (
	"io"
	"log/slog"
	"testing"

	"github.com/ymhhh/prome_exporters/parsers"
)

func TestParseMETRICLines(t *testing.T) {
	p, err := NewParser(slog.New(slog.NewTextHandler(io.Discard, nil)), parsers.Config{})
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`
starting collect
METRIC app_qps 123.4 cluster=prod
METRIC app_errors 2 cluster=prod code=500
metric ignored 1
done
`)
	mfs, err := p.Parse(body, map[string]string{"script": "disk.sh"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mfs["app_qps"]; !ok {
		t.Fatalf("missing app_qps: %#v", mfs)
	}
	if _, ok := mfs["app_errors"]; !ok {
		t.Fatalf("missing app_errors: %#v", mfs)
	}
	if _, ok := mfs["ignored"]; ok {
		t.Fatal("lowercase metric prefix should be ignored")
	}

	labels := map[string]string{}
	for _, lp := range mfs["app_qps"].GetMetric()[0].GetLabel() {
		labels[lp.GetName()] = lp.GetValue()
	}
	if labels["cluster"] != "prod" || labels["script"] != "disk.sh" {
		t.Fatalf("unexpected labels: %v", labels)
	}
}

func TestParseRequiresMETRIC(t *testing.T) {
	p, err := NewParser(slog.New(slog.NewTextHandler(io.Discard, nil)), parsers.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Parse([]byte("hello world\n"), nil, "")
	if err == nil {
		t.Fatal("expected error when no METRIC lines")
	}
}

func TestParseQuotedLabel(t *testing.T) {
	p, err := NewParser(slog.New(slog.NewTextHandler(io.Discard, nil)), parsers.Config{})
	if err != nil {
		t.Fatal(err)
	}
	mfs, err := p.Parse([]byte(`METRIC disk_used 10 path="/data/foo bar"`+"\n"), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	for _, lp := range mfs["disk_used"].GetMetric()[0].GetLabel() {
		labels[lp.GetName()] = lp.GetValue()
	}
	if labels["path"] != "/data/foo bar" {
		t.Fatalf("quoted label not parsed: %v", labels)
	}
}
