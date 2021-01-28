package staticvm

import "istio.io/istio/pkg/test/framework/components/namespace"

var _ namespace.Instance = fakeNamespace("")

// fakeNamespace allows matching echo.Configs against a namespace.Instance
type fakeNamespace string

func (f fakeNamespace) Name() string {
	return string(f)
}

func (f fakeNamespace) SetLabel(key, value string) error {
	panic("cannot interact with fake namespace, should not be exposed outside of staticvm")
}

func (f fakeNamespace) RemoveLabel(key string) error {
	panic("cannot interact with fake namespace, should not be exposed outside of staticvm")
}
