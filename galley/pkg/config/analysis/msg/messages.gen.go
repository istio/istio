// GENERATED FILE -- DO NOT EDIT
//

package msg

import (
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/resource"
)

// InternalError returns a new diag.Message for message "Internal Error".
//
// There was an internal error in the toolchain. This is almost always a bug in the implementation.
func InternalError(entry *resource.Entry, detail string) diag.Message {
	return diag.NewMessage(
		diag.Error,
		"IST0001",
		originOrNil(entry),
		"Internal error: %v",
		detail,
	)
}

// NotYetImplemented returns a new diag.Message for message "Not Yet Implemented".
//
// A feature that the configuration is depending on is not implemented yet.
func NotYetImplemented(entry *resource.Entry, detail string) diag.Message {
	return diag.NewMessage(
		diag.Error,
		"IST0002",
		originOrNil(entry),
		"Not yet implemented: %s",
		detail,
	)
}

// ParseError returns a new diag.Message for message "Parse Error".
//
// There was a parse error during the parsing of the configuration text
func ParseError(entry *resource.Entry, detail string) diag.Message {
	return diag.NewMessage(
		diag.Warning,
		"IST0003",
		originOrNil(entry),
		"Parse error: %s",
		detail,
	)
}

// Deprecated returns a new diag.Message for message "Deprecated".
//
// A feature that the configuration is depending on is now deprecated.
func Deprecated(entry *resource.Entry, detail string) diag.Message {
	return diag.NewMessage(
		diag.Warning,
		"IST0004",
		originOrNil(entry),
		"Deprecated: %s",
		detail,
	)
}

// ReferencedResourceNotFound returns a new diag.Message for message "Referenced Resource Not Found".
//
// A resource being referenced does not exist.
func ReferencedResourceNotFound(entry *resource.Entry, reftype string, refval string) diag.Message {
	return diag.NewMessage(
		diag.Error,
		"IST0101",
		originOrNil(entry),
		"Referenced %s not found: %q",
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
