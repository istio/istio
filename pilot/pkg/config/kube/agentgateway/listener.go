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
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	istio "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	kubecreds "istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	schematypes "istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

// NamespaceNameLabel represents that label added automatically to namespaces is newer Kubernetes clusters
const NamespaceNameLabel = "kubernetes.io/metadata.name"

var supportedProtocols = sets.New(
	gatewayv1.HTTPProtocolType,
	gatewayv1.HTTPSProtocolType,
	gatewayv1.TLSProtocolType,
	gatewayv1.TCPProtocolType,
	gatewayv1.ProtocolType(protocol.HBONE))

// dummyTLS is a sentinel value to send to agentgateway to signal that it should reject TLS connects due to invalid config
var dummyTLS = &TLSInfo{
	Cert: []byte("invalid"),
	Key:  []byte("invalid"),
}

// SecretReference holds a reference to a Kubernetes secret along with extracted TLS info.
type SecretReference struct {
	Source types.NamespacedName
	Kind   string
	Info   TLSInfo
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
	listenerConditions := map[string]*Condition{
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
	gwTLS := resolveGatewayTLS(l.Port, gw.TLS)
	tlsInfo, err := buildTLS(ctx, secrets, configMaps, grants, gwTLS, l.TLS, obj)
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
func namespacesFromSelector(
	ctx krt.HandlerContext,
	localNamespace string,
	namespaceCol krt.Collection[*corev1.Namespace],
	lr *gatewayv1.AllowedRoutes,
) []string {
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
			return dummyTLS, &ConfigError{Reason: InvalidTLS, Message: "exactly 1 certificateRefs should be present for TLS termination"}
		}
		tlsRes, err := buildSecretReference(ctx, tls.CertificateRefs[0], gw, secrets)
		if err != nil {
			return dummyTLS, err
		}
		// If we are going to send a cert, validate we can access it
		sameNamespace := tlsRes.Source.Namespace == namespace
		objectKind := schematypes.GvkFromObject(gw)
		if !sameNamespace && !AgwSecretAllowed(grants, ctx, objectKind, tlsRes.Source, namespace) {
			return dummyTLS, &ConfigError{
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
				return dummyTLS, &ConfigError{
					Reason:  InvalidTLS,
					Message: "only one caCertificateRef is supported",
				}
			}
			caCertRef := gatewayTLS.Validation.CACertificateRefs[0]
			cred, err := buildCaCertificateReference(ctx, caCertRef, gw, configMaps, secrets)
			if err != nil {
				return dummyTLS, err
			}
			sameNamespace := cred.Source.Namespace == namespace
			isSecret := cred.Kind == gvk.Secret.Kind
			if isSecret && !sameNamespace && !AgwSecretAllowed(grants, ctx, schematypes.GvkFromObject(gw), cred.Source, namespace) {
				return dummyTLS, &ConfigError{
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

func buildSecretReference(
	ctx krt.HandlerContext,
	ref gatewayv1.SecretObjectReference,
	gw controllers.Object,
	secrets krt.Collection[*corev1.Secret],
) (*SecretReference, *ConfigError) {
	if normalizeReference(ref.Group, ref.Kind, gvk.Secret) != gvk.Secret {
		return nil, &ConfigError{
			Reason:  InvalidTLS,
			Message: fmt.Sprintf("invalid certificate reference %v, only secret is allowed", secretObjectReferenceString(ref)),
		}
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
			Key:  certInfo.Key,
		},
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
