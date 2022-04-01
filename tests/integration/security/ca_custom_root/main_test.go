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

// cacustomroot creates cluster with custom plugin root CA (samples/cert/ca-cert.pem)
// instead of using the auto-generated self-signed root CA.
package cacustomroot

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"testing"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/tests/integration/security/util"
	"istio.io/istio/tests/integration/security/util/cert"
)

const (
	ASvc = "a"
	BSvc = "b"
)

type EchoDeployments struct {
	Namespace namespace.Instance
	// workloads for TestSecureNaming
	A, B echo.Instances
	// workloads for TestTrustDomainAliasSecureNaming
	// workload Client is also used by TestTrustDomainValidation
	// ServerNakedFooAlt using also used in the multi_root_test.go test case
	Client, ServerNakedFoo, ServerNakedBar, ServerNakedFooAlt echo.Instances
	// workloads for TestTrustDomainValidation
	Naked, Server echo.Instances
}

var (
	inst istio.Instance
	apps = &EchoDeployments{}
)

func SetupApps(ctx resource.Context, apps *EchoDeployments) error {
	var err error
	apps.Namespace, err = namespace.New(ctx, namespace.Config{
		Prefix: "test-ns",
		Inject: true,
	})
	if err != nil {
		return err
	}

	tmpdir, err := ctx.CreateTmpDirectory("ca-custom-root")
	if err != nil {
		return err
	}

	// Create testing certs using runtime namespace.
	err = generateCerts(tmpdir, apps.Namespace.Name())
	if err != nil {
		return err
	}
	rootCert, err := loadCert(path.Join(tmpdir, "root-cert.pem"))
	if err != nil {
		return err
	}
	clientCert, err := loadCert(path.Join(tmpdir, "workload-server-naked-foo-cert.pem"))
	if err != nil {
		return err
	}
	Key, err := loadCert(path.Join(tmpdir, "workload-server-naked-foo-key.pem"))
	if err != nil {
		return err
	}

	rootCertAlt, err := loadCert(path.Join(tmpdir, "root-cert-alt.pem"))
	if err != nil {
		return err
	}
	clientCertAlt, err := loadCert(path.Join(tmpdir, "workload-server-naked-foo-alt-cert.pem"))
	if err != nil {
		return err
	}
	keyAlt, err := loadCert(path.Join(tmpdir, "workload-server-naked-foo-alt-key.pem"))
	if err != nil {
		return err
	}

	builder := deployment.New(ctx)
	builder.
		WithClusters(ctx.Clusters()...).
		WithConfig(util.EchoConfig(ASvc, apps.Namespace, false, nil)).
		WithConfig(util.EchoConfig(BSvc, apps.Namespace, false, nil)).
		// Deploy 3 workloads:
		// client: echo app with istio-proxy sidecar injected, holds default trust domain cluster.local.
		// serverNakedFoo: echo app without istio-proxy sidecar, holds custom trust domain trust-domain-foo.
		// serverNakedBar: echo app without istio-proxy sidecar, holds custom trust domain trust-domain-bar.
		WithConfig(echo.Config{
			Namespace: apps.Namespace,
			Service:   "client",
		}).
		WithConfig(echo.Config{
			Namespace: apps.Namespace,
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
			Namespace: apps.Namespace,
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
			Namespace: apps.Namespace,
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
			Namespace: apps.Namespace,
			Service:   "naked",
			Subsets: []echo.SubsetConfig{
				{
					Annotations: echo.NewAnnotations().SetBool(echo.SidecarInject, false),
				},
			},
		}).
		WithConfig(echo.Config{
			Subsets:        []echo.SubsetConfig{{}},
			Namespace:      apps.Namespace,
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
	apps.A = match.ServiceName(echo.NamespacedName{Name: ASvc, Namespace: apps.Namespace}).GetMatches(echos)
	apps.B = match.ServiceName(echo.NamespacedName{Name: BSvc, Namespace: apps.Namespace}).GetMatches(echos)
	apps.Client = match.ServiceName(echo.NamespacedName{Name: "client", Namespace: apps.Namespace}).GetMatches(echos)
	apps.ServerNakedFoo = match.ServiceName(echo.NamespacedName{Name: "server-naked-foo", Namespace: apps.Namespace}).GetMatches(echos)
	apps.ServerNakedBar = match.ServiceName(echo.NamespacedName{Name: "server-naked-bar", Namespace: apps.Namespace}).GetMatches(echos)
	apps.ServerNakedFooAlt = match.ServiceName(echo.NamespacedName{Name: "server-naked-foo-alt", Namespace: apps.Namespace}).GetMatches(echos)
	apps.Naked = match.ServiceName(echo.NamespacedName{Name: "naked", Namespace: apps.Namespace}).GetMatches(echos)
	apps.Server = match.ServiceName(echo.NamespacedName{Name: "server", Namespace: apps.Namespace}).GetMatches(echos)
	return nil
}

func loadCert(filename string) (string, error) {
	data, err := cert.ReadSampleCertFromFile(filename)
	if err != nil {
		return "", err
	}
	return string(data), nil
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

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		// k8s is required because the plugin CA key and certificate are stored in a k8s secret.
		Label(label.CustomSetup).
		Setup(istio.Setup(&inst, setupConfig, cert.CreateCASecret)).
		Setup(func(ctx resource.Context) error {
			return SetupApps(ctx, apps)
		}).
		Run()
}

func setupConfig(_ resource.Context, cfg *istio.Config) {
	if cfg == nil {
		return
	}

	// Add alternate root certificate to list of trusted anchors
	script := path.Join(env.IstioSrc, "samples/certs", "root-cert-alt.pem")
	rootPEM, err := loadCert(script)
	if err != nil {
		return
	}

	cfgYaml := tmpl.MustEvaluate(`
values:
  pilot:
    env:
      ISTIO_MULTIROOT_MESH: true
  meshConfig:
    defaultConfig:
      proxyMetadata:
        PROXY_CONFIG_XDS_AGENT: "true"
    trustDomainAliases: [some-other, trust-domain-foo]
    caCertificates:
    - pem: |
{{.pem | indent 8}}
`, map[string]string{"pem": rootPEM})
	cfg.ControlPlaneValues = cfgYaml
}
