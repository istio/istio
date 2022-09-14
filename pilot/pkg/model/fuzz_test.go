package model

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pkg/fuzz"
)

func FuzzDeepCopyService(f *testing.F) {
	fuzzDeepCopy[*Service](f, cmp.AllowUnexported(), cmpopts.IgnoreFields(AddressMap{}, "mutex"))
}

func FuzzDeepCopyServiceInstance(f *testing.F) {
	fuzzDeepCopy[*ServiceInstance](f, cmp.AllowUnexported(), cmpopts.IgnoreFields(AddressMap{}, "mutex"))
}

func FuzzDeepCopyWorkloadInstance(f *testing.F) {
	fuzzDeepCopy[*WorkloadInstance](f, protocmp.Transform(), cmp.AllowUnexported())
}

func FuzzDeepCopyIstioEndpoint(f *testing.F) {
	fuzzDeepCopy[*IstioEndpoint](f, protocmp.Transform(), cmp.AllowUnexported())
}

type deepCopier[T any] interface {
	DeepCopy() T
}

func fuzzDeepCopy[T deepCopier[T]](f *testing.F, opts ...cmp.Option) {
	f.Fuzz(func(t *testing.T, data []byte) {
		fg := fuzz.New(t, data)
		orig := fuzz.Struct[T](fg)
		copied := orig.DeepCopy()
		if !cmp.Equal(orig, copied, opts...) {
			diff := cmp.Diff(orig, copied, opts...)
			t.Fatalf("unexpected diff %v", diff)
		}
	})
}
