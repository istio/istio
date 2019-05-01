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

package agent

import (
	"bufio"
	"bytes"
	"text/template"
)

const (
	serviceEntryYAML = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: {{.ServiceName}}
  labels:
    app: {{.ServiceName}}
    version: {{.Version}}
spec:
   hosts:
   - {{.ServiceName}}.{{.Namespace}}.{{.Domain}}
   addresses:
   - 127.0.0.1/32
   ports:
   {{ range $i, $p := .Ports -}}
   - number: {{$p.ProxyPort}} 
     name: {{$p.Name}}
     protocol: {{$p.Protocol}}
   {{ end -}}
   resolution: STATIC
   location: MESH_INTERNAL
   endpoints:
    - address: 127.0.0.1
      ports:
        {{ range $i, $p := .Ports -}}
        {{$p.Name}}: {{$p.ProxyPort}}
        {{ end -}}
`
)

var (
	serviceEntryTemplate = getServiceEntryTemplate()
)

func getServiceEntryTemplate() *template.Template {
	tmpl := template.New("service_entry")
	_, err := tmpl.Parse(serviceEntryYAML)
	if err != nil {
		panic("unable to parse Service Entry template")
	}
	return tmpl
}

func createServiceEntryYAML(a *Instance) ([]byte, error) {
	var filled bytes.Buffer
	w := bufio.NewWriter(&filled)
	if err := serviceEntryTemplate.Execute(w, map[string]interface{}{
		"ServiceName": a.cfg.ServiceName,
		"Version":     a.cfg.Version,
		"Namespace":   a.cfg.Namespace.Name(),
		"Domain":      a.cfg.Domain,
		"Ports":       a.ports,
	}); err != nil {
		return nil, err
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	return filled.Bytes(), nil
}
