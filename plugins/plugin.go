package plugins

import (
	"log/slog"

	"github.com/ymhhh/go-common/config"
)

// PluginDescriber contains the functions defaults plugins must implement to describe
// themselves to Telegraf. Note that defaults plugins may define a logger that is
// not part of the interface, but will receive an injected logger if it's set.
// eg: Log telegraf.Logger `toml:"-"`
type PluginDescriber interface {
	// SampleConfig returns the defaults configuration of the Processor
	SampleConfig() string

	// Description returns a one-sentence description on the Processor
	Description() string
}

type Option func(*Options)
type Options struct {
	Config config.Config
	Logger *slog.Logger
}

func Config(c config.Config) Option {
	return func(o *Options) {
		o.Config = c
	}
}

func Logger(l *slog.Logger) Option {
	return func(o *Options) {
		o.Logger = l
	}
}
