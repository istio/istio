/*
Copyright 2016 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package chartutil

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"k8s.io/helm/pkg/proto/hapi/chart"
)

const (
	// ChartfileName is the default Chart file name.
	ChartfileName = "Chart.yaml"
	// ValuesfileName is the default values file name.
	ValuesfileName = "values.yaml"
	// TemplatesDir is the relative directory name for templates.
	TemplatesDir = "templates"
	// ChartsDir is the relative directory name for charts dependencies.
	ChartsDir = "charts"
	// IgnorefileName is the name of the Helm ignore file.
	IgnorefileName = ".helmignore"
	// IngressFileName is the name of the example ingress file.
	IngressFileName = "ingress.yaml"
	// DeploymentName is the name of the example deployment file.
	DeploymentName = "deployment.yaml"
	// ServiceName is the name of the example service file.
	ServiceName = "service.yaml"
	// NotesName is the name of the example NOTES.txt file.
	NotesName = "NOTES.txt"
	// HelpersName is the name of the example NOTES.txt file.
	HelpersName = "_helpers.tpl"
)

const defaultValues = `# Default values for %s.
# This is a YAML-formatted file.
# Declare variables to be passed into your templates.

replicaCount: 1

image:
  repository: nginx
  tag: stable
  pullPolicy: IfNotPresent

service:
  type: ClusterIP
  port: 80

ingress:
  enabled: false
  annotations: {}
    # kubernetes.io/ingress.class: nginx
    # kubernetes.io/tls-acme: "true"
  path: /
  hosts:
    - chart-example.local
  tls: []
  #  - secretName: chart-example-tls
  #    hosts:
  #      - chart-example.local

resources: {}
  # We usually recommend not to specify default resources and to leave this as a conscious
  # choice for the user. This also increases chances charts run on environments with little
  # resources, such as Minikube. If you do want to specify resources, uncomment the following
  # lines, adjust them as necessary, and remove the curly braces after 'resources:'.
  # limits:
  #  cpu: 100m
  #  memory: 128Mi
  # requests:
  #  cpu: 100m
  #  memory: 128Mi

nodeSelector: {}

tolerations: []

affinity: {}
`

const defaultIgnore = `# Patterns to ignore when building packages.
# This supports shell glob matching, relative path matching, and
# negation (prefixed with !). Only one pattern per line.
.DS_Store
# Common VCS dirs
.git/
.gitignore
.bzr/
.bzrignore
.hg/
.hgignore
.svn/
# Common backup files
*.swp
*.bak
*.tmp
*~
# Various IDEs
.project
.idea/
*.tmproj
`

const defaultIngress = `{{- if .Values.ingress.enabled -}}
{{- $fullName := include "<CHARTNAME>.fullname" . -}}
{{- $servicePort := .Values.service.port -}}
{{- $ingressPath := .Values.ingress.path -}}
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: {{ $fullName }}
  labels:
    app: {{ template "<CHARTNAME>.name" . }}
    chart: {{ template "<CHARTNAME>.chart" . }}
    release: {{ .Release.Name }}
    heritage: {{ .Release.Service }}
{{- with .Values.ingress.annotations }}
  annotations:
{{ toYaml . | indent 4 }}
{{- end }}
spec:
{{- if .Values.ingress.tls }}
  tls:
  {{- range .Values.ingress.tls }}
    - hosts:
      {{- range .hosts }}
        - {{ . }}
      {{- end }}
      secretName: {{ .secretName }}
  {{- end }}
{{- end }}
  rules:
  {{- range .Values.ingress.hosts }}
    - host: {{ . }}
      http:
        paths:
          - path: {{ $ingressPath }}
            backend:
              serviceName: {{ $fullName }}
              servicePort: http
  {{- end }}
{{- end }}
`

const defaultDeployment = `apiVersion: apps/v1beta2
kind: Deployment
metadata:
  name: {{ template "<CHARTNAME>.fullname" . }}
  labels:
    app: {{ template "<CHARTNAME>.name" . }}
    chart: {{ template "<CHARTNAME>.chart" . }}
    release: {{ .Release.Name }}
    heritage: {{ .Release.Service }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector:
    matchLabels:
      app: {{ template "<CHARTNAME>.name" . }}
      release: {{ .Release.Name }}
  template:
    metadata:
      labels:
        app: {{ template "<CHARTNAME>.name" . }}
        release: {{ .Release.Name }}
    spec:
      containers:
        - name: {{ .Chart.Name }}
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          imagePullPolicy: {{ .Values.image.pullPolicy }}
          ports:
            - name: http
              containerPort: 80
              protocol: TCP
          livenessProbe:
            httpGet:
              path: /
              port: http
          readinessProbe:
            httpGet:
              path: /
              port: http
          resources:
{{ toYaml .Values.resources | indent 12 }}
    {{- with .Values.nodeSelector }}
      nodeSelector:
{{ toYaml . | indent 8 }}
    {{- end }}
    {{- with .Values.affinity }}
      affinity:
{{ toYaml . | indent 8 }}
    {{- end }}
    {{- with .Values.tolerations }}
      tolerations:
{{ toYaml . | indent 8 }}
    {{- end }}
`

const defaultService = `apiVersion: v1
kind: Service
metadata:
  name: {{ template "<CHARTNAME>.fullname" . }}
  labels:
    app: {{ template "<CHARTNAME>.name" . }}
    chart: {{ template "<CHARTNAME>.chart" . }}
    release: {{ .Release.Name }}
    heritage: {{ .Release.Service }}
spec:
  type: {{ .Values.service.type }}
  ports:
    - port: {{ .Values.service.port }}
      targetPort: http
      protocol: TCP
      name: http
  selector:
    app: {{ template "<CHARTNAME>.name" . }}
    release: {{ .Release.Name }}
`

const defaultNotes = `1. Get the application URL by running these commands:
{{- if .Values.ingress.enabled }}
{{- range .Values.ingress.hosts }}
  http{{ if $.Values.ingress.tls }}s{{ end }}://{{ . }}{{ $.Values.ingress.path }}
{{- end }}
{{- else if contains "NodePort" .Values.service.type }}
  export NODE_PORT=$(kubectl get --namespace {{ .Release.Namespace }} -o jsonpath="{.spec.ports[0].nodePort}" services {{ template "<CHARTNAME>.fullname" . }})
  export NODE_IP=$(kubectl get nodes --namespace {{ .Release.Namespace }} -o jsonpath="{.items[0].status.addresses[0].address}")
  echo http://$NODE_IP:$NODE_PORT
{{- else if contains "LoadBalancer" .Values.service.type }}
     NOTE: It may take a few minutes for the LoadBalancer IP to be available.
           You can watch the status of by running 'kubectl get svc -w {{ template "<CHARTNAME>.fullname" . }}'
  export SERVICE_IP=$(kubectl get svc --namespace {{ .Release.Namespace }} {{ template "<CHARTNAME>.fullname" . }} -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
  echo http://$SERVICE_IP:{{ .Values.service.port }}
{{- else if contains "ClusterIP" .Values.service.type }}
  export POD_NAME=$(kubectl get pods --namespace {{ .Release.Namespace }} -l "app={{ template "<CHARTNAME>.name" . }},release={{ .Release.Name }}" -o jsonpath="{.items[0].metadata.name}")
  echo "Visit http://127.0.0.1:8080 to use your application"
  kubectl port-forward $POD_NAME 8080:80
{{- end }}
`

const defaultHelpers = `{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "<CHARTNAME>.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "<CHARTNAME>.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "<CHARTNAME>.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}
`

// CreateFrom creates a new chart, but scaffolds it from the src chart.
func CreateFrom(chartfile *chart.Metadata, dest string, src string) error {
	schart, err := Load(src)
	if err != nil {
		return fmt.Errorf("could not load %s: %s", src, err)
	}

	schart.Metadata = chartfile

	var updatedTemplates []*chart.Template

	for _, template := range schart.Templates {
		newData := Transform(string(template.Data), "<CHARTNAME>", schart.Metadata.Name)
		updatedTemplates = append(updatedTemplates, &chart.Template{Name: template.Name, Data: newData})
	}

	schart.Templates = updatedTemplates
	schart.Values = &chart.Config{Raw: string(Transform(schart.Values.Raw, "<CHARTNAME>", schart.Metadata.Name))}

	return SaveDir(schart, dest)
}

// Create creates a new chart in a directory.
//
// Inside of dir, this will create a directory based on the name of
// chartfile.Name. It will then write the Chart.yaml into this directory and
// create the (empty) appropriate directories.
//
// The returned string will point to the newly created directory. It will be
// an absolute path, even if the provided base directory was relative.
//
// If dir does not exist, this will return an error.
// If Chart.yaml or any directories cannot be created, this will return an
// error. In such a case, this will attempt to clean up by removing the
// new chart directory.
func Create(chartfile *chart.Metadata, dir string) (string, error) {
	path, err := filepath.Abs(dir)
	if err != nil {
		return path, err
	}

	if fi, err := os.Stat(path); err != nil {
		return path, err
	} else if !fi.IsDir() {
		return path, fmt.Errorf("no such directory %s", path)
	}

	n := chartfile.Name
	cdir := filepath.Join(path, n)
	if fi, err := os.Stat(cdir); err == nil && !fi.IsDir() {
		return cdir, fmt.Errorf("file %s already exists and is not a directory", cdir)
	}
	if err := os.MkdirAll(cdir, 0755); err != nil {
		return cdir, err
	}

	cf := filepath.Join(cdir, ChartfileName)
	if _, err := os.Stat(cf); err != nil {
		if err := SaveChartfile(cf, chartfile); err != nil {
			return cdir, err
		}
	}

	for _, d := range []string{TemplatesDir, ChartsDir} {
		if err := os.MkdirAll(filepath.Join(cdir, d), 0755); err != nil {
			return cdir, err
		}
	}

	files := []struct {
		path    string
		content []byte
	}{
		{
			// values.yaml
			path:    filepath.Join(cdir, ValuesfileName),
			content: []byte(fmt.Sprintf(defaultValues, chartfile.Name)),
		},
		{
			// .helmignore
			path:    filepath.Join(cdir, IgnorefileName),
			content: []byte(defaultIgnore),
		},
		{
			// ingress.yaml
			path:    filepath.Join(cdir, TemplatesDir, IngressFileName),
			content: Transform(defaultIngress, "<CHARTNAME>", chartfile.Name),
		},
		{
			// deployment.yaml
			path:    filepath.Join(cdir, TemplatesDir, DeploymentName),
			content: Transform(defaultDeployment, "<CHARTNAME>", chartfile.Name),
		},
		{
			// service.yaml
			path:    filepath.Join(cdir, TemplatesDir, ServiceName),
			content: Transform(defaultService, "<CHARTNAME>", chartfile.Name),
		},
		{
			// NOTES.txt
			path:    filepath.Join(cdir, TemplatesDir, NotesName),
			content: Transform(defaultNotes, "<CHARTNAME>", chartfile.Name),
		},
		{
			// _helpers.tpl
			path:    filepath.Join(cdir, TemplatesDir, HelpersName),
			content: Transform(defaultHelpers, "<CHARTNAME>", chartfile.Name),
		},
	}

	for _, file := range files {
		if _, err := os.Stat(file.path); err == nil {
			// File exists and is okay. Skip it.
			continue
		}
		if err := ioutil.WriteFile(file.path, file.content, 0644); err != nil {
			return cdir, err
		}
	}
	return cdir, nil
}
