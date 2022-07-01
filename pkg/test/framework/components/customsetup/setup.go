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

package customsetup

import (
	"fmt"
	"os"
	"os/exec"
	"path"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	depl "istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/util/cert"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	httpPlaintext = "http-plaintext"
	httpMTLS      = "http-mtls"
	tcpPlaintext  = "tcp-plaintext"
	tcpMTLS       = "tcp-mtls"
	tcpWL         = "tcp-wl"
)

func SetupApps(apps *deployment.SingleNamespaceView, ctx resource.Context) error {
	var err error

	tmpdir, err := ctx.CreateTmpDirectory("ca-custom-root")
	if err != nil {
		return err
	}

	// Create testing certs using runtime namespace.
	err = generateCerts(tmpdir, apps.EchoNamespace.Namespace.Name())
	if err != nil {
		return err
	}
	rootCert, err := cert.LoadCert(path.Join(tmpdir, "root-cert.pem"))
	if err != nil {
		return err
	}
	clientCert, err := cert.LoadCert(path.Join(tmpdir, "workload-server-naked-foo-cert.pem"))
	if err != nil {
		return err
	}
	Key, err := cert.LoadCert(path.Join(tmpdir, "workload-server-naked-foo-key.pem"))
	if err != nil {
		return err
	}

	rootCertAlt, err := cert.LoadCert(path.Join(tmpdir, "root-cert-alt.pem"))
	if err != nil {
		return err
	}
	clientCertAlt, err := cert.LoadCert(path.Join(tmpdir, "workload-server-naked-foo-alt-cert.pem"))
	if err != nil {
		return err
	}
	keyAlt, err := cert.LoadCert(path.Join(tmpdir, "workload-server-naked-foo-alt-key.pem"))
	if err != nil {
		return err
	}

	builder := depl.New(ctx)
	builder.
		WithClusters(ctx.Clusters()...).
		// Deploy 3 workloads:
		// client: echo app with istio-proxy sidecar injected, holds default trust domain cluster.local.
		// serverNakedFoo: echo app without istio-proxy sidecar, holds custom trust domain trust-domain-foo.
		// serverNakedBar: echo app without istio-proxy sidecar, holds custom trust domain trust-domain-bar.
		WithConfig(echo.Config{
			Namespace: apps.Namespace,
			Service:   "client",
		}).
		WithConfig(echo.Config{
			Namespace: apps.EchoNamespace.Namespace,
			Service:   "server-naked-foo",
			Subsets: []echo.SubsetConfig{
				{
					Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
				},
			},
			ServiceAccount: true,
			Ports: []echo.Port{
				{
					Name:         "https",
					Protocol:     protocol.HTTPS,
					ServicePort:  443,
					WorkloadPort: 8443,
					TLS:          true,
				},
			},
			TLSSettings: &common.TLSSettings{
				RootCert:      rootCert,
				ClientCert:    clientCert,
				Key:           Key,
				AcceptAnyALPN: true,
			},
		}).
		WithConfig(echo.Config{
			Namespace: apps.EchoNamespace.Namespace,
			Service:   "server-naked-bar",
			Subsets: []echo.SubsetConfig{
				{
					Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
				},
			},
			ServiceAccount: true,
			Ports: []echo.Port{
				{
					Name:         "https",
					Protocol:     protocol.HTTPS,
					ServicePort:  443,
					WorkloadPort: 8443,
					TLS:          true,
				},
			},
			TLSSettings: &common.TLSSettings{
				RootCert:      rootCert,
				ClientCert:    clientCert,
				Key:           Key,
				AcceptAnyALPN: true,
			},
		}).
		WithConfig(echo.Config{
			// Adding echo server for multi-root tests
			Namespace: apps.EchoNamespace.Namespace,
			Service:   "server-naked-foo-alt",
			Subsets: []echo.SubsetConfig{
				{
					Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
				},
			},
			ServiceAccount: true,
			Ports: []echo.Port{
				{
					Name:         "https",
					Protocol:     protocol.HTTPS,
					ServicePort:  443,
					WorkloadPort: 8443,
					TLS:          true,
				},
			},
			TLSSettings: &common.TLSSettings{
				RootCert:      rootCertAlt,
				ClientCert:    clientCertAlt,
				Key:           keyAlt,
				AcceptAnyALPN: true,
			},
		}).
		WithConfig(echo.Config{
			Namespace: apps.EchoNamespace.Namespace,
			Service:   "naked",
			Subsets: []echo.SubsetConfig{
				{
					Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
				},
			},
		}).
		WithConfig(echo.Config{
			Subsets:        []echo.SubsetConfig{{}},
			Namespace:      apps.EchoNamespace.Namespace,
			Service:        "server",
			ServiceAccount: true,
			Ports: []echo.Port{
				{
					Name:         httpPlaintext,
					Protocol:     protocol.HTTP,
					ServicePort:  8090,
					WorkloadPort: 8090,
				},
				{
					Name:         httpMTLS,
					Protocol:     protocol.HTTP,
					ServicePort:  8091,
					WorkloadPort: 8091,
				},
				{
					Name:         tcpPlaintext,
					Protocol:     protocol.TCP,
					ServicePort:  8092,
					WorkloadPort: 8092,
				},
				{
					Name:         tcpMTLS,
					Protocol:     protocol.TCP,
					ServicePort:  8093,
					WorkloadPort: 8093,
				},
				{
					Name:         tcpWL,
					WorkloadPort: 9000,
					Protocol:     protocol.TCP,
				},
			},
		})
	echos, err := builder.Build()
	if err != nil {
		return err
	}
	client := match.ServiceName(echo.NamespacedName{Name: "client", Namespace: apps.Namespace}).GetMatches(echos)
	server := match.ServiceName(echo.NamespacedName{Name: "server", Namespace: apps.Namespace}).GetMatches(echos)
	serverNakedFoo := match.ServiceName(echo.NamespacedName{Name: "server-naked-foo", Namespace: apps.Namespace}).GetMatches(echos)
	serverNakedBar := match.ServiceName(echo.NamespacedName{Name: "server-naked-bar", Namespace: apps.Namespace}).GetMatches(echos)
	serverNakedFooAlt := match.ServiceName(echo.NamespacedName{Name: "server-naked-foo-alt", Namespace: apps.Namespace}).GetMatches(echos)
	apps.CustomApps = append(apps.CustomApps, client, server, serverNakedFoo, serverNakedBar, serverNakedFooAlt)
	apps.Naked = match.ServiceName(echo.NamespacedName{Name: "naked", Namespace: apps.Namespace}).GetMatches(echos)
	return nil
}

func generateCerts(tmpdir, ns string) error {
	workDir := path.Join(env.IstioSrc, "samples/certs")
	script := path.Join(workDir, "generate-workload.sh")

	// Create certificates signed by the same plugin CA that signs Istiod certificates
	crts := []struct {
		td string
		sa string
	}{
		{
			td: "foo",
			sa: "server-naked-foo",
		},
		{
			td: "bar",
			sa: "server-naked-bar",
		},
	}
	for _, crt := range crts {
		command := exec.Cmd{
			Path:   script,
			Args:   []string{script, crt.td, ns, crt.sa, tmpdir},
			Stdout: os.Stdout,
			Stderr: os.Stdout,
		}
		if err := command.Run(); err != nil {
			return fmt.Errorf("failed to create testing certificates: %s", err)
		}
	}

	// Create certificates signed by a Different ca with a different root
	command := exec.Cmd{
		Path:   script,
		Args:   []string{script, "foo", ns, "server-naked-foo-alt", tmpdir, "use-alternative-root"},
		Stdout: os.Stdout,
		Stderr: os.Stdout,
	}
	if err := command.Run(); err != nil {
		return fmt.Errorf("failed to create testing certificates: %s", err)
	}
	return nil
}
