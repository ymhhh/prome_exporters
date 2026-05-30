package serializers

var defaultSerializerConfig = &SerializerConfig{
	Name: "prometheus",
}

type SerializerConfig struct {
	Name string `yaml:"name" json:"name"`
}

func NewSerializer(c *SerializerConfig) (Serializer, error) {
	if c == nil || c.Name == "" {
		c = defaultSerializerConfig
	}

	f, err := GetFactory(c.Name)
	if err != nil {
		return nil, err
	}

	return f()
}
