package http

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
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
