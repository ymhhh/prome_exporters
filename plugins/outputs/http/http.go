package http

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ymhhh/prome_exporters/internal"
	"github.com/ymhhh/prome_exporters/plugins"
	"github.com/ymhhh/prome_exporters/plugins/outputs"
	"github.com/ymhhh/prome_exporters/plugins/serializers"

	dto "github.com/prometheus/client_model/go"
	"github.com/ymhhh/go-common/builder"
	"github.com/ymhhh/go-common/crypto/tlsconfig"
	"github.com/ymhhh/go-common/types"
)

const (
	maxErrMsgLen   = 1024
	defaultURL     = "http://127.0.0.1:9091/metrics/job/kolekti"
	defaultTimeout = 10 * time.Second
)

const (
	defaultContentType = "text/plain; charset=utf-8"
	defaultMethod      = http.MethodPost
)

type HTTP struct {
	URL                     string            `yaml:"url"`
	Method                  string            `yaml:"method"`
	Username                string            `yaml:"username"`
	Password                string            `yaml:"password"`
	Headers                 map[string]string `yaml:"headers"`
	ContentEncoding         string            `yaml:"content_encoding"`
	NonRetryableStatusCodes []int             `yaml:"non_retryable_statuscodes"`

	Timeout types.Duration `yaml:"timeout" json:"timeout"`

	SerializerConfig serializers.SerializerConfig `yaml:"serializer_config" json:"serializer_config"`

	TlsConfig *tlsconfig.Config `yaml:"tls_config" json:"tls_config"`

	client     *http.Client
	serializer serializers.Serializer

	PrintMetrics bool `yaml:"print_metrics" json:"print_metrics"`
}

func (h *HTTP) SetSerializer(serializer serializers.Serializer) {
	h.serializer = serializer
}

func (h *HTTP) Connect() error {

	if h.Method == "" {
		h.Method = defaultMethod
	}
	h.Method = strings.ToUpper(h.Method)
	if h.Method != http.MethodPost && h.Method != http.MethodPut {
		return fmt.Errorf("invalid method [%s] %s", h.URL, h.Method)
	}

	if h.URL == "" {
		h.URL = defaultURL
	}

	return nil
}

func (h *HTTP) SampleConfig() string {
	return ""
}

func (h *HTTP) Close() error {
	return nil
}

func (h *HTTP) Description() string {
	return "A plugin that can transmit metrics over HTTP"
}

func (h *HTTP) Write(metrics []*dto.MetricFamily) (err error) {
	var reqBody []byte
	reqBody, err = h.serializer.SerializeBatch(metrics)
	if err != nil {
		return
	}

	if h.PrintMetrics {
		fmt.Printf("http_output_metrics\n%s", string(reqBody))
	}

	if err = h.writeMetric(reqBody); err != nil {
		return
	}

	return nil
}

func (h *HTTP) writeMetric(reqBody []byte) error {
	var reqBodyBuffer io.Reader = bytes.NewBuffer(reqBody)

	var err error
	if h.ContentEncoding == "gzip" {
		rc, err := internal.CompressWithGzip(reqBodyBuffer)
		if err != nil {
			return err
		}
		defer rc.Close()
		reqBodyBuffer = rc
	}

	req, err := http.NewRequest(h.Method, h.URL, reqBodyBuffer)
	if err != nil {
		return err
	}

	if h.Username != "" || h.Password != "" {
		req.SetBasicAuth(h.Username, h.Password)
	}

	req.Header.Set("User-Agent", builder.Version())
	req.Header.Set("Content-Type", defaultContentType)
	if h.ContentEncoding == "gzip" {
		req.Header.Set("Content-Encoding", "gzip")
	}
	for k, v := range h.Headers {
		if strings.ToLower(k) == "host" {
			req.Host = v
		}
		req.Header.Set(k, v)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		for _, nonRetryableStatusCode := range h.NonRetryableStatusCodes {
			if resp.StatusCode == nonRetryableStatusCode {
				return nil
			}
		}

		errorLine := ""
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxErrMsgLen))
		if scanner.Scan() {
			errorLine = scanner.Text()
		}

		return fmt.Errorf("when writing to [%s] received status code: %d. body: %s", h.URL, resp.StatusCode, errorLine)
	}

	_, err = io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("when writing to [%s] received error: %v", h.URL, err)
	}

	return nil
}

func init() {
	outputs.RegisterFactory("http", func(opts ...plugins.Option) (plugins.Output, error) {
		options := &plugins.Options{}
		for _, opt := range opts {
			opt(options)
		}

		p := &HTTP{}

		if options.Config != nil {
			if err := options.Config.Object(p); err != nil {
				return nil, err
			}
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

		timeout := defaultTimeout
		if p.Timeout != 0 {
			timeout = time.Duration(p.Timeout)
		}

		p.client = &http.Client{
			Timeout:   timeout,
			Transport: transport,
		}

		var err error
		p.serializer, err = serializers.NewSerializer(&p.SerializerConfig)
		if err != nil {
			return nil, err
		}

		return p, nil
	})
}
