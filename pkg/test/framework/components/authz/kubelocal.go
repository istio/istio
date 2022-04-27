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

package authz

import (
	"errors"
	"fmt"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	localProviderTemplate = `
extensionProviders:
- name: "{{ .httpName }}"
  envoyExtAuthzHttp:
    service: "{{ .httpHost }}"
    port: {{ .httpPort }}
    headersToUpstreamOnAllow: ["x-ext-authz-*"]
    headersToDownstreamOnDeny: ["x-ext-authz-*"]
    includeRequestHeadersInCheck: ["x-ext-authz"]
    includeAdditionalHeadersInCheck:
      x-ext-authz-additional-header-new: additional-header-new-value
      x-ext-authz-additional-header-override: additional-header-override-value
- name: "{{ .grpcName }}"
  envoyExtAuthzGrpc:
    service: "{{ .grpcHost }}"
    port: {{ .grpcPort }}`

	localServiceEntryTemplate = `
apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: {{ .httpName }}
spec:
  hosts:
  - "{{ .httpHost }}"
  endpoints:
  - address: "127.0.0.1"
  ports:
  - name: http
    number: {{ .httpPort }}
    protocol: HTTP
  resolution: STATIC
---
apiVersion: networking.istio.io/v1beta1
kind: ServiceEntry
metadata:
  name: {{ .grpcName }}
spec:
  hosts:
  - "{{ .grpcHost }}"
  endpoints:
  - address: "127.0.0.1"
  ports:
  - name: grpc
    number: {{ .grpcPort }}
    protocol: GRPC
  resolution: STATIC
---`
)

func newLocalKubeServer(ctx resource.Context, ns namespace.Instance) (server *localServerImpl, err error) {
	if ns == nil {
		return nil, errors.New("namespace required for local authz server")
	}

	start := time.Now()
	scopes.Framework.Infof("=== BEGIN: Deploy local authz server (ns=%s) ===", ns.Name())
	defer func() {
		if err != nil {
			scopes.Framework.Errorf("=== FAILED: Deploy local authz server (ns=%s) ===", ns.Name())
			scopes.Framework.Error(err)
		} else {
			scopes.Framework.Infof("=== SUCCEEDED: Deploy local authz server (ns=%s) in %v ===",
				ns.Name(), time.Since(start))
		}
	}()

	server = &localServerImpl{
		ns: ns,
	}

	// Create the providers.
	server.providers = []Provider{
		&providerImpl{
			name: server.httpName(),
			api:  HTTP,
			protocolSupported: func(p protocol.Instance) bool {
				// HTTP protocol doesn't support raw TCP requests.
				return !p.IsTCP()
			},
			targetSupported: func(to echo.Target) bool {
				return to.Config().IncludeExtAuthz
			},
			check: checkHTTP,
		},
		&providerImpl{
			name: server.grpcName(),
			api:  GRPC,
			protocolSupported: func(protocol.Instance) bool {
				return true
			},
			targetSupported: func(to echo.Target) bool {
				return to.Config().IncludeExtAuthz
			},
			check: checkGRPC,
		},
	}
	server.id = ctx.TrackResource(server)

	// Install the providers in MeshConfig.
	if err = server.installProviders(ctx); err != nil {
		return
	}

	// Install a ServiceEntry for each provider to configure routing to the local provider host.
	err = server.installServiceEntries(ctx)
	return
}

type localServerImpl struct {
	id        resource.ID
	ns        namespace.Instance
	providers []Provider
}

func (s *localServerImpl) ID() resource.ID {
	return s.id
}

func (s *localServerImpl) Namespace() namespace.Instance {
	return s.ns
}

func (s *localServerImpl) Providers() []Provider {
	return append([]Provider{}, s.providers...)
}

func (s *localServerImpl) httpName() string {
	return fmt.Sprintf("%s-%s-local", httpName, s.ns.Prefix())
}

func (s *localServerImpl) grpcName() string {
	return fmt.Sprintf("%s-%s-local", grpcName, s.ns.Prefix())
}

func (s *localServerImpl) httpHost() string {
	return fmt.Sprintf("%s.%s.local", httpName, s.ns.Prefix())
}

func (s *localServerImpl) grpcHost() string {
	return fmt.Sprintf("%s.%s.local", grpcName, s.ns.Prefix())
}

func (s *localServerImpl) templateArgs() map[string]interface{} {
	return map[string]interface{}{
		"httpName": s.httpName(),
		"grpcName": s.grpcName(),
		"httpHost": s.httpHost(),
		"grpcHost": s.grpcHost(),
		"httpPort": httpPort,
		"grpcPort": grpcPort,
	}
}

func (s *localServerImpl) installProviders(ctx resource.Context) error {
	// Update the mesh config extension provider for the ext-authz service.
	providerYAML, err := tmpl.Evaluate(localProviderTemplate, s.templateArgs())
	if err != nil {
		return err
	}

	return installProviders(ctx, providerYAML)
}

func (s *localServerImpl) installServiceEntries(ctx resource.Context) error {
	return ctx.ConfigIstio().
		Eval(s.ns.Name(), s.templateArgs(), localServiceEntryTemplate).
		Apply(apply.CleanupConditionally)
}
