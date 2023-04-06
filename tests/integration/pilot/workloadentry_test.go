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
	"bytes"
	"fmt"
	"os/exec"
	"testing"

	"istio.io/istio/pkg/test/echo/common/scheme"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
)

func TestWorkloadEntry(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		Features("traffic.reachability").
		Run(func(t framework.TestContext) {
			// Define a simulated EW gateway
			gatewayCfg := `apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: ingress-ew
  namespace: istio-system
spec:
  selector:
    app: istio-ingressgateway
  servers:
  - port:
      number: 8443
      name: https
      protocol: TLS
    hosts:
    - serviceentry.mesh.cluster.local
    tls:
      mode: AUTO_PASSTHROUGH
`
			// Setup a Gateway to act as simulated EW
			if err := t.ConfigIstio().YAML("istio-system", gatewayCfg).Apply(apply.NoCleanup); err != nil {
				t.Fatal(err)
			}

			// get pod IP of simulated EW gateway so we can use that IP in as the WorkloadEntry address
			externalGetPodCommand := exec.Command("kubectl", "get", "pod", "-A", "-l", "app=istio-ingressgateway", "-o", `jsonpath="{.items[0].status.podIP}"`)
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			externalGetPodCommand.Stdout = stdout
			externalGetPodCommand.Stderr = stderr
			err := externalGetPodCommand.Run()
			if err != nil {
				t.Fatal(err)
			}
			ingressPodIP := stdout.String()

			workloadEntryYaml := fmt.Sprintf(`apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: a-se
spec:
  addresses:
  - 240.240.34.56
  hosts:
  - serviceentry.mesh.cluster.local
  ports:
  - name: http
    number: 80
    protocol: HTTP
    targetPort: 8080
  location: MESH_INTERNAL
  resolution: STATIC
  workloadSelector:
    labels:
      app: a
---
apiVersion: networking.istio.io/v1beta1
kind: WorkloadEntry
metadata:
  name: a-we
  labels:
    security.istio.io/tlsMode: istio
spec:
  network: other
  ports:
    http: 8443
  address: %s
  labels:
    security.istio.io/tlsMode: istio
    app: a`, ingressPodIP)

			aNamespace := apps.A.Instances().NamespaceName()
			if err := t.ConfigIstio().YAML(aNamespace, workloadEntryYaml).Apply(apply.NoCleanup); err != nil {
				t.Fatal(err)
			}

			srcs := apps.All.Instances()
			for _, src := range srcs {
				srcName := src.Config().NamespacedName().Name
				// Skipping tests for these workloads:
				//      external
				//      naked
				//      proxyless-grpc
				//      vm
				if srcName == "proxyless-grpc" || srcName == "naked" || srcName == "external" || srcName == "vm" {
					continue
				}

				// Assert that non-skipped workloads can reach the service which includes our workload entry
				t.NewSubTestf("%s to ServiceEntry+WorkloadEntry Responds with 200", srcName).Run(func(t framework.TestContext) {
					src.CallOrFail(t, echo.CallOptions{
						Address: "serviceentry.mesh.cluster.local",
						Port:    echo.Port{Name: "http", ServicePort: 80},
						Scheme:  scheme.HTTP,
						HTTP: echo.HTTP{
							Path: "/path",
						},
						Check: check.OK(),
					})
				})
			}

			if !t.Settings().NoCleanup { // perform cleanup when not using --istio.test.nocleanup

				if err := t.ConfigIstio().YAML(aNamespace, workloadEntryYaml).Delete(); err != nil {
					t.Fatal(err)
				}

				if err := t.ConfigIstio().YAML("istio-system", gatewayCfg).Delete(); err != nil {
					t.Fatal(err)
				}

				cleanupSrc := apps.B.Instances()[0] // get the first instance of app b to test the cleanup worked

				t.NewSubTestf("ServiceEntry+WorkloadEntry cleanup").Run(func(t framework.TestContext) {
					cleanupSrc.CallOrFail(t, echo.CallOptions{
						Address: "serviceentry.istio.io",
						Port:    echo.Port{Name: "http", ServicePort: 80},
						Scheme:  scheme.HTTP,
						HTTP: echo.HTTP{
							Path: "/any/path",
						},
						Check: check.NotOK(),
					})
				})
			}
		})
}
