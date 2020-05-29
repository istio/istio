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

package monitor_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

var gatewayYAML = `
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

var virtualServiceYAML = `
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

func TestFileSnapshotNoFilter(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	ts := &testState{
		ConfigFiles: map[string][]byte{"gateway.yml": []byte(gatewayYAML)},
	}

	ts.testSetup(t)
	defer ts.testTeardown(t)

	fileWatcher := monitor.NewFileSnapshot(ts.rootPath, collection.SchemasFor(), "foo")
	configs, err := fileWatcher.ReadConfigFiles()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configs).To(gomega.HaveLen(1))
	g.Expect(configs[0].Domain).To(gomega.Equal("foo"))

	gateway := configs[0].Spec.(*networking.Gateway)
	g.Expect(gateway.Servers[0].Port.Number).To(gomega.Equal(uint32(80)))
	g.Expect(gateway.Servers[0].Port.Protocol).To(gomega.Equal("http"))
	g.Expect(gateway.Servers[0].Hosts).To(gomega.Equal([]string{"*.example.com"}))
}

func TestFileSnapshotWithFilter(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	ts := &testState{
		ConfigFiles: map[string][]byte{
			"gateway.yml":         []byte(gatewayYAML),
			"virtual_service.yml": []byte(virtualServiceYAML),
		},
	}

	ts.testSetup(t)
	defer ts.testTeardown(t)

	fileWatcher := monitor.NewFileSnapshot(ts.rootPath, collection.SchemasFor(collections.IstioNetworkingV1Alpha3Virtualservices), "")
	configs, err := fileWatcher.ReadConfigFiles()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configs).To(gomega.HaveLen(1))

	virtualService := configs[0].Spec.(*networking.VirtualService)
	g.Expect(virtualService.Hosts).To(gomega.Equal([]string{"some.example.com"}))
}

func TestFileSnapshotSorting(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	ts := &testState{
		ConfigFiles: map[string][]byte{
			"z.yml": []byte(gatewayYAML),
			"a.yml": []byte(virtualServiceYAML),
		},
	}

	ts.testSetup(t)
	defer ts.testTeardown(t)

	fileWatcher := monitor.NewFileSnapshot(ts.rootPath, collection.SchemasFor(), "")

	configs, err := fileWatcher.ReadConfigFiles()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(configs).To(gomega.HaveLen(2))

	g.Expect(configs[0].Spec).To(gomega.BeAssignableToTypeOf(&networking.Gateway{}))
	g.Expect(configs[1].Spec).To(gomega.BeAssignableToTypeOf(&networking.VirtualService{}))
}

type testState struct {
	ConfigFiles map[string][]byte
	rootPath    string
}

func (ts *testState) testSetup(t *testing.T) {
	var err error

	ts.rootPath, err = ioutil.TempDir("", "config-root")
	if err != nil {
		t.Fatal(err)
	}

	for name, content := range ts.ConfigFiles {
		err = ioutil.WriteFile(filepath.Join(ts.rootPath, name), content, 0600)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func (ts *testState) testTeardown(t *testing.T) {
	err := os.RemoveAll(ts.rootPath)
	if err != nil {
		t.Fatal(err)
	}
}
