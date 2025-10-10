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

package deployment

import (
	"path"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common/ports"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/match"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/file"
)

const (
	ExternalSvc      = "external"
	ExternalHostname = "fake.external.com"
)

type External struct {
	// Namespace where external echo app will be deployed
	Namespace namespace.Instance

	// All external echo instances with no sidecar injected
	All echo.Instances
}

func (e External) Build(t resource.Context, b deployment.Builder) deployment.Builder {
	// External is a good case where MTLS makes sense, so add the port to default "All" list.
	withMTLS := append(ports.All(), ports.MTLS)
	config := echo.Config{
		Service:           ExternalSvc,
		Namespace:         e.Namespace,
		DefaultHostHeader: ExternalHostname,
		Ports:             withMTLS,
		// Set up TLS certs on the server. This will make the server listen with these credentials.
		TLSSettings: &common.TLSSettings{
			// Echo has these test certs baked into the docker image
			RootCert: file.MustAsString(path.Join(env.IstioSrc, "tests/testdata/certs/dns/root-cert.pem")),
			// Note: this certificate has the subject "CN=server.default.svc.cluster.local"
			ClientCert: file.MustAsString(path.Join(env.IstioSrc, "tests/testdata/certs/dns/cert-chain.pem")),
			Key:        file.MustAsString(path.Join(env.IstioSrc, "tests/testdata/certs/dns/key.pem")),
			// Override hostname to match the SAN in the cert we are using
			// TODO(nmittler): We should probably make this the same as ExternalHostname
			Hostname: "server.default.svc",
		},
		Subsets: []echo.SubsetConfig{
			{
				Version:     "v1",
				Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
			},
			{
				Version:     "v2",
				Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
			},
		},
	}
	return b.WithConfig(config)
}

func (e *External) LoadValues(echos echo.Instances) {
	e.All = match.ServiceName(echo.NamespacedName{Name: ExternalSvc, Namespace: e.Namespace}).GetMatches(echos)
}
