package jmx

import (
	"log/slog"
	"regexp"
	"testing"

	"github.com/ymhhh/prome_exporters/parsers"
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
