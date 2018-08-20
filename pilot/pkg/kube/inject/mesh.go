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
	parameterizedTemplate     = `
initContainers:
- name: istio-init
  image: {{ .InitImage }}
  args:
  - "-p"
  - [[ .MeshConfig.ProxyListenPort ]]
  - "-u"
  - {{ .SidecarProxyUID }}
  - "-m"
  - [[ or (index .ObjectMeta.Annotations "sidecar.istio.io/interceptionMode") .ProxyConfig.InterceptionMode.String ]]
  - "-i"
  [[ if (isset .ObjectMeta.Annotations "traffic.sidecar.istio.io/includeOutboundIPRanges") -]]
  - "[[ index .ObjectMeta.Annotations "traffic.sidecar.istio.io/includeOutboundIPRanges"]]"
  [[ else -]]
  - "{{ .IncludeIPRanges }}"
  [[ end -]]
  - "-x"
  [[ if (isset .ObjectMeta.Annotations "traffic.sidecar.istio.io/excludeOutboundIPRanges") -]]
  - "[[ index .ObjectMeta.Annotations "traffic.sidecar.istio.io/excludeOutboundIPRanges" ]]"
  [[ else -]]
  - "{{ .ExcludeIPRanges }}"
  [[ end -]]
  - "-b"
  [[ if (isset .ObjectMeta.Annotations "traffic.sidecar.istio.io/includeInboundPorts") -]]
  - "[[ index .ObjectMeta.Annotations "traffic.sidecar.istio.io/includeInboundPorts" ]]"
  [[ else -]]
  - [[ range .Spec.Containers -]]
      [[ range .Ports -]]
        [[ .ContainerPort -]],
      [[ end -]]
    [[ end -]]
  [[ end ]]
  - "-d"
  [[ if (isset .ObjectMeta.Annotations "traffic.sidecar.istio.io/excludeInboundPorts") -]]
  - "[[ index .ObjectMeta.Annotations "traffic.sidecar.istio.io/excludeInboundPorts" ]]"
  [[ else -]]
  - "{{ .ExcludeInboundPorts }}"
  [[ end -]]
  {{ if eq .ImagePullPolicy "" -}}
  imagePullPolicy: IfNotPresent
  {{ else -}}
  imagePullPolicy: {{ .ImagePullPolicy }}
  {{ end -}}
  securityContext:
    capabilities:
      add:
      - NET_ADMIN
    {{ if (or (eq .DebugMode true) (eq .Privileged true)) -}}
    privileged: true
    {{ end -}}
  restartPolicy: Always
{{ if eq .EnableCoreDump true -}}
- name: enable-core-dump
  args:
  - -c
  - sysctl -w kernel.core_pattern=/etc/istio/proxy/core.%e.%p.%t && ulimit -c unlimited
  command:
  - /bin/sh
  image: {{ .InitImage }}
  imagePullPolicy: IfNotPresent
  resources: {}
  security:
    privileged: true
{{ end -}}
containers:
- name: istio-proxy
  image: [[ if (isset .ObjectMeta.Annotations "sidecar.istio.io/proxyImage") -]]
  "[[ index .ObjectMeta.Annotations "sidecar.istio.io/proxyImage" ]]"
  [[ else -]]
  {{ .ProxyImage }}
  [[ end -]]
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
    value: [[ or (index .ObjectMeta.Annotations "sidecar.istio.io/interceptionMode") .ProxyConfig.InterceptionMode.String ]]
  {{ if eq .ImagePullPolicy "" -}}
  imagePullPolicy: IfNotPresent
  {{ else -}}
  imagePullPolicy: {{ .ImagePullPolicy }}
  {{ end -}}
  resources:
    requests:
      cpu: 10m
  securityContext:
    {{ if (or (eq .DebugMode true) (eq .Privileged true)) -}}
    privileged: true
    {{ end -}}
    readOnlyRootFilesystem: true
    [[ if eq (or (index .ObjectMeta.Annotations "sidecar.istio.io/interceptionMode") .ProxyConfig.InterceptionMode.String) "TPROXY" -]]
    capabilities:
      add:
      - NET_ADMIN
    [[ end -]]
    [[ if ne (or (index .ObjectMeta.Annotations "sidecar.istio.io/interceptionMode") .ProxyConfig.InterceptionMode.String) "TPROXY" -]]
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
