//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package service

import (
	"bufio"
	"bytes"
	"fmt"
	"text/template"

	istio_networking_api "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework/api/components"
)

const (
	// Namespace for services running in the local environment
	Namespace = "istio-system"
	// Domain for services running in the local environment
	Domain = "svc.local"
	// LocalIPAddress for connections to localhost
	LocalIPAddress = "127.0.0.1"
	// LocalCIDR for connections to localhost
	LocalCIDR = "127.0.0.1/32"

	serviceEntryYamlTemplateStr = `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: {{ .Name }}
  domain: {{ .Domain }}
  namespace: {{ .Namespace }}
  labels:
    app: {{ .App }}
    version: {{ .Version }}
spec:
  hosts: 
  - "{{ .Hosts }}"
  ports:
  {{range $i, $p := .Ports}}
  - number: {{$p.Number}}
    name: {{$p.Name}}
    protocol: {{$p.Protocol}}
  {{end}}
  addresses:
  - "127.0.0.1/32"
  resolution: STATIC
  location: MESH_INTERNAL
  endpoints:
  - address: "127.0.0.1"
`
)

var (
	// FullyQualifiedDomainName for local services
	FullyQualifiedDomainName = fmt.Sprintf("%s.%s", Namespace, Domain)

	// The Template object parsed from the template string
	serviceEntryYamlTemplate = getServiceEntryYamlTemplate()
)

// Manager is a wrapper around a model.ConfigStoreCache that simplifies service creation for the local environment.
type Manager struct {
	// ConfigStore for all deployments.
	ConfigStore model.ConfigStoreCache
	// Galley for configurations
	Galley components.Galley
}

type Port struct {
	Number   uint32
	Name     string
	Protocol string
}

// NewManager creates a new manager with an in-memory ConfigStore.
func NewManager() *Manager {
	return &Manager{
		ConfigStore: memory.NewController(memory.Make(model.IstioConfigTypes)),
	}
}

func getServiceEntryYamlTemplate() *template.Template {
	tmpl := template.New("istio_service_entry_config")
	_, err := tmpl.Parse(serviceEntryYamlTemplateStr)
	if err != nil {
		log.Warn("unable to parse Service Entry bootstrap config")
	}
	return tmpl
}

// Create a new ServiceEntry for the given service and adds it to the ConfigStore.
func (e *Manager) Create(serviceName, version string, ports model.PortList) (cfg model.Config, err error) {
	cfgPorts := make([]*istio_networking_api.Port, len(ports))
	for i, p := range ports {
		cfgPorts[i] = &istio_networking_api.Port{
			Name:     p.Name,
			Protocol: string(p.Protocol),
			Number:   uint32(p.Port),
		}
	}

	cfg = model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.ServiceEntry.Type,
			Name:      serviceName,
			Namespace: Namespace,
			Domain:    Domain,
			Labels: map[string]string{
				"app":     serviceName,
				"version": version,
			},
		},
		Spec: &istio_networking_api.ServiceEntry{
			Hosts: []string{
				fmt.Sprintf("%s.%s", serviceName, FullyQualifiedDomainName),
			},
			Addresses: []string{
				LocalCIDR,
			},
			Resolution: istio_networking_api.ServiceEntry_STATIC,
			Location:   istio_networking_api.ServiceEntry_MESH_INTERNAL,
			Endpoints: []*istio_networking_api.ServiceEntry_Endpoint{
				{
					Address: LocalIPAddress,
				},
			},
			Ports: cfgPorts,
		},
	}
	//used for pilot-agent unit test
	if e.Galley == nil {
		_, err = e.ConfigStore.Create(cfg)
		return
	}
	// Using Galley as Config Input
	cfgPortsforYaml := make([]*Port, len(ports))
	for i, p := range ports {
		cfgPortsforYaml[i] = &Port{
			Name:     p.Name,
			Protocol: string(p.Protocol),
			Number:   uint32(p.Port),
		}
	}
	var filled bytes.Buffer
	w := bufio.NewWriter(&filled)
	if err = serviceEntryYamlTemplate.Execute(w, map[string]interface{}{
		"Name":      serviceName,
		"Namespace": Namespace,
		"Domain":    Domain,
		"App":       serviceName,
		"Version":   version,
		"Ports":     cfgPortsforYaml,
		"Hosts":     fmt.Sprintf("%s.%s", serviceName, FullyQualifiedDomainName),
	}); err != nil {
		return
	}
	if err = w.Flush(); err != nil {
		return
	}
	e.Galley.ApplyConfig(string(filled.Bytes()))
	return
}
