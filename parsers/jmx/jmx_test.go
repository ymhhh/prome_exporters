package jmx

import (
	"log/slog"
	"regexp"
	"testing"

	"github.com/ymhhh/prome_exporters/parsers"

	dto "github.com/prometheus/client_model/go"
)

func TestParseFilterStateDoesNotLeak(t *testing.T) {
	wl, err := regexp.Compile("^allowed_")
	if err != nil {
		t.Fatal(err)
	}

	p := &Parser{
		logger: slog.Default(),
		cfg: parsers.Config{
			Whitelists: []*regexp.Regexp{wl},
		},
	}

	body := []byte(`{
		"beans": [
			{
				"name": "java.lang:type=Memory,name=allowed_metric",
				"allowed_metric": 1,
				"blocked_metric": 2
			}
		]
	}`)

	mfs, err := p.Parse(body, map[string]string{}, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := mfs["allowed_metric"]; !ok {
		t.Fatalf("expected allowed metric, got families: %v", mfs)
	}
	if _, ok := mfs["blocked_metric"]; ok {
		t.Fatalf("blocked metric should be filtered")
	}
}

func TestParseBlacklist(t *testing.T) {
	bl, err := regexp.Compile("^blocked_")
	if err != nil {
		t.Fatal(err)
	}

	p := &Parser{
		logger: slog.Default(),
		cfg: parsers.Config{
			Blacklists: []*regexp.Regexp{bl},
		},
	}

	body := []byte(`{
		"beans": [
			{
				"name": "java.lang:type=Memory,name=ok",
				"ok_metric": 1,
				"blocked_metric": 2
			}
		]
	}`)

	mfs, err := p.Parse(body, map[string]string{}, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := mfs["ok_metric"]; !ok {
		t.Fatalf("expected ok_metric, got: %v", mfs)
	}
	if _, ok := mfs["blocked_metric"]; ok {
		t.Fatalf("blocked_metric should be filtered")
	}
}

func metricLabels(t *testing.T, mfs map[string]*dto.MetricFamily, name string) map[string]string {
	t.Helper()
	mf, ok := mfs[name]
	if !ok || mf == nil || len(mf.GetMetric()) == 0 {
		t.Fatalf("expected metric family %q, got %#v", name, mfs)
	}
	labels := map[string]string{}
	for _, lp := range mf.GetMetric()[0].GetLabel() {
		labels[lp.GetName()] = lp.GetValue()
	}
	return labels
}

func TestParseTagPrefixAndNoCrossBeanLeak(t *testing.T) {
	p := &Parser{logger: slog.Default()}

	callerTags := map[string]string{"instance": "host:1"}
	body := []byte(`{
		"beans": [
			{
				"name": "java.lang:type=Memory,name=bean1",
				"tag.gc": "G1",
				"tag.attr": "x",
				"tag.foo": "from_bean1",
				"metric_a": 1
			},
			{
				"name": "java.lang:type=Memory,name=bean2",
				"metric_b": 2
			}
		]
	}`)

	mfs, err := p.Parse(body, callerTags, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := callerTags["name"]; ok {
		t.Fatalf("Parse must not mutate caller tags, got %v", callerTags)
	}
	if callerTags["instance"] != "host:1" || len(callerTags) != 1 {
		t.Fatalf("caller tags mutated: %v", callerTags)
	}

	a := metricLabels(t, mfs, "metric_a")
	if a["gc"] != "G1" {
		t.Fatalf("expected tag.gc -> gc, got %v", a)
	}
	if a["attr"] != "x" {
		t.Fatalf("expected tag.attr -> attr, got %v", a)
	}
	if _, ok := a["c"]; ok {
		t.Fatalf("TrimLeft would produce c; got labels %v", a)
	}
	if _, ok := a["ttr"]; ok {
		t.Fatalf("TrimLeft would produce ttr; got labels %v", a)
	}
	if a["foo"] != "from_bean1" {
		t.Fatalf("expected foo=from_bean1 on bean1, got %v", a)
	}

	b := metricLabels(t, mfs, "metric_b")
	if _, ok := b["foo"]; ok {
		t.Fatalf("bean2 should not inherit bean1 tag.foo, got %v", b)
	}
	if b["name"] != "bean2" {
		t.Fatalf("expected bean2 name label, got %v", b)
	}
	if b["instance"] != "host:1" {
		t.Fatalf("expected instance to copy onto bean2, got %v", b)
	}
}
