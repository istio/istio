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

package trafficmanagement

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
)

var (
	inst istio.Instance
)

func TestMain(m *testing.M) {
	framework.
		NewSuite("trafficmanagement", m).
		SetupOnEnv(environment.Kube, istio.Setup(&inst, setupConfig)).
		RequireEnvironment(environment.Kube).
		Run()
}

func setupConfig(cfg *istio.Config) {
	if cfg == nil {
		return
	}
	// add a gateway configuration that includes a TCP port
	cfg.ControlPlaneValues = `
values:
  gateways:
    istio-ingressgateway:
      ports:
      - port: 15020
        targetPort: 15020
        name: status-port
      - port: 80
        targetPort: 8080
        name: http2
      - port: 443
        targetPort: 8443
        name: https
      - port: 31400
        targetPort: 31400
        name: tcp
      - port: 15029
        targetPort: 15029
        name: https-kiali
      - port: 15030
        targetPort: 15030
        name: https-prometheus
      - port: 15031
        targetPort: 15031
        name: https-grafana
      - port: 15032
        targetPort: 15032
        name: https-tracing
      - port: 15443
        targetPort: 15443
        name: tls
`
}
