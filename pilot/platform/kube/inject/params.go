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

import (
	"bytes"
	"text/template"

	meshconfig "istio.io/api/mesh/v1alpha1"
)

// Defaults values for injecting istio proxy into kubernetes
// resources.
const (
	DefaultSidecarProxyUID = uint32(1337)
	DefaultVerbosity       = 2
	DefaultImagePullPolicy = "IfNotPresent"

	// e2e integration tests use container name for fetching logs
	ProxyContainerName = "istio-proxy"
)

// Params describes configurable parameters for injecting istio proxy
// into a kubernetes resource.
type Params struct {
	InitImage       string                 `json:"initImage"`
	ProxyImage      string                 `json:"proxyImage"`
	Verbosity       int                    `json:"verbosity"`
	SidecarProxyUID uint32                 `json:"sidecarProxyUID"`
	Version         string                 `json:"version"`
	EnableCoreDump  bool                   `json:"enableCoreDump"`
	DebugMode       bool                   `json:"debugMode"`
	Mesh            *meshconfig.MeshConfig `json:"-"`
	ImagePullPolicy string                 `json:"imagePullPolicy"`
	// Comma separated list of IP ranges in CIDR form. If set, only
	// redirect outbound traffic to Envoy for these IP
	// ranges. Otherwise all outbound traffic is redirected to Envoy.
	IncludeIPRanges string `json:"includeIPRanges"`
}

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
    privileged: true
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
  - -v
  [[ if gt .Verbosity 0 -]]
  - [[ .Verbosity ]]
  [[ else -]]
  - "2"
  [[ end -]]
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
  - {{ .MeshConfig.AuthPolicy }}
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

// InitImageName returns the fully qualified image name for the istio
// init image given a docker hub and tag and debug flag
func InitImageName(hub string, tag string, _ bool) string {
	return hub + "/proxy_init:" + tag
}

// ProxyImageName returns the fully qualified image name for the istio
// proxy image given a docker hub and tag and whether to use debug or not.
func ProxyImageName(hub string, tag string, debug bool) string {
	if debug {
		return hub + "/proxy_debug:" + tag
	}
	return hub + "/proxy:" + tag
}

// GenerateTemplateFromParams generates a sidecar template from the legacy injection parameters
func GenerateTemplateFromParams(params *Params) (string, error) {
	t := template.New("inject").Delims(parameterizedTemplateDelimBegin, parameterizedTemplateDelimEnd)
	var tmp bytes.Buffer
	err := template.Must(t.Parse(parameterizedTemplate)).Execute(&tmp, params)
	return tmp.String(), err
}
