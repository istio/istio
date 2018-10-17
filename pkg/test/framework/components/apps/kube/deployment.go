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

package kube

import (
	"fmt"
	"strconv"

	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/framework/tmpl"
)

const (
	template = `
{{- if eq .serviceAccount "true" }}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .service }}
---
{{- end }}
apiVersion: v1
kind: Service
metadata:
  name: {{ .service }}
  labels:
    app: {{ .service }}
spec:
{{- if eq .headless "true" }}
  clusterIP: None
{{- end }}
  ports:
  - port: 80
    targetPort: {{ .port1 }}
    name: http
  - port: 8080
    targetPort: {{ .port2 }}
    name: http-two
{{- if eq .headless "true" }}
  - port: 10090
    targetPort: {{ .port3 }}
    name: tcp
{{- else }}
  - port: 90
    targetPort: {{ .port3 }}
    name: tcp
  - port: 9090
    targetPort: {{ .port4 }}
    name: https
{{- end }}
  - port: 70
    targetPort: {{ .port5 }}
    name: http2-example
  - port: 7070
    targetPort: {{ .port6 }}
    name: grpc
  selector:
    app: {{ .service }}
---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: {{ .deployment }}
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: {{ .service }}
        version: {{ .version }}
{{- if eq .injectProxy "false" }}
      annotations:
        sidecar.istio.io/inject: "false"
{{- end }}
    spec:
{{- if eq .serviceAccount "true" }}
      serviceAccountName: {{ .service }}
{{- end }}
      containers:
      - name: app
        image: {{ .Hub }}/app:{{ .Tag }}
        imagePullPolicy: {{ .ImagePullPolicy }}
        args:
          - --port
          - "{{ .port1 }}"
          - --port
          - "{{ .port2 }}"
          - --port
          - "{{ .port3 }}"
          - --port
          - "{{ .port4 }}"
          - --grpc
          - "{{ .port5 }}"
          - --grpc
          - "{{ .port6 }}"
{{- if eq .healthPort "true" }}
          - --port
          - "3333"
{{- end }}
          - --version
          - "{{ .version }}"
        ports:
        - containerPort: {{ .port1 }}
        - containerPort: {{ .port2 }}
        - containerPort: {{ .port3 }}
        - containerPort: {{ .port4 }}
        - containerPort: {{ .port5 }}
        - containerPort: {{ .port6 }}
{{- if eq .healthPort "true" }}
        - name: tcp-health-port
          containerPort: 3333
        livenessProbe:
          httpGet:
            path: /healthz
            port: 3333
          initialDelaySeconds: 10
          periodSeconds: 10
          failureThreshold: 10
        readinessProbe:
          tcpSocket:
            port: tcp-health-port
          initialDelaySeconds: 10
          periodSeconds: 10
          failureThreshold: 10
{{- end }}
---
`
)

type deployment struct {
	deployment     string
	service        string
	version        string
	port1          int
	port2          int
	port3          int
	port4          int
	port5          int
	port6          int
	injectProxy    bool
	headless       bool
	serviceAccount bool
}

func (d *deployment) apply(e *kubernetes.Implementation) error {
	s := e.KubeSettings()
	result, err := tmpl.Evaluate(template, map[string]string{
		"Hub":             s.Values[kubernetes.HubValuesKey],
		"Tag":             s.Values[kubernetes.TagValuesKey],
		"ImagePullPolicy": s.Values[kubernetes.ImagePullPolicyValuesKey],
		"deployment":      d.deployment,
		"service":         d.service,
		"app":             d.service,
		"version":         d.version,
		"port1":           strconv.Itoa(d.port1),
		"port2":           strconv.Itoa(d.port2),
		"port3":           strconv.Itoa(d.port3),
		"port4":           strconv.Itoa(d.port4),
		"port5":           strconv.Itoa(d.port5),
		"port6":           strconv.Itoa(d.port6),
		"healthPort":      "true",
		"injectProxy":     strconv.FormatBool(d.injectProxy),
		"headless":        strconv.FormatBool(d.headless),
		"serviceAccount":  strconv.FormatBool(d.serviceAccount),
	})
	if err != nil {
		return err
	}

	if err = e.Accessor.ApplyContents(s.DependencyNamespace, result); err != nil {
		return err
	}
	return nil
}

func (d *deployment) wait(e *kubernetes.Implementation) error {
	n := e.KubeSettings().DependencyNamespace
	pod, err := e.Accessor.WaitForPodBySelectors(n, fmt.Sprintf("app=%s", d.service), fmt.Sprintf("version=%s", d.version))
	if err != nil {
		return err
	}

	if err = e.Accessor.WaitUntilPodIsRunning(n, pod.Name); err != nil {
		return err
	}
	return nil
}
