// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
