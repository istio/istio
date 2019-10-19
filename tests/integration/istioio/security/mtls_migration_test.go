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

package security

import (
	"testing"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/istioio"
)

//https://istio.io/docs/tasks/security/mtls-migration/
//https://github.com/istio/istio.io/blob/release-1.2/content/docs/tasks/security/mtls-migration/index.md
func TestMutualTLSMigration(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/17909")

	framework.
		NewTest(t).
		Run(istioio.NewBuilder("tasks__security__mututal_tls_migration").
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "create_ns_foo_bar_legacy.sh",
					Value: `
$ kubectl create ns foo
$ kubectl apply -f <(istioctl kube-inject -f @samples/httpbin/httpbin.yaml@) -n foo
$ kubectl apply -f <(istioctl kube-inject -f @samples/sleep/sleep.yaml@) -n foo
$ kubectl create ns bar
$ kubectl apply -f <(istioctl kube-inject -f @samples/httpbin/httpbin.yaml@) -n bar
$ kubectl apply -f <(istioctl kube-inject -f @samples/sleep/sleep.yaml@) -n bar
$ kubectl create ns legacy
$ kubectl apply -f samples/sleep/sleep.yaml -n legacy`,
				},
				WorkDir:       env.IstioSrc,
				CreateSnippet: true,
			}).

			// Wait for pods to start.
			Add(istioio.MultiPodWait("foo"),
				istioio.MultiPodWait("bar"),
				istioio.MultiPodWait("legacy")).

			// Verify everything is setup correctly.
			Add(istioio.Command{
				Input: istioio.Inline{
					FileName: "curl_foo_bar_legacy.sh",
					// nolint: llll
					Value: `$ for from in "foo" "bar" "legacy"; do kubectl exec $(kubectl get pod -l app=sleep -n ${from} -o jsonpath={.items..metadata.name}) -c sleep -n ${from} -- curl http://httpbin.foo:8000/ip -s -o /dev/null -w "sleep.${from} to httpbin.foo: %{http_code}\n"; done`,
				},
				Verify: istioio.TokenVerifier(istioio.Inline{
					Value: `
sleep.foo to httpbin.foo: 200
sleep.bar to httpbin.foo: 200
sleep.legacy to httpbin.foo: 200`,
				}),
				CreateSnippet:          true,
				IncludeOutputInSnippet: true,
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "verify_initial_policies.sh",
					Value:    "$ kubectl get policies.authentication.istio.io --all-namespaces",
				},
				Verify: istioio.TokenVerifier(
					istioio.Inline{
						Value: `
NAMESPACE      NAME                          AGE
istio-system   grafana-ports-mtls-disabled   ?`,
					}),
				CreateSnippet:          true,
				IncludeOutputInSnippet: true,
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "verify_initial_destinationrules.sh",
					Value:    "$ kubectl get destinationrule --all-namespaces",
				},
				Verify: istioio.TokenVerifier(
					istioio.Inline{
						Value: `
NAMESPACE      NAME              HOST                                             AGE
istio-system   istio-policy      istio-policy.istio-system.svc.cluster.local      ?
istio-system   istio-telemetry   istio-telemetry.istio-system.svc.cluster.local   ?`,
					}),
				CreateSnippet:          true,
				IncludeOutputInSnippet: true,
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "configure_mtls_destinationrule.sh",
					Value: `
$ cat <<EOF | kubectl apply -n foo -f -
apiVersion: "networking.istio.io/v1alpha3"
kind: "DestinationRule"
metadata:
  name: "example-httpbin-istio-client-mtls"
spec:
  host: httpbin.foo.svc.cluster.local
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
EOF`,
				},
				CreateSnippet: true,
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "curl_foo_bar_legacy_post_dr.sh",
					// nolint: lll
					Value: `$ for from in "foo" "bar" "legacy"; do kubectl exec $(kubectl get pod -l app=sleep -n ${from} -o jsonpath={.items..metadata.name}) -c sleep -n ${from} -- curl http://httpbin.foo:8000/ip -s -o /dev/null -w "sleep.${from} to httpbin.foo: %{http_code}\n"; done`,
				},
				Verify: istioio.TokenVerifier(
					istioio.Inline{
						Value: `
sleep.foo to httpbin.foo: 200
sleep.bar to httpbin.foo: 200
sleep.legacy to httpbin.foo: 200`,
					}),
				CreateSnippet:          true,
				IncludeOutputInSnippet: true,
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "httpbin_foo_mtls_only.sh",
					Value: `
$ cat <<EOF | kubectl apply -n foo -f -
apiVersion: "authentication.istio.io/v1alpha1"
kind: "Policy"
metadata:
  name: "example-httpbin-strict"
  namespace: foo
spec:
  targets:
  - name: httpbin
  peers:
  - mtls:
      mode: STRICT
EOF`,
				},
				CreateSnippet: true,
			}, istioio.Command{
				Input: istioio.Inline{
					FileName: "curl_foo_bar_legacy_httpbin_foo_mtls.sh",
					// nolint: lll
					Value: `$ for from in "foo" "bar" "legacy"; do kubectl exec $(kubectl get pod -l app=sleep -n ${from} -o jsonpath={.items..metadata.name}) -c sleep -n ${from} -- curl http://httpbin.foo:8000/ip -s -o /dev/null -w "sleep.${from} to httpbin.foo: %{http_code}\n"; done`,
				},
				Verify: istioio.TokenVerifier(
					istioio.Inline{
						Value: `
sleep.foo to httpbin.foo: 200
sleep.bar to httpbin.foo: 200
sleep.legacy to httpbin.foo: 000
command terminated with exit code 56`,
					}),
				CreateSnippet:          true,
				IncludeOutputInSnippet: true,
			}).

			// Cleanup.
			Defer(istioio.Command{
				Input: istioio.Inline{
					FileName: "cleanup.sh",
					Value:    "$ kubectl delete ns foo bar legacy",
				},
				CreateSnippet:          true,
				IncludeOutputInSnippet: true,
			}).
			Build())
}
