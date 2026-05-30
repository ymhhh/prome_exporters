package defaults

import (
	"fmt"
	"log/slog"
	"regexp"

	"github.com/ymhhh/prome_exporters/parsers"
	"github.com/ymhhh/prome_exporters/parsers/jmx"
	"github.com/ymhhh/prome_exporters/parsers/opentsdb"
	"github.com/ymhhh/prome_exporters/parsers/prometheus"
)

func NewParser(logger *slog.Logger, cfg parsers.Config) (parsers.Parser, error) {

	for _, s := range cfg.PrefixWhitelist {
		cfg.Whitelists = append(cfg.Whitelists, regexp.MustCompile(s))
	}

	for _, s := range cfg.PrefixBlacklist {
		cfg.Blacklists = append(cfg.Blacklists, regexp.MustCompile(s))
	}

	switch cfg.Name {
	case "", "prometheus":
		return prometheus.NewParser(logger, cfg)
	case "jmx":
		return jmx.NewParser(logger, cfg)
	case "opentsdb":
		return opentsdb.NewParser(logger, cfg)
	default:
		return nil, fmt.Errorf("unsupported parser type")
	}
}
