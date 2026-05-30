# prome_exporters

A pluggable framework to **collect**, **parse**, and **forward** Prometheus-style metrics.

## Quick start

```bash
go test ./...
go run . --config.file prome_exporters.yaml
```

## Running mode (`exporter.command_type`)

- `0`: run **command** mode
- `1`: run **server** mode (HTTP endpoint, optional blackbox `/probe`)

Example:

```yaml
exporter:
  command_type: 1

  ### supported Prometheus Blackbox_exporter
  blackbox_probe:
    open: false # command_type = 1 & open = true
```

## Server flags

This project uses `exporter-toolkit` web flags. Common ones:

- `--web.listen-address` (repeatable; first address is used as HTTP server bind)
- `--web.config.file` (TLS / auth config for exporter-toolkit)
- `--web.telemetry-path` (metrics path, default `/metrics`)

## Inputs

Inputs are registered by name and created via factory functions:

- Prometheus collector input: `func(...plugins.Option) (plugins.InputPrometheusCollector, error)`
- Metrics-collector input: `func(...plugins.Option) (plugins.InputMetricsCollector, error)`

### Feature

- Prometheus NodeExporter (`prometheus_node_exporter`)
- HTTP GET from target endpoints (`http`)
  - Supported parsers: `prometheus`, `jmx`, `opentsdb`
  - Alias: `syncer` is registered as an alias of `http` for config compatibility
- Zookeeper TCP `mntr` (`zookeeper`)

### HTTP input config notes

For `http`/`syncer` inputs, `options.parser` supports both forms:

```yaml
options:
  parser: prometheus
```

or:

```yaml
options:
  parser:
    name: prometheus
```

## output

NewOutputFactory = func(opts ...outputs.Option) (outputs.Output, error)
outputs.RegisterFactory("name", NewOutputFactory)

```go
type Output interface {
    plugins.PluginDescriber

	// Connect to the Output; connect is only called once when the plugin starts
	Connect() error
	// Close any connections to the Output. Close is called once when the output
	// is shutting down. Close will not be called until defaults writes have finished,
	// and Write() will not be called once Close() has been, so locking is not
	// necessary.
	Close() error
	// Write takes in group of points to be written to the Output
	Write(metrics []*dto.MetricFamily) error
}
```

## Parsers

Parsers convert raw bytes into `map[string]*dto.MetricFamily`.

### Feature

- Java JMX over HTTP (`jmx`)
- Prometheus text / protobuf-delimited (`prometheus`)
- OpenTSDB (`opentsdb`)

[config sample](prome_exporters_sample.yaml)

## todo

* Output: Metrics To Kafka