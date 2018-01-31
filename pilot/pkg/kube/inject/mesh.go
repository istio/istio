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
	parameterizedTemplateDelimBegin = "[["
	parameterizedTemplateDelimEnd   = "]]"
	parameterizedTemplate           = `
initContainers:
- name: istio-init
  image: [[ .InitImage ]]
  args:
  - "-p"
  - {{ .MeshConfig.ProxyListenPort }}
  - "-u"
  - [[ .SidecarProxyUID ]]
  [[ if ne .IncludeIPRanges "" -]]
  - "-i"
  - [[ .IncludeIPRanges ]]
  [[ end -]]
  [[ if eq .ImagePullPolicy "" -]]
  imagePullPolicy: IfNotPresent
  [[ else -]]
  imagePullPolicy: [[ .ImagePullPolicy ]]
  [[ end -]]
  securityContext:
    capabilities:
      add:
      - NET_ADMIN
    [[ if eq .DebugMode true -]]  
    privileged: true
    [[ end -]]
  restartPolicy: Always
[[ if eq .EnableCoreDump true -]]
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
[[ end -]]
containers:
- name: istio-proxy
  image: [[ .ProxyImage ]]
  args:
  - proxy
  - sidecar
  - --configPath
  - {{ .ProxyConfig.ConfigPath }}
  - --binaryPath
  - {{ .ProxyConfig.BinaryPath }}
  - --serviceCluster
  {{ if ne "" (index .ObjectMeta.Labels "app") -}}
  - {{ index .ObjectMeta.Labels "app" }}
  {{ else -}}
  - "istio-proxy"
  {{ end -}}
  - --drainDuration
  - 2s
  - --parentShutdownDuration
  - 3s
  - --discoveryAddress
  - {{ .ProxyConfig.DiscoveryAddress }}
  - --discoveryRefreshDelay
  - 1s
  - --zipkinAddress
  - {{ .ProxyConfig.ZipkinAddress }}
  - --connectTimeout
  - 1s
  - --statsdUdpAddress
  - {{ .ProxyConfig.StatsdUdpAddress }}
  - --proxyAdminPort
  - {{ .ProxyConfig.ProxyAdminPort }}
  - --controlPlaneAuthPolicy
  - {{ .ProxyConfig.ControlPlaneAuthPolicy }}
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
  [[ if eq .ImagePullPolicy "" -]]
  imagePullPolicy: IfNotPresent
  [[ else -]]
  imagePullPolicy: [[ .ImagePullPolicy ]]
  [[ end -]]
  securityContext:
      [[ if eq .DebugMode true -]]
      privileged: true
      readOnlyRootFilesystem: false
      [[ else -]]
      privileged: false
      readOnlyRootFilesystem: true
      [[ end -]]
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
    secretName: istio.default
    {{ else -}}
    secretName: {{ printf "istio.%s" .Spec.ServiceAccountName }}
    {{ end -}}
`
)
