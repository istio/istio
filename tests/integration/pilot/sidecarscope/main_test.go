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

package sidecarscope

import (
	"bytes"
	"fmt"
	"testing"
	"text/template"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
)

const (
	SidecarConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: Sidecar
metadata:
  name: sidecar
  namespace:  {{.AppNamespace}}
spec:
{{- if .IngressListener }}
  ingress:
    - port:
        number: 9080
        protocol: HTTP
        name: custom-http
      defaultEndpoint: unix:///var/run/someuds.sock
{{- end }}
  egress:
    - hosts:
{{ range $i, $ns := .ImportedNamespaces }}
      - {{$ns}}
{{ end }}
`

	AppConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: app
  namespace: {{.AppNamespace}}
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: app.com
{{- else }}
  - address: 1.1.1.1
{{- end }}
`

	ExcludedConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: excluded
  namespace: {{.ExcludedNamespace}}
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: excluded.com
{{- else }}
  - address: 9.9.9.9
{{- end }}
`

	IncludedConfig = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: included
  namespace: {{.IncludedNamespace}}
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: included.com
{{- else }}
  - address: 2.2.2.2
{{- end }}
`

	AppConfigListener = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: app-https
  namespace: {{.AppNamespace}}
spec:
  hosts:
  - {{.AppNamespace}}.cluster.local
  addresses:
  - 5.5.5.5
  ports:
  - number: 443
    name: https
    protocol: HTTPS
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: app.com
{{- else }}
  - address: 10.10.10.10
{{- end }}
`
	IncludedConfigListener = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: excluded-https
  namespace: {{.ExcludedNamespace}}
spec:
  hosts:
  - {{.AppNamespace}}.cluster.local
  addresses:
  - 5.5.5.5
  ports:
  - number: 4430
    name: https
    protocol: HTTPS
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: app.com
{{- else }}
  - address: 10.10.10.10
{{- end }}
`
	ExcludedConfigListener = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: excluded-https
  namespace: {{.ExcludedNamespace}}
spec:
  hosts:
  - {{.AppNamespace}}.cluster.local
  addresses:
  - 5.5.5.5
  ports:
  - number: 4431
    name: https
    protocol: HTTPS
  resolution: {{.Resolution}}
  endpoints:
{{- if eq .Resolution "DNS" }}
  - address: app.com
{{- else }}
  - address: 10.10.10.10
{{- end }}
`
)

type Config struct {
	ImportedNamespaces []string
	IncludedNamespace  string
	ExcludedNamespace  string
	AppNamespace       string
	Resolution         string
	IngressListener    bool
}

func setupTest(t *testing.T, ctx resource.Context, modifyConfig func(c Config) Config) (pilot.Instance, *model.Proxy) {
	p := pilot.NewOrFail(t, ctx, pilot.Config{})

	includedNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "included",
		Inject: true,
	})
	excludedNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "excluded",
		Inject: true,
	})
	appNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix: "app",
		Inject: true,
	})

	config := modifyConfig(Config{
		IncludedNamespace:  includedNamespace.Name(),
		ExcludedNamespace:  excludedNamespace.Name(),
		AppNamespace:       appNamespace.Name(),
		ImportedNamespaces: []string{"./*", includedNamespace.Name() + "/*"},
	})

	// Apply all configs
	createConfig(t, ctx, config, SidecarConfig, appNamespace)
	createConfig(t, ctx, config, AppConfig, appNamespace)
	createConfig(t, ctx, config, AppConfigListener, appNamespace)
	createConfig(t, ctx, config, ExcludedConfig, excludedNamespace)
	createConfig(t, ctx, config, IncludedConfig, includedNamespace)
	createConfig(t, ctx, config, IncludedConfigListener, includedNamespace)
	createConfig(t, ctx, config, ExcludedConfigListener, excludedNamespace)

	time.Sleep(time.Second * 2)

	nodeID := &model.Proxy{
		Metadata:        &model.NodeMetadata{ClusterID: "integration-test"},
		ID:              fmt.Sprintf("app.%s", appNamespace.Name()),
		DNSDomain:       appNamespace.Name() + ".cluster.local",
		Type:            model.SidecarProxy,
		IPAddresses:     []string{"1.1.1.1"},
		ConfigNamespace: appNamespace.Name(),
	}
	return p, nodeID
}

func createConfig(t *testing.T, ctx resource.Context, config Config, yaml string, namespace namespace.Instance) {
	tmpl, err := template.New("Config").Parse(yaml)
	if err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		t.Errorf("failed to create template: %v", err)
	}
	if err := ctx.ApplyConfig(namespace.Name(), buf.String()); err != nil {
		t.Fatalf("failed to apply config: %v. Config: %v", err, buf.String())
	}
}

func TestMain(m *testing.M) {
	framework.
		NewSuite("sidecar_scope_test", m).
		RequireEnvironment(environment.Native).
		Run()
}
