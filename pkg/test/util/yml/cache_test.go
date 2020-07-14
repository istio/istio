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

func TestCache_Apply_Basic(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	c := NewCache(d)

	keys, err := c.Apply(gateway)
	g.Expect(err).To(BeNil())
	g.Expect(keys).To(HaveLen(1))
	expected := CacheKey{
		group:     "networking.istio.io",
		kind:      "Gateway",
		namespace: "",
		name:      "some-ingress",
	}
	g.Expect(keys[0]).To(Equal(expected))
	key1 := keys[0]

	file := c.GetFileFor(keys[0])
	by, err := ioutil.ReadFile(file)
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(gateway)))

	keys, err = c.Apply(virtualService)
	g.Expect(err).To(BeNil())
	g.Expect(keys).To(HaveLen(1))
	expected = CacheKey{
		group:     "networking.istio.io",
		kind:      "VirtualService",
		namespace: "",
		name:      "route-for-myapp",
	}
	g.Expect(keys[0]).To(Equal(expected))
	key2 := keys[0]

	file = c.GetFileFor(keys[0])
	by, err = ioutil.ReadFile(file)
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(virtualService)))

	keys = c.AllKeys()
	g.Expect(keys).To(HaveLen(2))
	g.Expect(keys).To(ContainElement(key1))
	g.Expect(keys).To(ContainElement(key2))

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(2))
}

func TestCache_Apply_MultiPart(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	c := NewCache(d)

	keys, err := c.Apply(JoinString(gateway, virtualService))
	g.Expect(err).To(BeNil())
	g.Expect(keys).To(HaveLen(2))
	expected := CacheKey{
		group:     "networking.istio.io",
		kind:      "Gateway",
		namespace: "",
		name:      "some-ingress",
	}
	g.Expect(keys[0]).To(Equal(expected))

	file := c.GetFileFor(keys[0])
	by, err := ioutil.ReadFile(file)
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(gateway)))

	expected = CacheKey{
		group:     "networking.istio.io",
		kind:      "VirtualService",
		namespace: "",
		name:      "route-for-myapp",
	}
	g.Expect(keys[1]).To(Equal(expected))

	file = c.GetFileFor(keys[1])
	by, err = ioutil.ReadFile(file)
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(virtualService)))

	applyKeys := keys
	keys = c.AllKeys()
	g.Expect(keys).To(HaveLen(2))
	g.Expect(keys).To(ContainElement(applyKeys[0]))
	g.Expect(keys).To(ContainElement(applyKeys[1]))

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(2))
}

func TestCache_Apply_Add_Update(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	c := NewCache(d)

	keys, err := c.Apply(gateway)
	g.Expect(err).To(BeNil())

	file := c.GetFileFor(keys[0])
	by, err := ioutil.ReadFile(file)
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(gateway)))

	keys, err = c.Apply(updatedGateway)
	g.Expect(err).To(BeNil())
	file = c.GetFileFor(keys[0])
	by, err = ioutil.ReadFile(file)
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(updatedGateway)))

	applyKeys := keys
	keys = c.AllKeys()
	g.Expect(keys).To(HaveLen(1))
	g.Expect(keys).To(ContainElement(applyKeys[0]))

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(1))
}

func TestCache_Apply_SameContent(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	c := NewCache(d)

	keys1, err := c.Apply(gateway)
	g.Expect(err).To(BeNil())

	keys2, err := c.Apply(gateway)
	g.Expect(err).To(BeNil())

	keys := c.AllKeys()
	g.Expect(keys).To(HaveLen(1))
	g.Expect(keys1).To(HaveLen(1))
	g.Expect(keys2).To(HaveLen(1))
	g.Expect(keys).To(ContainElement(keys1[0]))
	g.Expect(keys).To(ContainElement(keys2[0]))

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(1))
}

func TestCache_Clear(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	c := NewCache(d)

	_, err = c.Apply(gateway)
	g.Expect(err).To(BeNil())

	_, err = c.Apply(virtualService)
	g.Expect(err).To(BeNil())

	err = c.Clear()
	g.Expect(err).To(BeNil())

	keys := c.AllKeys()
	g.Expect(keys).To(HaveLen(0))

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(0))
}

func TestCache_GetFileFor_Empty(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	c := NewCache(d)

	f := c.GetFileFor(CacheKey{})
	g.Expect(f).To(BeEmpty())
}

func TestCache_Delete(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	c := NewCache(d)

	_, err = c.Apply(gateway)
	g.Expect(err).To(BeNil())

	keys1, err := c.Apply(virtualService)
	g.Expect(err).To(BeNil())

	err = c.Delete(gateway)
	g.Expect(err).To(BeNil())

	keys := c.AllKeys()
	g.Expect(keys).To(HaveLen(1))
	g.Expect(keys1).To(HaveLen(1))
	g.Expect(keys).To(ContainElement(keys1[0]))

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(1))

	by, err := ioutil.ReadFile(path.Join(d, items[0].Name()))
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(virtualService)))
}

func TestCache_Delete_Missing(t *testing.T) {
	g := NewGomegaWithT(t)
	d, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())
	t.Logf("Test Dir: %q", d)

	c := NewCache(d)

	_, err = c.Apply(gateway)
	g.Expect(err).To(BeNil())

	err = c.Delete(virtualService)
	g.Expect(err).To(BeNil())

	items, err := ioutil.ReadDir(d)
	g.Expect(err).To(BeNil())
	g.Expect(items).To(HaveLen(1))

	by, err := ioutil.ReadFile(path.Join(d, items[0].Name()))
	g.Expect(err).To(BeNil())
	g.Expect(strings.TrimSpace(string(by))).To(Equal(strings.TrimSpace(gateway)))
}
