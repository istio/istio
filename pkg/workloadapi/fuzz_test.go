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

package workloadapi_test

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/fuzz"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/workloadapi"
)

func FuzzMarshal(f *testing.F) {
	fuzzMarshal[*workloadapi.Address](f)
}

func fuzzMarshal[T proto.Message](f test.Fuzzer) {
	fuzz.Fuzz(f, func(fg fuzz.Helper) {
		obj := fuzz.Struct[T](fg)
		protoconv.MessageToAny(obj)
		assert.Equal(fg.T(), protoconv.Equals(obj, obj), true)
	})
}
