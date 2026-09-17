# prome_exporters

A pluggable agent that **collects**, **parses**, and **forwards** Prometheus-style metrics.

## Quick start

```bash
go test ./...
go run . --config.file prome_exporters.yaml
```

Full example: [prome_exporters_sample.yaml](prome_exporters_sample.yaml).

## Running mode (`exporter.command_type`)

| Value | Mode | Behavior |
| ----- | ---- | -------- |
| `0` | command | Collect and push in the foreground. Stops on `SIGINT` / `SIGTERM` / `SIGUSR1` / `SIGUSR2`. |
| `1` | server | Same collect/push loop, plus an HTTP server (`/metrics`, optional `/probe`). |

```yaml
exporter:
  command_type: 1
  flush_interval: 10s          # output flush period; default 1s (minimum 1s)
  metric_buffer_limit: 10000   # default 10000
  metric_batch_size: 10000     # default 10000
  global_tags:
    env: prod
  blackbox_probe:
    open: false                # requires command_type: 1 and at least one module
```

`global_tags` are added on flush and skipped when a sample already has that label.

## Inputs

Each input needs `name` and (optionally) `interval` (default / minimum `1s`). Plugin-specific fields belong under `options`, not at the input root.

### `prometheus_node_exporter`

```yaml
- name: prometheus_node_exporter
  interval: 5s
  options:
    filters: ["processes", "textfile.directory", "systemd"]
```

`filters` selects node_exporter collectors. Omit it to enable the default set.

The `instance` label is the host IP:

- **Linux**: source address of the default route (UDP connect, no packet sent). `hostname -I` is not used.
- **macOS**: `ipconfig getifaddr en0` / `en1`, then interface enumeration.
- **Windows / other**: interface enumeration.

The first successful lookup is cached. An empty result is not cached, so a later scrape can still pick up the address if the interface was not ready at startup.

### `http` (alias `syncer`)

Scrapes one or more URLs over HTTP GET.

```yaml
- name: http
  interval: 5s
  options:
    urls: ["http://127.0.0.1:8080/metrics"]
    parser: prometheus          # or jmx / opentsdb; default prometheus
    timeout: 10s                # default 10s
    max_body_size: 16777216     # bytes; default 16MiB
    headers:
      Authorization: Bearer ...
    tags:
      cluster: test
    tls_config:
      insecure_skip_verify: false
```

`parser` also accepts an object:

```yaml
parser:
  name: prometheus
  prefix_whitelist: ["^http_"]
  prefix_blacklist: ["^go_"]
  jmx_ignore_prefix: false
  opentsdb_ignore_timestamp: false
```

`prefix_whitelist` / `prefix_blacklist` are regular expressions. `syncer` is the same plugin as `http`.

### `zookeeper`

Reads the `mntr` command over TCP (optional TLS).

```yaml
- name: zookeeper
  interval: 10s
  options:
    servers: ["127.0.0.1:2181"]
    timeout: 5s
    tags:
      parser_type: zookeeper
```

## Output

Only `name` and `options` are read. A top-level `output.url` or `output.interval` is ignored; flush timing is `exporter.flush_interval`.

### `http`

```yaml
output:
  name: http
  options:
    url: http://127.0.0.1:9091/metrics/job/test
    method: POST                # POST or PUT; default POST
    timeout: 10s
    content_encoding: gzip      # optional
    print_metrics: false
    username: ""
    password: ""
    serializer_config:
      name: prometheus
```

Default URL is `http://127.0.0.1:9091/metrics/job/kolekti`.

## Parsers

| Name | Input | Notes |
| ---- | ----- | ----- |
| `prometheus` | exposition text or protobuf-delimited | Content-Type selects the decoder |
| `jmx` | Jolokia-style JSON beans | Bean tags are isolated; `tag.*` uses a real prefix strip (`tag.gc` → `gc`) |
| `opentsdb` | JSON array of datapoints | Optional `opentsdb_ignore_timestamp` |

## Flags

Shared:

- `--config.file` (default `prome_exporters.yaml`)
- promslog flags (use `--log.level=debug` to see per-interval gather/flush logs)

Server mode (exporter-toolkit):

- `--web.listen-address` (repeatable; first address is the bind address, default `:10031`)
- `--web.config.file` (TLS / auth)
- `--web.telemetry-path` (default `/metrics`)
- `--web.max-requests` (default `40`; `0` disables the limit)
- `--web.disable-exporter-metrics` — omit `process_*`, `go_*`, and `promhttp_*`
- `--probe.history.limit` (default `100`)
- `--probe.timeout-offset` (default `0.5`)

When `exporter.blackbox_probe.open` is true, `/probe` is served with Prometheus blackbox modules.

## Extending

Inputs register a factory:

- Prometheus collector: `func(...plugins.Option) (plugins.InputPrometheusCollector, error)`
- Metrics collector: `func(...plugins.Option) (plugins.InputMetricsCollector, error)`

Outputs register `func(...plugins.Option) (plugins.Output, error)` implementing `Connect` / `Write` / `Close`.

## todo

* Output: Metrics To Kafka
