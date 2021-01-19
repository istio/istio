package cluster

import "fmt"

var factoryRegistry = map[Kind]Factory{}

// RegisterFactory globally registers a base factory of a given Kind.
// The given factory should be immutable, as it will be used globally.
func RegisterFactory(factory Factory) {
	factoryRegistry[factory.Kind()] = factory
}

func GetFactory(kind Kind) (Factory, error) {
	f, ok := factoryRegistry[kind]
	if !ok {
		return nil, fmt.Errorf("unsupported cluster kind: %q", kind)
	}
	return f, nil
}
