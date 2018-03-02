// Copyright 2018 Istio Authors
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

	v3routing "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/model"
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
      protocol: http
    hosts:
    - "*.example.com"
`

var routeRuleYAML = `
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
        name: some.example.internal
`

func TestFileSnapshotterNoFilter(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	ts := &testState{
		ConfigFiles: map[string][]byte{"gateway.yml": []byte(gatewayYAML)},
	}

	ts.testSetup(t)
	defer ts.testTeardown(t)

	fileWatcher := monitor.NewFileSnapshot(ts.rootPath, nil)
	configs := fileWatcher.ReadFile()

	g.Expect(configs).To(gomega.HaveLen(1))

	gateway := configs[0].Spec.(*v3routing.Gateway)
	g.Expect(gateway.Servers[0].Port.Number).To(gomega.Equal(uint32(80)))
	g.Expect(gateway.Servers[0].Port.Protocol).To(gomega.Equal("http"))
	g.Expect(gateway.Servers[0].Hosts).To(gomega.Equal([]string{"*.example.com"}))
}

func TestFileSnapshotterWithFilter(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	ts := &testState{
		ConfigFiles: map[string][]byte{
			"gateway.yml":    []byte(gatewayYAML),
			"route_rule.yml": []byte(routeRuleYAML),
		},
	}

	ts.testSetup(t)
	defer ts.testTeardown(t)

	fileWatcher := monitor.NewFileSnapshot(ts.rootPath, model.ConfigDescriptor{model.VirtualService})
	configs := fileWatcher.ReadFile()

	g.Expect(configs).To(gomega.HaveLen(1))

	routeRule := configs[0].Spec.(*v3routing.VirtualService)
	g.Expect(routeRule.Hosts).To(gomega.Equal([]string{"some.example.com"}))
}

func TestFileSnapshotterSorting(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	ts := &testState{
		ConfigFiles: map[string][]byte{
			"z.yml": []byte(gatewayYAML),
			"a.yml": []byte(routeRuleYAML),
		},
	}

	ts.testSetup(t)
	defer ts.testTeardown(t)

	fileWatcher := monitor.NewFileSnapshot(ts.rootPath, nil)

	configs := fileWatcher.ReadFile()

	g.Expect(configs).To(gomega.HaveLen(2))

	g.Expect(configs[0].Spec).To(gomega.BeAssignableToTypeOf(&v3routing.Gateway{}))
	g.Expect(configs[1].Spec).To(gomega.BeAssignableToTypeOf(&v3routing.VirtualService{}))
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
