package server

import (
	"net/http"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/ymhhh/prome_exporters/agent"
)

var (
	metricsPath            *string
	webConfig              *string
	maxRequests            *int
	disableExporterMetrics *bool
	historyLimit           *uint
	timeoutOffset          *float64
)

func init() {
	metricsPath = kingpin.Flag(
		"web.telemetry-path",
		"Path under which to expose metrics.",
	).Default("/metrics").String()

	webConfig = kingpin.Flag(
		"web.config",
		"[EXPERIMENTAL] Path to config yaml file that can enable TLS or authentication.",
	).Default("").String()

	maxRequests = kingpin.Flag(
		"web.max-requests",
		"Maximum number of parallel scrape requests. Use 0 to disable.",
	).Default("40").Int()

	disableExporterMetrics = kingpin.Flag(
		"web.disable-exporter-metrics",
		"Exclude metrics about the exporter itself (promhttp_*, process_*, go_*).",
	).Bool()

	historyLimit = kingpin.Flag("probe.history.limit",
		"The maximum amount of items to keep in the history.").Default("100").Uint()
	timeoutOffset = kingpin.Flag("probe.timeout-offset",
		"Offset to subtract from timeout in seconds.").Default("0.5").Float64()
}

func Run(a *agent.Agent, webConfig *web.FlagConfig) int {

	if err := a.Run(); err != nil {
		a.Logger.Error("failed_run_agent", "error", err, "config", a.Config)
		return 3
	}

	reg := prometheus.NewRegistry()

	reg.MustRegister(
		version.NewCollector("prome_exporters"),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector())
	h := promhttp.HandlerFor(
		prometheus.Gatherers{reg},
		promhttp.HandlerOpts{
			ErrorHandling:       promhttp.ContinueOnError,
			MaxRequestsInFlight: *maxRequests,
			Registry:            reg,
		},
	)

	if *disableExporterMetrics {
		h = promhttp.InstrumentMetricHandler(reg, h)
	}

	http.Handle(*metricsPath, h)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>Node Exporter</title></head>
			<body>
			<h1>Node Exporter</h1>
			<p><a href="` + *metricsPath + `">Metrics</a></p>
			</body>
			</html>`))
	})

	if a.Config.Exporter.BlackboxProbe.Open {
		a.Logger.Info("probe api open")

		rh := &resultHistory{maxResults: *historyLimit}

		http.HandleFunc("/probe", func(w http.ResponseWriter, r *http.Request) {
			probeHandler(w, r, a.Config.Exporter.BlackboxProbe.Modules, a.Logger, rh)
		})

	}

	addr := ""
	if webConfig != nil && webConfig.WebListenAddresses != nil && len(*webConfig.WebListenAddresses) > 0 {
		addr = (*webConfig.WebListenAddresses)[0]
	}
	if addr == "" {
		addr = ":10031"
	}

	a.Logger.Info("Listening on", "address", addr)
	server := &http.Server{Addr: addr}
	if err := web.ListenAndServe(server, webConfig, a.Logger); err != nil {
		a.Logger.Error("listen failed", "err", err)
		return 1
	}
	a.Stop()
	return 0
}
