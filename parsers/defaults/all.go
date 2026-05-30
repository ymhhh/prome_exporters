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

func compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, s := range patterns {
		re, err := regexp.Compile(s)
		if err != nil {
			return nil, fmt.Errorf("invalid regexp %q: %w", s, err)
		}
		out = append(out, re)
	}
	return out, nil
}

func NewParser(logger *slog.Logger, cfg parsers.Config) (parsers.Parser, error) {
	var err error

	cfg.Whitelists, err = compilePatterns(cfg.PrefixWhitelist)
	if err != nil {
		return nil, err
	}

	cfg.Blacklists, err = compilePatterns(cfg.PrefixBlacklist)
	if err != nil {
		return nil, err
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
