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

package inject

const (
	sidecarTemplateDelimBegin = "[["
	sidecarTemplateDelimEnd   = "]]"

	// nolint: lll
	parameterizedTemplate = `
[[- $proxyImageKey                  := "sidecar.istio.io/proxyImage" -]]
[[- $interceptionModeKey            := "sidecar.istio.io/interceptionMode" -]]
[[- $includeOutboundIPRangesKey     := "traffic.sidecar.istio.io/includeOutboundIPRanges" -]]
[[- $excludeOutboundIPRangesKey     := "traffic.sidecar.istio.io/excludeOutboundIPRanges" -]]
[[- $includeInboundPortsKey         := "traffic.sidecar.istio.io/includeInboundPorts" -]]
[[- $excludeInboundPortsKey         := "traffic.sidecar.istio.io/excludeInboundPorts" -]]
initContainers:
- name: istio-init
  image: {{ .InitImage }}
  args:
  - "-p"
  - [[ .MeshConfig.ProxyListenPort ]]
  - "-u"
  - {{ .SidecarProxyUID }}
  - "-m"
  - [[ annotation .ObjectMeta $interceptionModeKey .ProxyConfig.InterceptionMode ]]
  - "-i"
  - "[[ annotation .ObjectMeta $includeOutboundIPRangesKey "{{ .IncludeIPRanges }}" ]]"
  - "-x"
  - "[[ annotation .ObjectMeta $excludeOutboundIPRangesKey "{{ .ExcludeIPRanges }}" ]]"
  - "-b"
  - "[[ annotation .ObjectMeta $includeInboundPortsKey (includeInboundPorts .Spec.Containers) ]]"
  - "-d"
  - "[[ annotation .ObjectMeta $excludeInboundPortsKey "{{ .ExcludeInboundPorts }}" ]]"
  {{ if eq .ImagePullPolicy "" -}}
  imagePullPolicy: IfNotPresent
  {{ else -}}
  imagePullPolicy: {{ .ImagePullPolicy }}
  {{ end -}}
  securityContext:
    capabilities:
      add:
      - NET_ADMIN
    {{ if eq .DebugMode true -}}
    privileged: true
    {{ end -}}
  restartPolicy: Always
{{ if eq .EnableCoreDump true -}}
- args:
  - -c
  - sysctl -w kernel.core_pattern=/etc/istio/proxy/core.%e.%p.%t && ulimit -c unlimited
  command:
  - /bin/sh
  image: {{ .InitImage }}
  imagePullPolicy: IfNotPresent
  name: enable-core-dump
  resources: {}
  securityContext:
    privileged: true
{{ end -}}
containers:
- name: istio-proxy
  image: [[ annotation .ObjectMeta $proxyImageKey "{{ .ProxyImage }}" ]] 
  args:
  - proxy
  - sidecar
  - --configPath
  - [[ .ProxyConfig.ConfigPath ]]
  - --binaryPath
  - [[ .ProxyConfig.BinaryPath ]]
  - --serviceCluster
  [[ if ne "" (index .ObjectMeta.Labels "app") -]]
  - [[ index .ObjectMeta.Labels "app" ]]
  [[ else -]]
  - "istio-proxy"
  [[ end -]]
  - --drainDuration
  - [[ formatDuration .ProxyConfig.DrainDuration ]]
  - --parentShutdownDuration
  - [[ formatDuration .ProxyConfig.ParentShutdownDuration ]]
  - --discoveryAddress
  - [[ .ProxyConfig.DiscoveryAddress ]]
  - --discoveryRefreshDelay
  - [[ formatDuration .ProxyConfig.DiscoveryRefreshDelay ]]
  - --zipkinAddress
  - [[ .ProxyConfig.ZipkinAddress ]]
  - --connectTimeout
  - [[ formatDuration .ProxyConfig.ConnectTimeout ]]
  - --statsdUdpAddress
  - [[ .ProxyConfig.StatsdUdpAddress ]]
  - --proxyAdminPort
  - [[ .ProxyConfig.ProxyAdminPort ]]
  [[ if gt .ProxyConfig.Concurrency 0 -]]
  - --concurrency
  - [[ .ProxyConfig.Concurrency ]]
  [[ end -]]
  - --controlPlaneAuthPolicy
  - [[ .ProxyConfig.ControlPlaneAuthPolicy ]]
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
  - name: ISTIO_META_POD_NAME
    valueFrom:
      fieldRef:
        fieldPath: metadata.name
  - name: ISTIO_META_INTERCEPTION_MODE
    value: [[ annotation .ObjectMeta $interceptionModeKey .ProxyConfig.InterceptionMode ]]
  {{ if eq .ImagePullPolicy "" -}}
  imagePullPolicy: IfNotPresent
  {{ else -}}
  imagePullPolicy: {{ .ImagePullPolicy }}
  {{ end -}}
  resources:
    requests:
      cpu: 10m
  securityContext:
    {{ if eq .DebugMode true -}}
    privileged: true
    readOnlyRootFilesystem: false
    {{ else -}}
    privileged: false
    readOnlyRootFilesystem: true
    [[ if eq (annotation .ObjectMeta $interceptionModeKey .ProxyConfig.InterceptionMode) "TPROXY" -]]
    capabilities:
      add:
      - NET_ADMIN
    [[ end -]]
    {{ end -}}
    [[ if ne (annotation .ObjectMeta $interceptionModeKey .ProxyConfig.InterceptionMode) "TPROXY" -]]
    runAsUser: 1337
    [[ end -]]
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
    [[ if eq .Spec.ServiceAccountName "" -]]
    secretName: istio.default
    [[ else -]]
    secretName: [[ printf "istio.%s" .Spec.ServiceAccountName ]]
    [[ end -]]
`
)
