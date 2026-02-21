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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/agentgateway/agentgateway/go/api"
	"github.com/golang/protobuf/ptypes/duration"
	"google.golang.org/protobuf/types/known/durationpb"
	"istio.io/api/annotation"
	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	kubecreds "istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	schematypes "istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"
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

func GetStatus[I, IS any](spec I) IS {
	switch t := any(spec).(type) {
	case *gatewayalpha.TCPRoute:
		return any(t.Status).(IS)
	case *gatewayalpha.TLSRoute:
		return any(t.Status).(IS)
	case *gatewayv1.HTTPRoute:
		return any(t.Status).(IS)
	case *gatewayv1.GRPCRoute:
		return any(t.Status).(IS)
	case *gatewayv1.Gateway:
		return any(t.Status).(IS)
	case *gatewayv1.GatewayClass:
		return any(t.Status).(IS)
	case *gatewayx.XBackendTrafficPolicy:
		return any(t.Status).(IS)
	case *gatewayv1.BackendTLSPolicy:
		return any(t.Status).(IS)
	case *gatewayx.XListenerSet:
		return any(t.Status).(IS)
	case *inferencev1.InferencePool:
		return any(t.Status).(IS)
	default:
		logger.Fatalf("unknown type %T", t)
		return ptr.Empty[IS]()
	}
}

func validateTLS(certInfo *TLSInfo) *ConfigError {
	if _, err := tls.X509KeyPair(certInfo.Cert, certInfo.Key); err != nil {
		return &ConfigError{
			Reason:  InvalidTLS,
			Message: fmt.Sprintf("invalid certificate reference, the certificate is malformed: %v", err),
		}
	}
	if certInfo.CaCert != nil {
		if !x509.NewCertPool().AppendCertsFromPEM(certInfo.Cert) {
			return &ConfigError{
				Reason:  InvalidTLS,
				Message: "invalid CA certificate reference, the bundle is malformed",
			}
		}
	}
	return nil
}

// Same as buildHostnameMatch in gateway/conversion.go
// buildHostnameMatch generates a Gateway.spec.servers.hosts section from a listener
func buildHostnameMatch(ctx krt.HandlerContext, localNamespace string, namespaces krt.Collection[*corev1.Namespace], l gatewayv1.Listener) []string {
	// We may allow all hostnames or a specific one
	hostname := "*"
	if l.Hostname != nil {
		hostname = string(*l.Hostname)
	}

	resp := []string{}
	for _, ns := range namespacesFromSelector(ctx, localNamespace, namespaces, l.AllowedRoutes) {
		// This check is necessary to prevent adding a hostname with an invalid empty namespace
		if len(ns) > 0 {
			resp = append(resp, fmt.Sprintf("%s/%s", ns, hostname))
		}
	}

	// If nothing matched use ~ namespace (match nothing). We need this since its illegal to have an
	// empty hostname list, but we still need the Gateway provisioned to ensure status is properly set and
	// SNI matches are established; we just don't want to actually match any routing rules (yet).
	if len(resp) == 0 {
		return []string{"~/" + hostname}
	}
	return resp
}

// namespacesFromSelector determines a list of allowed namespaces for a given AllowedRoutes
func namespacesFromSelector(ctx krt.HandlerContext, localNamespace string, namespaceCol krt.Collection[*corev1.Namespace], lr *gatewayv1.AllowedRoutes) []string {
	// Default is to allow only the same namespace
	if lr == nil || lr.Namespaces == nil || lr.Namespaces.From == nil || *lr.Namespaces.From == gatewayv1.NamespacesFromSame {
		return []string{localNamespace}
	}
	if *lr.Namespaces.From == gatewayv1.NamespacesFromAll {
		return []string{"*"}
	}

	if lr.Namespaces.Selector == nil {
		// Should never happen, invalid config
		return []string{"*"}
	}

	// gateway-api has selectors, but Istio Gateway just has a list of names. We will run the selector
	// against all namespaces and get a list of matching namespaces that can be converted into a list
	// Istio can handle.
	ls, err := metav1.LabelSelectorAsSelector(lr.Namespaces.Selector)
	if err != nil {
		return nil
	}
	namespaces := []string{}
	namespaceObjects := krt.Fetch(ctx, namespaceCol)
	for _, ns := range namespaceObjects {
		if ls.Matches(toNamespaceSet(ns.Name, ns.Labels)) {
			namespaces = append(namespaces, ns.Name)
		}
	}
	// Ensure stable order
	sort.Strings(namespaces)
	return namespaces
}

// NamespaceNameLabel represents that label added automatically to namespaces is newer Kubernetes clusters
const NamespaceNameLabel = "kubernetes.io/metadata.name"

// toNamespaceSet converts a set of namespace labels to a Set that can be used to select against.
func toNamespaceSet(name string, labels map[string]string) klabels.Set {
	// If namespace label is not set, implicitly insert it to support older Kubernetes versions
	if labels[NamespaceNameLabel] == name {
		// Already set, avoid copies
		return labels
	}
	// First we need a copy to not modify the underlying object
	ret := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		ret[k] = v
	}
	ret[NamespaceNameLabel] = name
	return ret
}

// dummyTls is a sentinel value to send to agentgateway to signal that it should reject TLS connects due to invalid config
var dummyTls = &TLSInfo{
	Cert: []byte("invalid"),
	Key:  []byte("invalid"),
}

type SecretReference struct {
	Source types.NamespacedName
	Kind   string
	Info   TLSInfo
}

func buildSecretReference(
	ctx krt.HandlerContext,
	ref gatewayv1.SecretObjectReference,
	gw controllers.Object,
	secrets krt.Collection[*corev1.Secret],
) (*SecretReference, *ConfigError) {
	if normalizeReference(ref.Group, ref.Kind, gvk.Secret) != gvk.Secret {
		return nil, &ConfigError{Reason: InvalidTLS, Message: fmt.Sprintf("invalid certificate reference %v, only secret is allowed", secretObjectReferenceString(ref))}
	}

	secret := types.NamespacedName{
		Name:      string(ref.Name),
		Namespace: ptr.OrDefault((*string)(ref.Namespace), gw.GetNamespace()),
	}

	scrt := ptr.Flatten(krt.FetchOne(ctx, secrets, krt.FilterObjectName(secret)))
	if scrt == nil {
		return nil, &ConfigError{
			Reason:  InvalidTLS,
			Message: fmt.Sprintf("invalid certificate reference %v, secret not found", secretObjectReferenceString(ref)),
		}
	}
	certInfo, err := kubecreds.ExtractCertInfo(scrt)
	if err != nil {
		return nil, &ConfigError{
			Reason:  InvalidTLS,
			Message: fmt.Sprintf("invalid certificate reference %v, %v", secretObjectReferenceString(ref), err),
		}
	}
	res := SecretReference{
		Source: secret,
		Kind:   gvk.Secret.Kind,
		Info: TLSInfo{
			Cert: certInfo.Cert,
			Key:  certInfo.Key},
	}
	return &res, nil
}

func plainObjectReferenceString(ref gatewayv1.ObjectReference) string {
	return fmt.Sprintf("%s/%s/%s.%s", ref.Group, ref.Kind, ref.Name, ptr.OrEmpty(ref.Namespace))
}

func secretObjectReferenceString(ref gatewayv1.SecretObjectReference) string {
	return fmt.Sprintf("%s/%s/%s.%s",
		ptr.OrEmpty(ref.Group),
		ptr.OrEmpty(ref.Kind),
		ref.Name,
		ptr.OrEmpty(ref.Namespace))
}

func buildTLS(
	ctx krt.HandlerContext,
	secrets krt.Collection[*corev1.Secret],
	configMaps krt.Collection[*corev1.ConfigMap],
	grants gatewaycommon.ReferenceGrants,
	gatewayTLS *gatewayv1.TLSConfig,
	tls *gatewayv1.ListenerTLSConfig,
	gw controllers.Object,
) (*TLSInfo, *ConfigError) {
	if tls == nil {
		return nil, nil
	}
	mode := gatewayv1.TLSModeTerminate
	if tls.Mode != nil {
		mode = *tls.Mode
	}
	namespace := gw.GetNamespace()
	switch mode {
	case gatewayv1.TLSModeTerminate:
		// Important: all failures MUST include dummyTls, as this is the signal to the dataplane to actually do TLS (but fail)
		if len(tls.CertificateRefs) != 1 {
			// This is required in the API, should be rejected in validation
			return dummyTls, &ConfigError{Reason: InvalidTLS, Message: "exactly 1 certificateRefs should be present for TLS termination"}
		}
		tlsRes, err := buildSecretReference(ctx, tls.CertificateRefs[0], gw, secrets)
		if err != nil {
			return dummyTls, err
		}
		// If we are going to send a cert, validate we can access it
		sameNamespace := tlsRes.Source.Namespace == namespace
		objectKind := schematypes.GvkFromObject(gw)
		if !sameNamespace && !AgwSecretAllowed(grants, ctx, objectKind, tlsRes.Source, namespace) {
			return dummyTls, &ConfigError{
				Reason: InvalidListenerRefNotPermitted,
				Message: fmt.Sprintf(
					"certificateRef %v/%v not accessible to a Gateway in namespace %q (missing a ReferenceGrant?)",
					tls.CertificateRefs[0].Name, tlsRes.Source.Namespace, namespace,
				),
			}
		}

		if gatewayTLS != nil && gatewayTLS.Validation != nil && len(gatewayTLS.Validation.CACertificateRefs) > 0 {
			// TODO: add 'Mode'
			if len(gatewayTLS.Validation.CACertificateRefs) > 1 {
				return dummyTls, &ConfigError{
					Reason:  InvalidTLS,
					Message: "only one caCertificateRef is supported",
				}
			}
			caCertRef := gatewayTLS.Validation.CACertificateRefs[0]
			cred, err := buildCaCertificateReference(ctx, caCertRef, gw, configMaps, secrets)
			if err != nil {
				return dummyTls, err
			}
			sameNamespace := cred.Source.Namespace == namespace
			isSecret := cred.Kind == gvk.Secret.Kind
			if isSecret && !sameNamespace && !AgwSecretAllowed(grants, ctx, schematypes.GvkFromObject(gw), cred.Source, namespace) {
				return dummyTls, &ConfigError{
					Reason: InvalidListenerRefNotPermitted,
					Message: fmt.Sprintf(
						"caCertificateRef %v/%v not accessible to a Gateway in namespace %q (missing a ReferenceGrant?)",
						cred.Source.Namespace, caCertRef.Name, namespace,
					),
				}
			}
			tlsRes.Info.CaCert = cred.Info.CaCert
		}
		return &tlsRes.Info, nil
	case gatewayv1.TLSModePassthrough:
		// Handled outside of this function. This only handles termination
		return nil, nil
	}
	return nil, nil
}

func buildCaCertificateReference(
	ctx krt.HandlerContext,
	ref gatewayv1.ObjectReference,
	gw controllers.Object,
	configMaps krt.Collection[*corev1.ConfigMap],
	secrets krt.Collection[*corev1.Secret],
) (*SecretReference, *ConfigError) {
	namespace := ptr.OrDefault((*string)(ref.Namespace), gw.GetNamespace())
	name := string(ref.Name)
	res := SecretReference{
		Source: types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
		Info: TLSInfo{},
	}

	switch normalizeReference(&ref.Group, &ref.Kind, config.GroupVersionKind{}) {
	case gvk.ConfigMap:
		res.Kind = gvk.ConfigMap.Kind
		cm := ptr.Flatten(krt.FetchOne(ctx, configMaps, krt.FilterObjectName(res.Source)))
		if cm == nil {
			return nil, &ConfigError{
				Reason:  InvalidTLS,
				Message: fmt.Sprintf("invalid CA certificate reference, configmap %v not found", res.Source),
			}
		}
		certInfo, err := kubecreds.ExtractRootFromString(cm.Data)
		if err != nil {
			return nil, &ConfigError{
				Reason:  InvalidTLS,
				Message: fmt.Sprintf("invalid CA certificate reference %v, %v", plainObjectReferenceString(ref), err),
			}
		}
		res.Info.CaCert = certInfo.Cert
	case gvk.Secret:
		res.Kind = gvk.Secret.Kind
		scrt := ptr.Flatten(krt.FetchOne(ctx, secrets, krt.FilterObjectName(res.Source)))
		if scrt == nil {
			return nil, &ConfigError{
				Reason:  InvalidTLS,
				Message: fmt.Sprintf("invalid CA certificate reference, secret %v not found", res.Source),
			}
		}
		certInfo, err := kubecreds.ExtractRoot(scrt.Data)
		if err != nil {
			return nil, &ConfigError{
				Reason:  InvalidTLS,
				Message: fmt.Sprintf("invalid CA certificate reference %v, %v", plainObjectReferenceString(ref), err),
			}
		}
		res.Info.CaCert = certInfo.Cert
	default:
		return nil, &ConfigError{
			Reason:  InvalidTLS,
			Message: fmt.Sprintf("invalid CA certificate reference %v, only secret and configmap are allowed", plainObjectReferenceString(ref)),
		}
	}

	return &res, nil
}

// buildListener constructs the listener components of a listener in a Gateway spec
func buildListener(
	ctx krt.HandlerContext,
	secrets krt.Collection[*corev1.Secret],
	configMaps krt.Collection[*corev1.ConfigMap],
	grants gatewaycommon.ReferenceGrants,
	namespaces krt.Collection[*corev1.Namespace],
	obj controllers.Object,
	status []gatewayv1.ListenerStatus,
	gw gatewayv1.GatewaySpec,
	l gatewayv1.Listener,
	listenerIndex int,
	controllerName gatewayv1.GatewayController,
	portErr error,
) (*istio.Server, *TLSInfo, []gatewayv1.ListenerStatus, bool) {
	listenerConditions := map[string]*condition{
		string(gatewayv1.ListenerConditionAccepted): {
			reason:  string(gatewayv1.ListenerReasonAccepted),
			message: "No errors found",
		},
		string(gatewayv1.ListenerConditionProgrammed): {
			reason:  string(gatewayv1.ListenerReasonProgrammed),
			message: "No errors found",
		},
		string(gatewayv1.ListenerConditionConflicted): {
			reason:  string(gatewayv1.ListenerReasonNoConflicts),
			message: "No errors found",
			status:  kstatus.StatusFalse,
		},
		string(gatewayv1.ListenerConditionResolvedRefs): {
			reason:  string(gatewayv1.ListenerReasonResolvedRefs),
			message: "No errors found",
		},
	}

	ok := true
	gwTls := resolveGatewayTLS(l.Port, gw.TLS)
	tlsInfo, err := buildTLS(ctx, secrets, configMaps, grants, gwTls, l.TLS, obj)
	if err == nil && tlsInfo != nil {
		// If there were no other errors, also check the Key/Cert are actually valid
		err = validateTLS(tlsInfo)
	}
	if err != nil {
		listenerConditions[string(gatewayv1.ListenerConditionResolvedRefs)].error = err
		listenerConditions[string(gatewayv1.GatewayConditionProgrammed)].error = &ConfigError{
			Reason:  string(gatewayv1.GatewayReasonInvalid),
			Message: "Bad TLS configuration",
		}
		ok = false
	}
	if portErr != nil {
		listenerConditions[string(gatewayv1.ListenerConditionAccepted)].error = &ConfigError{
			Reason:  string(gatewayv1.ListenerReasonUnsupportedProtocol),
			Message: portErr.Error(),
		}
		ok = false
	}

	hostnames := buildHostnameMatch(ctx, obj.GetNamespace(), namespaces, l)
	_, perr := listenerProtocolToIstio(controllerName, l.Protocol)
	if perr != nil {
		listenerConditions[string(gatewayv1.ListenerConditionAccepted)].error = &ConfigError{
			Reason:  string(gatewayv1.ListenerReasonUnsupportedProtocol),
			Message: perr.Error(),
		}
		ok = false
	}

	// TODO(jaellio): Consider not using istio server type
	server := &istio.Server{
		Port: &istio.Port{
			// Name is required. We only have one server per Gateway, so we can just name them all the same
			Name:   "default",
			Number: uint32(l.Port),
		},
		Hosts: hostnames,
	}

	updatedStatus := reportListenerCondition(listenerIndex, l, obj, status, listenerConditions)
	return server, tlsInfo, updatedStatus, ok
}

var supportedProtocols = sets.New(
	gatewayv1.HTTPProtocolType,
	gatewayv1.HTTPSProtocolType,
	gatewayv1.TLSProtocolType,
	gatewayv1.TCPProtocolType,
	gatewayv1.ProtocolType(protocol.HBONE))

func listenerProtocolToIstio(name gatewayv1.GatewayController, p gatewayv1.ProtocolType) (string, error) {
	switch p {
	// Standard protocol types
	case gatewayv1.HTTPProtocolType:
		return string(p), nil
	case gatewayv1.HTTPSProtocolType:
		return string(p), nil
	case gatewayv1.TLSProtocolType, gatewayv1.TCPProtocolType:
		if !features.EnableAlphaGatewayAPI {
			return "", fmt.Errorf("protocol %q is supported, but only when %v=true is configured", p, features.EnableAlphaGatewayAPIName)
		}
		return string(p), nil
	// Our own custom types
	case gatewayv1.ProtocolType(protocol.HBONE):
		if name != constants.ManagedGatewayMeshController && name != constants.ManagedGatewayEastWestController {
			return "", fmt.Errorf("protocol %q is only supported for waypoint proxies", p)
		}
		return string(p), nil
	}
	up := gatewayv1.ProtocolType(strings.ToUpper(string(p)))
	if supportedProtocols.Contains(up) {
		return "", fmt.Errorf("protocol %q is unsupported. hint: %q (uppercase) may be supported", p, up)
	}
	// Note: the gatewayv1.UDPProtocolType is explicitly left to hit this path
	return "", fmt.Errorf("protocol %q is unsupported", p)
}

// resolveGatewayTLS finds the TLS config for a given port from the GatewayTLSConfig for frontend TLS
func resolveGatewayTLS(port gatewayv1.PortNumber, gw *gatewayv1.GatewayTLSConfig) *gatewayv1.TLSConfig {
	if gw == nil || gw.Frontend == nil {
		return nil
	}
	f := gw.Frontend
	pp := slices.FindFunc(f.PerPort, func(portConfig gatewayv1.TLSPortConfig) bool {
		return portConfig.Port == port
	})
	if pp != nil {
		return &pp.TLS
	}
	return &f.Default
}

func extractGatewayServices(domainSuffix string, kgw *gatewayv1.Gateway, info gatewaycommon.ClassInfo) ([]string, *condition) {
	if gatewaycommon.IsManaged(&kgw.Spec) {
		name := model.GetOrDefault(kgw.Annotations[annotation.GatewayNameOverride.Name], gatewaycommon.GetDefaultName(kgw.Name, &kgw.Spec, info.DisableNameSuffix))
		return []string{fmt.Sprintf("%s.%s.svc.%v", name, kgw.Namespace, domainSuffix)}, nil
	}
	gatewayServices := []string{}
	skippedAddresses := []string{}
	for _, addr := range kgw.Spec.Addresses {
		if addr.Type != nil && *addr.Type != gatewayv1.HostnameAddressType {
			// We only support HostnameAddressType. Keep track of invalid ones so we can report in status.
			skippedAddresses = append(skippedAddresses, addr.Value)
			continue
		}
		// TODO: For now we are using Addresses. There has been some discussion of allowing inline
		// parameters on the class field like a URL, in which case we will probably just use that. See
		// https://github.com/kubernetes-sigs/gateway-api/pull/614
		fqdn := addr.Value
		if !strings.Contains(fqdn, ".") {
			// Short name, expand it
			fqdn = fmt.Sprintf("%s.%s.svc.%s", fqdn, kgw.Namespace, domainSuffix)
		}
		gatewayServices = append(gatewayServices, fqdn)
	}
	if len(skippedAddresses) > 0 {
		// Give error but return services, this is a soft failure
		return gatewayServices, &condition{
			status: metav1.ConditionFalse,
			error: &ConfigError{
				Reason:  InvalidAddress,
				Message: fmt.Sprintf("only Hostname is supported, ignoring %v", skippedAddresses),
			},
		}
	}
	if _, f := kgw.Annotations[annotation.NetworkingServiceType.Name]; f {
		// Give error but return services, this is a soft failure
		// Remove entirely in 1.20
		return gatewayServices, &condition{
			status: metav1.ConditionFalse,
			error: &ConfigError{
				Reason:  DeprecateFieldUsage,
				Message: fmt.Sprintf("annotation %v is deprecated, use Spec.Infrastructure.Routeability", annotation.NetworkingServiceType.Name),
			},
		}
	}
	return gatewayServices, nil
}

func reportGatewayStatus(
	r *gatewaycommon.GatewayContext,
	obj *gatewayv1.Gateway,
	gs *gatewayv1.GatewayStatus,
	classInfo gatewaycommon.ClassInfo,
	gatewayServices []string,
	servers []*istio.Server,
	listenerSetCount int,
	gatewayErr *ConfigError,
) {
	// TODO: we lose address if servers is empty due to an error
	internal, internalIP, external, pending, warnings, allUsable := r.ResolveGatewayInstances(obj.Namespace, gatewayServices, servers)

	// Setup initial conditions to the success state. If we encounter errors, we will update this.
	// We have two status
	// Accepted: is the configuration valid. We only have errors in listeners, and the status is not supposed to
	// be tied to listeners, so this is always accepted
	// Programmed: is the data plane "ready" (note: eventually consistent)
	gatewayConditions := map[string]*condition{
		string(gatewayv1.GatewayConditionAccepted): {
			reason:  string(gatewayv1.GatewayReasonAccepted),
			message: "Resource accepted",
		},
		string(gatewayv1.GatewayConditionProgrammed): {
			reason:  string(gatewayv1.GatewayReasonProgrammed),
			message: "Resource programmed",
		},
	}
	if gatewayErr != nil {
		gatewayConditions[string(gatewayv1.GatewayConditionAccepted)].error = gatewayErr
	}

	// Not defined in upstream API
	const AttachedListenerSets = "AttachedListenerSets"
	if obj.Spec.AllowedListeners != nil {
		gatewayConditions[AttachedListenerSets] = &condition{
			reason:  "ListenersAttached",
			message: "At least one ListenerSet is attached",
		}
		if !features.EnableAlphaGatewayAPI {
			gatewayConditions[AttachedListenerSets].error = &ConfigError{
				Reason: "Unsupported",
				Message: fmt.Sprintf("AllowedListeners is configured, but ListenerSets are not enabled (set %v=true)",
					features.EnableAlphaGatewayAPIName),
			}
		} else if listenerSetCount == 0 {
			gatewayConditions[AttachedListenerSets].error = &ConfigError{
				Reason:  "NoListenersAttached",
				Message: "AllowedListeners is configured, but no ListenerSets are attached",
			}
		}
	}

	setProgrammedCondition(gatewayConditions, internal, gatewayServices, warnings, allUsable)

	addressesToReport := external
	if len(addressesToReport) == 0 {
		wantAddressType := classInfo.AddressType
		if override, ok := obj.Annotations[addressTypeOverride]; ok {
			wantAddressType = gatewayv1.AddressType(override)
		}
		// There are no external addresses, so report the internal ones
		// This can be IP, Hostname, or both (indicated by empty wantAddressType)
		if wantAddressType != gatewayv1.HostnameAddressType {
			addressesToReport = internalIP
		}
		if wantAddressType != gatewayv1.IPAddressType {
			for _, hostport := range internal {
				svchost, _, _ := net.SplitHostPort(hostport)
				if !slices.Contains(pending, svchost) && !slices.Contains(addressesToReport, svchost) {
					addressesToReport = append(addressesToReport, svchost)
				}
			}
		}
	}
	// Do not report an address until we are ready. But once we are ready, never remove the address.
	if len(addressesToReport) > 0 {
		gs.Addresses = make([]gatewayv1.GatewayStatusAddress, 0, len(addressesToReport))
		for _, addr := range addressesToReport {
			var addrType gatewayv1.AddressType
			if _, err := netip.ParseAddr(addr); err == nil {
				addrType = gatewayv1.IPAddressType
			} else {
				addrType = gatewayv1.HostnameAddressType
			}
			gs.Addresses = append(gs.Addresses, gatewayv1.GatewayStatusAddress{
				Value: addr,
				Type:  &addrType,
			})
		}
	}
	// Prune listeners that have been removed
	haveListeners := getListenerNames(&obj.Spec)
	listeners := make([]gatewayv1.ListenerStatus, 0, len(gs.Listeners))
	for _, l := range gs.Listeners {
		if haveListeners.Contains(l.Name) {
			haveListeners.Delete(l.Name)
			listeners = append(listeners, l)
		}
	}
	gs.Listeners = listeners
	gs.Conditions = setConditions(obj.Generation, gs.Conditions, gatewayConditions)
}

func setProgrammedCondition(gatewayConditions map[string]*condition, internal []string, gatewayServices []string, warnings []string, allUsable bool) {
	if len(internal) > 0 {
		msg := fmt.Sprintf("Resource programmed, assigned to service(s) %s", humanReadableJoin(internal))
		gatewayConditions[string(gatewayv1.GatewayConditionProgrammed)].message = msg
	}

	if len(gatewayServices) == 0 {
		gatewayConditions[string(gatewayv1.GatewayConditionProgrammed)].error = &ConfigError{
			Reason:  InvalidAddress,
			Message: "Failed to assign to any requested addresses",
		}
	} else if len(warnings) > 0 {
		var msg string
		var reason string
		if len(internal) != 0 {
			msg = fmt.Sprintf("Assigned to service(s) %s, but failed to assign to all requested addresses: %s",
				humanReadableJoin(internal), strings.Join(warnings, "; "))
		} else {
			msg = fmt.Sprintf("Failed to assign to any requested addresses: %s", strings.Join(warnings, "; "))
		}
		if allUsable {
			reason = string(gatewayv1.GatewayReasonAddressNotAssigned)
		} else {
			reason = string(gatewayv1.GatewayReasonAddressNotUsable)
		}
		gatewayConditions[string(gatewayv1.GatewayConditionProgrammed)].error = &ConfigError{
			// TODO: this only checks Service ready, we should also check Deployment ready?
			Reason:  reason,
			Message: msg,
		}
	}
}

func humanReadableJoin(ss []string) string {
	switch len(ss) {
	case 0:
		return ""
	case 1:
		return ss[0]
	case 2:
		return ss[0] + " and " + ss[1]
	default:
		return strings.Join(ss[:len(ss)-1], ", ") + ", and " + ss[len(ss)-1]
	}
}

func getListenerNames(spec *gatewayv1.GatewaySpec) sets.Set[gatewayv1.SectionName] {
	res := sets.New[gatewayv1.SectionName]()
	for _, l := range spec.Listeners {
		res.Insert(l.Name)
	}
	return res
}

func toRouteKind(g config.GroupVersionKind) gatewayv1.RouteGroupKind {
	return gatewayv1.RouteGroupKind{Group: (*gatewayv1.Group)(&g.Group), Kind: gatewayv1.Kind(g.Kind)}
}

// createAgwExtensionRefFilter creates Agw filter from Gateway API ExtensionRef filter
func createAgwExtensionRefFilter(
	extensionRef *gatewayv1.LocalObjectReference,
) *condition {
	if extensionRef == nil {
		return nil
	}

	// TODO: support other types of extension refs (TrafficPolicySpec, etc.) https://github.com/kgateway-dev/kgateway/issues/12037

	// Unsupported ExtensionRef
	return &condition{
		status: metav1.ConditionFalse,
		error: &ConfigError{
			Reason:  ConfigErrorReason(gatewayv1.RouteReasonIncompatibleFilters),
			Message: fmt.Sprintf("unsupported ExtensionRef: %s/%s", extensionRef.Group, extensionRef.Kind),
		},
	}
}

func routeGroupKindEqual(rgk1, rgk2 gatewayv1.RouteGroupKind) bool {
	return rgk1.Kind == rgk2.Kind && getGroup(rgk1) == getGroup(rgk2)
}

func getGroup(rgk gatewayv1.RouteGroupKind) gatewayv1.Group {
	return ptr.OrDefault(rgk.Group, gatewayv1.GroupName)
}

// RouteParentReference holds information about a route's parent reference
type RouteParentReference struct {
	// InternalName refers to the internal name of the parent we can reference it by. For example "my-ns/my-gateway"
	InternalName string
	// InternalKind is the Group/Kind of the Parent
	InternalKind schema.GroupVersionKind
	// DeniedReason, if present, indicates why the reference was not valid
	DeniedReason *ParentError
	// OriginalReference contains the original reference
	OriginalReference gatewayv1.ParentReference
	// Hostname is the hostname match of the Parent, if any
	Hostname        string
	BannedHostnames sets.Set[string]
	ParentKey       AgwParentKey
	ParentSection   gatewayv1.SectionName
	Accepted        bool
	ParentGateway   types.NamespacedName
}

func RouteName[T ~string](kind string, namespace, name string, routeRule *T) *api.RouteName {
	var ls *string
	if routeRule != nil {
		ls = ptr.Of((string)(*routeRule))
	}
	return &api.RouteName{
		Name:      name,
		Namespace: namespace,
		RuleName:  ls,
		Kind:      kind,
	}
}

// InternalRouteRuleKey returns the name of the internal Route Rule corresponding to the
// specified route. If ruleName is not specified, returns the internal name without the route rule.
// Format: routeNs/routeName.ruleName
func InternalRouteRuleKey(routeNamespace, routeName, ruleName string) string {
	if ruleName == "" {
		return fmt.Sprintf("%s/%s", routeNamespace, routeName)
	}
	return fmt.Sprintf("%s/%s.%s", routeNamespace, routeName, ruleName)
}

func isPolicyErrorCritical(filterError *condition) bool {
	criticalReasons := []gatewayv1.RouteConditionReason{
		"FilterNotSupported",
		"FilterConfigInvalid",
		// Add other critical filter error reasons as needed
	}

	return slices.Contains(criticalReasons, gatewayv1.RouteConditionReason(filterError.reason))
}

// ConvertTCPRouteToAgw converts a TCPRouteRule to an agentgateway TCPRoute
func ConvertTCPRouteToAgw(ctx RouteContext, r gatewayalpha.TCPRouteRule,
	obj *gatewayalpha.TCPRoute, pos int,
) (*api.TCPRoute, *condition) {
	routeRuleKey := strconv.Itoa(pos)
	res := &api.TCPRoute{
		// unique for route rule
		Key:         InternalRouteRuleKey(obj.Namespace, obj.Name, routeRuleKey),
		Name:        RouteName(gvk.TCPRoute.Kind, obj.Namespace, obj.Name, r.Name),
		ListenerKey: "",
	}

	// Build TCP destinations
	route, backendErr, err := buildAgwTCPDestination(ctx, r.BackendRefs, obj.Namespace)
	if err != nil {
		logger.Errorf("failed to translate tcp destination", "err", err)
		return nil, err
	}
	res.Backends = route

	return res, backendErr
}

// ConvertGRPCRouteToAgw converts a GRPCRouteRule to an agentgateway HTTPRoute
func ConvertGRPCRouteToAgw(ctx RouteContext, r gatewayv1.GRPCRouteRule,
	obj *gatewayv1.GRPCRoute, pos int,
) (*api.Route, *condition) {
	routeRuleKey := strconv.Itoa(pos)
	res := &api.Route{
		// unique for route rule
		Key:         InternalRouteRuleKey(obj.Namespace, obj.Name, routeRuleKey),
		Name:        RouteName(gvk.GRPCRoute.Kind, obj.Namespace, obj.Name, r.Name),
		ListenerKey: "",
	}

	// Convert GRPC matches to Agw format
	for _, match := range r.Matches {
		headers, err := CreateAgwGRPCHeadersMatch(match)
		if err != nil {
			logger.Errorf("failed to translate grpc header match", "err", err, "route_name", obj.Name, "route_ns", obj.Namespace)
			return nil, err
		}
		// For GRPC, we don't have path match in the traditional sense, so we'll derive it from method
		var path *api.PathMatch
		if match.Method != nil {
			// Convert GRPC method to path for routing purposes
			if match.Method.Service != nil && match.Method.Method != nil {
				pathStr := fmt.Sprintf("/%s/%s", *match.Method.Service, *match.Method.Method)
				path = &api.PathMatch{Kind: &api.PathMatch_Exact{Exact: pathStr}}
			} else if match.Method.Service != nil {
				pathStr := fmt.Sprintf("/%s/", *match.Method.Service)
				path = &api.PathMatch{Kind: &api.PathMatch_Exact{Exact: pathStr}}
			} else if match.Method.Method != nil {
				// Convert wildcard to regex: "/*/{method}" becomes "/[^/]+/{method}"
				pathStr := fmt.Sprintf("/[^/]+/%s", *match.Method.Method)
				path = &api.PathMatch{Kind: &api.PathMatch_Regex{Regex: pathStr}}
			}
		}
		res.Matches = append(res.GetMatches(), &api.RouteMatch{
			Path:    path,
			Headers: headers,
			// note: the RouteMatch method field only applies for http methods
		})
	}
	if len(res.Matches) == 0 {
		// HTTPRoute defaults in the CRD itself, but GRPCRoute does not.
		// Agentgateway expects there to always be a match set.
		res.Matches = []*api.RouteMatch{{
			Path: &api.PathMatch{Kind: &api.PathMatch_PathPrefix{PathPrefix: "/"}},
		}}
	}

	policies, err := BuildAgwGRPCTrafficPolicies(ctx, obj.Namespace, r.Filters)
	if err != nil {
		logger.Errorf("failed to translate grpc filter", "err", err, "route_name", obj.Name, "route_ns", obj.Namespace)
		return nil, err
	}
	res.TrafficPolicies = policies

	route, backendErr, err := buildAgwGRPCDestination(ctx, r.BackendRefs, obj.Namespace)
	if err != nil {
		logger.Errorf("failed to translate grpc destination", "err", err, "route_name", obj.Name, "route_ns", obj.Namespace)
		return nil, err
	}
	res.Backends = route
	res.Hostnames = slices.Map(obj.Spec.Hostnames, func(e gatewayv1.Hostname) string {
		return string(e)
	})
	return res, backendErr
}

// ConvertTLSRouteToAgw converts a TLSRouteRule to an agentgateway TCPRoute
func ConvertTLSRouteToAgw(ctx RouteContext, r gatewayalpha.TLSRouteRule,
	obj *gatewayalpha.TLSRoute, pos int,
) (*api.TCPRoute, *condition) {
	routeRuleKey := strconv.Itoa(pos)
	res := &api.TCPRoute{
		// unique for route rule
		Key:         InternalRouteRuleKey(obj.Namespace, obj.Name, routeRuleKey),
		Name:        RouteName(gvk.TLSRoute.Kind, obj.Namespace, obj.Name, r.Name),
		ListenerKey: "",
	}

	// Build TLS destinations
	route, backendErr, err := buildAgwTLSDestination(ctx, r.BackendRefs, obj.Namespace)
	if err != nil {
		logger.Errorf("failed to translate tls destination", "err", err, "route_name", obj.Name, "route_ns", obj.Namespace)
		return nil, err
	}
	res.Backends = route

	// TLS Routes have hostnames in the spec (unlike TCP Routes)
	res.Hostnames = slices.Map(obj.Spec.Hostnames, func(e gatewayv1.Hostname) string {
		return string(e)
	})

	return res, backendErr
}

func buildAgwTCPDestination(
	ctx RouteContext,
	forwardTo []gatewayv1.BackendRef,
	ns string,
) ([]*api.RouteBackend, *condition, *condition) {
	if forwardTo == nil {
		return nil, nil, nil
	}

	var invalidBackendErr *condition
	var res []*api.RouteBackend
	for _, fwd := range forwardTo {
		dst, err := buildAgwDestination(ctx, gatewayv1.HTTPBackendRef{
			BackendRef: fwd,
			Filters:    nil, // TCP Routes don't have per-backend filters?
		}, ns, gvk.TCPRoute)
		if err != nil {
			logger.Errorf("error building agent gateway destination", "error", err)
			if isInvalidBackend(err) {
				invalidBackendErr = err
				// keep going, we will gracefully drop invalid backends
			} else {
				return nil, nil, err
			}
		}
		res = append(res, dst)
	}
	return res, invalidBackendErr, nil
}

// first condition return value is for invalid backends, the second is for other errors
func buildAgwTLSDestination(
	ctx RouteContext,
	forwardTo []gatewayv1.BackendRef,
	ns string,
) ([]*api.RouteBackend, *condition, *condition) {
	if forwardTo == nil {
		return nil, nil, nil
	}
	var invalidBackendErr *condition
	var res []*api.RouteBackend
	for _, fwd := range forwardTo {
		dst, err := buildAgwDestination(ctx, gatewayv1.HTTPBackendRef{
			BackendRef: fwd,
			Filters:    nil, // TLS Routes don't have per-backend filters
		}, ns, gvk.TLSRoute)
		if err != nil {
			logger.Errorf("error building agent gateway destination", "error", err)
			if isInvalidBackend(err) {
				invalidBackendErr = err
				// keep going, we will gracefully drop invalid backends
			} else {
				return nil, nil, err
			}
		}
		res = append(res, dst)
	}
	return res, invalidBackendErr, nil
}

func buildAgwDestination(
	ctx RouteContext,
	to gatewayv1.HTTPBackendRef,
	ns string,
	k config.GroupVersionKind,
) (*api.RouteBackend, *condition) {
	ref := normalizeReference(to.Group, to.Kind, gvk.Service)
	// check if the reference is allowed
	if toNs := to.Namespace; toNs != nil && string(*toNs) != ns {
		if !ctx.Grants.BackendAllowed(ctx.Krt, k, ref, to.Name, *toNs, ns) {
			msg := fmt.Sprintf("backendRef %v/%v not accessible to a %s in namespace %q (missing a ReferenceGrant?)", to.Name, *toNs, k.Kind, ns)
			logger.Debug(msg)
			return nil, &condition{
				status: metav1.ConditionFalse,
				error: &ConfigError{
					Reason:  ConfigErrorReason(gatewayv1.RouteReasonRefNotPermitted),
					Message: msg,
				},
			}
		}
	}

	namespace := ns // use default
	if to.Namespace != nil {
		namespace = string(*to.Namespace)
	}
	var invalidBackendErr *condition
	var hostname string
	weight := int32(1) // default
	if to.Weight != nil {
		weight = *to.Weight
	}
	rb := &api.RouteBackend{
		Weight: weight,
	}
	var port *gatewayv1.PortNumber

	switch ref.Kind {
	case gvk.InferencePool.Kind:
		if strings.Contains(string(to.Name), ".") {
			msg := fmt.Sprintf("service name invalid; the name of the InferencePool must be used, not the hostname. Got %q", to.Name)
			logger.Debug(msg)
			return nil, &condition{
				status: metav1.ConditionFalse,
				error: &ConfigError{
					Reason:  ConfigErrorReason(gatewayv1.RouteReasonUnsupportedValue),
					Message: msg,
				},
			}
		}
		hostname = GetInferenceServiceHostname(ctx, string(to.Name), namespace)
		key := namespace + "/" + string(to.Name)
		svc := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.InferencePools, krt.FilterKey(key)))
		logger.Debugf("found pull pool for service", "svc", svc, "key", key)
		if svc == nil {
			msg := fmt.Sprintf("backendRef %s not found", key)
			logger.Debug(msg)
			invalidBackendErr = &condition{
				status: metav1.ConditionFalse,
				error: &ConfigError{
					Reason:  ConfigErrorReason(gatewayv1.RouteReasonBackendNotFound),
					Message: fmt.Sprintf("backendRef %s  not found", key),
				},
			}
		} else {
			rb.Backend = &api.BackendReference{
				Kind: &api.BackendReference_Service_{
					Service: &api.BackendReference_Service{
						Hostname:  hostname,
						Namespace: namespace,
					},
				},
				// InferencePool only supports single port
				Port: uint32(svc.Spec.TargetPorts[0].Number), //nolint:gosec // G115: InferencePool TargetPort is int32 with validation 1-65535, always safe
			}
		}
	case "Hostname":
		// Hostname is an Istio-specific backend kind where the name is the literal hostname
		// Used for referencing services by their full hostname (e.g., from ServiceEntry)
		// The actual resolution to ServiceEntry happens via the BackendIndex alias mechanism
		port = to.Port
		if port == nil {
			msg := fmt.Sprintf("port is required in backendRef for Hostname kind when referencing %q", to.Name)
			logger.Debug(msg)
			return nil, &condition{
				status: metav1.ConditionFalse,
				error: &ConfigError{
					Reason:  ConfigErrorReason(gatewayv1.RouteReasonUnsupportedValue),
					Message: "port is required in backendRef for Hostname kind",
				},
			}
		}
		if to.Namespace != nil {
			msg := fmt.Sprintf("namespace may not be set with Hostname type when referencing %q", to.Name)
			logger.Debug(msg)
			return nil, &condition{
				status: metav1.ConditionFalse,
				error: &ConfigError{
					Reason:  ConfigErrorReason(gatewayv1.RouteReasonUnsupportedValue),
					Message: "namespace may not be set with Hostname type",
				},
			}
		}
		// Use the name directly as the hostname
		hostname = string(to.Name)
		// Note: Backend validation happens via BackendIndex which uses the Hostname->ServiceEntry alias
		// No need to explicitly check ServiceEntries here as the BackendIndex handles the resolution
		rb.Backend = &api.BackendReference{
			Kind: &api.BackendReference_Service_{
				Service: &api.BackendReference_Service{
					Hostname:  hostname,
					Namespace: namespace,
				},
			},
			Port: uint32(*port), //nolint:gosec // G115: Gateway API PortNumber is int32 with validation 1-65535, always safe
		}
	case gvk.Service.Kind:
		port = to.Port
		if strings.Contains(string(to.Name), ".") {
			msg := fmt.Sprintf("service name invalid; the name of the Service must be used, not the hostname. Got %q", to.Name)
			logger.Debug(msg)
			return nil, &condition{
				status: metav1.ConditionFalse,
				error: &ConfigError{
					Reason:  ConfigErrorReason(gatewayv1.RouteReasonUnsupportedValue),
					Message: msg,
				},
			}
		}
		hostname = GetServiceHostname(ctx, string(to.Name), namespace)
		key := namespace + "/" + string(to.Name)
		svc := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.Services, krt.FilterKey(key)))
		if svc == nil {
			msg := fmt.Sprintf("backend(%s) not found", hostname)
			logger.Debug(msg)
			invalidBackendErr = &condition{
				status: metav1.ConditionFalse,
				error: &ConfigError{
					Reason:  ConfigErrorReason(gatewayv1.RouteReasonBackendNotFound),
					Message: msg,
				},
			}
		}
		// TODO: All kubernetes service types currently require a Port, so we do this for everything; consider making this per-type if we have future types
		// that do not require port.
		if port == nil {
			// "Port is required when the referent is a Kubernetes Service."
			msg := fmt.Sprintf("port is required in backendRef when referencing service %q", hostname)
			logger.Debug(msg)
			return nil, &condition{
				status: metav1.ConditionFalse,
				error: &ConfigError{
					Reason:  ConfigErrorReason(gatewayv1.RouteReasonUnsupportedValue),
					Message: "port is required in backendRef",
				},
			}
		}
		rb.Backend = &api.BackendReference{
			Kind: &api.BackendReference_Service_{
				Service: &api.BackendReference_Service{
					Hostname:  hostname,
					Namespace: namespace,
				},
			},
			Port: uint32(*port), //nolint:gosec // G115: Gateway API PortNumber is int32 with validation 1-65535, always safe
		}
	default:
		msg := fmt.Sprintf("unsupported backendRef kind %q with group %q", ptr.OrEmpty(to.Group), ptr.OrEmpty(to.Kind))
		logger.Debug(msg)
		return nil, &condition{
			status: metav1.ConditionFalse,
			error: &ConfigError{
				Reason:  ConfigErrorReason(gatewayv1.RouteReasonInvalidKind),
				Message: fmt.Sprintf("referencing unsupported backendRef: group %q kind %q", ptr.OrEmpty(to.Group), ptr.OrEmpty(to.Kind)),
			},
		}
	}
	return rb, invalidBackendErr
}

// reportUnsupportedListenerSet reports a status message for a ListenerSet that is not supported
func reportUnsupportedListenerSet(class string, status *gatewayx.ListenerSetStatus, obj *gatewayx.XListenerSet) {
	gatewayConditions := map[string]*condition{
		string(gatewayv1.GatewayConditionAccepted): {
			reason: string(gatewayv1.GatewayReasonAccepted),
			error: &ConfigError{
				Reason:  string(gatewayx.ListenerSetReasonNotAllowed),
				Message: fmt.Sprintf("The %q GatewayClass does not support ListenerSet", class),
			},
		},
		string(gatewayv1.GatewayConditionProgrammed): {
			reason: string(gatewayv1.GatewayReasonProgrammed),
			error: &ConfigError{
				Reason:  string(gatewayx.ListenerSetReasonNotAllowed),
				Message: fmt.Sprintf("The %q GatewayClass does not support ListenerSet", class),
			},
		},
	}
	status.Listeners = nil
	status.Conditions = setConditions(obj.Generation, status.Conditions, gatewayConditions)
}

// reportNotAllowedListenerSet reports a status message for a ListenerSet that is not allowed to be selected
func reportNotAllowedListenerSet(status *gatewayx.ListenerSetStatus, obj *gatewayx.XListenerSet) {
	gatewayConditions := map[string]*condition{
		string(gatewayv1.GatewayConditionAccepted): {
			reason: string(gatewayv1.GatewayReasonAccepted),
			error: &ConfigError{
				Reason:  string(gatewayx.ListenerSetReasonNotAllowed),
				Message: "The parent Gateway does not allow this reference; check the 'spec.allowedRoutes'",
			},
		},
		string(gatewayv1.GatewayConditionProgrammed): {
			reason: string(gatewayv1.GatewayReasonProgrammed),
			error: &ConfigError{
				Reason:  string(gatewayx.ListenerSetReasonNotAllowed),
				Message: "The parent Gateway does not allow this reference; check the 'spec.allowedRoutes'",
			},
		},
	}
	status.Listeners = nil
	status.Conditions = setConditions(obj.Generation, status.Conditions, gatewayConditions)
}

// mergeHeaderModifiers merges two api.HeaderModifier instances by concatenating their Add/Set/Remove lists.
// Later entries are applied after earlier ones by preserving order in the resulting slices.
func mergeHeaderModifiers(dst, src *api.HeaderModifier) *api.HeaderModifier {
	if src == nil {
		return dst
	}
	if dst == nil {
		// Create a copy of src to avoid mutating input
		out := &api.HeaderModifier{}
		if len(src.Add) > 0 {
			out.Add = append([]*api.Header{}, src.Add...)
		}
		if len(src.Set) > 0 {
			out.Set = append([]*api.Header{}, src.Set...)
		}
		if len(src.Remove) > 0 {
			out.Remove = append([]string{}, src.Remove...)
		}
		return out
	}
	if len(src.Add) > 0 {
		dst.Add = append(dst.Add, src.Add...)
	}
	if len(src.Set) > 0 {
		dst.Set = append(dst.Set, src.Set...)
	}
	if len(src.Remove) > 0 {
		dst.Remove = append(dst.Remove, src.Remove...)
	}
	return dst
}

// BuildAgwTrafficPolicyFilters builds a list of agentgateway TrafficPolicySpec from a list of k8s gateway api HTTPRoute filters
func BuildAgwTrafficPolicyFilters(
	ctx RouteContext,
	ns string,
	inputFilters []gatewayv1.HTTPRouteFilter,
) ([]*api.TrafficPolicySpec, *condition) {
	var policies []*api.TrafficPolicySpec
	var hasTerminalFilter bool
	var terminalFilterType string
	var policyError *condition
	// Collect multiples of same-type filters to merge
	var mergedReqHdr *api.HeaderModifier
	var mergedRespHdr *api.HeaderModifier
	var mergedMirror []*api.RequestMirrors_Mirror
	for _, filter := range inputFilters {
		switch filter.Type {
		case gatewayv1.HTTPRouteFilterRequestHeaderModifier:
			h := CreateAgwHeadersFilter(filter.RequestHeaderModifier)
			if h == nil {
				continue
			}
			mergedReqHdr = mergeHeaderModifiers(mergedReqHdr, h)
		case gatewayv1.HTTPRouteFilterResponseHeaderModifier:
			h := CreateAgwResponseHeadersFilter(filter.ResponseHeaderModifier)
			if h == nil {
				continue
			}
			mergedRespHdr = mergeHeaderModifiers(mergedRespHdr, h)
		case gatewayv1.HTTPRouteFilterRequestRedirect:
			if hasTerminalFilter {
				policyError = &condition{
					status: metav1.ConditionFalse,
					error: &ConfigError{
						Reason:  ConfigErrorReason(gatewayv1.RouteReasonIncompatibleFilters),
						Message: terminalFilterCombinationError(terminalFilterType, "RequestRedirect"),
					},
				}
				continue
			}
			h := CreateAgwRedirectFilter(filter.RequestRedirect)
			if h == nil {
				continue
			}
			policies = append(policies, &api.TrafficPolicySpec{Kind: &api.TrafficPolicySpec_RequestRedirect{RequestRedirect: h}})
			hasTerminalFilter = true
			terminalFilterType = "RequestRedirect"
		case gatewayv1.HTTPRouteFilterRequestMirror:
			h, err := CreateAgwMirrorFilter(ctx, filter.RequestMirror, ns, gvk.HTTPRoute)
			if err != nil {
				if policyError == nil {
					policyError = err
				}
			} else {
				mergedMirror = append(mergedMirror, h)
			}
		case gatewayv1.HTTPRouteFilterURLRewrite:
			h := CreateAgwRewriteFilter(filter.URLRewrite)
			if h == nil {
				continue
			}
			policies = append(policies, h)
		case gatewayv1.HTTPRouteFilterCORS:
			h := createAgwCorsFilter(filter.CORS)
			if h == nil {
				continue
			}
			policies = append(policies, h)
		case gatewayv1.HTTPRouteFilterExternalAuth:
			h, err := CreateAgwExternalAuthFilter(ctx, filter.ExternalAuth, ns, gvk.HTTPRoute)
			if err != nil {
				if policyError == nil {
					policyError = err
				}
			}
			policies = append(policies, h)
		case gatewayv1.HTTPRouteFilterExtensionRef:
			err := createAgwExtensionRefFilter(filter.ExtensionRef)
			if err != nil {
				if policyError == nil {
					policyError = err
				}
				continue
			}
		default:
			return nil, &condition{
				status: metav1.ConditionFalse,
				error: &ConfigError{
					Reason:  ConfigErrorReason(gatewayv1.RouteReasonIncompatibleFilters),
					Message: fmt.Sprintf("unsupported filter type: %v", filter.Type),
				},
			}
		}
	}
	// Append merged header modifiers at the end to avoid duplicates
	if mergedReqHdr != nil {
		policies = append(policies, &api.TrafficPolicySpec{Kind: &api.TrafficPolicySpec_RequestHeaderModifier{RequestHeaderModifier: mergedReqHdr}})
	}
	if mergedRespHdr != nil {
		policies = append(policies, &api.TrafficPolicySpec{Kind: &api.TrafficPolicySpec_ResponseHeaderModifier{ResponseHeaderModifier: mergedRespHdr}})
	}
	if mergedMirror != nil {
		policies = append(policies, &api.TrafficPolicySpec{Kind: &api.TrafficPolicySpec_RequestMirror{RequestMirror: &api.RequestMirrors{Mirrors: mergedMirror}}})
	}
	return policies, policyError
}

// BuildAgwBackendPolicyFilters builds a list of agentgateway BackendPolicySpec from a list of k8s gateway api HTTPRoute filters
func BuildAgwBackendPolicyFilters(
	ctx RouteContext,
	ns string,
	inputFilters []gatewayv1.HTTPRouteFilter,
) ([]*api.BackendPolicySpec, *condition) {
	var policies []*api.BackendPolicySpec
	var hasTerminalFilter bool
	var terminalFilterType string
	var policyError *condition
	// Collect multiples of same-type filters to merge
	var mergedReqHdr *api.HeaderModifier
	var mergedRespHdr *api.HeaderModifier
	var mergedMirror []*api.RequestMirrors_Mirror
	for _, filter := range inputFilters {
		switch filter.Type {
		case gatewayv1.HTTPRouteFilterRequestHeaderModifier:
			h := CreateAgwHeadersFilter(filter.RequestHeaderModifier)
			if h == nil {
				continue
			}
			mergedReqHdr = mergeHeaderModifiers(mergedReqHdr, h)
		case gatewayv1.HTTPRouteFilterResponseHeaderModifier:
			h := CreateAgwResponseHeadersFilter(filter.ResponseHeaderModifier)
			if h == nil {
				continue
			}
			mergedRespHdr = mergeHeaderModifiers(mergedRespHdr, h)
		case gatewayv1.HTTPRouteFilterRequestRedirect:
			if hasTerminalFilter {
				policyError = &condition{
					status: metav1.ConditionFalse,
					error: &ConfigError{
						Reason:  ConfigErrorReason(gatewayv1.RouteReasonIncompatibleFilters),
						Message: terminalFilterCombinationError(terminalFilterType, "RequestRedirect"),
					},
				}
				continue
			}
			h := CreateAgwRedirectFilter(filter.RequestRedirect)
			if h == nil {
				continue
			}
			policies = append(policies, &api.BackendPolicySpec{Kind: &api.BackendPolicySpec_RequestRedirect{RequestRedirect: h}})
			hasTerminalFilter = true
			terminalFilterType = "RequestRedirect"
		case gatewayv1.HTTPRouteFilterRequestMirror:
			h, err := CreateAgwMirrorFilter(ctx, filter.RequestMirror, ns, gvk.HTTPRoute)
			if err != nil {
				if policyError == nil {
					policyError = err
				}
			} else {
				mergedMirror = append(mergedMirror, h)
			}
		default:
			return nil, &condition{
				status: metav1.ConditionFalse,
				error: &ConfigError{
					Reason:  ConfigErrorReason(gatewayv1.RouteReasonIncompatibleFilters),
					Message: fmt.Sprintf("unsupported filter type: %v", filter.Type),
				},
			}
		}
	}
	// Append merged header modifiers at the end to avoid duplicates
	if mergedReqHdr != nil {
		policies = append(policies, &api.BackendPolicySpec{Kind: &api.BackendPolicySpec_RequestHeaderModifier{RequestHeaderModifier: mergedReqHdr}})
	}
	if mergedRespHdr != nil {
		policies = append(policies, &api.BackendPolicySpec{Kind: &api.BackendPolicySpec_ResponseHeaderModifier{ResponseHeaderModifier: mergedRespHdr}})
	}
	if mergedMirror != nil {
		policies = append(policies, &api.BackendPolicySpec{Kind: &api.BackendPolicySpec_RequestMirror{RequestMirror: &api.RequestMirrors{Mirrors: mergedMirror}}})
	}
	return policies, policyError
}

// terminalFilterCombinationError creates a standardized error message for when multiple terminal filters are used together
func terminalFilterCombinationError(existingFilter, newFilter string) string {
	return fmt.Sprintf("Cannot combine multiple terminal filters: %s and %s are mutually exclusive. Only one terminal filter is allowed per route rule.", existingFilter, newFilter)
}

func buildAgwHTTPDestination(
	ctx RouteContext,
	forwardTo []gatewayv1.HTTPBackendRef,
	ns string,
) ([]*api.RouteBackend, *condition, *condition) {
	if forwardTo == nil {
		return nil, nil, nil
	}

	var invalidBackendErr *condition
	var res []*api.RouteBackend
	for _, fwd := range forwardTo {
		dst, err := buildAgwDestination(ctx, fwd, ns, gvk.HTTPRoute)
		if err != nil {
			logger.Errorf("erroring building agent gateway destination", "error", err, "error message", err.error.Message)
			if isInvalidBackend(err) {
				invalidBackendErr = err
				// keep going, we will gracefully drop invalid backends
			} else {
				return nil, nil, err
			}
		}
		if dst != nil {
			policies, err := BuildAgwBackendPolicyFilters(ctx, ns, fwd.Filters)
			if err != nil {
				return nil, nil, err
			}
			dst.BackendPolicies = policies
		}
		res = append(res, dst)
	}
	return res, invalidBackendErr, nil
}

// TODO(jaellio): move to helper
// ApplyTimeouts applies timeouts to an agw route
func ApplyTimeouts(rule *gatewayv1.HTTPRouteRule, route *api.Route) error {
	if rule == nil || rule.Timeouts == nil {
		return nil
	}
	if route.TrafficPolicies == nil {
		route.TrafficPolicies = []*api.TrafficPolicySpec{}
	}
	var reqDur, beDur *durationpb.Duration

	if rule.Timeouts.Request != nil {
		d, err := time.ParseDuration(string(*rule.Timeouts.Request))
		if err != nil {
			return fmt.Errorf("failed to parse request timeout: %w", err)
		}
		if d != 0 {
			// "Setting a timeout to the zero duration (e.g. "0s") SHOULD disable the timeout"
			// However, agentgateway already defaults to no timeout, so only set for non-zero
			reqDur = durationpb.New(d)
		}
	}
	if rule.Timeouts.BackendRequest != nil {
		d, err := time.ParseDuration(string(*rule.Timeouts.BackendRequest))
		if err != nil {
			return fmt.Errorf("failed to parse backend request timeout: %w", err)
		}
		if d != 0 {
			// "Setting a timeout to the zero duration (e.g. "0s") SHOULD disable the timeout"
			// However, agentgateway already defaults to no timeout, so only set for non-zero
			beDur = durationpb.New(d)
		}
	}
	if reqDur != nil || beDur != nil {
		route.TrafficPolicies = append(route.TrafficPolicies, &api.TrafficPolicySpec{
			Kind: &api.TrafficPolicySpec_Timeout{
				Timeout: &api.Timeout{
					Request:        reqDur,
					BackendRequest: beDur,
				},
			},
		})
	}
	return nil
}

// ApplyRetries applies retries to an agw route
func ApplyRetries(rule *gatewayv1.HTTPRouteRule, route *api.Route) error {
	if rule == nil || rule.Retry == nil {
		return nil
	}
	if a := rule.Retry.Attempts; a != nil && *a == 0 {
		return nil
	}
	if route.TrafficPolicies == nil {
		route.TrafficPolicies = []*api.TrafficPolicySpec{}
	}
	tpRetry := &api.Retry{}
	if rule.Retry.Codes != nil {
		for _, c := range rule.Retry.Codes {
			tpRetry.RetryStatusCodes = append(tpRetry.RetryStatusCodes, int32(c)) //nolint:gosec // G115: HTTP status codes are always positive integers (100-599)
		}
	}
	if rule.Retry.Backoff != nil {
		if d, err := time.ParseDuration(string(*rule.Retry.Backoff)); err == nil {
			tpRetry.Backoff = durationpb.New(d)
		}
	}
	if rule.Retry.Attempts != nil {
		tpRetry.Attempts = int32(*rule.Retry.Attempts) //nolint:gosec // G115: kubebuilder validation ensures 0 <= value, safe for int32
	}
	route.TrafficPolicies = append(route.TrafficPolicies, &api.TrafficPolicySpec{
		Kind: &api.TrafficPolicySpec_Retry{
			Retry: tpRetry,
		},
	})
	return nil
}

// Helper function to convert hostnames
func convertHostnames(hostnames []gatewayv1.Hostname) []string {
	return slices.Map(hostnames, func(h gatewayv1.Hostname) string {
		return string(h)
	})
}

// ConvertHTTPRouteToAgw converts a HTTPRouteRule to an agentgateway HTTPRoute
func ConvertHTTPRouteToAgw(ctx RouteContext, r gatewayv1.HTTPRouteRule,
	obj *gatewayv1.HTTPRoute, pos int, matchPos int,
) (*api.Route, *condition) {
	routeRuleKey := strconv.Itoa(pos) + "." + strconv.Itoa(matchPos)
	res := &api.Route{
		// unique for route rule
		Key:  InternalRouteRuleKey(obj.Namespace, obj.Name, routeRuleKey),
		Name: RouteName("HTTPRoute", obj.Namespace, obj.Name, r.Name),
		// filled in later
		ListenerKey: "",
	}

	if err := processRouteMatches(&r, res); err != nil {
		return nil, &condition{
			status: metav1.ConditionFalse,
			error: &ConfigError{
				Reason:  "InvalidMatch",
				Message: fmt.Sprintf("failed to process route matches: %v", err),
			},
		}
	}

	policies, policiesErr := BuildAgwTrafficPolicyFilters(ctx, obj.Namespace, r.Filters)
	res.TrafficPolicies = policies

	if err := ApplyTimeouts(&r, res); err != nil {
		return nil, &condition{
			status: metav1.ConditionFalse,
			error: &ConfigError{
				Reason:  "TranslationError",
				Message: fmt.Sprintf("failed to apply builtin route timeout: %v", err),
			},
		}
	}
	if err := ApplyRetries(&r, res); err != nil {
		return nil, &condition{
			status: metav1.ConditionFalse,
			error: &ConfigError{
				Reason:  "TranslationError",
				Message: fmt.Sprintf("failed to apply builtin route retries: %v", err),
			},
		}
	}

	backends, backendErr, err := buildAgwHTTPDestination(ctx, r.BackendRefs, obj.Namespace)
	if err != nil {
		return nil, &condition{
			status: metav1.ConditionFalse,
			error: &ConfigError{
				Reason:  "BackendError",
				Message: fmt.Sprintf("failed to build backend destination: %v", err),
			},
		}
	}
	res.Backends = backends

	res.Hostnames = convertHostnames(obj.Spec.Hostnames)

	if policiesErr != nil && !isPolicyErrorCritical(policiesErr) {
		return nil, policiesErr
	}

	return res, backendErr
}

// Helper function to process route matches
func processRouteMatches(r *gatewayv1.HTTPRouteRule, res *api.Route) error {
	for _, match := range r.Matches {
		path := CreateAgwPathMatch(match)
		headers := CreateAgwHeadersMatch(match)
		method := CreateAgwMethodMatch(match)
		query := CreateAgwQueryMatch(match)

		res.Matches = append(res.GetMatches(), &api.RouteMatch{
			Path:        path,
			Headers:     headers,
			Method:      method,
			QueryParams: query,
		})
	}
	return nil
}

func createAgwCorsFilter(cors *gatewayv1.HTTPCORSFilter) *api.TrafficPolicySpec {
	if cors == nil {
		return nil
	}
	return &api.TrafficPolicySpec{
		Kind: &api.TrafficPolicySpec_Cors{Cors: &api.CORS{
			AllowCredentials: ptr.OrEmpty(cors.AllowCredentials),
			AllowHeaders:     slices.Map(cors.AllowHeaders, func(h gatewayv1.HTTPHeaderName) string { return string(h) }),
			AllowMethods:     slices.Map(cors.AllowMethods, func(m gatewayv1.HTTPMethodWithWildcard) string { return string(m) }),
			AllowOrigins:     slices.Map(cors.AllowOrigins, func(o gatewayv1.CORSOrigin) string { return string(o) }),
			ExposeHeaders:    slices.Map(cors.ExposeHeaders, func(h gatewayv1.HTTPHeaderName) string { return string(h) }),
			MaxAge: &duration.Duration{
				Seconds: int64(cors.MaxAge),
			},
		}},
	}
}

func GetCommonRouteInfo(spec any) ([]gatewayv1.ParentReference, []gatewayv1.Hostname, config.GroupVersionKind) {
	switch t := spec.(type) {
	case *gatewayalpha.TCPRoute:
		return t.Spec.ParentRefs, nil, gvk.TCPRoute
	case *gatewayalpha.TLSRoute:
		return t.Spec.ParentRefs, t.Spec.Hostnames, gvk.TLSRoute
	case *gatewayv1.HTTPRoute:
		return t.Spec.ParentRefs, t.Spec.Hostnames, gvk.HTTPRoute
	case *gatewayv1.GRPCRoute:
		return t.Spec.ParentRefs, t.Spec.Hostnames, gvk.GRPCRoute
	default:
		logger.Fatalf("unknown type %T", t)
		return nil, nil, config.GroupVersionKind{}
	}
}

var allowedParentReferences = sets.New(
	gvk.KubernetesGateway,
	gvk.Service,
	gvk.ServiceEntry,
	gvk.XListenerSet,
)

func defaultString[T ~string](s *T, def string) string {
	if s == nil {
		return def
	}
	return string(*s)
}

func toInternalParentReference(p gatewayv1.ParentReference, localNamespace string) (AgwParentKey, error) {
	ref := normalizeReference(p.Group, p.Kind, gvk.KubernetesGateway)
	if !allowedParentReferences.Contains(ref) {
		return AgwParentKey{}, fmt.Errorf("unsupported parent: %v/%v", p.Group, p.Kind)
	}
	return AgwParentKey{
		Kind: ref.Kubernetes(),
		Name: string(p.Name),
		// Unset namespace means "same namespace"
		Namespace: defaultString(p.Namespace, localNamespace),
	}, nil
}

// ReferenceAllowed validates if a route can reference a specified parent based on rules like section, port, and hostnames.
// Returns a *ParentError if the reference violates any constraints or is disallowed.
// Returns nil if the reference is valid and permitted for the given route and ParentInfo.
func ReferenceAllowed(
	ctx RouteContext,
	parent *AgwParentInfo,
	routeKind schema.GroupVersionKind,
	parentRef ParentReference,
	hostnames []gatewayv1.Hostname,
	localNamespace string,
) *ParentError {
	if parentRef.Kind == gvk.Service.Kubernetes() {
		key := parentRef.Namespace + "/" + parentRef.Name
		svc := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.Services, krt.FilterKey(key)))

		// check that the referenced svc exists
		if svc == nil {
			return &ParentError{
				Reason:  ParentErrorNotAccepted,
				Message: fmt.Sprintf("parent service: %q not found", parentRef.Name),
			}
		}
	} else if parentRef.Kind == gvk.ServiceEntry.Kubernetes() {
		// check that the referenced svc entry exists
		key := parentRef.Namespace + "/" + parentRef.Name
		svcEntry := ptr.Flatten(krt.FetchOne(ctx.Krt, ctx.ServiceEntries, krt.FilterKey(key)))
		if svcEntry == nil {
			return &ParentError{
				Reason:  ParentErrorNotAccepted,
				Message: fmt.Sprintf("parent service entry: %q not found", parentRef.Name),
			}
		}
	} else {
		// First, check section and port apply. This must come first
		if parentRef.Port != 0 && parentRef.Port != parent.Port {
			return &ParentError{
				Reason:  ParentErrorNotAccepted,
				Message: fmt.Sprintf("port %v not found", parentRef.Port),
			}
		}
		if len(parentRef.SectionName) > 0 && parentRef.SectionName != parent.SectionName {
			return &ParentError{
				Reason:  ParentErrorNotAccepted,
				Message: fmt.Sprintf("sectionName %q not found", parentRef.SectionName),
			}
		}

		// Next check the hostnames are a match. This is a bi-directional wildcard match. Only one route
		// hostname must match for it to be allowed (but the others will be filtered at runtime)
		// If either is empty its treated as a wildcard which always matches

		if len(hostnames) == 0 {
			hostnames = []gatewayv1.Hostname{"*"}
		}
		if len(parent.Hostnames) > 0 {
			matched := false
			hostMatched := false
		out:
			for _, routeHostname := range hostnames {
				for _, parentHostNamespace := range parent.Hostnames {
					var parentNamespace, parentHostname string
					if strings.Contains(parentHostNamespace, "/") {
						spl := strings.Split(parentHostNamespace, "/")
						parentNamespace, parentHostname = spl[0], spl[1]
					} else {
						parentNamespace, parentHostname = "*", parentHostNamespace
					}

					hostnameMatch := host.Name(parentHostname).Matches(host.Name(routeHostname))
					namespaceMatch := parentNamespace == "*" || parentNamespace == localNamespace

					hostMatched = hostMatched || hostnameMatch
					if hostnameMatch && namespaceMatch {
						matched = true
						break out
					}
				}
			}
			if !matched {
				if hostMatched {
					return &ParentError{
						Reason: ParentErrorNotAllowed,
						Message: fmt.Sprintf(
							"hostnames matched parent hostname %q, but namespace %q is not allowed by the parent",
							parent.OriginalHostname, localNamespace,
						),
					}
				}
				return &ParentError{
					Reason: ParentErrorNoHostname,
					Message: fmt.Sprintf(
						"no hostnames matched parent hostname %q",
						parent.OriginalHostname,
					),
				}
			}
		}
	}
	// Also make sure this route kind is allowed
	matched := false
	for _, ak := range parent.AllowedKinds {
		if string(ak.Kind) == routeKind.Kind && ptr.OrDefault((*string)(ak.Group), gvk.GatewayClass.Group) == routeKind.Group {
			matched = true
			break
		}
	}
	if !matched {
		return &ParentError{
			Reason:  ParentErrorNotAllowed,
			Message: fmt.Sprintf("kind %v is not allowed", routeKind),
		}
	}
	return nil
}

func parentRefString(ref gatewayv1.ParentReference) string {
	return fmt.Sprintf("%s/%s/%s/%s/%d.%s",
		defaultString(ref.Group, gvk.KubernetesGateway.Group),
		defaultString(ref.Kind, gvk.KubernetesGateway.Kind),
		ref.Name,
		ptr.OrEmpty(ref.SectionName),
		ptr.OrEmpty(ref.Port),
		ptr.OrEmpty(ref.Namespace))
}

func extractParentReferenceInfo(ctx RouteContext, parents RouteParents, obj controllers.Object) []RouteParentReference {
	routeRefs, hostnames, kind := GetCommonRouteInfo(obj)
	localNamespace := obj.GetNamespace()
	var parentRefs []RouteParentReference
	for _, ref := range routeRefs {
		ir, err := toInternalParentReference(ref, localNamespace)
		if err != nil {
			continue
		}
		pk := ParentReference{
			AgwParentKey: ir,
			SectionName:  ptr.OrEmpty(ref.SectionName),
			Port:         ptr.OrEmpty(ref.Port),
		}
		gk := ir
		currentParents := parents.fetch(ctx.Krt, gk)
		appendParent := func(pr *AgwParentInfo, pk ParentReference) {
			bannedHostnames := sets.New[string]()
			for _, gw := range currentParents {
				if gw == pr {
					continue // do not ban ourself
				}
				if gw.Port != pr.Port {
					continue
				}
				if gw.Protocol != pr.Protocol {
					continue
				}
				bannedHostnames.Insert(gw.OriginalHostname)
			}
			deniedReason := ReferenceAllowed(ctx, pr, kind.Kubernetes(), pk, hostnames, localNamespace)

			rpi := RouteParentReference{
				ParentGateway:     pr.ParentGateway,
				InternalName:      pr.InternalName,
				InternalKind:      ir.Kind,
				Hostname:          pr.OriginalHostname,
				DeniedReason:      deniedReason,
				OriginalReference: ref,
				BannedHostnames:   bannedHostnames.Copy().Delete(pr.OriginalHostname),
				ParentKey:         ir,
				ParentSection:     pr.SectionName,
				Accepted:          deniedReason == nil,
			}
			parentRefs = append(parentRefs, rpi)
		}
		for _, gw := range currentParents {
			appendParent(gw, pk)
		}
	}
	// Ensure stable order
	slices.SortBy(parentRefs, func(a RouteParentReference) string {
		return parentRefString(a.OriginalReference)
	})
	return parentRefs
}

// FilteredReferences filters out references that are not accepted by the Parent.
func FilteredReferences(parents []RouteParentReference) []RouteParentReference {
	ret := make([]RouteParentReference, 0, len(parents))
	for _, p := range parents {
		if p.DeniedReason != nil {
			// We should filter this out
			continue
		}
		ret = append(ret, p)
	}
	// To ensure deterministic order, sort them
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].InternalName < ret[j].InternalName
	})
	return ret
}

// TODO(jaellio): Is domain suffix correct here?
// GetServiceHostname returns the fully qualified service hostname
func GetServiceHostname(ctx RouteContext, name, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.%s", name, namespace, ctx.DomainSuffix)
}

// GetInferenceServiceHostname returns the fully qualified service hostname for InferencePools
func GetInferenceServiceHostname(ctx RouteContext, name, namespace string) string {
	return fmt.Sprintf("%s.%s.inference.%s", name, namespace, ctx.DomainSuffix)
}

// https://github.com/kubernetes-sigs/gateway-api/blob/cea484e38e078a2c1997d8c7a62f410a1540f519/apis/v1beta1/httproute_types.go#L207-L212
func isInvalidBackend(err *condition) bool {
	return err.reason == ConfigErrorReason(gatewayv1.RouteReasonRefNotPermitted) ||
		err.reason == ConfigErrorReason(gatewayv1.RouteReasonBackendNotFound) ||
		err.reason == ConfigErrorReason(gatewayv1.RouteReasonInvalidKind)
}
