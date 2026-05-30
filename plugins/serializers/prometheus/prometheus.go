package prometheus

import (
	"bytes"

	"github.com/ymhhh/prome_exporters/plugins/serializers"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func init() {
	serializers.RegisterFactory("prometheus", NewSerializer)
}

type Serializer struct{}

func NewSerializer(...serializers.Option) (serializers.Serializer, error) {
	return &Serializer{}, nil
}

func (s *Serializer) Serialize(metric *dto.MetricFamily) ([]byte, error) {
	return s.SerializeBatch([]*dto.MetricFamily{metric})
}

func (s *Serializer) SerializeBatch(metrics []*dto.MetricFamily) ([]byte, error) {

	var buf bytes.Buffer
	for _, mf := range metrics {
		enc := expfmt.NewEncoder(&buf, expfmt.FmtText)
		err := enc.Encode(mf)
		if err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}
