package cluster

import (
	"fmt"
)

type Factory interface {
	Kind() Kind
	With(config ...Config) Factory
	Build() (Clusters, error)
}

// FactoryFunc validates a config and builds a single echo instance.
type FactoryFunc func(cfg Config, allClusters Map) (Cluster, error)

var factoryRegistry = map[Kind]FactoryFunc{}

// RegisterFactory globally registers a base factory of a given Kind.
// The given factory should be immutable, as it will be used globally.
func RegisterFactory(kind Kind, factory FactoryFunc) {
	factoryRegistry[kind] = factory
}

func GetFactory(config Config) (FactoryFunc, error) {
	f, ok := factoryRegistry[config.Kind]
	if !ok {
		return nil, fmt.Errorf("unsupported cluster kind %s", config.Kind)
	}
	return f, nil
}
