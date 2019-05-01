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

package native

import (
	"bufio"
	"bytes"
	"fmt"
	"text/template"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

const (
	serviceEntryYAML = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: {{ .ServiceName }}
  labels:
    app: {{ .ServiceName }}
    version: {{ .Version }}
spec:
   hosts:
   - {{ .ServiceName }}.{{ .Namespace }}.{{ .Domain }}
   addresses:
   - 127.0.0.1/32
   ports:
   {{ range $i, $p := .Ports -}}
   - number: {{ $p.ServicePort }} 
     name: {{ $p.Name }}
     protocol: {{ $p.Protocol }}
   {{ end -}}
   resolution: STATIC
   location: MESH_INTERNAL
   endpoints:
    - address: 127.0.0.1
      {{ if ne .Locality "" }}
      locality: {{ .Locality }}
      {{ end -}}
      ports:
        {{ range $i, $p := .Ports -}}
        {{$p.Name}}: {{$p.ServicePort}}
        {{ end -}}
`
)

var (
	serviceEntryTemplate *template.Template

	serviceEntryCollection = metadata.IstioNetworkingV1alpha3Serviceentries.Collection.String()
)

func init() {
	serviceEntryTemplate = template.New("service_entry")
	if _, err := serviceEntryTemplate.Parse(serviceEntryYAML); err != nil {
		panic("unable to parse Service Entry template")
	}
}

type serviceConfig struct {
	ns       namespace.Instance
	ports    []echo.Port
	service  string
	domain   string
	version  string
	locality string
}

func (c serviceConfig) applyTo(g galley.Instance) (*galley.SnapshotObject, error) {
	// Generate the ServiceEntry YAML.
	var filled bytes.Buffer
	w := bufio.NewWriter(&filled)
	if err := serviceEntryTemplate.Execute(w, map[string]interface{}{
		"ServiceName": c.service,
		"Version":     c.version,
		"Namespace":   c.ns.Name(),
		"Domain":      c.domain,
		"Ports":       c.ports,
		"Locality":    c.locality,
	}); err != nil {
		return nil, err
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	serviceEntryYAML := filled.String()

	// Apply the config to Galley.
	if err := g.ApplyConfig(c.ns, serviceEntryYAML); err != nil {
		return nil, err
	}

	// Wait for the ServiceEntry to be made available by Galley.
	var out *galley.SnapshotObject
	mcpName := c.ns.Name() + "/" + c.service
	err := g.WaitForSnapshot(serviceEntryCollection, func(actuals []*galley.SnapshotObject) error {
		for _, actual := range actuals {
			if actual.Metadata.Name == mcpName {
				out = actual
				return nil
			}
		}
		return fmt.Errorf("never received ServiceEntry %s from Galley", mcpName)
	})

	if err != nil {
		return nil, err
	}
	return out, nil
}
