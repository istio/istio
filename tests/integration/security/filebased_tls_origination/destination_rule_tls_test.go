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

package filebasedtlsorigination

import (
	"fmt"
	"io/ioutil"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"path"
	"testing"
	"time"

	"istio.io/istio/pkg/test/echo/common/scheme"

	"istio.io/istio/pkg/test/env"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/util/retry"
)

func mustReadFile(t *testing.T, f string) string {
	b, err := ioutil.ReadFile(path.Join(env.IstioSrc, "tests/testdata/certs/dns", f))
	if err != nil {
		t.Fatalf("failed to read %v: %v", f, err)
	}
	return string(b)
}

// TestDestinationRuleTLS tests that MUTUAL tls mode is respected in DestinationRule.
// This sets up a client and server with appropriate cert config and ensures we can successfully send a message.
func TestDestinationRuleTLS(t *testing.T) {
	framework.
		NewTest(t).
		Features("security.egress.mtls").
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix: "tls",
				Inject: true,
			})

			env := ctx.Environment().(*kube.Environment)
			serverTLSSettings := &common.TLSSettings{
				RootCert:   mustReadFile(t, "root-cert.pem"),
				ClientCert: mustReadFile(t, "cert-chain.pem"),
				Key:        mustReadFile(t, "key.pem"),
				// Override hostname to match the SAN in the cert we are using
				Hostname: "server.default.svc",
			}

			svcs := make([][2]echo.Instance, len(env.Clusters()))
			builder := echoboot.NewBuilderOrFail(t, ctx)
			for _, c := range env.Clusters() {
				serverSvcName := fmt.Sprintf("server-%d", c.Index())

				// Setup our destination rule, enforcing TLS to "server". These certs will be created/mounted below.
				for _, cp := range env.ControlPlaneClusters() {
					cp.ApplyConfigOrFail(t, ns.Name(), fmt.Sprintf(`
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: db-mtls-%d
spec:
  exportTo: ["."]
  host: %s
  trafficPolicy:
    tls:
      mode: MUTUAL
      clientCertificate: /etc/certs/custom/cert-chain.pem
      privateKey: /etc/certs/custom/key.pem
      caCertificates: /etc/certs/custom/root-cert.pem
`, c.Index(), serverSvcName))
				}

				svcs[c.Index()] = [2]echo.Instance{}
				builder = builder.
					With(&svcs[c.Index()][0], echo.Config{
						Service:   fmt.Sprintf("client-%d", c.Index()),
						Namespace: ns,
						Ports:     []echo.Port{},
						// TODO rebase on main multicluster testing branch, these are unused anyway
						// Pilot:     pilots[c.Index()],
						Subsets: []echo.SubsetConfig{{
							Version: "v1",
							// Set up custom annotations to mount the certs. We will re-use the configmap created by "server"
							// so that we don't need to manage it ourselves.
							// The paths here match the destination rule above
							Annotations: echo.NewAnnotations().
								Set(echo.SidecarVolume, fmt.Sprintf(`{"custom-certs":{"configMap":{"name":"%s-certs"}}}`, serverSvcName)).
								Set(echo.SidecarVolumeMount, `{"custom-certs":{"mountPath":"/etc/certs/custom"}}`),
						}},
					}).
					With(&svcs[c.Index()][1], echo.Config{
						Service:   serverSvcName,
						Namespace: ns,
						Ports: []echo.Port{
							{
								Name:         "grpc",
								Protocol:     protocol.GRPC,
								InstancePort: 8090,
								TLS:          true,
							},
							{
								Name:         "http",
								Protocol:     protocol.HTTP,
								InstancePort: 8091,
								TLS:          true,
							},
							{
								Name:         "tcp",
								Protocol:     protocol.TCP,
								InstancePort: 8092,
								TLS:          true,
							},
						},
						// TODO rebase on main multicluster testing branch, these are unused anyway
						// Pilot: pilots[c.Index()],
						// Set up TLS certs on the server. This will make the server listen with these credentials.
						TLSSettings: serverTLSSettings,
						// Do not inject, as we are testing non-Istio TLS here
						Subsets: []echo.SubsetConfig{{
							Version:     "v1",
							Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
						}},
					})
			}
			builder.BuildOrFail(t)

			for _, src := range env.Clusters() {
				for _, dst := range env.Clusters() {
					client, server := svcs[src.Index()][0], svcs[dst.Index()][1]
					ctx.NewSubTest(fmt.Sprintf("%d->%d", src.Index(), dst.Index())).Run(func(ctx framework.TestContext) {
						for _, tt := range []string{"grpc", "http", "tcp"} {
							ctx.NewSubTest(tt).RunParallel(func(ctx framework.TestContext) {
								retry.UntilSuccessOrFail(ctx, func() error {
									opts := echo.CallOptions{
										Target:   server,
										PortName: tt,
									}
									if tt == "tcp" {
										opts.Scheme = scheme.TCP
									}
									resp, err := client.Call(opts)
									if err != nil {
										return err
									}
									return resp.CheckOK()
								}, retry.Delay(time.Millisecond*100))
							})
						}
					})
				}
			}
		})
}
