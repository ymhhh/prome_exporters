package exec

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/robfig/cron/v3"
	"github.com/ymhhh/go-common/types"
	"github.com/ymhhh/prome_exporters/parsers"
	"github.com/ymhhh/prome_exporters/parsers/defaults"
	"github.com/ymhhh/prome_exporters/plugins"
	"github.com/ymhhh/prome_exporters/plugins/inputs"
	"gopkg.in/yaml.v3"
)

const (
	defaultDiscoverInterval = 5 * time.Second
	defaultScriptInterval   = 30 * time.Second
	defaultTimeout          = 10 * time.Second
	defaultMaxBodySize      = 16 << 20
	headerScanLines         = 32
)

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

// Collector discovers and runs shell/python scripts under Directory.
type Collector struct {
	Directory        string            `yaml:"directory" json:"directory"`
	Recursive        *bool             `yaml:"recursive" json:"recursive"`
	DefaultInterval  types.Duration    `yaml:"default_interval" json:"default_interval"`
	Timeout          types.Duration    `yaml:"timeout" json:"timeout"`
	DiscoverInterval types.Duration    `yaml:"discover_interval" json:"discover_interval"`
	Python           string            `yaml:"python" json:"python"`
	Shell            string            `yaml:"shell" json:"shell"`
	MaxBodySize      int64             `yaml:"max_body_size" json:"max_body_size"`
	Parser           ParserConfig      `yaml:"parser" json:"parser"`
	Env              map[string]string `yaml:"env" json:"env"`

	logger *slog.Logger
	parser parsers.Parser

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	runners map[string]*scriptRunner
	buffer  []*dto.MetricFamily
	wg      sync.WaitGroup
	started bool
}

type scriptMeta struct {
	path     string
	relPath  string
	md5      string
	kind     string // sh | py
	interval time.Duration
	schedule string
	timeout  time.Duration
}

type scriptRunner struct {
	meta   scriptMeta
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (*Collector) SampleConfig() string { return "" }
func (*Collector) Description() string {
	return "Discovers and runs shell/python scripts that emit METRIC lines"
}

func (c *Collector) Gather() ([]*dto.MetricFamily, error) {
	c.start()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.buffer) == 0 {
		return nil, nil
	}
	out := c.buffer
	c.buffer = nil
	return out, nil
}

func (c *Collector) Close() error {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	for _, r := range c.runners {
		if r.cancel != nil {
			r.cancel()
		}
	}
	c.mu.Unlock()
	c.wg.Wait()
	c.mu.Lock()
	for _, r := range c.runners {
		r.wg.Wait()
	}
	c.runners = map[string]*scriptRunner{}
	c.started = false
	c.mu.Unlock()
	return nil
}

func (c *Collector) start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.runners = make(map[string]*scriptRunner)
	c.started = true
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.discoverLoop()
	}()
}

func (c *Collector) discoverLoop() {
	c.reconcile()
	ticker := time.NewTicker(c.discoverEvery())
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.reconcile()
		}
	}
}

func (c *Collector) discoverEvery() time.Duration {
	if c.DiscoverInterval > 0 {
		return time.Duration(c.DiscoverInterval)
	}
	return defaultDiscoverInterval
}

func (c *Collector) reconcile() {
	found, err := c.scanScripts()
	if err != nil {
		c.logger.Error("exec_discover_failed", "error", err)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for path, runner := range c.runners {
		meta, ok := found[path]
		if !ok {
			c.logger.Info("exec_script_removed", "script", runner.meta.relPath)
			runner.cancel()
			delete(c.runners, path)
			c.mu.Unlock()
			runner.wg.Wait()
			c.mu.Lock()
			continue
		}
		if meta.md5 != runner.meta.md5 ||
			meta.schedule != runner.meta.schedule ||
			meta.interval != runner.meta.interval ||
			meta.timeout != runner.meta.timeout {
			c.logger.Info("exec_script_changed", "script", meta.relPath)
			old := runner
			old.cancel()
			delete(c.runners, path)
			c.mu.Unlock()
			old.wg.Wait()
			c.mu.Lock()
			c.startRunnerLocked(meta)
		}
		delete(found, path)
	}

	for _, meta := range found {
		c.logger.Info("exec_script_added", "script", meta.relPath)
		c.startRunnerLocked(meta)
	}
}

func (c *Collector) startRunnerLocked(meta scriptMeta) {
	ctx, cancel := context.WithCancel(c.ctx)
	runner := &scriptRunner{meta: meta, cancel: cancel}
	c.runners[meta.path] = runner
	runner.wg.Add(1)
	go func() {
		defer runner.wg.Done()
		c.runScript(ctx, meta)
	}()
}

func (c *Collector) runScript(ctx context.Context, meta scriptMeta) {
	runOnce := func() {
		if err := c.executeOnce(ctx, meta); err != nil {
			c.logger.Warn("exec_script_failed", "script", meta.relPath, "error", err)
		}
	}

	if meta.schedule != "" {
		cr := cron.New()
		_, err := cr.AddFunc(meta.schedule, runOnce)
		if err != nil {
			c.logger.Warn("exec_invalid_schedule", "script", meta.relPath, "schedule", meta.schedule, "error", err)
			return
		}
		cr.Start()
		defer cr.Stop()
		<-ctx.Done()
		return
	}

	runOnce()
	ticker := time.NewTicker(meta.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}

func (c *Collector) executeOnce(parent context.Context, meta scriptMeta) error {
	ctx, cancel := context.WithTimeout(parent, meta.timeout)
	defer cancel()

	var cmd *exec.Cmd
	switch meta.kind {
	case "py":
		cmd = exec.CommandContext(ctx, c.pythonBin(), meta.path)
	default:
		cmd = exec.CommandContext(ctx, c.shellBin(), meta.path)
	}
	cmd.Dir = filepath.Dir(meta.path)
	cmd.Env = os.Environ()
	for k, v := range c.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if stderr.Len() > 0 {
		c.logger.Debug("exec_script_stderr", "script", meta.relPath, "stderr", truncate(stderr.String(), 512))
	}
	if err != nil {
		return err
	}

	limit := c.maxBody()
	bs := stdout.Bytes()
	if int64(len(bs)) > limit {
		return fmt.Errorf("stdout exceeds max_body_size %d", limit)
	}

	mfs, err := c.parser.Parse(bs, map[string]string{"script": meta.relPath}, "")
	if err != nil {
		return err
	}

	c.mu.Lock()
	for _, mf := range mfs {
		c.buffer = append(c.buffer, mf)
	}
	c.mu.Unlock()
	return nil
}

func (c *Collector) scanScripts() (map[string]scriptMeta, error) {
	if c.Directory == "" {
		return nil, fmt.Errorf("directory is required")
	}
	root, err := filepath.Abs(c.Directory)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("directory is not a directory: %s", root)
	}

	recursive := true
	if c.Recursive != nil {
		recursive = *c.Recursive
	}

	out := make(map[string]scriptMeta)
	walkFn := func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path == root {
				return nil
			}
			if !recursive {
				return filepath.SkipDir
			}
			if name == ".git" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, "~") {
			return nil
		}
		kind := ""
		switch strings.ToLower(filepath.Ext(name)) {
		case ".sh":
			kind = "sh"
		case ".py":
			kind = "py"
		default:
			return nil
		}

		sum, body, err := fileMD5(path)
		if err != nil {
			c.logger.Warn("exec_read_failed", "path", path, "error", err)
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		meta := scriptMeta{
			path:     path,
			relPath:  filepath.ToSlash(rel),
			md5:      sum,
			kind:     kind,
			interval: c.defaultInterval(),
			timeout:  c.defaultTimeout(),
		}
		applyHeader(&meta, body, c.defaultInterval(), c.defaultTimeout(), c.logger)
		out[path] = meta
		return nil
	}

	if err := filepath.WalkDir(root, walkFn); err != nil {
		return nil, err
	}
	return out, nil
}

func applyHeader(meta *scriptMeta, body []byte, defaultInterval, defaultTimeout time.Duration, logger *slog.Logger) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	lines := 0
	for scanner.Scan() {
		lines++
		if lines > headerScanLines {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#!") {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			break
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "prome.schedule":
			meta.schedule = val
			meta.interval = 0
		case "prome.interval":
			d, err := time.ParseDuration(val)
			if err != nil {
				logger.Warn("exec_invalid_interval", "script", meta.relPath, "value", val, "error", err)
				continue
			}
			if d < time.Second {
				d = time.Second
			}
			meta.interval = d
			meta.schedule = ""
		case "prome.timeout":
			d, err := time.ParseDuration(val)
			if err != nil {
				logger.Warn("exec_invalid_timeout", "script", meta.relPath, "value", val, "error", err)
				continue
			}
			if d > 0 {
				meta.timeout = d
			}
		}
	}
	if meta.schedule == "" && meta.interval <= 0 {
		meta.interval = defaultInterval
	}
	if meta.timeout <= 0 {
		meta.timeout = defaultTimeout
	}
}

func fileMD5(path string) (string, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	sum := md5.New()
	body, err := io.ReadAll(io.TeeReader(f, sum))
	if err != nil {
		return "", nil, err
	}
	return hex.EncodeToString(sum.Sum(nil)), body, nil
}

func (c *Collector) defaultInterval() time.Duration {
	if c.DefaultInterval > 0 {
		d := time.Duration(c.DefaultInterval)
		if d < time.Second {
			return time.Second
		}
		return d
	}
	return defaultScriptInterval
}

func (c *Collector) defaultTimeout() time.Duration {
	if c.Timeout > 0 {
		return time.Duration(c.Timeout)
	}
	return defaultTimeout
}

func (c *Collector) maxBody() int64 {
	if c.MaxBodySize > 0 {
		return c.MaxBodySize
	}
	return defaultMaxBodySize
}

func (c *Collector) pythonBin() string {
	if c.Python != "" {
		return c.Python
	}
	return "python3"
}

func (c *Collector) shellBin() string {
	if c.Shell != "" {
		return c.Shell
	}
	return "bash"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func init() {
	inputs.RegisterFactory("exec", func(opts ...plugins.Option) (plugins.InputMetricsCollector, error) {
		options := &plugins.Options{}
		for _, o := range opts {
			o(options)
		}
		c := &Collector{logger: options.Logger}
		if options.Config != nil {
			if err := options.Config.Object(c); err != nil {
				return nil, err
			}
		}
		if c.Directory == "" {
			return nil, fmt.Errorf("exec directory is required")
		}
		if c.Parser.Name == "" {
			c.Parser.Name = "simple"
		}
		var err error
		c.parser, err = defaults.NewParser(c.logger.With("parser", c.Parser.Name), parsers.Config(c.Parser))
		if err != nil {
			return nil, err
		}
		return c, nil
	})
}
