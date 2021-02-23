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
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
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

	builder := echoboot.NewBuilder(ctx)
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
					Name:         HTTPS,
					Protocol:     protocol.HTTPS,
					ServicePort:  443,
					InstancePort: 8443,
					TLS:          true,
				},
			},
			TLSSettings: &common.TLSSettings{
				RootCert:   rootCert,
				ClientCert: clientCert,
				Key:        Key,
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
					Name:         HTTPS,
					Protocol:     protocol.HTTPS,
					ServicePort:  443,
					InstancePort: 8443,
					TLS:          true,
				},
			},
			TLSSettings: &common.TLSSettings{
				RootCert:   rootCert,
				ClientCert: clientCert,
				Key:        Key,
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
					Name:         HTTPS,
					Protocol:     protocol.HTTPS,
					ServicePort:  443,
					InstancePort: 8443,
					TLS:          true,
				},
			},
			TLSSettings: &common.TLSSettings{
				RootCert:   rootCertAlt,
				ClientCert: clientCertAlt,
				Key:        keyAlt,
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
					InstancePort: 8090,
				},
				{
					Name:         httpMTLS,
					Protocol:     protocol.HTTP,
					ServicePort:  8091,
					InstancePort: 8091,
				},
				{
					Name:         tcpPlaintext,
					Protocol:     protocol.TCP,
					ServicePort:  8092,
					InstancePort: 8092,
				},
				{
					Name:         tcpMTLS,
					Protocol:     protocol.TCP,
					ServicePort:  8093,
					InstancePort: 8093,
				},
			},
			WorkloadOnlyPorts: []echo.WorkloadPort{
				{
					Port:     9000,
					Protocol: protocol.TCP,
				},
			},
		})
	echos, err := builder.Build()
	if err != nil {
		return err
	}
	apps.A = echos.Match(echo.Service(ASvc))
	apps.B = echos.Match(echo.Service(BSvc))
	apps.Client = echos.Match(echo.Service("client"))
	apps.ServerNakedFoo = echos.Match(echo.Service("server-naked-foo"))
	apps.ServerNakedBar = echos.Match(echo.Service("server-naked-bar"))
	apps.ServerNakedFooAlt = echos.Match(echo.Service("server-naked-foo-alt"))
	apps.Naked = echos.Match(echo.Service("naked"))
	apps.Server = echos.Match(echo.Service("server"))
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
			Args:   []string{script, crt.td, ns, crt.sa, tmpdir, "false"},
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
		Args:   []string{script, "foo", ns, "server-naked-foo-alt", tmpdir, "true"},
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

	// Add root certificate to list of trusted anchors
	// TODO: Read certificate from directory
	// script := path.Join(env.IstioSrc, "samples/certs", "root-cert-2.pem")
	// rootPEM, err := loadCert(script)
	// if err != nil {
	//	return
	//}

	cfgYaml := `
values:
  meshConfig:
    trustDomainAliases: [some-other, trust-domain-foo]
    caCertificates:
    - pem: |
        -----BEGIN CERTIFICATE-----
        MIIFCTCCAvGgAwIBAgIJAL4uxHfykeWSMA0GCSqGSIb3DQEBBQUAMCIxDjAMBgNV
        BAoMBUlzdGlvMRAwDgYDVQQDDAdSb290IENBMB4XDTIxMDIxNzIyNDA1N1oXDTMx
        MDIxNTIyNDA1N1owIjEOMAwGA1UECgwFSXN0aW8xEDAOBgNVBAMMB1Jvb3QgQ0Ew
        ggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoICAQCkESA1E1psP/v9wkdimcqZ
        X832eMRKomDxFFwbk9ayMF/XrGMAUmsvqeN9a73m5UD3MpArBiRc97XXzW1K1hnW
        sCtcN42C25NDXgHGjzyhplNogR6/SsKYg2oZx2iBRJUxwroi3/iTv7KPousQwGpF
        a/leoNxfr0+twbA5Y9nS17zO8CfJLlJz+c8MIbSdCTckcRxvVSXWsUlH1BJS/Bfh
        TnlaVqk/YGWBxhtm8BowB0hzaxFrQnwuxsRXgnFmlAV0iZ35jvrhM6vmU2RqvUUo
        BEgTTPuToC/2VRmyhFw/9cWcjzxgkvkjLsmVg5icuNvKQ4PgJL07zguRjk0XFchz
        SZuqimjDYSRQv3I0TOn+eT0b2KX8neg1pqh7w81YotyqFcJ7SdpQaau7CeMbus92
        P7XsCpCSVe82Y8BRcdtPgDEzn7AOA2IlgxDC1hex80+10aL8naWGdxxUEom8wQwS
        gvRHrdDsRigVvcygvVhfcoMak4RUxFeaQK5c1ruMlNvuuwZ20C4mUvZTvlaz7RmN
        yazzjqQYT4GHbR2e1kwBqe6YtlOrHY1Fpg5V6+S1rQkbbZrfQVQOXz7VQ7jOsmEr
        kNkrtgS8ZjAwgnOrf878Rr1g8Ac+I4q7Mpei2humdAydO3cEaGskcoozsxjPAKvd
        8be76nUjjkBv6eURp1ziEQIDAQABo0IwQDAdBgNVHQ4EFgQUPyNoAnWNHwP+2NFi
        zWLW0hz3Cw0wDwYDVR0TAQH/BAUwAwEB/zAOBgNVHQ8BAf8EBAMCAuQwDQYJKoZI
        hvcNAQEFBQADggIBAIQx3aCt5GFuWxLLYlL2wbrO8tFoQnN4Poa/uli65YF47abb
        zZkDm6OomYIsWVce4tdoJZy1TLlyKZPb+MDDnelOzNhpljjpw2ZdhEtnv703513q
        o1zCgVrO1YWvk6Xv1gt3wVhQvJhq87BqrYFcCo899k09haXU4ddtP+YMPjyIngVb
        ucxML2xqjzS1Cfs+CD/OpwntISzWOEi5r/3IkbPlMT15hFa2oAVKBhOkyk0QQP8t
        bV9i4AC32gvshwIiGjbXUmnlRwBxUi8GBq5ZyR66nqoV9wBHPoqJZ3z+j6DNZSYm
        QGaO0wwWgSePRNPodzPAw6vofDjBe/hcyCk2d2uRrOLJICWbAdx76+j6h3zX2sPS
        FVSK1eVZaPUylL9rE+AyVGgl8/FqLNTwOHdSSovgIVVID7eXSpebnFtQEtlCSnik
        naaVSrG+sTH77WD9mQO9LmYS8JVceLE+ErSEAXkFKim131317sS5Z310U/L021M4
        xGH6zZHK9W9dx1X4gZKfoqGwSAHhs4rjEZCU7CKR1ouJBPWQ/cGrrk8n8ZdmKxmz
        OHNB4GteIEKJKrJTKQil8hsdSIqSUX4H4tw4GXlpyBSmZNt9iOjo4tWUGoUlQRIp
        QDpfEx1ep9pVDwQNGXVf+m9iqbc3DAiSN+1CGSZI5Kv0RzZSih5zIaxB2gJ7
        -----END CERTIFICATE-----
`
	cfg.ControlPlaneValues = cfgYaml
}
