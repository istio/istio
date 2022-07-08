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
	"istio.io/istio/pkg/test/framework/components/namespace"
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

func SetupApps(ctx resource.Context, customNs namespace.Getter, customCfg *[]echo.Config) error {
	tmpdir, err := ctx.CreateTmpDirectory("ca-custom-root")
	if err != nil {
		return err
	}

	// Create testing certs using runtime namespace.
	err = generateCerts(tmpdir, customNs.Get().Name())
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

	var customConfig []echo.Config

	clientConfig := echo.Config{
		Namespace: customNs.Get(),
		Service:   "client",
	}
	serverNakedFooConfig := echo.Config{
		Namespace: customNs.Get(),
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
	}

	serverNakedBarConfig := echo.Config{
		Namespace: customNs.Get(),
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
	}

	serverNakedFooAltConfig := echo.Config{
		// Adding echo server for multi-root tests
		Namespace: customNs.Get(),
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
	}

	serverConfig := echo.Config{
		Subsets:        []echo.SubsetConfig{{}},
		Namespace:      customNs.Get(),
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
	}

	customConfig = append(customConfig, clientConfig, serverNakedFooConfig, serverNakedBarConfig,
		serverNakedFooAltConfig, serverConfig)

	*customCfg = customConfig
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
