package http

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/ymhhh/go-common/config"
	"github.com/ymhhh/prome_exporters/internal"
	"github.com/ymhhh/prome_exporters/parsers"
	"github.com/ymhhh/prome_exporters/parsers/defaults"
	"github.com/ymhhh/prome_exporters/plugins"
	"github.com/ymhhh/prome_exporters/plugins/inputs"

	"github.com/ymhhh/go-common/crypto/tlsconfig"
	"github.com/ymhhh/go-common/types"
	"gopkg.in/yaml.v3"
)

var (
	maxErrMsgLen int64 = 1024

	defaultTimeout = 10 * time.Second
	labelInstance  = "instance"
)

type Collector struct {
	client *http.Client
	Logger *slog.Logger

	Timeout types.Duration    `yaml:"timeout" json:"timeout"`
	Urls    []string          `yaml:"urls" json:"urls"`
	Headers map[string]string `yaml:"headers" json:"headers"`

	TlsConfig *tlsconfig.Config `yaml:"tls_config" json:"tls_config"`

	Tags map[string]string `yaml:"tags" json:"tags"`

	Parser ParserConfig `yaml:"parser" json:"parser"`

	parser parsers.Parser
}

// ParserConfig supports both:
//
//	parser: prometheus
//
// and
//
//	parser:
//	  name: prometheus
//	  prefix_whitelist: [...]
type ParserConfig parsers.Config

func (c *ParserConfig) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*c = ParserConfig(parsers.Config{Name: node.Value})
		return nil
	}
	var tmp parsers.Config
	if err := node.Decode(&tmp); err != nil {
		return err
	}
	*c = ParserConfig(tmp)
	return nil
}

func (c *ParserConfig) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*c = ParserConfig(parsers.Config{})
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*c = ParserConfig(parsers.Config{Name: s})
		return nil
	}
	var tmp parsers.Config
	if err := json.Unmarshal(b, &tmp); err != nil {
		return err
	}
	*c = ParserConfig(tmp)
	return nil
}

// SampleConfig returns the sample config
func (*Collector) SampleConfig() string {
	return ``
}

// Description returns the description
func (*Collector) Description() string {
	return ``
}

// Gather ...
func (p *Collector) Gather() ([]*dto.MetricFamily, error) {

	mfs := make(map[string]*dto.MetricFamily)
	var gatherErrs []error
	for _, urlStr := range p.Urls {
		urlP, err := url.Parse(urlStr)
		if err != nil {
			p.Logger.Error("parse_url_failed", "url", urlStr, "error", err)
			gatherErrs = append(gatherErrs, err)
			continue
		}
		mfsServer, err := p.gatherServer(urlP)
		if err != nil {
			p.Logger.Error("gather_server_failed", "url", urlStr, "error", err)
			gatherErrs = append(gatherErrs, err)
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

	if len(mfs) == 0 && len(gatherErrs) > 0 {
		return nil, gatherErrs[0]
	}

	var metrics []*dto.MetricFamily
	for _, family := range mfs {
		metrics = append(metrics, family)
	}
	return metrics, nil
}

func (p *Collector) gatherServer(urlP *url.URL) (map[string]*dto.MetricFamily, error) {

	req, err := http.NewRequest("GET", urlP.String(), nil)
	if err != nil {
		return nil, err
	}
	for key, value := range p.Headers {
		req.Header.Set(key, value)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		p.Logger.Error("http request failed", "url", urlP.String(), "err", err)
		return nil, err
	}
	defer internal.IOClose(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errorLine := ""
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxErrMsgLen))
		if scanner.Scan() {
			errorLine = scanner.Text()
		}
		err := fmt.Errorf("when scraping [%s] received status code: %d. body: %s", urlP.String(), resp.StatusCode, errorLine)
		p.Logger.Error("http request failed", "err", err)
		return nil, err
	}

	bs, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	tags := make(map[string]string)
	if p.Tags != nil {
		tags = config.DeepCopy(p.Tags).(map[string]string)
	}

	tags[labelInstance] = urlP.Host

	return p.parser.Parse(bs, tags, resp.Header.Get("Content-Type"))
}

func init() {
	httpFactory := func(opts ...plugins.Option) (_ plugins.InputMetricsCollector, err error) {

		options := &plugins.Options{}
		for _, o := range opts {
			o(options)
		}

		p := &Collector{
			Logger: options.Logger,
		}

		if options.Config != nil {
			if err := options.Config.Object(p); err != nil {
				return nil, err
			}
		}

		timeout := defaultTimeout
		if p.Timeout != 0 {
			timeout = time.Duration(p.Timeout)
		}

		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		}

		if p.TlsConfig != nil {
			tlsConfig, err := p.TlsConfig.GetTLSConfig()
			if err != nil {
				return nil, err
			}
			transport.TLSClientConfig = tlsConfig
		}

		p.client = &http.Client{
			Timeout:   timeout,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		p.parser, err = defaults.NewParser(p.Logger.With("parser", p.Parser.Name), parsers.Config(p.Parser))
		if err != nil {
			return nil, err
		}

		return p, nil
	}

	// Primary name.
	inputs.RegisterFactory("http", httpFactory)
	// Backward/Config compatibility alias (e.g. sample config uses "syncer").
	inputs.RegisterFactory("syncer", httpFactory)
}
