// Copyright 2017 Istio Authors
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

package inject

var (
	productionTemplate = `
initContainers:
- name: istio-init
  image: {{ printf "%s" .MConfig.InitImage }}
  args:
  - "-p"
  - {{ printf "%d" .MConfig.Mesh.ProxyListenPort }}
  - "-u"
  - {{ printf "%d" .MConfig.SidecarProxyUID }}
  {{ if ne .MConfig.IncludeIPRanges "" -}}
  - "-i"
  - {{ printf "%v" .MConfig.IncludeIPRanges }}
  {{ end -}}
  {{ if eq .MConfig.ImagePullPolicy "" -}}
  imagePullPolicy: {{ "IfNotPresent" }}
  {{ else -}}
  imagePullPolicy: {{ printf "%s" .MConfig.ImagePullPolicy }}
  {{ end -}}
  securityContext:
    capabilities:
      add:
      - NET_ADMIN
    privileged: true
  restartPolicy: Always
{{ if eq .MConfig.EnableCoreDump true -}}
- args:
  - -c
  - sysctl -w kernel.core_pattern=/etc/istio/proxy/core.%e.%p.%t && ulimit -c
    unlimited
  command:
  - /bin/sh
  image: alpine
  imagePullPolicy: IfNotPresent
  name: enable-core-dump
  resources: {}
  securityContext:
    privileged: true
{{ end -}}
containers:
- name: istio-proxy
  image: {{ printf "%s" .MConfig.ProxyImage }}
  args:
  - proxy
  - sidecar
  - -v
  {{ if gt .MConfig.Verbosity 0 -}}
  - {{ printf "%v" .MConfig.Verbosity }}
  {{ else -}}
  - "2"
  {{ end -}}
  - --configPath
  - {{ printf "%s" .MConfig.Mesh.DefaultConfig.ConfigPath }}
  - --binaryPath
  - {{ printf "%s" .MConfig.Mesh.DefaultConfig.BinaryPath }}
  - --serviceCluster
  {{ if eq .ServiceCluster "" -}}
  - {{ printf "%s" .ServiceCluster }}
  {{ else -}}
  - {{ printf "%s" .ServiceCluster }}
  {{ end -}}
  - --drainDuration
  - 2s
  - --parentShutdownDuration
  - 3s
  - --discoveryAddress
  - {{ printf "%s" .MConfig.Mesh.DefaultConfig.DiscoveryAddress }}
  - --discoveryRefreshDelay
  - 1s
  - --zipkinAddress
  - {{ printf "%s" .MConfig.Mesh.DefaultConfig.ZipkinAddress }}
  - --connectTimeout
  - 1s
  - --statsdUdpAddress
  - {{ printf "%s" .MConfig.Mesh.DefaultConfig.StatsdUdpAddress }}
  - --proxyAdminPort
  - {{ printf "%v" .MConfig.Mesh.DefaultConfig.ProxyAdminPort }}
  - --controlPlaneAuthPolicy
  - {{ printf "%s"  .AuthPolicy }}
  env:
  - name: POD_NAME
    valueFrom:
      fieldRef:
        fieldPath: metadata.name
  - name: POD_NAMESPACE
    valueFrom:
      fieldRef:
        fieldPath: metadata.namespace
  - name: INSTANCE_IP
    valueFrom:
      fieldRef:
        fieldPath: status.podIP
  {{ if eq .MConfig.ImagePullPolicy "" -}}
  imagePullPolicy: {{ "IfNotPresent" }}
  {{ else -}}
  imagePullPolicy: {{ printf "%s" .MConfig.ImagePullPolicy }}
  {{ end -}}
  securityContext:
      {{ if eq .MConfig.DebugMode true -}}
      privileged: true
      readOnlyRootFilesystem: false
      {{ else -}}
      privileged: false
      readOnlyRootFilesystem: true 
      {{ end -}}
      runAsUser: 1337
  restartPolicy: Always
  volumeMounts:
  - mountPath: /etc/istio/proxy
    name: istio-envoy
  - mountPath: /etc/certs/
    name: istio-certs
    readOnly: true
volumes:
- emptyDir:
    medium: Memory
  name: istio-envoy
- name: istio-certs
  secret:
    optional: true
    {{ if eq .Spec.ServiceAccountName "" -}}
    secretName: {{ "istio.default" }}
    {{ else -}}
    secretName: {{ printf "istio.%s" .Spec.ServiceAccountName }}
    {{ end -}}`
)
