package exec

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ymhhh/go-common/types"
	"github.com/ymhhh/prome_exporters/parsers"
	"github.com/ymhhh/prome_exporters/parsers/defaults"
)

func TestApplyHeaderIntervalAndSchedule(t *testing.T) {
	meta := scriptMeta{relPath: "a.sh"}
	body := []byte("#!/usr/bin/env bash\n# prome.interval: 15s\n# prome.timeout: 3s\necho hi\n")
	applyHeader(&meta, body, 30*time.Second, 10*time.Second, slog.Default())
	if meta.interval != 15*time.Second {
		t.Fatalf("interval=%v", meta.interval)
	}
	if meta.timeout != 3*time.Second {
		t.Fatalf("timeout=%v", meta.timeout)
	}
	if meta.schedule != "" {
		t.Fatalf("schedule should be empty, got %q", meta.schedule)
	}

	meta = scriptMeta{relPath: "b.py"}
	body = []byte("#!/usr/bin/env python3\n# prome.schedule: */5 * * * *\nprint(1)\n")
	applyHeader(&meta, body, 30*time.Second, 10*time.Second, slog.Default())
	if meta.schedule != "*/5 * * * *" {
		t.Fatalf("schedule=%q", meta.schedule)
	}
}

func TestGatherEndToEnd(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(sub, "disk.sh")
	content := "#!/usr/bin/env bash\n# prome.interval: 1s\necho noisy\necho \"METRIC disk_used 42 mount=/data\"\necho \"metric ignored 1\"\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	c := newTestCollector(t, dir)
	defer c.Close()

	var families map[string]bool
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		mfs, err := c.Gather()
		if err != nil {
			t.Fatal(err)
		}
		if len(mfs) > 0 {
			families = map[string]bool{}
			for _, mf := range mfs {
				families[mf.GetName()] = true
				for _, metric := range mf.GetMetric() {
					hasScript := false
					for _, lp := range metric.GetLabel() {
						if lp.GetName() == "script" && strings.Contains(lp.GetValue(), "disk.sh") {
							hasScript = true
						}
					}
					if !hasScript {
						t.Fatalf("missing script label: %#v", metric.GetLabel())
					}
				}
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !families["disk_used"] {
		t.Fatalf("expected disk_used, got %v", families)
	}
	if families["ignored"] {
		t.Fatal("lowercase metric prefix should not be parsed")
	}
}

func TestMD5ChangeRestartsScript(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "x.sh")
	v1 := "#!/usr/bin/env bash\n# prome.interval: 1s\necho \"METRIC version 1\"\n"
	if err := os.WriteFile(script, []byte(v1), 0o755); err != nil {
		t.Fatal(err)
	}

	c := newTestCollector(t, dir)
	c.DiscoverInterval = types.Duration(200 * time.Millisecond)
	defer c.Close()

	waitForMetric(t, c, "version")

	v2 := "#!/usr/bin/env bash\n# prome.interval: 1s\necho \"METRIC version2 2\"\n"
	if err := os.WriteFile(script, []byte(v2), 0o755); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mfs, err := c.Gather()
		if err != nil {
			t.Fatal(err)
		}
		for _, mf := range mfs {
			if mf.GetName() == "version2" {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("expected version2 after script content change")
}

func TestScriptRemovalStopsRunner(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "gone.sh")
	content := "#!/usr/bin/env bash\n# prome.interval: 1s\necho \"METRIC alive 1\"\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	c := newTestCollector(t, dir)
	c.DiscoverInterval = types.Duration(200 * time.Millisecond)
	defer c.Close()

	waitForMetric(t, c, "alive")
	if err := os.Remove(script); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		n := len(c.runners)
		c.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("expected runner to be removed after script delete")
}

func newTestCollector(t *testing.T, dir string) *Collector {
	t.Helper()
	logger := slog.Default()
	parser, err := defaults.NewParser(logger, parsers.Config{Name: "simple"})
	if err != nil {
		t.Fatal(err)
	}
	rec := true
	return &Collector{
		Directory:        dir,
		Recursive:        &rec,
		DefaultInterval:  types.Duration(time.Second),
		Timeout:          types.Duration(5 * time.Second),
		DiscoverInterval: types.Duration(200 * time.Millisecond),
		logger:           logger,
		parser:           parser,
	}
}

func waitForMetric(t *testing.T, c *Collector, name string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		mfs, err := c.Gather()
		if err != nil {
			t.Fatal(err)
		}
		for _, mf := range mfs {
			if mf.GetName() == name {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for metric %s", name)
}
