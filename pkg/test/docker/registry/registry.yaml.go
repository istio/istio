// Copyright 2018 Istio Authors
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

package registry

import "text/template"

type registryOptions struct {
	Namespace string
	Port      uint16
}

var registryYAMLTemplateStr = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kube-registry
  namespace: {{ .Namespace }}
spec:
  selector:
    matchLabels:
      k8s-app: kube-registry
  replicas: 1
  template:
    metadata:
      labels:
        k8s-app: kube-registry
    spec:
      containers:
      - name: registry
        image: registry:2
        resources:
          limits:
            cpu: 100m
            memory: 100Mi
        env:
        - name: REGISTRY_HTTP_ADDR
          value: :{{ .Port }}
        - name: REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY
          value: /var/lib/registry
        volumeMounts:
        - name: image-store
          mountPath: /var/lib/registry
        ports:
        - containerPort: {{ .Port }}
          name: registry
          protocol: TCP
      volumes:
      - name: image-store
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: kube-registry
  namespace: {{ .Namespace }}
  labels:
    k8s-app: kube-registry
spec:
  selector:
    k8s-app: kube-registry
  ports:
  - name: registry
    port: {{ .Port }}
    protocol: TCP
---
apiVersion: extensions/v1beta1
kind: DaemonSet
metadata:
  name: kube-registry-proxy
  namespace: {{ .Namespace }}
  labels:
    k8s-app: kube-registry
spec:
  template:
    metadata:
      labels:
        k8s-app: kube-registry-proxy
    spec:
      containers:
      - name: kube-registry-proxy
        image: gcr.io/google_containers/kube-registry-proxy:0.4
        resources:
          limits:
            cpu: 100m
            memory: 50Mi
        env:
        - name: REGISTRY_HOST
          value: kube-registry.{{ .Namespace }}.svc.cluster.local
        - name: REGISTRY_PORT
          value: "{{ .Port }}"
        ports:
        - name: registry
          containerPort: 80
          hostPort: {{ .Port }}
`

func getRegistryYAMLTemplate() (*template.Template, error) {
	tmpl := template.New("in_cluster_registry")
	_, err := tmpl.Parse(registryYAMLTemplateStr)
	if err != nil {
		return nil, err
	}
	return tmpl, nil
}
