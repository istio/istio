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

package agentgateway

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
)

const (
	gatewayTLSTerminateModeKey = "gateway.istio.io/tls-terminate-mode"
	addressTypeOverride        = "networking.istio.io/address-type"
	gatewayClassDefaults       = "gateway.istio.io/defaults-for-class"
	gatewayTLSCipherSuites     = "gateway.istio.io/tls-cipher-suites"
)

// AgwParentKey holds info about a parentRef (eg route binding to a Gateway). This is a mirror of
// gwv1.ParentReference in a form that can be stored in a map
type AgwParentKey struct {
	Kind schema.GroupVersionKind
	// Name is the original name of the resource (eg Kubernetes Gateway name)
	Name string
	// Namespace is the namespace of the resource
	Namespace string
}

func (p AgwParentKey) String() string {
	return p.Kind.String() + "/" + p.Namespace + "/" + p.Name
}

// ParentReference holds the parent key, section name and port for a parent reference.
type ParentReference struct {
	AgwParentKey

	SectionName gatewayv1.SectionName
	Port        gatewayv1.PortNumber
}

func (p ParentReference) String() string {
	return p.AgwParentKey.String() + "/" + string(p.SectionName) + "/" + fmt.Sprint(p.Port)
}

// AgwParentInfo holds info about a "Parent" - something that can be referenced as a ParentRef in the API.
// Today, this is just Gateway
type AgwParentInfo struct {
	ParentGateway types.NamespacedName
	// +krtEqualsTodo ensure gateway class changes trigger equality differences
	ParentGatewayClassName string
	// InternalName refers to the internal name we can reference it by. For example "my-ns/my-gateway"
	InternalName string
	// AllowedKinds indicates which kinds can be admitted by this Parent
	AllowedKinds []gatewayv1.RouteGroupKind
	// Hostnames is the hostnames that must be match to reference to the Parent. For gateway this is listener hostname
	// Format is ns/hostname
	Hostnames []string
	// OriginalHostname is the unprocessed form of Hostnames; how it appeared in users' config
	OriginalHostname string

	SectionName    gatewayv1.SectionName
	Port           gatewayv1.PortNumber
	Protocol       gatewayv1.ProtocolType
	TLSPassthrough bool
}

// ConfigErrorReason represents a reason for a configuration error.
type ConfigErrorReason = string

const (
	// InvalidDestination indicates an issue with the destination
	InvalidDestination ConfigErrorReason = "InvalidDestination"
	InvalidAddress     ConfigErrorReason = ConfigErrorReason(gatewayv1.GatewayReasonUnsupportedAddress)
	// InvalidDestinationPermit indicates a destination was not permitted
	InvalidDestinationPermit ConfigErrorReason = ConfigErrorReason(gatewayv1.RouteReasonRefNotPermitted)
	// InvalidDestinationKind indicates an issue with the destination kind
	InvalidDestinationKind ConfigErrorReason = ConfigErrorReason(gatewayv1.RouteReasonInvalidKind)
	// InvalidDestinationNotFound indicates a destination does not exist
	InvalidDestinationNotFound ConfigErrorReason = ConfigErrorReason(gatewayv1.RouteReasonBackendNotFound)
	// InvalidFilter indicates an issue with the filters
	InvalidFilter ConfigErrorReason = "InvalidFilter"
	// InvalidTLS indicates an issue with TLS settings
	InvalidTLS ConfigErrorReason = ConfigErrorReason(gatewayv1.ListenerReasonInvalidCertificateRef)
	// InvalidListenerRefNotPermitted indicates a listener reference was not permitted
	InvalidListenerRefNotPermitted ConfigErrorReason = ConfigErrorReason(gatewayv1.ListenerReasonRefNotPermitted)
	// InvalidConfiguration indicates a generic error for all other invalid configurations
	InvalidConfiguration ConfigErrorReason = "InvalidConfiguration"
	DeprecateFieldUsage  ConfigErrorReason = "DeprecatedField"
)

// ConfigError represents an invalid configuration that will be reported back to the user.
type ConfigError struct {
	Reason  ConfigErrorReason
	Message string
}

type condition struct {
	// reason defines the reason to report on success. Ignored if error is set
	reason string
	// message defines the message to report on success. Ignored if error is set
	message string
	// status defines the status to report on success. The inverse will be set if error is set
	// If not set, will default to StatusTrue
	status metav1.ConditionStatus
	// error defines an error state; the reason and message will be replaced with that of the error and
	// the status inverted
	error *ConfigError
	// setOnce, if enabled, will only set the condition if it is not yet present or set to this reason
	setOnce string
}

// ParentErrorReason describes why a parent reference was denied.
type ParentErrorReason string

const (
	ParentErrorNotAccepted       = ParentErrorReason(gatewayv1.RouteReasonNoMatchingParent)
	ParentErrorNotAllowed        = ParentErrorReason(gatewayv1.RouteReasonNotAllowedByListeners)
	ParentErrorNoHostname        = ParentErrorReason(gatewayv1.RouteReasonNoMatchingListenerHostname)
	ParentErrorParentRefConflict = ParentErrorReason("ParentRefConflict")
	ParentNoError                = ParentErrorReason("")
)

// ParentError represents that a parent could not be referenced
type ParentError struct {
	Reason  ParentErrorReason
	Message string
}

// normalizeReference takes a generic Group/Kind (the API uses a few variations) and converts to a known GroupVersionKind.
// Defaults for the group/kind are also passed.
func normalizeReference[G ~string, K ~string](group *G, kind *K, def config.GroupVersionKind) config.GroupVersionKind {
	k := def.Kind
	if kind != nil {
		k = string(*kind)
	}
	g := def.Group
	if group != nil {
		g = string(*group)
	}
	gk := config.GroupVersionKind{
		Group: g,
		Kind:  k,
	}
	s, f := collections.All.FindByGroupKind(gk)
	if f {
		return s.GroupVersionKind()
	}
	return gk
}
