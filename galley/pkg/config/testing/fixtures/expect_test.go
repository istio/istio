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

	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
)

func TestExpect(t *testing.T) {
	acc := &fixtures.Accumulator{}
	acc.Handle(data.Event1Col1AddItem1)
	acc.Handle(data.Event2Col1AddItem2)

	fixtures.Expect(t, acc, data.Event1Col1AddItem1, data.Event2Col1AddItem2)
}

func TestExpect_FullSync(t *testing.T) {
	acc := &fixtures.Accumulator{}
	acc.Handle(data.Event1Col1Synced)

	fixtures.ExpectFullSync(t, acc, basicmeta.K8SCollection1)
}

func TestExpect_None(t *testing.T) {
	acc := &fixtures.Accumulator{}

	fixtures.ExpectNone(t, acc)
}

func TestExpect_ExpectFilter(t *testing.T) {
	acc := &fixtures.Accumulator{}
	acc.Handle(data.Event1Col1AddItem1)
	acc.Handle(data.Event1Col1Synced)
	acc.Handle(data.Event2Col1AddItem2)

	fixtures.ExpectFilter(
		t, acc, fixtures.NoFullSync, data.Event1Col1AddItem1, data.Event2Col1AddItem2)
}
