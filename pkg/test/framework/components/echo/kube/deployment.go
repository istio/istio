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

package kube

import (
	"fmt"
	"text/template"

	"github.com/Masterminds/sprig"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/util/tmpl"
)

const (
	serviceYAML = `
{{- if .ServiceAccount }}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .Service }}
---
{{- end }}
apiVersion: v1
kind: Service
metadata:
  name: {{ .Service }}
  labels:
    app: {{ .Service }}
{{- if .ServiceAnnotations }}
  annotations:
{{- range $name, $value := .ServiceAnnotations }}
    {{ $name.Name }}: {{ printf "%q" $value.Value }}
{{- end }}
{{- end }}
spec:
{{- if .Headless }}
  clusterIP: None
{{- end }}
  ports:
{{- range $i, $p := .Ports }}
  - name: {{ $p.Name }}
    port: {{ $p.ServicePort }}
    targetPort: {{ $p.InstancePort }}
{{- end }}
  selector:
    app: {{ .Service }}
`

	deploymentYAML = `
{{- $subsets := .Subsets }}
{{- $cluster := .Cluster }}
{{- range $i, $subset := $subsets }}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ $.Service }}-{{ $subset.Version }}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: {{ $.Service }}
      version: {{ $subset.Version }}
{{- if ne $.Locality "" }}
      istio-locality: {{ $.Locality }}
{{- end }}
  template:
    metadata:
      labels:
        app: {{ $.Service }}
        version: {{ $subset.Version }}
{{- if ne $.Locality "" }}
        istio-locality: {{ $.Locality }}
{{- end }}
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "15014"
{{- range $name, $value := $subset.Annotations }}
        {{ $name.Name }}: {{ printf "%q" $value.Value }}
{{- end }}
{{- if $.IncludeInboundPorts }}
        traffic.sidecar.istio.io/includeInboundPorts: "{{ $.IncludeInboundPorts }}"
{{- end }}
    spec:
{{- if $.ServiceAccount }}
      serviceAccountName: {{ $.Service }}
{{- end }}
      containers:
      - name: app
        image: {{ $.Hub }}/app:{{ $.Tag }}
        imagePullPolicy: {{ $.PullPolicy }}
        securityContext:
          runAsUser: 1
        args:
          - --metrics=15014
          - --cluster
          - "{{ $cluster }}"
{{- range $i, $p := $.ContainerPorts }}
{{- if eq .Protocol "GRPC" }}
          - --grpc
{{- else if eq .Protocol "TCP" }}
          - --tcp
{{- else }}
          - --port
{{- end }}
          - "{{ $p.Port }}"
{{- if $p.TLS }}
          - --tls={{ $p.Port }}
{{- end }}
{{- end }}
{{- range $i, $p := $.WorkloadOnlyPorts }}
{{- if eq .Protocol "TCP" }}
          - --tcp
{{- else }}
          - --port
{{- end }}
          - "{{ $p.Port }}"
{{- if $p.TLS }}
          - --tls={{ $p.Port }}
{{- end }}
{{- end }}
          - --version
          - "{{ $subset.Version }}"
{{- if $.TLSSettings }}
          - --crt=/etc/certs/custom/cert-chain.pem
          - --key=/etc/certs/custom/key.pem
{{- end }}
        ports:
{{- range $i, $p := $.ContainerPorts }}
        - containerPort: {{ $p.Port }} 
{{- if eq .Port 3333 }}
          name: tcp-health-port
{{- end }}
{{- end }}
        readinessProbe:
          httpGet:
            path: /
            port: 8080
          initialDelaySeconds: 1
          periodSeconds: 2
          failureThreshold: 10
        livenessProbe:
          tcpSocket:
            port: tcp-health-port
          initialDelaySeconds: 10
          periodSeconds: 10
          failureThreshold: 10
{{- if $.TLSSettings }}
        volumeMounts:
        - mountPath: /etc/certs/custom
          name: custom-certs
      volumes:
      - configMap:
          name: {{ $.Service }}-certs
        name: custom-certs
{{- end}}
---
{{- end}}
{{- if .TLSSettings }}
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ $.Service }}-certs
data:
  root-cert.pem: |
{{ .TLSSettings.RootCert | indent 4 }}
  cert-chain.pem: |
{{ .TLSSettings.ClientCert | indent 4 }}
  key.pem: |
{{.TLSSettings.Key | indent 4}}
---
{{- end}}
`
)

var (
	serviceTemplate    *template.Template
	deploymentTemplate *template.Template
)

func init() {
	serviceTemplate = template.New("echo_service")
	if _, err := serviceTemplate.Funcs(sprig.TxtFuncMap()).Parse(serviceYAML); err != nil {
		panic(fmt.Sprintf("unable to parse echo service template: %v", err))
	}

	deploymentTemplate = template.New("echo_deployment")
	if _, err := deploymentTemplate.Funcs(sprig.TxtFuncMap()).Parse(deploymentYAML); err != nil {
		panic(fmt.Sprintf("unable to parse echo deployment template: %v", err))
	}
}

func generateYAML(cfg echo.Config) (serviceYAML string, deploymentYAML string, err error) {
	// Create the parameters for the YAML template.
	settings, err := image.SettingsFromCommandLine()
	if err != nil {
		return "", "", err
	}
	return generateYAMLWithSettings(cfg, settings)
}

func generateYAMLWithSettings(cfg echo.Config, settings *image.Settings) (serviceYAML string, deploymentYAML string, err error) {
	// Convert legacy config to workload oritended.
	if cfg.Subsets == nil {
		cfg.Subsets = []echo.SubsetConfig{
			{
				Version: cfg.Version,
			},
		}
	}

	for i := range cfg.Subsets {
		if cfg.Subsets[i].Version == "" {
			cfg.Subsets[i].Version = "v1"
		}
	}

	params := map[string]interface{}{
		"Hub":                 settings.Hub,
		"Tag":                 settings.Tag,
		"PullPolicy":          settings.PullPolicy,
		"Service":             cfg.Service,
		"Version":             cfg.Version,
		"Headless":            cfg.Headless,
		"Locality":            cfg.Locality,
		"ServiceAccount":      cfg.ServiceAccount,
		"Ports":               cfg.Ports,
		"WorkloadOnlyPorts":   cfg.WorkloadOnlyPorts,
		"ContainerPorts":      getContainerPorts(cfg.Ports),
		"ServiceAnnotations":  cfg.ServiceAnnotations,
		"IncludeInboundPorts": cfg.IncludeInboundPorts,
		"Subsets":             cfg.Subsets,
		"TLSSettings":         cfg.TLSSettings,
		"Cluster":             cfg.ClusterIndex(),
	}

	serviceYAML, err = tmpl.Execute(serviceTemplate, params)
	if err != nil {
		return
	}

	// Generate the YAML content.
	deploymentYAML, err = tmpl.Execute(deploymentTemplate, params)
	return
}
