// Code generated for package schema by go-bindata DO NOT EDIT. (@generated)
// sources:
// metadata.yaml
package schema

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)
type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _metadataYaml = []byte(`# Copyright 2019 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in conformance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#
# This is the main metadata file for Galley processing.
# ####  KEEP ENTRIES ALPHASORTED! ####
#

# The total set of collections, both Istio (i.e. MCP) and K8s (API Server/K8s).
collections:
  ## Istio collections
  - name: "istio/extensions/v1alpha1/wasmplugins"
    kind: "WasmPlugin"
    group: "extensions.istio.io"
    pilot: true

  - name: "istio/mesh/v1alpha1/MeshConfig"
    kind: "MeshConfig"
    group: ""

  - name: "istio/mesh/v1alpha1/MeshNetworks"
    kind: "MeshNetworks"
    group: ""

  - name: "istio/networking/v1alpha3/destinationrules"
    kind: DestinationRule
    group: "networking.istio.io"
    pilot: true

  - name: "istio/networking/v1alpha3/envoyfilters"
    kind: "EnvoyFilter"
    group: "networking.istio.io"
    pilot: true

  - name: "istio/networking/v1alpha3/gateways"
    kind: "Gateway"
    group: "networking.istio.io"
    pilot: true

  - name: "istio/networking/v1alpha3/serviceentries"
    kind: "ServiceEntry"
    group: "networking.istio.io"
    pilot: true

  - name: "istio/networking/v1alpha3/workloadentries"
    kind: "WorkloadEntry"
    group: "networking.istio.io"
    pilot: true

  - name: "istio/networking/v1alpha3/workloadgroups"
    kind: "WorkloadGroup"
    group: "networking.istio.io"
    pilot: true

  - name: "istio/networking/v1alpha3/sidecars"
    kind: "Sidecar"
    group: "networking.istio.io"
    pilot: true

  - name: "istio/networking/v1alpha3/virtualservices"
    kind: "VirtualService"
    group: "networking.istio.io"
    pilot: true

  - name: "istio/security/v1beta1/authorizationpolicies"
    kind: AuthorizationPolicy
    group: "security.istio.io"
    pilot: true

  - name: "istio/security/v1beta1/requestauthentications"
    kind: RequestAuthentication
    group: "security.istio.io"
    pilot: true

  - name: "istio/security/v1beta1/peerauthentications"
    kind: PeerAuthentication
    group: "security.istio.io"
    pilot: true

  - name: "istio/telemetry/v1alpha1/telemetries"
    kind: "Telemetry"
    group: "telemetry.istio.io"
    pilot: true

  ### K8s collections ###

  # Built-in K8s collections
  - name: "k8s/apiextensions.k8s.io/v1/customresourcedefinitions"
    kind: "CustomResourceDefinition"
    group: "apiextensions.k8s.io"

  - name: "k8s/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations"
    kind: "MutatingWebhookConfiguration"
    group: "admissionregistration.k8s.io"

  - name: "k8s/apps/v1/deployments"
    kind: "Deployment"
    group: "apps"

  - name: "k8s/core/v1/endpoints"
    kind: "Endpoints"
    group: ""

  - name: "k8s/core/v1/namespaces"
    kind: "Namespace"
    group: ""

  - name: "k8s/core/v1/nodes"
    kind: "Node"
    group: ""

  - name: "k8s/core/v1/pods"
    kind: "Pod"
    group: ""

  - name: "k8s/core/v1/secrets"
    kind: "Secret"
    group: ""

  - name: "k8s/core/v1/services"
    kind: "Service"
    group: ""

  - name: "k8s/core/v1/configmaps"
    kind: "ConfigMap"
    group: ""

  - name: "k8s/extensions/v1beta1/ingresses"
    kind: "Ingress"
    group: "extensions"

  - kind: "GatewayClass"
    name: "k8s/service_apis/v1alpha1/gatewayclasses"
    group: "networking.x-k8s.io"

  - kind: "Gateway"
    name: "k8s/service_apis/v1alpha1/gateways"
    group: "networking.x-k8s.io"

  - kind: "HTTPRoute"
    name: "k8s/service_apis/v1alpha1/httproutes"
    group: "networking.x-k8s.io"

  - kind: "TCPRoute"
    name: "k8s/service_apis/v1alpha1/tcproutes"
    group: "networking.x-k8s.io"

  - kind: "TLSRoute"
    name: "k8s/service_apis/v1alpha1/tlsroutes"
    group: "networking.x-k8s.io"

  - kind: "BackendPolicy"
    name: "k8s/service_apis/v1alpha1/backendpolicies"
    group: "networking.x-k8s.io"

  # Istio CRD collections
  - name: "k8s/extensions.istio.io/v1alpha1/wasmplugins"
    kind: WasmPlugin
    group: "extensions.istio.io"

  - name: "k8s/networking.istio.io/v1alpha3/destinationrules"
    kind: DestinationRule
    group: "networking.istio.io"

  - name: "k8s/networking.istio.io/v1alpha3/envoyfilters"
    kind: "EnvoyFilter"
    group: "networking.istio.io"

  - name: "k8s/networking.istio.io/v1alpha3/gateways"
    kind: "Gateway"
    group: "networking.istio.io"

  - name: "k8s/networking.istio.io/v1alpha3/serviceentries"
    kind: "ServiceEntry"
    group: "networking.istio.io"

  - name: "k8s/networking.istio.io/v1alpha3/workloadentries"
    kind: "WorkloadEntry"
    group: "networking.istio.io"

  - name: "k8s/networking.istio.io/v1alpha3/workloadgroups"
    kind: "WorkloadGroup"
    group: "networking.istio.io"

  - name: "k8s/networking.istio.io/v1alpha3/sidecars"
    kind: "Sidecar"
    group: "networking.istio.io"

  - name: "k8s/networking.istio.io/v1alpha3/virtualservices"
    kind: "VirtualService"
    group: "networking.istio.io"

  - name: "k8s/security.istio.io/v1beta1/authorizationpolicies"
    kind: AuthorizationPolicy
    group: "security.istio.io"

  - name: "k8s/security.istio.io/v1beta1/requestauthentications"
    kind: RequestAuthentication
    group: "security.istio.io"

  - name: "k8s/security.istio.io/v1beta1/peerauthentications"
    kind: PeerAuthentication
    group: "security.istio.io"

  - name: "k8s/telemetry.istio.io/v1alpha1/telemetries"
    kind: "Telemetry"
    group: "telemetry.istio.io"

# The snapshots to generate
snapshots:
  # Used by Galley to distribute configuration.
  - name: "default"
    strategy: debounce
    collections:
      - "istio/extensions/v1alpha1/wasmplugins"
      - "istio/mesh/v1alpha1/MeshConfig"
      - "istio/networking/v1alpha3/destinationrules"
      - "istio/networking/v1alpha3/envoyfilters"
      - "istio/networking/v1alpha3/gateways"
      - "istio/networking/v1alpha3/serviceentries"
      - "istio/networking/v1alpha3/workloadentries"
      - "istio/networking/v1alpha3/workloadgroups"
      - "istio/networking/v1alpha3/sidecars"
      - "istio/networking/v1alpha3/virtualservices"
      - "istio/security/v1beta1/authorizationpolicies"
      - "istio/security/v1beta1/requestauthentications"
      - "istio/security/v1beta1/peerauthentications"
      - "istio/telemetry/v1alpha1/telemetries"
      - "k8s/core/v1/namespaces"
      - "k8s/core/v1/services"

    # Used by istioctl to perform analysis
  - name: "localAnalysis"
    strategy: immediate
    collections:
      - "istio/mesh/v1alpha1/MeshConfig"
      - "istio/mesh/v1alpha1/MeshNetworks"
      - "istio/networking/v1alpha3/envoyfilters"
      - "istio/networking/v1alpha3/destinationrules"
      - "istio/networking/v1alpha3/gateways"
      - "istio/networking/v1alpha3/serviceentries"
      - "istio/networking/v1alpha3/sidecars"
      - "istio/networking/v1alpha3/virtualservices"
      - "istio/security/v1beta1/authorizationpolicies"
      - "k8s/apiextensions.k8s.io/v1/customresourcedefinitions"
      - "k8s/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations"
      - "k8s/apps/v1/deployments"
      - "k8s/core/v1/namespaces"
      - "k8s/core/v1/pods"
      - "k8s/core/v1/secrets"
      - "k8s/core/v1/services"
      - "k8s/core/v1/configmaps"

# Configuration for resource types.
resources:
  # Kubernetes specific configuration.
  - kind: "CustomResourceDefinition"
    plural: "CustomResourceDefinitions"
    group: "apiextensions.k8s.io"
    version: "v1"
    proto: "k8s.io.apiextensions_apiserver.pkg.apis.apiextensions.v1.CustomResourceDefinition"
    protoPackage: "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

  - kind: "MutatingWebhookConfiguration"
    plural: "MutatingWebhookConfigurations"
    group: "admissionregistration.k8s.io"
    version: "v1"
    proto: "k8s.io.api.admissionregistration.v1.MutatingWebhookConfiguration"
    protoPackage: "k8s.io/api/admissionregistration/v1"

  - kind: "Deployment"
    plural: "Deployments"
    group: "apps"
    version: "v1"
    proto: "k8s.io.api.apps.v1.Deployment"
    protoPackage: "k8s.io/api/apps/v1"

  - kind: "Endpoints"
    plural: "endpoints"
    version: "v1"
    proto: "k8s.io.api.core.v1.Endpoints"
    protoPackage: "k8s.io/api/core/v1"

  - kind: "Namespace"
    plural: "namespaces"
    version: "v1"
    clusterScoped: true
    proto: "k8s.io.api.core.v1.NamespaceSpec"
    protoPackage: "k8s.io/api/core/v1"

  - kind: "Node"
    plural: "nodes"
    version: "v1"
    clusterScoped: true
    proto: "k8s.io.api.core.v1.NodeSpec"
    protoPackage: "k8s.io/api/core/v1"

  - kind: "Pod"
    plural: "pods"
    version: "v1"
    proto: "k8s.io.api.core.v1.Pod"
    protoPackage: "k8s.io/api/core/v1"

  - kind: "Secret"
    plural: "secrets"
    version: "v1"
    proto: "k8s.io.api.core.v1.Secret"
    protoPackage: "k8s.io/api/core/v1"

  - kind: "Service"
    plural: "services"
    version: "v1"
    proto: "k8s.io.api.core.v1.ServiceSpec"
    protoPackage: "k8s.io/api/core/v1"

  - kind: "ConfigMap"
    plural: "configmaps"
    version: "v1"
    proto: "k8s.io.api.core.v1.ConfigMap"
    protoPackage: "k8s.io/api/core/v1"

  - kind: "Ingress"
    plural: "ingresses"
    group: "extensions"
    version: "v1beta1"
    proto: "k8s.io.api.extensions.v1beta1.IngressSpec"
    protoPackage: "k8s.io/api/extensions/v1beta1"
    statusProto: "k8s.io.service_apis.api.v1alpha1.IngressStatus"
    statusProtoPackage: "k8s.io/api/extensions/v1beta1"

  - kind: "GatewayClass"
    plural: "gatewayclasses"
    group: "networking.x-k8s.io"
    version: "v1alpha1"
    clusterScoped: true
    protoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1"
    proto: "k8s.io.service_apis.api.v1alpha1.GatewayClassSpec"
    statusProto: "k8s.io.service_apis.api.v1alpha1.GatewayClassStatus"
    statusProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1"

  - kind: "Gateway"
    plural: "gateways"
    group: "networking.x-k8s.io"
    version: "v1alpha1"
    protoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1"
    proto: "k8s.io.service_apis.api.v1alpha1.GatewaySpec"
    validate: "EmptyValidate"
    statusProto: "k8s.io.service_apis.api.v1alpha1.GatewayStatus"
    statusProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1"

  - kind: "HTTPRoute"
    plural: "httproutes"
    group: "networking.x-k8s.io"
    version: "v1alpha1"
    protoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1"
    proto: "k8s.io.service_apis.api.v1alpha1.HTTPRouteSpec"
    statusProto: "k8s.io.service_apis.api.v1alpha1.HTTPRouteStatus"
    statusProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1"

  - kind: "TCPRoute"
    plural: "tcproutes"
    group: "networking.x-k8s.io"
    version: "v1alpha1"
    protoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1"
    proto: "k8s.io.service_apis.api.v1alpha1.TCPRouteSpec"
    statusProto: "k8s.io.service_apis.api.v1alpha1.TCPRouteStatus"
    statusProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1"

  - kind: "TLSRoute"
    plural: "tlsroutes"
    group: "networking.x-k8s.io"
    version: "v1alpha1"
    protoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1"
    proto: "k8s.io.service_apis.api.v1alpha1.TLSRouteSpec"
    statusProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1"
    statusProto: "k8s.io.service_apis.api.v1alpha1.TLSRouteStatus"

  - kind: "BackendPolicy"
    plural: "backendpolicies"
    group: "networking.x-k8s.io"
    version: "v1alpha1"
    protoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1"
    proto: "k8s.io.service_apis.api.v1alpha1.BackendPolicySpec"
    statusProtoPackage: "sigs.k8s.io/gateway-api/apis/v1alpha1"
    statusProto: "k8s.io.service_apis.api.v1alpha1.BackendPolicyStatus"

  ## Istio resources
  - kind: "VirtualService"
    plural: "virtualservices"
    group: "networking.istio.io"
    version: "v1alpha3"
    proto: "istio.networking.v1alpha3.VirtualService"
    protoPackage: "istio.io/api/networking/v1alpha3"
    description: "describes v1alpha3 route rules"
    statusProto: "istio.meta.v1alpha1.IstioStatus"
    statusProtoPackage: "istio.io/api/meta/v1alpha1"

  - kind: "Gateway"
    plural: "gateways"
    group: "networking.istio.io"
    version: "v1alpha3"
    proto: "istio.networking.v1alpha3.Gateway"
    protoPackage: "istio.io/api/networking/v1alpha3"
    description: "describes a gateway (how a proxy is exposed on the network)"
    statusProto: "istio.meta.v1alpha1.IstioStatus"
    statusProtoPackage: "istio.io/api/meta/v1alpha1"

  - kind: "ServiceEntry"
    plural: "serviceentries"
    group: "networking.istio.io"
    version: "v1alpha3"
    proto: "istio.networking.v1alpha3.ServiceEntry"
    protoPackage: "istio.io/api/networking/v1alpha3"
    description: "describes service entries"
    statusProto: "istio.meta.v1alpha1.IstioStatus"
    statusProtoPackage: "istio.io/api/meta/v1alpha1"

  - kind: "WorkloadEntry"
    plural: "workloadentries"
    group: "networking.istio.io"
    version: "v1alpha3"
    proto: "istio.networking.v1alpha3.WorkloadEntry"
    protoPackage: "istio.io/api/networking/v1alpha3"
    description: "describes workload entries"
    statusProto: "istio.meta.v1alpha1.IstioStatus"
    statusProtoPackage: "istio.io/api/meta/v1alpha1"

  - kind: "WorkloadGroup"
    plural: "workloadgroups"
    group: "networking.istio.io"
    version: "v1alpha3"
    proto: "istio.networking.v1alpha3.WorkloadGroup"
    protoPackage: "istio.io/api/networking/v1alpha3"
    description: "describes workload groups"
    statusProto: "istio.meta.v1alpha1.IstioStatus"
    statusProtoPackage: "istio.io/api/meta/v1alpha1"

  - kind: DestinationRule
    plural: "destinationrules"
    group: "networking.istio.io"
    version: "v1alpha3"
    proto: "istio.networking.v1alpha3.DestinationRule"
    protoPackage: "istio.io/api/networking/v1alpha3"
    description: "describes destination rules"
    statusProto: "istio.meta.v1alpha1.IstioStatus"
    statusProtoPackage: "istio.io/api/meta/v1alpha1"

  - kind: "EnvoyFilter"
    plural: "envoyfilters"
    group: "networking.istio.io"
    version: "v1alpha3"
    proto: "istio.networking.v1alpha3.EnvoyFilter"
    protoPackage: "istio.io/api/networking/v1alpha3"
    description: "describes additional envoy filters to be inserted by Pilot"
    statusProto: "istio.meta.v1alpha1.IstioStatus"
    statusProtoPackage: "istio.io/api/meta/v1alpha1"

  - kind: "Sidecar"
    plural: "sidecars"
    group: "networking.istio.io"
    version: "v1alpha3"
    proto: "istio.networking.v1alpha3.Sidecar"
    protoPackage: "istio.io/api/networking/v1alpha3"
    description: "describes the listeners associated with sidecars in a namespace"
    statusProto: "istio.meta.v1alpha1.IstioStatus"
    statusProtoPackage: "istio.io/api/meta/v1alpha1"

  - kind: "MeshConfig"
    plural: "meshconfigs"
    group: ""
    version: "v1alpha1"
    proto: "istio.mesh.v1alpha1.MeshConfig"
    protoPackage: "istio.io/api/mesh/v1alpha1"
    description: "describes the configuration for the Istio mesh."

  - kind: "MeshNetworks"
    plural: "meshnetworks"
    group: ""
    version: "v1alpha1"
    proto: "istio.mesh.v1alpha1.MeshNetworks"
    protoPackage: "istio.io/api/mesh/v1alpha1"
    description: "describes the networks for the Istio mesh."

  - kind: AuthorizationPolicy
    plural: "authorizationpolicies"
    group: "security.istio.io"
    version: "v1beta1"
    proto: "istio.security.v1beta1.AuthorizationPolicy"
    protoPackage: "istio.io/api/security/v1beta1"
    description: "describes the authorization policy."
    statusProto: "istio.meta.v1alpha1.IstioStatus"
    statusProtoPackage: "istio.io/api/meta/v1alpha1"

  - kind: RequestAuthentication
    plural: "requestauthentications"
    group: "security.istio.io"
    version: "v1beta1"
    proto: "istio.security.v1beta1.RequestAuthentication"
    protoPackage: "istio.io/api/security/v1beta1"
    description: "describes the request authentication."
    statusProto: "istio.meta.v1alpha1.IstioStatus"
    statusProtoPackage: "istio.io/api/meta/v1alpha1"

  - kind: PeerAuthentication
    plural: "peerauthentications"
    group: "security.istio.io"
    version: "v1beta1"
    proto: "istio.security.v1beta1.PeerAuthentication"
    protoPackage: "istio.io/api/security/v1beta1"
    validate: "ValidatePeerAuthentication"
    description: "describes the peer authentication."
    statusProto: "istio.meta.v1alpha1.IstioStatus"
    statusProtoPackage: "istio.io/api/meta/v1alpha1"

  - kind: "Telemetry"
    plural: "telemetries"
    group: "telemetry.istio.io"
    version: "v1alpha1"
    proto: "istio.telemetry.v1alpha1.Telemetry"
    protoPackage: "istio.io/api/telemetry/v1alpha1"
    description: "describes telemetry configuration for workloads"
    statusProto: "istio.meta.v1alpha1.IstioStatus"
    statusProtoPackage: "istio.io/api/meta/v1alpha1"

  - kind: "WasmPlugin"
    plural: "wasmplugins"
    group: "extensions.istio.io"
    version: "v1alpha1"
    proto: "istio.extensions.v1alpha1.WasmPlugin"
    protoPackage: "istio.io/api/extensions/v1alpha1"
    description: ""
    statusProto: "istio.meta.v1alpha1.IstioStatus"
    statusProtoPackage: "istio.io/api/meta/v1alpha1"

# Transform specific configurations
transforms:
  - type: direct
    mapping:
      "k8s/apiextensions.k8s.io/v1/customresourcedefinitions": "k8s/apiextensions.k8s.io/v1/customresourcedefinitions"
      "k8s/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations": "k8s/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations"
      "k8s/extensions.istio.io/v1alpha1/wasmplugins": "istio/extensions/v1alpha1/wasmplugins"
      "k8s/networking.istio.io/v1alpha3/destinationrules": "istio/networking/v1alpha3/destinationrules"
      "k8s/networking.istio.io/v1alpha3/envoyfilters": "istio/networking/v1alpha3/envoyfilters"
      "k8s/networking.istio.io/v1alpha3/gateways": "istio/networking/v1alpha3/gateways"
      "k8s/networking.istio.io/v1alpha3/serviceentries": "istio/networking/v1alpha3/serviceentries"
      "k8s/networking.istio.io/v1alpha3/workloadentries": "istio/networking/v1alpha3/workloadentries"
      "k8s/networking.istio.io/v1alpha3/workloadgroups": "istio/networking/v1alpha3/workloadgroups"
      "k8s/networking.istio.io/v1alpha3/sidecars": "istio/networking/v1alpha3/sidecars"
      "k8s/networking.istio.io/v1alpha3/virtualservices": "istio/networking/v1alpha3/virtualservices"
      "k8s/security.istio.io/v1beta1/authorizationpolicies": "istio/security/v1beta1/authorizationpolicies"
      "k8s/security.istio.io/v1beta1/requestauthentications": "istio/security/v1beta1/requestauthentications"
      "k8s/security.istio.io/v1beta1/peerauthentications": "istio/security/v1beta1/peerauthentications"
      "k8s/telemetry.istio.io/v1alpha1/telemetries": "istio/telemetry/v1alpha1/telemetries"
      "k8s/apps/v1/deployments": "k8s/apps/v1/deployments"
      "k8s/core/v1/namespaces": "k8s/core/v1/namespaces"
      "k8s/core/v1/pods": "k8s/core/v1/pods"
      "k8s/core/v1/secrets": "k8s/core/v1/secrets"
      "k8s/core/v1/services": "k8s/core/v1/services"
      "k8s/core/v1/configmaps": "k8s/core/v1/configmaps"
      "istio/mesh/v1alpha1/MeshConfig": "istio/mesh/v1alpha1/MeshConfig"
      "istio/mesh/v1alpha1/MeshNetworks": "istio/mesh/v1alpha1/MeshNetworks"
`)

func metadataYamlBytes() ([]byte, error) {
	return _metadataYaml, nil
}

func metadataYaml() (*asset, error) {
	bytes, err := metadataYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "metadata.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"metadata.yaml": metadataYaml,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"metadata.yaml": &bintree{metadataYaml, map[string]*bintree{}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}
