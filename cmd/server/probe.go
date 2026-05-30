// Copyright 2016 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// "github.com/prometheus/blackbox_exporter"

package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"log/slog"

	"github.com/prometheus/blackbox_exporter/config"
	"github.com/prometheus/blackbox_exporter/prober"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/expfmt"
	"gopkg.in/yaml.v3"
)

var (
	Probers = map[string]prober.ProbeFn{
		"http": prober.ProbeHTTP,
		"tcp":  prober.ProbeTCP,
		"icmp": prober.ProbeICMP,
		"dns":  prober.ProbeDNS,
		"grpc": prober.ProbeGRPC,
	}

	moduleUnknownCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "blackbox_module_unknown_total",
		Help: "Count of unknown modules requested by probes",
	})
)

func probeHandler(w http.ResponseWriter, r *http.Request, c *config.Config, logger *slog.Logger, rh *resultHistory) {
	moduleName := r.URL.Query().Get("module")
	if moduleName == "" {
		moduleName = "http_2xx"
	}
	module, ok := c.Modules[moduleName]
	if !ok {
		http.Error(w, fmt.Sprintf("Unknown module %q", moduleName), http.StatusBadRequest)
		logger.Debug("Unknown module", "module", moduleName)
		moduleUnknownCounter.Add(1)
		return
	}

	module, err := copyModule(module)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to copy module config: %s", err), http.StatusInternalServerError)
		return
	}

	timeoutSeconds, err := getTimeout(r, module, *timeoutOffset)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse timeout from Prometheus header: %s", err), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSeconds*float64(time.Second)))
	defer cancel()
	r = r.WithContext(ctx)

	probeSuccessGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_success",
		Help: "Displays whether or not the probe was a success",
	})
	probeDurationGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_duration_seconds",
		Help: "Returns how long the probe took to complete in seconds",
	})

	params := r.URL.Query()
	target := params.Get("target")
	if target == "" {
		http.Error(w, "Target parameter is missing", http.StatusBadRequest)
		return
	}

	prober, ok := Probers[module.Prober]
	if !ok {
		http.Error(w, fmt.Sprintf("Unknown prober %q", module.Prober), http.StatusBadRequest)
		return
	}

	hostname := params.Get("hostname")
	if module.Prober == "http" && hostname != "" {
		err = setHTTPHost(hostname, &module)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	slLogger, slBuffer := newScrapeLogger(logger, moduleName, target)
	slLogger.Info("Beginning probe", "probe", module.Prober, "timeout_seconds", timeoutSeconds)

	start := time.Now()
	registry := prometheus.NewRegistry()
	registry.MustRegister(probeSuccessGauge)
	registry.MustRegister(probeDurationGauge)
	success := prober(ctx, target, module, registry, slLogger)
	duration := time.Since(start).Seconds()
	probeDurationGauge.Set(duration)
	if success {
		probeSuccessGauge.Set(1)
		slLogger.Info("Probe succeeded", "duration_seconds", duration)
	} else {
		slLogger.Info("Probe failed", "duration_seconds", duration)
	}

	debugOutput := DebugOutput(&module, slBuffer, registry)
	rh.Add(moduleName, target, debugOutput, success)

	if r.URL.Query().Get("debug") == "true" {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(debugOutput))
		return
	}

	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
}

type scrapeLogger struct {
	next    slog.Handler
	buffer  *bytes.Buffer
	bufferH slog.Handler
}

type teeHandler struct {
	next slog.Handler
	tee  slog.Handler
}

func (h *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level) || h.tee.Enabled(ctx, level)
}

func (h *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	// Best-effort: write to both; return the first error if any.
	if err := h.tee.Handle(ctx, r); err != nil {
		_ = h.next.Handle(ctx, r)
		return err
	}
	return h.next.Handle(ctx, r)
}

func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{
		next: h.next.WithAttrs(attrs),
		tee:  h.tee.WithAttrs(attrs),
	}
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{
		next: h.next.WithGroup(name),
		tee:  h.tee.WithGroup(name),
	}
}

func newScrapeLogger(logger *slog.Logger, module string, target string) (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	attrs := []slog.Attr{
		slog.String("module", module),
		slog.String("target", target),
	}

	// Buffer logger in text format for debug output.
	bufH := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	// Forward to the original logger handler as-is.
	nextH := logger.Handler()

	h := (&teeHandler{next: nextH, tee: bufH}).WithAttrs(attrs)
	return slog.New(h), buf
}

// DebugOutput returns plaintext debug output for a probe.
func DebugOutput(module *config.Module, logBuffer *bytes.Buffer, registry *prometheus.Registry) string {
	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "Logs for the probe:\n")
	logBuffer.WriteTo(buf)
	fmt.Fprintf(buf, "\n\n\nMetrics that would have been returned:\n")
	mfs, err := registry.Gather()
	if err != nil {
		fmt.Fprintf(buf, "Error gathering metrics: %s\n", err)
	}
	for _, mf := range mfs {
		expfmt.MetricFamilyToText(buf, mf)
	}
	fmt.Fprintf(buf, "\n\n\nModule configuration:\n")
	c, err := yaml.Marshal(module)
	if err != nil {
		fmt.Fprintf(buf, "Error marshalling config: %s\n", err)
	}
	buf.Write(c)

	return buf.String()
}

func getTimeout(r *http.Request, module config.Module, offset float64) (timeoutSeconds float64, err error) {
	// If a timeout is configured via the Prometheus header, add it to the request.
	if v := r.Header.Get("X-Prometheus-Scrape-Timeout-Seconds"); v != "" {
		var err error
		timeoutSeconds, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, err
		}
	}
	if timeoutSeconds == 0 {
		timeoutSeconds = 120
	}

	var maxTimeoutSeconds = timeoutSeconds - offset
	if module.Timeout.Seconds() < maxTimeoutSeconds && module.Timeout.Seconds() > 0 {
		timeoutSeconds = module.Timeout.Seconds()
	} else {
		timeoutSeconds = maxTimeoutSeconds
	}

	return timeoutSeconds, nil
}

func copyModule(module config.Module) (config.Module, error) {
	b, err := yaml.Marshal(module)
	if err != nil {
		return config.Module{}, err
	}
	var copied config.Module
	if err := yaml.Unmarshal(b, &copied); err != nil {
		return config.Module{}, err
	}
	return copied, nil
}

func setHTTPHost(hostname string, module *config.Module) error {
	// By creating a new hashmap and copying values there we
	// ensure that the initial configuration remain intact.
	headers := make(map[string]string)
	if module.HTTP.Headers != nil {
		for name, value := range module.HTTP.Headers {
			if strings.EqualFold(name, "Host") && value != hostname {
				return fmt.Errorf("host header defined both in module configuration (%s) and with URL-parameter 'hostname' (%s)", value, hostname)
			}
			headers[name] = value
		}
	}
	headers["Host"] = hostname
	module.HTTP.Headers = headers
	return nil
}
