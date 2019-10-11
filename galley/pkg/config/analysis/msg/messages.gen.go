
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
	
	// NotYetImplemented defines a diag.MessageType for message "NotYetImplemented".
	// Description: A feature that the configuration is depending on is not implemented yet.
	NotYetImplemented = diag.NewMessageType(diag.Error, "IST0002", "Not yet implemented: %s")
	
	// ParseError defines a diag.MessageType for message "ParseError".
	// Description: There was a parse error during the parsing of the configuration text
	ParseError = diag.NewMessageType(diag.Warning, "IST0003", "Parse error: %s")
	
	// Deprecated defines a diag.MessageType for message "Deprecated".
	// Description: A feature that the configuration is depending on is now deprecated.
	Deprecated = diag.NewMessageType(diag.Warning, "IST0004", "Deprecated: %s")
	
	// MisplacedAnnotation defines a diag.MessageType for message "MisplacedAnnotation".
	// Description: An Istio annotation is applied to the wrong kind of resource.
	MisplacedAnnotation = diag.NewMessageType(diag.Warning, "IST0005", "Misplaced annotation: %s can only be applied to %s")
	
	// UnknownAnnotation defines a diag.MessageType for message "UnknownAnnotation".
	// Description: An Istio annotation is not recognized for any kind of resource
	UnknownAnnotation = diag.NewMessageType(diag.Warning, "IST0006", "Unknown annotation: %s")
	
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
	// Description: The resource has one or more schema validation errors.
	SchemaValidationError = diag.NewMessageType(diag.Error, "IST0106", "The resource has one or more schema validation errors: %v")
	
)


// NewInternalError returns a new diag.Message based on InternalError.
func NewInternalError(entry *resource.Entry, detail string) diag.Message {
	return diag.NewMessage(
		InternalError,
		originOrNil(entry),
			detail,
	)
}

// NewNotYetImplemented returns a new diag.Message based on NotYetImplemented.
func NewNotYetImplemented(entry *resource.Entry, detail string) diag.Message {
	return diag.NewMessage(
		NotYetImplemented,
		originOrNil(entry),
			detail,
	)
}

// NewParseError returns a new diag.Message based on ParseError.
func NewParseError(entry *resource.Entry, detail string) diag.Message {
	return diag.NewMessage(
		ParseError,
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
func NewSchemaValidationError(entry *resource.Entry, combinedErr error) diag.Message {
	return diag.NewMessage(
		SchemaValidationError,
		originOrNil(entry),
			combinedErr,
	)
}


func originOrNil(e *resource.Entry) resource.Origin {
	var o resource.Origin
	if e != nil {
		o = e.Origin
	}
	return o
}
