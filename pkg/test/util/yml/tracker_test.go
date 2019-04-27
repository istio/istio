// Copyright 2019 Istio Authors
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

package yml

import (
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

var (
	gateway = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: some-ingress
spec:
  servers:
  - port:
      number: 80
      name: http
      protocol: http
    hosts:
    - "*.example.com"
`

	updatedGateway = `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: some-ingress
spec:
  servers:
  - port:
      number: 8080
      name: https
      protocol: https
    hosts:
    - "*.example.com"
`

	virtualService = `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: route-for-myapp
spec:
  hosts:
  - some.example.com
  gateways:
  - some-ingress
  http:
  - route:
    - destination:
        host: some.example.internal
`
)

func TestTracker_Apply_Basic(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	tr := NewTracker(d)

	keys, err := tr.Apply(gateway)
	g.Expect(err).To(BeNil())
	g.Expect(keys).To(HaveLen(1))
	expected := TrackerKey{
		group:     "networking.istio.io",
		kind:      "Gateway",
		namespace: "",
		name:      "some-ingress",
	}
	g.Expect(keys[0]).To(Equal(expected))

	file := tr.GetFileFor(keys[0])
	by, err := ioutil.ReadFile(file)
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(gateway)))

	keys, err = tr.Apply(virtualService)
	g.Expect(err).To(BeNil())
	g.Expect(keys).To(HaveLen(1))
	expected = TrackerKey{
		group:     "networking.istio.io",
		kind:      "VirtualService",
		namespace: "",
		name:      "route-for-myapp",
	}
	g.Expect(keys[0]).To(Equal(expected))

	file = tr.GetFileFor(keys[0])
	by, err = ioutil.ReadFile(file)
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(virtualService)))

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(2))
}

func TestTracker_Apply_MultiPart(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	tr := NewTracker(d)

	keys, err := tr.Apply(JoinString(gateway, virtualService))
	g.Expect(err).To(BeNil())
	g.Expect(keys).To(HaveLen(2))
	expected := TrackerKey{
		group:     "networking.istio.io",
		kind:      "Gateway",
		namespace: "",
		name:      "some-ingress",
	}
	g.Expect(keys[0]).To(Equal(expected))

	file := tr.GetFileFor(keys[0])
	by, err := ioutil.ReadFile(file)
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(gateway)))

	expected = TrackerKey{
		group:     "networking.istio.io",
		kind:      "VirtualService",
		namespace: "",
		name:      "route-for-myapp",
	}
	g.Expect(keys[1]).To(Equal(expected))

	file = tr.GetFileFor(keys[1])
	by, err = ioutil.ReadFile(file)
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(virtualService)))

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(2))
}

func TestTracker_Apply_Add_Update(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	tr := NewTracker(d)

	keys, err := tr.Apply(gateway)
	g.Expect(err).To(BeNil())

	file := tr.GetFileFor(keys[0])
	by, err := ioutil.ReadFile(file)
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(gateway)))

	keys, err = tr.Apply(updatedGateway)
	g.Expect(err).To(BeNil())
	file = tr.GetFileFor(keys[0])
	by, err = ioutil.ReadFile(file)
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(updatedGateway)))

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(1))
}

func TestTracker_Apply_SameContent(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	tr := NewTracker(d)

	_, err = tr.Apply(gateway)
	g.Expect(err).To(BeNil())

	_, err = tr.Apply(gateway)
	g.Expect(err).To(BeNil())

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(1))
}

func TestTracker_Clear(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	tr := NewTracker(d)

	_, err = tr.Apply(gateway)
	g.Expect(err).To(BeNil())

	_, err = tr.Apply(virtualService)
	g.Expect(err).To(BeNil())

	err = tr.Clear()
	g.Expect(err).To(BeNil())

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(0))
}

func TestTracker_GetFileFor_Empty(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	tr := NewTracker(d)

	f := tr.GetFileFor(TrackerKey{})
	g.Expect(f).To(BeEmpty())
}

func TestTracker_Delete(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	tr := NewTracker(d)

	_, err = tr.Apply(gateway)
	g.Expect(err).To(BeNil())

	_, err = tr.Apply(virtualService)
	g.Expect(err).To(BeNil())

	err = tr.Delete(gateway)
	g.Expect(err).To(BeNil())

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(1))

	by, err := ioutil.ReadFile(path.Join(d, items[0].Name()))
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(virtualService)))
}

func TestTracker_Delete_Missing(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	tr := NewTracker(d)

	_, err = tr.Apply(gateway)
	g.Expect(err).To(BeNil())

	err = tr.Delete(virtualService)
	g.Expect(err).To(BeNil())

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(1))

	by, err := ioutil.ReadFile(path.Join(d, items[0].Name()))
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(gateway)))
}
