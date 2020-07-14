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

package fixtures

import (
	"testing"

	"github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/pkg/config/event"
)

func TestDispatcher(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	d := &Listener{}

	h1 := &Accumulator{}
	d.Dispatch(h1)

	d.Handlers.Handle(data.Event1Col1AddItem1)

	expected := []event.Event{data.Event1Col1AddItem1}
	g.Expect(h1.Events()).To(gomega.Equal(expected))

}
