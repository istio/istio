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

package docker

import (
	"fmt"
	"text/template"
	"time"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	serviceEntryTemplateYAML = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: {{ .ServiceName }}
  creationTimestamp: {{ .CreationTimestamp }}
  labels:
    app: {{ .ServiceName }}
    version: {{ .Version }}
spec:
  hosts:
  - {{ .ServiceName }}.{{ .Namespace }}.svc.{{ .Domain }}
  ports:
  {{ range $i, $p := .Ports -}}
  - number: {{ $p.ServicePort }} 
    name: {{ $p.Name }}
    protocol: {{ $p.Protocol }}
  {{ end -}}
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: {{ .Address }}
    {{ if ne .Locality "" -}}
    locality: {{ .Locality }}
    {{ end -}}
    labels:
      app: {{ .ServiceName }}
      version: {{ .Version }}
    ports:
      {{ range $i, $p := .Ports -}}
      {{$p.Name}}: {{$p.ServicePort}}
      {{ end -}}
`
)

var (
	serviceEntryTemplate *template.Template
)

func init() {
	var err error
	if serviceEntryTemplate, err = tmpl.Parse(serviceEntryTemplateYAML); err != nil {
		panic("unable to parse Service Entry template")
	}
}

type serviceEntry struct {
	yaml string
	ns   namespace.Instance
	cfg  resource.ConfigManager
}

func newServiceEntry(address string, cfg echo.Config) (out *serviceEntry, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("error applying ServiceEntry for %s/%s: %v",
				cfg.Namespace.Name(), cfg.Service, err)
		}
	}()

	se := &serviceEntry{
		ns:  cfg.Namespace,
		cfg: cfg.Cluster,
	}

	// Generate the YAML
	se.yaml, err = createServiceEntryYaml(address, cfg)
	if err != nil {
		return nil, err
	}

	// Apply the config to Galley.
	if err = cfg.Cluster.ApplyConfig(cfg.Namespace.Name(), se.yaml); err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			_ = se.Close()
		}
	}()

	if err != nil {
		return nil, err
	}
	return se, nil
}

// Close implements io.Closer
func (s *serviceEntry) Close() error {
	return s.cfg.DeleteConfig(s.ns.Name(), s.yaml)
}

func createServiceEntryYaml(address string, cfg echo.Config) (string, error) {
	// Generate the ServiceEntry YAML.
	yaml, err := tmpl.Execute(serviceEntryTemplate, map[string]interface{}{
		"ServiceName":       cfg.Service,
		"Version":           cfg.Version,
		"Namespace":         cfg.Namespace.Name(),
		"Domain":            cfg.Domain,
		"Ports":             cfg.Ports,
		"Locality":          cfg.Locality,
		"Address":           address,
		"CreationTimestamp": time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return "", err
	}
	return yaml, nil
}
