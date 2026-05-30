package zookeeper

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ymhhh/prome_exporters/plugins"
	"github.com/ymhhh/prome_exporters/plugins/inputs"

	"log/slog"

	dto "github.com/prometheus/client_model/go"
	"github.com/ymhhh/go-common/crypto/tlsconfig"
	"github.com/ymhhh/go-common/types"
)

var (
	zookeeperFormatRE = regexp.MustCompile(`(^zk_\w+)\s+([\w\.\-]+)`)

	defaultTimeout = 5 * time.Second

	metricPrefix              = "zookeeper_"
	zkServerStateName         = fmt.Sprintf("%szk_server_state", metricPrefix)
	zkVersionName             = fmt.Sprintf("%szk_version", metricPrefix)
	defaultValue      float64 = 1
)

const (
	sampleConfig = ``
)

var (
	labelPort     = "port"
	labelState    = "state"
	labelServer   = "server"
	labelInstance = "instance"
)

type Collector struct {
	logger    *slog.Logger
	tlsConfig *tls.Config

	Servers []string       `yaml:"servers" json:"servers"`
	Timeout types.Duration `yaml:"timeout" json:"timeout"`

	TlsConfig *tlsconfig.Config `yaml:"tls_config" json:"tls_config"`

	Tags map[string]string `yaml:"tags" json:"tags"`
}

// SampleConfig returns sample configuration message
func (p *Collector) SampleConfig() string {
	return sampleConfig
}

// Description returns description of Zookeeper plugin
func (p *Collector) Description() string {
	return `Reads 'mntr' stats from one or many zookeeper servers`
}

// Gather reads stats from defaults configured servers accumulates stats
func (p *Collector) Gather() ([]*dto.MetricFamily, error) {
	ctx := context.Background()

	if p.Timeout < types.Duration(1*time.Second) {
		p.Timeout = types.Duration(defaultTimeout)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.Timeout))
	defer cancel()

	if len(p.Servers) == 0 {
		p.Servers = []string{":2181"}
	}

	mfs := make(map[string]*dto.MetricFamily)
	for _, serverAddress := range p.Servers {
		mfsServer, err := p.gatherServer(ctx, serverAddress)
		if err != nil {
			continue
		}
		for name, family := range mfsServer {
			mf, ok := mfs[name]
			if !ok {
				mfs[name] = family
				continue
			}
			mf.Metric = append(mf.Metric, family.GetMetric()...)
		}
	}
	var metrics []*dto.MetricFamily
	for _, family := range mfs {
		metrics = append(metrics, family)
	}
	return metrics, nil
}

func (p *Collector) dial(ctx context.Context, addr string) (net.Conn, error) {
	var dialer net.Dialer
	if p.tlsConfig != nil {
		deadline, ok := ctx.Deadline()
		if ok {
			dialer.Deadline = deadline
		}
		return tls.DialWithDialer(&dialer, "tcp", addr, p.tlsConfig)
	}
	return dialer.DialContext(ctx, "tcp", addr)
}

func (p *Collector) gatherServer(ctx context.Context, address string) (map[string]*dto.MetricFamily, error) {
	_, _, err := net.SplitHostPort(address)
	if err != nil {
		address = address + ":2181"
	}

	c, err := p.dial(ctx, address)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	// Apply deadline to connection
	deadline, ok := ctx.Deadline()
	if ok {
		if err := c.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}

	if _, err := fmt.Fprintf(c, "%s\n", "mntr"); err != nil {
		return nil, err
	}
	rdr := bufio.NewReader(c)
	scanner := bufio.NewScanner(rdr)

	service := strings.Split(address, ":")
	if len(service) != 2 {
		return nil, fmt.Errorf("invalid service address: %s", address)
	}

	srv := "localhost"
	if service[0] != "" {
		srv = service[0]
	}

	var metrics = make(map[string]*dto.MetricFamily)
	var zookeeperState string

	for scanner.Scan() {
		line := scanner.Text()
		parts := zookeeperFormatRE.FindStringSubmatch(line)

		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected line in mntr response: %q", line)
		}

		//metricName := strings.TrimPrefix(parts[1], "zk_")
		//if metricName == "server_state" {
		//	zookeeperState = parts[2]
		//} else {
		//	sValue := parts[2]
		//
		//	iVal, err := strconv.ParseInt(sValue, 10, 64)
		//	if err == nil {
		//		fields[measurement] = iVal
		//	} else {
		//		fields[measurement] = sValue
		//	}
		//}

		//measurement := strings.TrimPrefix(parts[1], "zk_") // 社区去掉了zk_
		metricName := fmt.Sprintf("%s%s", metricPrefix, parts[1])

		mf, ok := metrics[metricName]
		if !ok {
			typ := dto.MetricType_UNTYPED
			mf = &dto.MetricFamily{
				Name: &metricName,
				Type: &typ,
			}
		}

		if metricName == zkServerStateName {
			zookeeperState = parts[2]
			mf.Metric = append(mf.Metric, &dto.Metric{
				Untyped: &dto.Untyped{Value: &defaultValue},
			})
		} else if metricName == zkVersionName {
			mf.Metric = append(mf.Metric, &dto.Metric{
				Label: []*dto.LabelPair{
					{Name: &parts[1], Value: &parts[2]},
				},
				Untyped: &dto.Untyped{Value: &defaultValue},
			})

		} else {
			sValue := parts[2]
			iVal, err := strconv.ParseFloat(sValue, 64)
			if err == nil {
				mf.Metric = append(mf.Metric, &dto.Metric{
					Untyped: &dto.Untyped{Value: &iVal},
				})
			}
		}
		metrics[metricName] = mf
	}

	for _, mf := range metrics {
		for i, metric := range mf.GetMetric() {
			metric.Label = append(metric.Label,
				&dto.LabelPair{Name: &labelState, Value: &zookeeperState},
				&dto.LabelPair{Name: &labelInstance, Value: &address},
				&dto.LabelPair{Name: &labelServer, Value: &srv},
				&dto.LabelPair{Name: &labelPort, Value: &service[1]})
			mf.GetMetric()[i] = metric
			for k, v := range p.Tags {
				key, value := k, v //
				metric.Label = append(metric.Label, &dto.LabelPair{Name: &key, Value: &value})
			}
		}
	}

	return metrics, nil
}

func init() {
	inputs.RegisterFactory("zookeeper", func(opts ...plugins.Option) (plugins.InputMetricsCollector, error) {

		options := &plugins.Options{}
		for _, o := range opts {
			o(options)
		}

		p := &Collector{
			logger: options.Logger,
		}

		if options.Config != nil {
			if err := options.Config.Object(p); err != nil {
				return nil, err
			}
		}

		if p.TlsConfig != nil {
			tlsConfig, err := p.TlsConfig.GetTLSConfig()
			if err != nil {
				return nil, err
			}
			p.tlsConfig = tlsConfig
		}

		return p, nil
	})
}
