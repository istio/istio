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

package diag

import (
	"encoding/json"
	"testing"

	"istio.io/istio/pkg/config/resource"

	. "github.com/onsi/gomega"
)

func TestMessage_String(t *testing.T) {
	g := NewGomegaWithT(t)
	mt := NewMessageType(Error, "IST-0042", "Cheese type not found: %q")
	m := NewMessage(mt, nil, "Feta")

	g.Expect(m.String()).To(Equal(`Error [IST-0042] Cheese type not found: "Feta"`))
}

func TestMessageWithResource_String(t *testing.T) {
	g := NewGomegaWithT(t)
	mt := NewMessageType(Error, "IST-0042", "Cheese type not found: %q")
	m := NewMessage(mt, &resource.Instance{Origin: testOrigin{name: "toppings/cheese", ref: testReference{"path/to/file"}}}, "Feta")

	g.Expect(m.String()).To(Equal(`Error [IST-0042] (toppings/cheese path/to/file) Cheese type not found: "Feta"`))
}

func TestMessage_Unstructured(t *testing.T) {
	g := NewGomegaWithT(t)
	mt := NewMessageType(Error, "IST-0042", "Cheese type not found: %q")
	m := NewMessage(mt, nil, "Feta")

	g.Expect(m.Unstructured(true)).To(Not(HaveKey("origin")))
	g.Expect(m.Unstructured(false)).To(Not(HaveKey("origin")))

	m = NewMessage(mt, &resource.Instance{Origin: testOrigin{name: "toppings/cheese"}}, "Feta")

	g.Expect(m.Unstructured(true)).To((HaveKey("origin")))
	g.Expect(m.Unstructured(false)).To(Not(HaveKey("origin")))
}

func TestMessageWithDocRef(t *testing.T) {
	g := NewGomegaWithT(t)
	mt := NewMessageType(Error, "IST0042", "Cheese type not found: %q")
	m := NewMessage(mt, nil, "Feta")
	m.DocRef = "test-ref"
	g.Expect(m.Unstructured(false)["documentation_url"]).To(Equal("https://istio.io/docs/reference/config/analysis/ist0042/?ref=test-ref"))
}

func TestMessage_JSON(t *testing.T) {
	g := NewGomegaWithT(t)
	mt := NewMessageType(Error, "IST0042", "Cheese type not found: %q")
	m := NewMessage(mt, &resource.Instance{Origin: testOrigin{name: "toppings/cheese", ref: testReference{"path/to/file"}}}, "Feta")

	j, _ := json.Marshal(&m)
	g.Expect(string(j)).To(Equal(`{"code":"IST0042","documentation_url":"https://istio.io/docs/reference/config/analysis/ist0042/"` +
		`,"level":"Error","message":"Cheese type not found: \"Feta\"","origin":"toppings/cheese","reference":"path/to/file"}`))
}
