package gateway

import (
	"testing"

	"istio.io/istio/pkg/fuzz"
)

func FuzzConvertResources(f *testing.F) {
	fuzz.BaseCases(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		fg := fuzz.New(t, data)
		r := fuzz.Struct[KubernetesResources](fg)
		convertResources(r)
	})
}
