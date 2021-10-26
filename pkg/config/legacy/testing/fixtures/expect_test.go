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

package fixtures_test

import (
	"testing"

	basicmeta2 "istio.io/istio/pkg/config/legacy/testing/basicmeta"
	data2 "istio.io/istio/pkg/config/legacy/testing/data"
	fixtures2 "istio.io/istio/pkg/config/legacy/testing/fixtures"
)

func TestExpect(t *testing.T) {
	acc := &fixtures2.Accumulator{}
	acc.Handle(data2.Event1Col1AddItem1)
	acc.Handle(data2.Event2Col1AddItem2)

	fixtures2.Expect(t, acc, data2.Event1Col1AddItem1, data2.Event2Col1AddItem2)
}

func TestExpect_FullSync(t *testing.T) {
	acc := &fixtures2.Accumulator{}
	acc.Handle(data2.Event1Col1Synced)

	fixtures2.ExpectFullSync(t, acc, basicmeta2.K8SCollection1)
}

func TestExpect_None(t *testing.T) {
	acc := &fixtures2.Accumulator{}

	fixtures2.ExpectNone(t, acc)
}

func TestExpect_ExpectFilter(t *testing.T) {
	acc := &fixtures2.Accumulator{}
	acc.Handle(data2.Event1Col1AddItem1)
	acc.Handle(data2.Event1Col1Synced)
	acc.Handle(data2.Event2Col1AddItem2)

	fixtures2.ExpectFilter(
		t, acc, fixtures2.NoFullSync, data2.Event1Col1AddItem1, data2.Event2Col1AddItem2)
}
