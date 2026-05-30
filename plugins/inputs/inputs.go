package inputs

import (
	"reflect"
	"strings"

	"github.com/ymhhh/prome_exporters/plugins"

	"github.com/ymhhh/go-common/errcode"
)

type FactoryPrometheusCollector func(...plugins.Option) (plugins.InputPrometheusCollector, error)
type FactoryMetricsCollector func(...plugins.Option) (plugins.InputMetricsCollector, error)

var (
	mapNewCollectorFunc = make(map[string]*Input)
)

type Input struct {
	inputType plugins.InputType

	pcFactory FactoryPrometheusCollector
	mcFactory FactoryMetricsCollector
}

func (p *Input) InputType() plugins.InputType {
	return p.inputType
}

func (p *Input) NewPrometheusCollector(opts ...plugins.Option) (plugins.InputPrometheusCollector, error) {
	if p.inputType != plugins.InputTypePrometheusCollector || p.pcFactory == nil {
		return nil, errcode.Newf("its not prometheus collector factory, type: %d, %+v", p.inputType, p.pcFactory)
	}

	return p.pcFactory(opts...)
}

func (p *Input) NewMetricsCollector(opts ...plugins.Option) (plugins.InputMetricsCollector, error) {
	if p.inputType != plugins.InputTypeMetricsCollector || p.mcFactory == nil {
		return nil, errcode.Newf("its not metrics collector factory, type: %d, %+v", p.inputType, p.mcFactory)
	}

	return p.mcFactory(opts...)
}

func RegisterFactory(name string, fn any) {
	if name = strings.TrimSpace(name); name == "" {
		panic(errcode.New("empty collector name"))
	}
	if fn == nil {
		panic(errcode.New("nil collector factory"))
	}
	if _, ok := mapNewCollectorFunc[name]; ok {
		panic(errcode.Newf("collector factory already exists: %s", name))
	}

	var input = &Input{}
	switch t := fn.(type) {
	case FactoryPrometheusCollector:
		input.inputType = plugins.InputTypePrometheusCollector
		input.pcFactory = t
	case func(...plugins.Option) (plugins.InputPrometheusCollector, error):
		input.inputType = plugins.InputTypePrometheusCollector
		input.pcFactory = t
	case FactoryMetricsCollector:
		input.inputType = plugins.InputTypeMetricsCollector
		input.mcFactory = t
	case func(...plugins.Option) (plugins.InputMetricsCollector, error):
		input.inputType = plugins.InputTypeMetricsCollector
		input.mcFactory = t

	default:
		panic(errcode.Newf("not supported type : %s, %+v", name, reflect.TypeOf(t).String()))
	}
	mapNewCollectorFunc[name] = input
}

func GetFactory(name string) (*Input, error) {
	input, ok := mapNewCollectorFunc[name]
	if !ok || input == nil {
		return nil, errcode.Newf("not found collector factory: %s", name)
	}
	return input, nil
}
