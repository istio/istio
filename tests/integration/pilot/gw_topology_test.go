//go:build integ
// +build integ

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

package pilot

import (
	"fmt"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/tests/integration/pilot/common"
)

func TestXFFGateway(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ingress.topology").
		Run(func(t framework.TestContext) {
			gatewayNs := namespace.NewOrFail(t, t, namespace.Config{Prefix: "custom-gateway"})
			injectLabel := `sidecar.istio.io/inject: "true"`
			if len(t.Settings().Revisions.Default()) > 0 {
				injectLabel = fmt.Sprintf(`istio.io/rev: "%v"`, t.Settings().Revisions.Default())
			}

			templateParams := map[string]string{
				"imagePullSecret": t.Settings().Image.PullSecret,
				"injectLabel":     injectLabel,
				"imagePullPolicy": t.Settings().Image.PullPolicy,
			}

			// we only apply to config clusters
			t.ConfigIstio().Eval(gatewayNs.Name(), templateParams, `apiVersion: v1
kind: Service
metadata:
  name: custom-gateway
  labels:
    istio: ingressgateway
spec:
  ports:
  - port: 80
    targetPort: 8080
    name: http
  selector:
    istio: ingressgateway
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: custom-gateway
spec:
  selector:
    matchLabels:
      istio: ingressgateway
  template:
    metadata:
      annotations:
        inject.istio.io/templates: gateway
        proxy.istio.io/config: |
          gatewayTopology:
            numTrustedProxies: 2
      labels:
        istio: ingressgateway
        {{ .injectLabel }}
    spec:
      {{- if ne .imagePullSecret "" }}
      imagePullSecrets:
      - name: {{ .imagePullSecret }}
      {{- end }}
      containers:
      - name: istio-proxy
        image: auto
        imagePullPolicy: {{ .imagePullPolicy }}
---
`).ApplyOrFail(t)
			cs := t.Clusters().Default().(*kubecluster.Cluster)
			retry.UntilSuccessOrFail(t, func() error {
				_, err := kubetest.CheckPodsAreReady(kubetest.NewPodFetch(cs, gatewayNs.Name(), "istio=ingressgateway"))
				return err
			}, retry.Timeout(time.Minute*2), retry.Delay(time.Second))
			for _, tt := range common.XFFGatewayCase(&apps, fmt.Sprintf("custom-gateway.%s.svc.cluster.local", gatewayNs.Name())) {
				tt.Run(t, apps.Namespace.Name())
			}
		})
}
