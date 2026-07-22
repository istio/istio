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

package gateway

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pilot/pkg/model"
	destinationmodel "istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
)

type BackendEndpointKind string

const BackendEndpointExternalHostname BackendEndpointKind = "ExternalHostname"

type BackendEndpoint struct {
	Kind     BackendEndpointKind
	Hostname string
}

// BackendBinding is the route-activated, consumer-aware representation of an
// XBackend. It deliberately contains no frontend or general mesh visibility.
type BackendBinding struct {
	// Destination is the source-neutral runtime contract. The remaining fields
	// are a temporary Gateway adapter for status and API-specific TLS policy
	// compilation and can be removed as those compilers consume the core IR.
	Destination  destinationmodel.DestinationBinding
	Source       TypedNamespacedName
	SourceUID    types.UID
	Gateway      types.NamespacedName
	InternalName host.Name
	Namespace    string
	Port         model.Port
	Endpoint     BackendEndpoint
	Protocol     gatewayx.BackendProtocol
	TLS          *gatewayx.BackendTLS
	CreationTime time.Time
}

func (b BackendBinding) ResourceName() string {
	return b.Source.String() + "/" + b.Gateway.String()
}

func (b BackendBinding) Equals(other BackendBinding) bool {
	return reflect.DeepEqual(b, other)
}

// BackendBindingResult retains route-level failures for route translation and
// status. Binding is set only when the reference can participate in activation.
type BackendBindingResult struct {
	Source  TypedNamespacedName
	Gateway types.NamespacedName
	Route   TypedNamespacedName
	Edge    destinationmodel.ReferenceEdge
	Binding *BackendBinding
	Error   string
}

func (r BackendBindingResult) ResourceName() string {
	return r.Route.String() + "/" + r.Gateway.String() + "/" + r.Source.String()
}

func (r BackendBindingResult) Equals(other BackendBindingResult) bool {
	return r.Source == other.Source && r.Gateway == other.Gateway && r.Route == other.Route && r.Edge == other.Edge &&
		r.Error == other.Error && reflect.DeepEqual(r.Binding, other.Binding)
}

type BackendBindingCollections struct {
	// Results has one entry per attached Route reference, including invalid references.
	Results krt.Collection[BackendBindingResult]
	// Active has one entry per XBackend/Gateway pair with at least one valid Route reference.
	Active krt.Collection[BackendBinding]
}

type backendAttachmentKey struct {
	Route   TypedNamespacedName
	Gateway types.NamespacedName
}

func (k backendAttachmentKey) String() string {
	return k.Route.String() + "/" + k.Gateway.String()
}

// BackendBindings builds the XBackend activation graph from accepted managed
// Gateway attachments. Declared parentRefs alone never activate a binding.
func BackendBindings(
	backends krt.Collection[*gatewayx.XBackend],
	ancestors krt.Collection[AncestorBackend],
	attachments krt.Collection[RouteAttachment],
	grants gatewaycommon.ReferenceGrants,
	opts krt.OptionsBuilder,
) BackendBindingCollections {
	ancestorIndex := krt.NewIndex(ancestors, "xbackend", func(a AncestorBackend) []TypedNamespacedName {
		return []TypedNamespacedName{a.Backend}
	})
	attachmentIndex := krt.NewIndex(attachments, "route-gateway", func(a RouteAttachment) []backendAttachmentKey {
		return []backendAttachmentKey{{
			Route:   TypedNamespacedName{NamespacedName: a.From.Name, Kind: kind.FromString(a.From.Kind.Kind)},
			Gateway: a.To,
		}}
	})

	results := krt.NewManyCollection(backends, func(ctx krt.HandlerContext, backend *gatewayx.XBackend) []BackendBindingResult {
		source := TypedNamespacedName{NamespacedName: config.NamespacedName(backend), Kind: kind.XBackend}
		refs := ancestorIndex.Fetch(ctx, source)
		out := make([]BackendBindingResult, 0, len(refs))
		for _, ref := range refs {
			if len(attachmentIndex.Fetch(ctx, backendAttachmentKey{Route: ref.Source, Gateway: ref.Gateway})) == 0 {
				continue
			}
			definitionID := backendDefinitionID(backend, portIdentity(backend))
			result := BackendBindingResult{Source: source, Gateway: ref.Gateway, Route: ref.Source, Edge: destinationmodel.ReferenceEdge{
				Consumer: gatewayConsumer(ref.Gateway), Referencer: destinationConfigKey(ref.Source), Destination: definitionID,
				Port: definitionID.Port, Grant: destinationmodel.AuthorizationAllowed,
			}}
			if backend.Namespace != ref.Source.Namespace && !grants.BackendAllowed(
				ctx, routeGVK(ref.Source.Kind), gvk.XBackend, gatewayv1.ObjectName(backend.Name),
				gatewayv1.Namespace(backend.Namespace), ref.Source.Namespace,
			) {
				result.Edge.Grant = destinationmodel.AuthorizationDenied
				result.Error = fmt.Sprintf("XBackend %s is not accessible from %s (missing a ReferenceGrant)", source.NamespacedName, ref.Source)
				out = append(out, result)
				continue
			}
			binding, err := compileBackendBinding(backend, ref.Gateway)
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Binding = binding
			}
			out = append(out, result)
		}
		return out
	}, opts.WithName("XBackendBindingResults")...)

	resultIndex := krt.NewIndex(results, "xbackend-gateway", func(r BackendBindingResult) []string {
		return []string{r.Source.String() + "/" + r.Gateway.String()}
	})
	active := krt.NewCollection(resultIndex.AsCollection(opts.WithName("XBackendBindingEdges")...),
		func(_ krt.HandlerContext, edge krt.IndexObject[string, BackendBindingResult]) *BackendBinding {
			for _, result := range edge.Objects {
				if result.Binding != nil {
					return result.Binding
				}
			}
			return nil
		}, opts.WithName("ActiveXBackendBindings")...)
	return BackendBindingCollections{Results: results, Active: active}
}

func compileBackendBinding(backend *gatewayx.XBackend, gateway types.NamespacedName) (*BackendBinding, error) {
	if backend.Spec.Type != gatewayx.BackendTypeExternalHostname || backend.Spec.ExternalHostname == nil {
		return nil, fmt.Errorf("unsupported XBackend type %q", backend.Spec.Type)
	}
	portNumber := int(backend.Spec.Port.Port)
	if portNumber < 1 || portNumber > 65535 {
		return nil, fmt.Errorf("invalid XBackend port %d", portNumber)
	}
	portName := "xbackend"
	if backend.Spec.Port.Name != nil && *backend.Spec.Port.Name != "" {
		portName = *backend.Spec.Port.Name
	}
	backendProtocol, modelProtocol, err := normalizeBackendProtocol(backend.Spec.Protocol)
	if err != nil {
		return nil, err
	}
	source := TypedNamespacedName{NamespacedName: config.NamespacedName(backend), Kind: kind.XBackend}
	definitionID := backendDefinitionID(backend, portName)
	consumer := gatewayConsumer(gateway)
	endpoint := destinationmodel.EndpointSource{
		Kind: destinationmodel.DNS, Source: definitionID.Source,
		Hostname: host.Name(backend.Spec.ExternalHostname.Hostname), Port: uint32(portNumber),
	}
	port := destinationmodel.DestinationPort{Name: portName, Number: portNumber, Protocol: modelProtocol}
	connection := destinationmodel.ConnectionPolicy{Protocol: modelProtocol}
	if backend.Spec.TLS != nil {
		connection.TLS.SNI = string(backend.Spec.TLS.Validation.Hostname)
		connection.TLS.SubjectAltNames = make([]string, 0, len(backend.Spec.TLS.Validation.SubjectAltNames))
		for _, san := range backend.Spec.TLS.Validation.SubjectAltNames {
			if san.Hostname != "" {
				connection.TLS.SubjectAltNames = append(connection.TLS.SubjectAltNames, string(san.Hostname))
			} else if san.URI != "" {
				connection.TLS.SubjectAltNames = append(connection.TLS.SubjectAltNames, string(san.URI))
			}
		}
		switch backend.Spec.TLS.Mode {
		case gatewayx.BackendTLSModeNone:
			connection.TLS.Mode = destinationmodel.TLSDisabled
		case gatewayx.BackendTLSModeServerOnly:
			connection.TLS.Mode = destinationmodel.TLSSimple
		case gatewayx.BackendTLSModeClientAndServer:
			connection.TLS.Mode = destinationmodel.TLSMutual
		}
	}
	runtimeName := backendInternalName(backend.Namespace, backend.Name, backend.UID)
	destination := destinationmodel.DestinationBinding{
		Key: destinationmodel.BindingKey{Definition: definitionID, Consumer: consumer}, RuntimeName: runtimeName,
		Definition: definitionID, Consumer: consumer, Port: port, Endpoints: endpoint, Connection: connection,
		Dependencies: destinationmodel.NormalizeDependencies(definitionID.Source), Namespace: backend.Namespace,
		CreationTime: backend.CreationTimestamp.Time,
	}
	return &BackendBinding{
		Destination:  destination,
		Source:       source,
		SourceUID:    backend.UID,
		Gateway:      gateway,
		InternalName: runtimeName,
		Namespace:    backend.Namespace,
		Port:         model.Port{Name: portName, Port: portNumber, Protocol: modelProtocol},
		Endpoint: BackendEndpoint{
			Kind:     BackendEndpointExternalHostname,
			Hostname: string(backend.Spec.ExternalHostname.Hostname),
		},
		Protocol:     backendProtocol,
		TLS:          backend.Spec.TLS.DeepCopy(),
		CreationTime: backend.CreationTimestamp.Time,
	}, nil
}

func backendDefinitionID(backend *gatewayx.XBackend, port string) destinationmodel.DefinitionID {
	return destinationmodel.DefinitionID{
		Source: model.ConfigKey{Kind: kind.XBackend, Namespace: backend.Namespace, Name: backend.Name},
		UID:    string(backend.UID), Port: port,
	}
}

func portIdentity(backend *gatewayx.XBackend) string {
	if backend.Spec.Port.Name != nil && *backend.Spec.Port.Name != "" {
		return *backend.Spec.Port.Name
	}
	return "xbackend"
}

func gatewayConsumer(gateway types.NamespacedName) destinationmodel.ConsumerID {
	return destinationmodel.ConsumerID{Kind: "Gateway", Namespace: gateway.Namespace, Name: gateway.Name}
}

func destinationConfigKey(name TypedNamespacedName) model.ConfigKey {
	return model.ConfigKey{Kind: name.Kind, Namespace: name.Namespace, Name: name.Name}
}

func normalizeBackendProtocol(p *gatewayx.BackendProtocol) (gatewayx.BackendProtocol, protocol.Instance, error) {
	if p == nil {
		return "", protocol.Unsupported, nil
	}
	switch *p {
	case gatewayx.BackendProtocolTCP:
		return *p, protocol.TCP, nil
	case gatewayx.BackendProtocolHTTP, gatewayx.BackendProtocolHTTP11:
		return *p, protocol.HTTP, nil
	case gatewayx.BackendProtocolHTTP2, gatewayx.BackendProtocolH2C:
		return *p, protocol.HTTP2, nil
	case gatewayx.BackendProtocolGRPC:
		return *p, protocol.GRPC, nil
	case gatewayx.BackendProtocolMCP:
		return "", protocol.Unsupported, fmt.Errorf("unsupported XBackend protocol %q: Istio has no MCP backend primitive", *p)
	default:
		return "", protocol.Unsupported, fmt.Errorf("unsupported XBackend protocol %q", *p)
	}
}

func backendInternalName(namespace, name string, uid types.UID) host.Name {
	identity := namespace + "/" + name + "/" + string(uid)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))[:16]
	label := strings.Trim(strings.ToLower(name), "-")
	if len(label) > 40 {
		label = strings.TrimRight(label[:40], "-")
	}
	if label == "" {
		label = "backend"
	}
	return host.Name(fmt.Sprintf("%s-%s.%s.xbackend.internal", label, digest, namespace))
}

func routeGVK(k kind.Kind) config.GroupVersionKind {
	switch k {
	case kind.HTTPRoute:
		return gvk.HTTPRoute
	case kind.GRPCRoute:
		return gvk.GRPCRoute
	case kind.TCPRoute:
		return gvk.TCPRoute
	case kind.TLSRoute:
		return gvk.TLSRoute
	default:
		return config.GroupVersionKind{Kind: k.String()}
	}
}
