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
	"strings"

	"github.com/agentgateway/agentgateway/go/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
)

// GetInferenceServiceHostname returns the fully qualified service hostname for InferencePools
func GetInferenceServiceHostname(ctx RouteContext, name, namespace string) string {
	return fmt.Sprintf("%s.%s.inference.%s", name, namespace, ctx.DomainSuffix)
}

// GetServiceHostname returns the fully qualified service hostname
func GetServiceHostname(ctx RouteContext, name, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.%s", name, namespace, ctx.DomainSuffix)
}

// buildAgwDestination builds the RouteBackend for a given HTTPBackendRef
func buildAgwDestination(
	ctx RouteContext,
	to gatewayv1.HTTPBackendRef,
	ns string,
	k config.GroupVersionKind,
) (*api.RouteBackend, *Condition) {
	ref := normalizeReference(to.Group, to.Kind, gvk.Service)
	// check if the reference is allowed
	if toNs := to.Namespace; toNs != nil && string(*toNs) != ns {
		if !ctx.Grants.BackendAllowed(ctx.Krt, k, ref, to.Name, *toNs, ns) {
			msg := fmt.Sprintf("backendRef %v/%v not accessible to a %s in namespace %q (missing a ReferenceGrant?)", to.Name, *toNs, k.Kind, ns)
			log.Debug(msg)
			return nil, &Condition{
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
	var invalidBackendErr *Condition
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
			log.Debug(msg)
			return nil, &Condition{
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
		log.Debugf("found pull pool for service", "svc", svc, "key", key)
		if svc == nil {
			msg := fmt.Sprintf("backendRef %s not found", key)
			log.Debug(msg)
			invalidBackendErr = &Condition{
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
				// TODO(jaellio): Update to support multiple ports
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
			log.Debug(msg)
			return nil, &Condition{
				status: metav1.ConditionFalse,
				error: &ConfigError{
					Reason:  ConfigErrorReason(gatewayv1.RouteReasonUnsupportedValue),
					Message: "port is required in backendRef for Hostname kind",
				},
			}
		}
		if to.Namespace != nil {
			msg := fmt.Sprintf("namespace may not be set with Hostname type when referencing %q", to.Name)
			log.Debug(msg)
			return nil, &Condition{
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
			log.Debug(msg)
			return nil, &Condition{
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
			log.Debug(msg)
			invalidBackendErr = &Condition{
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
			log.Debug(msg)
			return nil, &Condition{
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
		log.Debug(msg)
		return nil, &Condition{
			status: metav1.ConditionFalse,
			error: &ConfigError{
				Reason:  ConfigErrorReason(gatewayv1.RouteReasonInvalidKind),
				Message: fmt.Sprintf("referencing unsupported backendRef: group %q kind %q", ptr.OrEmpty(to.Group), ptr.OrEmpty(to.Kind)),
			},
		}
	}
	return rb, invalidBackendErr
}

// buildAgwHTTPDestination builds RouteBackends for a list of HTTPBackendRefs.
func buildAgwHTTPDestination(
	ctx RouteContext,
	forwardTo []gatewayv1.HTTPBackendRef,
	ns string,
) ([]*api.RouteBackend, *Condition, *Condition) {
	if forwardTo == nil {
		return nil, nil, nil
	}

	var invalidBackendErr *Condition
	var res []*api.RouteBackend
	for _, fwd := range forwardTo {
		dst, err := buildAgwDestination(ctx, fwd, ns, gvk.HTTPRoute)
		if err != nil {
			log.Errorf("erroring building agent gateway destination. error: %v, error message: %s", err, err.error.Message)
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

// buildAgwTCPDestination builds RouteBackends for a list of BackendRefs for TCPRoute.
func buildAgwTCPDestination(
	ctx RouteContext,
	forwardTo []gatewayv1.BackendRef,
	ns string,
) ([]*api.RouteBackend, *Condition, *Condition) {
	if forwardTo == nil {
		return nil, nil, nil
	}

	var invalidBackendErr *Condition
	var res []*api.RouteBackend
	for _, fwd := range forwardTo {
		dst, err := buildAgwDestination(ctx, gatewayv1.HTTPBackendRef{
			BackendRef: fwd,
			Filters:    nil, // TCP Routes don't have per-backend filters?
		}, ns, gvk.TCPRoute)
		if err != nil {
			log.Errorf("error building agent gateway destination. error: %v, error message: %s", err, err.error.Message)
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

// buildAgwTLSDestination builds RouteBackends for a list of BackendRefs for TLSRoute.
// first condition return value is for invalid backends, the second is for other errors
func buildAgwTLSDestination(
	ctx RouteContext,
	forwardTo []gatewayv1.BackendRef,
	ns string,
) ([]*api.RouteBackend, *Condition, *Condition) {
	if forwardTo == nil {
		return nil, nil, nil
	}
	var invalidBackendErr *Condition
	var res []*api.RouteBackend
	for _, fwd := range forwardTo {
		dst, err := buildAgwDestination(ctx, gatewayv1.HTTPBackendRef{
			BackendRef: fwd,
			Filters:    nil, // TLS Routes don't have per-backend filters
		}, ns, gvk.TLSRoute)
		if err != nil {
			log.Errorf("error building agent gateway destination. error: %v, error message: %s", err, err.error.Message)
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

// https://github.com/kubernetes-sigs/gateway-api/blob/cea484e38e078a2c1997d8c7a62f410a1540f519/apis/v1beta1/httproute_types.go#L207-L212
func isInvalidBackend(err *Condition) bool {
	return err.reason == ConfigErrorReason(gatewayv1.RouteReasonRefNotPermitted) ||
		err.reason == ConfigErrorReason(gatewayv1.RouteReasonBackendNotFound) ||
		err.reason == ConfigErrorReason(gatewayv1.RouteReasonInvalidKind)
}
