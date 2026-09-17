package http

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ymhhh/go-common/types"
	"github.com/ymhhh/prome_exporters/parsers"
	"github.com/ymhhh/prome_exporters/parsers/defaults"
)

func TestGatherAllURLsFailReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	defer server.Close()

	parser, err := defaults.NewParser(slog.Default(), parsers.Config{Name: "prometheus"})
	if err != nil {
		t.Fatal(err)
	}

	p := &Collector{
		Logger: slog.Default(),
		Urls:   []string{server.URL},
		client: &http.Client{Timeout: time.Second},
		parser: parser,
	}

	_, err = p.Gather()
	if err == nil {
		t.Fatal("expected error when all URLs fail")
	}
}

func TestGatherPartialSuccessReturnsMetrics(t *testing.T) {
	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("# HELP ok_metric ok\n# TYPE ok_metric gauge\nok_metric 1\n"))
	}))
	defer okServer.Close()

	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	defer failServer.Close()

	parser, err := defaults.NewParser(slog.Default(), parsers.Config{Name: "prometheus"})
	if err != nil {
		t.Fatal(err)
	}

	p := &Collector{
		Logger:  slog.Default(),
		Urls:    []string{failServer.URL, okServer.URL},
		Timeout: types.Duration(time.Second),
		client:  &http.Client{Timeout: time.Second},
		parser:  parser,
	}

	metrics, err := p.Gather()
	if err != nil {
		t.Fatalf("partial success should not return error, got %v", err)
	}
	if len(metrics) == 0 {
		t.Fatal("expected metrics from successful URL")
	}
}

func TestGatherRejectsOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 64)))
	}))
	defer server.Close()

	parser, err := defaults.NewParser(slog.Default(), parsers.Config{Name: "prometheus"})
	if err != nil {
		t.Fatal(err)
	}

	p := &Collector{
		Logger:      slog.Default(),
		Urls:        []string{server.URL},
		MaxBodySize: 16,
		client:      &http.Client{Timeout: time.Second},
		parser:      parser,
	}

	_, err = p.Gather()
	if err == nil {
		t.Fatal("expected error for oversized body")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected exceeds error, got %v", err)
	}
}

func TestMaxBodySizeDefault(t *testing.T) {
	p := &Collector{}
	if p.maxBodySize() != defaultMaxBodySize {
		t.Fatalf("default max body size = %d, want %d", p.maxBodySize(), defaultMaxBodySize)
	}
	p.MaxBodySize = 32
	if p.maxBodySize() != 32 {
		t.Fatalf("max body size = %d, want 32", p.maxBodySize())
	}
}
