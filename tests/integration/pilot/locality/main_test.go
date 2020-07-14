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

package locality

import (
	"bytes"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"testing"
	"text/template"
	"time"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)

const (
	sendCount = 50

	deploymentYAML = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  hosts:
  - {{.Host}}
  exportTo:
  - "."
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  location: MESH_EXTERNAL
  endpoints:
  {{ if ne .NonExistantService "" }}
  - address: {{.NonExistantService}}
    locality: "{{.NonExistantServiceLocality}}"
  {{ end }}
  - address: {{.ServiceBAddress}}
    locality: {{.ServiceBLocality}}
  - address: {{.ServiceCAddress}}
    locality: {{.ServiceCLocality}}
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: {{.Name}}-route
  namespace: {{.Namespace}}
spec:
  hosts:
  - {{.Host}}
  http:
  - route:
    - destination:
        host: {{.Host}}
    retries:
      attempts: 3
      perTryTimeout: 1s
      retryOn: gateway-error,connect-failure,refused-stream
`

	failoverYaml = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: {{.Name}}-destination
  namespace: {{.Namespace}}
spec:
  host: {{.Host}}
  trafficPolicy:
    loadBalancer:
      simple: ROUND_ROBIN
      localityLbSetting:
        failover:
        - from: region
          to: closeregion
    outlierDetection:
      interval: 1s
      baseEjectionTime: 3m
      maxEjectionPercent: 100
`
	distributeYaml = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: {{.Name}}-destination
  namespace: {{.Namespace}}
spec:
  host: {{.Host}}
  trafficPolicy:
    loadBalancer:
      simple: ROUND_ROBIN
      localityLbSetting:
        distribute:
        - from: region
          to:
            notregion: 80
            region: 20
    outlierDetection:
      interval: 1s
      baseEjectionTime: 3m
      maxEjectionPercent: 100
`
	disabledYaml = `
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: {{.Name}}-destination
  namespace: {{.Namespace}}
spec:
  host: {{.Host}}
  trafficPolicy:
    loadBalancer:
      simple: ROUND_ROBIN
      localityLbSetting:
        enabled: false
    outlierDetection:
      interval: 1s
      baseEjectionTime: 3m
      maxEjectionPercent: 100
`
)

var (
	deploymentTemplate *template.Template
	failoverTemplate   *template.Template
	distributeTemplate *template.Template
	disabledTemplate   *template.Template

	expectAllTrafficToB = map[string]int{"b": sendCount}

	ist istio.Instance
	r   *rand.Rand
)

func init() {
	var err error
	deploymentTemplate, err = template.New("localityTemplate").Parse(deploymentYAML)
	if err != nil {
		panic(err)
	}
	failoverTemplate, err = template.New("localityTemplate").Parse(failoverYaml)
	if err != nil {
		panic(err)
	}
	distributeTemplate, err = template.New("localityTemplate").Parse(distributeYaml)
	if err != nil {
		panic(err)
	}
	disabledTemplate, err = template.New("localityTemplate").Parse(disabledYaml)
	if err != nil {
		panic(err)
	}
}

func TestMain(m *testing.M) {
	framework.NewSuite(m).
		RequireSingleCluster().
		Label(label.CustomSetup).
		Setup(istio.Setup(&ist, nil)).
		Setup(func(ctx resource.Context) (err error) {
			r = rand.New(rand.NewSource(time.Now().UnixNano()))
			return nil
		}).
		Run()
}

func echoConfig(ns namespace.Instance, name string) echo.Config {
	return echo.Config{
		Service:   name,
		Namespace: ns,
		Locality:  "region.zone.subzone",
		Subsets:   []echo.SubsetConfig{{}},
		Ports: []echo.Port{
			{
				Name:        "http",
				Protocol:    protocol.HTTP,
				ServicePort: 80,
				// We use a port > 1024 to not require root
				InstancePort: 8090,
			},
		},
	}
}

type serviceConfig struct {
	Name                       string
	Host                       string
	Namespace                  string
	Resolution                 string
	ServiceBAddress            string
	ServiceBLocality           string
	ServiceCAddress            string
	ServiceCLocality           string
	NonExistantService         string
	NonExistantServiceLocality string
}

func deploy(t test.Failer, ctx resource.Context, ns namespace.Instance, se serviceConfig, from echo.Instance, tmpl *template.Template) {
	t.Helper()
	var buf bytes.Buffer
	if err := deploymentTemplate.Execute(&buf, se); err != nil {
		t.Fatal(err)
	}
	ctx.Config().ApplyYAMLOrFail(t, ns.Name(), buf.String())
	buf.Reset()
	if err := tmpl.Execute(&buf, se); err != nil {
		t.Fatal(err)
	}
	ctx.Config().ApplyYAMLOrFail(t, ns.Name(), buf.String())

	err := WaitUntilRoute(from, se.Host)
	if err != nil {
		t.Fatalf("Failed to get expected route: %v", err)
	}
}

// Wait for our route for the "fake" target to be established
func WaitUntilRoute(c echo.Instance, dest string) error {
	accept := func(cfg *envoyAdmin.ConfigDump) (bool, error) {
		validator := structpath.ForProto(cfg)
		if err := validator.
			Exists("{.configs[*].dynamicRouteConfigs[*].routeConfig.virtualHosts[?(@.name == '%s')]}", dest+":80").
			Check(); err != nil {
			return false, err
		}
		clusterName := fmt.Sprintf("outbound|%d||%s", 80, dest)
		if err := validator.
			Exists("{.configs[*].dynamicActiveClusters[?(@.cluster.name == '%s')]}", clusterName).
			Check(); err != nil {
			return false, err
		}
		return true, nil
	}

	workloads, _ := c.Workloads()
	// Wait for the outbound config to be received by each workload from Pilot.
	for _, w := range workloads {
		if w.Sidecar() != nil {
			if err := w.Sidecar().WaitForConfig(accept, retry.Timeout(time.Second*10)); err != nil {
				return err
			}
		}
	}

	return nil
}

func sendTraffic(from echo.Instance, host string, expected map[string]int) error {
	headers := http.Header{}
	headers.Add("Host", host)
	// This is a hack to remain infrastructure agnostic when running these tests
	// We actually call the host set above not the endpoint we pass
	resp, err := from.Call(echo.CallOptions{
		Target:   from,
		PortName: "http",
		Headers:  headers,
		Count:    sendCount,
	})
	if err != nil {
		return fmt.Errorf("%s->%s failed sending: %v", from.Config().Service, host, err)
	}
	if len(resp) != sendCount {
		return fmt.Errorf("%s->%s expected %d responses, received %d", from.Config().Service, host, sendCount, len(resp))
	}
	got := map[string]int{}
	for _, r := range resp {
		// Hostname will take form of svc-v1-random. We want to extract just 'svc'
		parts := strings.SplitN(r.Hostname, "-", 2)
		if len(parts) < 2 {
			return fmt.Errorf("unexpected hostname: %v", r)
		}
		gotHost := parts[0]
		got[gotHost]++
	}
	for svc, reqs := range got {
		expect := expected[svc]
		if !almostEquals(reqs, expect, 3) {
			return fmt.Errorf("unexpected request distribution. Expected: %+v, got: %+v", expected, got)
		}
	}
	return nil
}

func almostEquals(a, b, precision int) bool {
	upper := a + precision
	lower := a - precision
	if b < lower || b > upper {
		return false
	}
	return true
}
