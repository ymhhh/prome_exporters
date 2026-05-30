package server

import (
	"sync"
	"testing"

	"github.com/prometheus/blackbox_exporter/config"
)

func TestCopyModuleIsIndependent(t *testing.T) {
	original := config.Module{
		Prober: "http",
		HTTP: config.HTTPProbe{
			Headers: map[string]string{"X-Custom": "value"},
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(hostname string) {
			defer wg.Done()
			copied, err := copyModule(original)
			if err != nil {
				t.Errorf("copyModule failed: %v", err)
				return
			}
			if err := setHTTPHost(hostname, &copied); err != nil {
				t.Errorf("setHTTPHost failed: %v", err)
				return
			}
			if copied.HTTP.Headers["Host"] != hostname {
				t.Errorf("expected Host %q, got %q", hostname, copied.HTTP.Headers["Host"])
			}
		}("host-" + string(rune('a'+i)) + ".example.com")
	}
	wg.Wait()

	if original.HTTP.Headers["Host"] != "" {
		t.Fatalf("original module should not have Host set: %v", original.HTTP.Headers)
	}
	if original.HTTP.Headers["X-Custom"] != "value" {
		t.Fatalf("original module header mutated: %v", original.HTTP.Headers)
	}
}

func TestCopyModuleRoundTrip(t *testing.T) {
	original := config.Module{Prober: "http"}
	copied, err := copyModule(original)
	if err != nil {
		t.Fatal(err)
	}
	if copied.Prober != "http" {
		t.Fatalf("expected prober http, got %q", copied.Prober)
	}
}
