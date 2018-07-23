package svctype

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ServiceType describes what protocol the service supports.
type ServiceType int

const (
	// ServiceUnknown is the default, useless value for ServiceType.
	ServiceUnknown ServiceType = iota
	// ServiceHTTP indicates the service should run an HTTP server.
	ServiceHTTP
	// ServiceGRPC indicates the service should run a GRPC server.
	ServiceGRPC
)

func (t ServiceType) String() (s string) {
	switch t {
	case ServiceHTTP:
		s = "HTTP"
	case ServiceGRPC:
		s = "gRPC"
	}
	return
}

// MarshalJSON encodes the ServiceType as a JSON string.
func (t ServiceType) MarshalJSON() ([]byte, error) {
	return json.Marshal(strings.ToLower(t.String()))
}

// UnmarshalJSON converts a JSON string to a ServiceType.
func (t *ServiceType) UnmarshalJSON(b []byte) (err error) {
	var s string
	err = json.Unmarshal(b, &s)
	if err != nil {
		return
	}
	*t, err = FromString(s)
	if err != nil {
		return
	}
	return
}

// FromString converts a string to a ServiceType.
func FromString(s string) (t ServiceType, err error) {
	switch s {
	case "http":
		t = ServiceHTTP
	case "grpc":
		t = ServiceGRPC
	default:
		err = InvalidServiceTypeStringError{s}
	}
	return
}

// InvalidServiceTypeStringError is returned when a string is not parsable to a
// ServiceType.
type InvalidServiceTypeStringError struct {
	String string
}

func (e InvalidServiceTypeStringError) Error() string {
	return fmt.Sprintf("unknown service type: %s", e.String)
}
