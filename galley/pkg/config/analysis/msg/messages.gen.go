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

	// ReferencedResourceNotFound defines a diag.MessageType for message "ReferencedResourceNotFound".
	// Description: A resource being referenced does not exist.
	ReferencedResourceNotFound = diag.NewMessageType(diag.Error, "IST0101", "Referenced %s not found: %q")
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

// NewReferencedResourceNotFound returns a new diag.Message based on ReferencedResourceNotFound.
func NewReferencedResourceNotFound(entry *resource.Entry, reftype string, refval string) diag.Message {
	return diag.NewMessage(
		ReferencedResourceNotFound,
		originOrNil(entry),
		reftype,
		refval,
	)
}

func originOrNil(e *resource.Entry) resource.Origin {
	var o resource.Origin
	if e != nil {
		o = e.Origin
	}
	return o
}
