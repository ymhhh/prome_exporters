package serializers

import (
	"strings"

	"github.com/go-kit/log"
	"github.com/ymhhh/go-common/config"
	"github.com/ymhhh/go-common/errcode"
)

type Factory func(...Option) (Serializer, error)

type Option func(*Options)
type Options struct {
	Config config.Config
	Logger log.Logger
}

func Config(c config.Config) Option {
	return func(o *Options) {
		o.Config = c
	}
}

func Logger(l log.Logger) Option {
	return func(o *Options) {
		o.Logger = l
	}
}

var outputs = map[string]Factory{}

func RegisterFactory(name string, fn any) {
	if name = strings.TrimSpace(name); name == "" {
		panic(errcode.New("empty serializer name"))
	}
	if fn == nil {
		panic(errcode.New("nil serializer factory"))
	}

	switch f := fn.(type) {
	case Factory:
		outputs[name] = f
	case func(...Option) (Serializer, error):
		outputs[name] = f
	default:
		panic(errcode.Newf("not supported serializer factory: %s", name))
	}
}

func GetFactory(name string) (Factory, error) {
	fn, ok := outputs[name]
	if !ok || fn == nil {
		return nil, errcode.Newf("not found serializer factory: %s", name)
	}
	return fn, nil
}
