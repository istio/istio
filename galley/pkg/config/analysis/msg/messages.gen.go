// GENERATED FILE -- DO NOT EDIT
//

package msg

import (
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/resource"
)

var (
	// InternalError defines a diag.MessageType for message "InternalError".
	// Description: There was an internal error in the toolchain. This is almost always a bug in the implementation.
	InternalError = diag.NewMessageType(diag.Error, "IST0001", "Internal error: %v")

	// Deprecated defines a diag.MessageType for message "Deprecated".
	// Description: A feature that the configuration is depending on is now deprecated.
	Deprecated = diag.NewMessageType(diag.Warning, "IST0002", "Deprecated: %s")

	// ReferencedResourceNotFound defines a diag.MessageType for message "ReferencedResourceNotFound".
	// Description: A resource being referenced does not exist.
	ReferencedResourceNotFound = diag.NewMessageType(diag.Error, "IST0101", "Referenced %s not found: %q")

	// NamespaceNotInjected defines a diag.MessageType for message "NamespaceNotInjected".
	// Description: A namespace is not enabled for Istio injection.
	NamespaceNotInjected = diag.NewMessageType(diag.Warning, "IST0102", "The namespace is not enabled for Istio injection. Run 'kubectl label namespace %s istio-injection=enabled' to enable it, or 'kubectl label namespace %s istio-injection=disabled' to explicitly mark it as not needing injection")

	// PodMissingProxy defines a diag.MessageType for message "PodMissingProxy".
	// Description: A pod is missing the Istio proxy.
	PodMissingProxy = diag.NewMessageType(diag.Warning, "IST0103", "The pod is missing its Istio proxy. Run 'kubectl delete pod %s -n %s' to restart it")

	// GatewayPortNotOnWorkload defines a diag.MessageType for message "GatewayPortNotOnWorkload".
	// Description: Unhandled gateway port
	GatewayPortNotOnWorkload = diag.NewMessageType(diag.Warning, "IST0104", "The gateway refers to a port that is not exposed on the workload (pod selector %s; port %d)")

	// IstioProxyVersionMismatch defines a diag.MessageType for message "IstioProxyVersionMismatch".
	// Description: The version of the Istio proxy running on the pod does not match the version used by the istio injector.
	IstioProxyVersionMismatch = diag.NewMessageType(diag.Warning, "IST0105", "The version of the Istio proxy running on the pod does not match the version used by the istio injector (pod version: %s; injector version: %s). This often happens after upgrading the Istio control-plane and can be fixed by redeploying the pod.")

	// SchemaValidationError defines a diag.MessageType for message "SchemaValidationError".
	// Description: The resource has a schema validation error.
	SchemaValidationError = diag.NewMessageType(diag.Error, "IST0106", "Schema validation error: %v")

	// MisplacedAnnotation defines a diag.MessageType for message "MisplacedAnnotation".
	// Description: An Istio annotation is applied to the wrong kind of resource.
	MisplacedAnnotation = diag.NewMessageType(diag.Warning, "IST0107", "Misplaced annotation: %s can only be applied to %s")

	// UnknownAnnotation defines a diag.MessageType for message "UnknownAnnotation".
	// Description: An Istio annotation is not recognized for any kind of resource
	UnknownAnnotation = diag.NewMessageType(diag.Warning, "IST0108", "Unknown annotation: %s")

	// ConflictingMeshGatewayVirtualServiceHosts defines a diag.MessageType for message "ConflictingMeshGatewayVirtualServiceHosts".
	// Description: Conflicting hosts on VirtualServices associated with mesh gateway
	ConflictingMeshGatewayVirtualServiceHosts = diag.NewMessageType(diag.Error, "IST0109", "The VirtualServices %s associated with mesh gateway define the same host %s which can lead to undefined behavior. This can be fixed by merging the conflicting VirtualServices into a single resource.")

	// ConflictingSidecarWorkloadSelectors defines a diag.MessageType for message "ConflictingSidecarWorkloadSelectors".
	// Description: A Sidecar resource selects the same workloads as another Sidecar resource
	ConflictingSidecarWorkloadSelectors = diag.NewMessageType(diag.Error, "IST0110", "The Sidecars %v in namespace %q select the same workload pod %q, which can lead to undefined behavior.")

	// MultipleSidecarsWithoutWorkloadSelectors defines a diag.MessageType for message "MultipleSidecarsWithoutWorkloadSelectors".
	// Description: More than one sidecar resource in a namespace has no workload selector
	MultipleSidecarsWithoutWorkloadSelectors = diag.NewMessageType(diag.Error, "IST0111", "The Sidecars %v in namespace %q have no workload selector, which can lead to undefined behavior.")

	// VirtualServiceDestinationPortSelectorRequired defines a diag.MessageType for message "VirtualServiceDestinationPortSelectorRequired".
	// Description: A VirtualService routes to a service with more than one port exposed, but does not specify which to use.
	VirtualServiceDestinationPortSelectorRequired = diag.NewMessageType(diag.Error, "IST0112", "This VirtualService routes to a service %q that exposes multiple ports %v. Specifying a port in the destination is required to disambiguate.")
)

// NewInternalError returns a new diag.Message based on InternalError.
func NewInternalError(entry *resource.Entry, detail string) diag.Message {
	return diag.NewMessage(
		InternalError,
		originOrNil(entry),
		detail,
	)
}

// NewDeprecated returns a new diag.Message based on Deprecated.
func NewDeprecated(entry *resource.Entry, detail string) diag.Message {
	return diag.NewMessage(
		Deprecated,
		originOrNil(entry),
		detail,
	)
}

// NewReferencedResourceNotFound returns a new diag.Message based on ReferencedResourceNotFound.
func NewReferencedResourceNotFound(entry *resource.Entry, reftype string, refval string) diag.Message {
	return diag.NewMessage(
		ReferencedResourceNotFound,
		originOrNil(entry),
		reftype,
		refval,
	)
}

// NewNamespaceNotInjected returns a new diag.Message based on NamespaceNotInjected.
func NewNamespaceNotInjected(entry *resource.Entry, namespace string, namespace2 string) diag.Message {
	return diag.NewMessage(
		NamespaceNotInjected,
		originOrNil(entry),
		namespace,
		namespace2,
	)
}

// NewPodMissingProxy returns a new diag.Message based on PodMissingProxy.
func NewPodMissingProxy(entry *resource.Entry, pod string, namespace string) diag.Message {
	return diag.NewMessage(
		PodMissingProxy,
		originOrNil(entry),
		pod,
		namespace,
	)
}

// NewGatewayPortNotOnWorkload returns a new diag.Message based on GatewayPortNotOnWorkload.
func NewGatewayPortNotOnWorkload(entry *resource.Entry, selector string, port int) diag.Message {
	return diag.NewMessage(
		GatewayPortNotOnWorkload,
		originOrNil(entry),
		selector,
		port,
	)
}

// NewIstioProxyVersionMismatch returns a new diag.Message based on IstioProxyVersionMismatch.
func NewIstioProxyVersionMismatch(entry *resource.Entry, proxyVersion string, injectionVersion string) diag.Message {
	return diag.NewMessage(
		IstioProxyVersionMismatch,
		originOrNil(entry),
		proxyVersion,
		injectionVersion,
	)
}

// NewSchemaValidationError returns a new diag.Message based on SchemaValidationError.
func NewSchemaValidationError(entry *resource.Entry, err error) diag.Message {
	return diag.NewMessage(
		SchemaValidationError,
		originOrNil(entry),
		err,
	)
}

// NewMisplacedAnnotation returns a new diag.Message based on MisplacedAnnotation.
func NewMisplacedAnnotation(entry *resource.Entry, annotation string, kind string) diag.Message {
	return diag.NewMessage(
		MisplacedAnnotation,
		originOrNil(entry),
		annotation,
		kind,
	)
}

// NewUnknownAnnotation returns a new diag.Message based on UnknownAnnotation.
func NewUnknownAnnotation(entry *resource.Entry, annotation string) diag.Message {
	return diag.NewMessage(
		UnknownAnnotation,
		originOrNil(entry),
		annotation,
	)
}

// NewConflictingMeshGatewayVirtualServiceHosts returns a new diag.Message based on ConflictingMeshGatewayVirtualServiceHosts.
func NewConflictingMeshGatewayVirtualServiceHosts(entry *resource.Entry, virtualServices string, host string) diag.Message {
	return diag.NewMessage(
		ConflictingMeshGatewayVirtualServiceHosts,
		originOrNil(entry),
		virtualServices,
		host,
	)
}

// NewConflictingSidecarWorkloadSelectors returns a new diag.Message based on ConflictingSidecarWorkloadSelectors.
func NewConflictingSidecarWorkloadSelectors(entry *resource.Entry, conflictingSidecars []string, namespace string, workloadPod string) diag.Message {
	return diag.NewMessage(
		ConflictingSidecarWorkloadSelectors,
		originOrNil(entry),
		conflictingSidecars,
		namespace,
		workloadPod,
	)
}

// NewMultipleSidecarsWithoutWorkloadSelectors returns a new diag.Message based on MultipleSidecarsWithoutWorkloadSelectors.
func NewMultipleSidecarsWithoutWorkloadSelectors(entry *resource.Entry, conflictingSidecars []string, namespace string) diag.Message {
	return diag.NewMessage(
		MultipleSidecarsWithoutWorkloadSelectors,
		originOrNil(entry),
		conflictingSidecars,
		namespace,
	)
}

// NewVirtualServiceDestinationPortSelectorRequired returns a new diag.Message based on VirtualServiceDestinationPortSelectorRequired.
func NewVirtualServiceDestinationPortSelectorRequired(entry *resource.Entry, destHost string, destPorts []int) diag.Message {
	return diag.NewMessage(
		VirtualServiceDestinationPortSelectorRequired,
		originOrNil(entry),
		destHost,
		destPorts,
	)
}

func originOrNil(e *resource.Entry) resource.Origin {
	var o resource.Origin
	if e != nil {
		o = e.Origin
	}
	return o
}
