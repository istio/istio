// Copyright 2017 Istio Authors
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

package model

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/golang/protobuf/proto"

	multierror "github.com/hashicorp/go-multierror"

	proxyconfig "istio.io/manager/model/proxy/alphav1/config"
)

const (
	dns1123LabelMaxLength int    = 63
	dns1123LabelFmt       string = "[a-z0-9]([-a-z0-9]*[a-z0-9])?"
	// TODO: there is a stricter regex for the labels from validation.go in k8s
	qualifiedNameFmt string = "[-A-Za-z0-9_./]*"
)

var (
	dns1123LabelRex = regexp.MustCompile("^" + dns1123LabelFmt + "$")
	tagRegexp       = regexp.MustCompile("^" + qualifiedNameFmt + "$")
)

// IsDNS1123Label tests for a string that conforms to the definition of a label in
// DNS (RFC 1123).
func IsDNS1123Label(value string) bool {
	return len(value) <= dns1123LabelMaxLength && dns1123LabelRex.MatchString(value)
}

// Validate confirms that the names in the configuration key are appropriate
func (k *Key) Validate() error {
	var errs error
	if !IsDNS1123Label(k.Kind) {
		errs = multierror.Append(errs, fmt.Errorf("Invalid kind: %q", k.Kind))
	}
	if !IsDNS1123Label(k.Name) {
		errs = multierror.Append(errs, fmt.Errorf("Invalid name: %q", k.Name))
	}
	if !IsDNS1123Label(k.Namespace) {
		errs = multierror.Append(errs, fmt.Errorf("Invalid namespace: %q", k.Namespace))
	}
	return errs
}

// Validate checks that each name conforms to the spec and has a ProtoMessage
func (km KindMap) Validate() error {
	var errs error
	for k, v := range km {
		if !IsDNS1123Label(k) {
			errs = multierror.Append(errs, fmt.Errorf("Invalid kind: %q", k))
		}
		if proto.MessageType(v.MessageName) == nil {
			errs = multierror.Append(errs, fmt.Errorf("Cannot find proto message type: %q", v.MessageName))
		}
	}
	return errs
}

// ValidateKey ensures that the key is well-defined and kind is well-defined
func (km KindMap) ValidateKey(k *Key) error {
	if err := k.Validate(); err != nil {
		return err
	}
	if _, ok := km[k.Kind]; !ok {
		return fmt.Errorf("Kind %q is not defined", k.Kind)
	}
	return nil
}

// ValidateConfig ensures that the config object is well-defined
func (km KindMap) ValidateConfig(k *Key, obj interface{}) error {
	if k == nil || obj == nil {
		return fmt.Errorf("Invalid nil configuration object")
	}

	if err := k.Validate(); err != nil {
		return err
	}
	t, ok := km[k.Kind]
	if !ok {
		return fmt.Errorf("Undeclared kind: %q", k.Kind)
	}

	v, ok := obj.(proto.Message)
	if !ok {
		return fmt.Errorf("Cannot cast to a proto message")
	}
	if proto.MessageName(v) != t.MessageName {
		return fmt.Errorf("Mismatched message type %q and kind %q",
			proto.MessageName(v), t.MessageName)
	}
	if err := t.Validate(v); err != nil {
		return err
	}

	return nil
}

// Validate ensures that the service object is well-defined
func (s *Service) Validate() error {
	var errs error
	if len(s.Hostname) == 0 {
		errs = multierror.Append(errs, fmt.Errorf("Invalid empty hostname"))
	}
	parts := strings.Split(s.Hostname, ".")
	for _, part := range parts {
		if !IsDNS1123Label(part) {
			errs = multierror.Append(errs, fmt.Errorf("Invalid hostname part: %q", part))
		}
	}

	// Require at least one port
	if len(s.Ports) == 0 {
		errs = multierror.Append(errs, fmt.Errorf("Service must have at least one declared port"))
	}

	// Port names can be empty if there exists only one port
	for _, port := range s.Ports {
		if port.Name == "" {
			if len(s.Ports) > 1 {
				errs = multierror.Append(errs,
					fmt.Errorf("Empty port names are not allowed for services with multiple ports"))
			}
		} else if !IsDNS1123Label(port.Name) {
			errs = multierror.Append(errs, fmt.Errorf("Invalid name: %q", port.Name))
		}
		if port.Port < 0 {
			errs = multierror.Append(errs, fmt.Errorf("Invalid service port value %d for %q", port.Port, port.Name))
		}
	}
	return errs
}

// Validate ensures tag is well-formed
func (t Tag) Validate() error {
	var errs error
	if len(t) == 0 {
		errs = multierror.Append(errs, fmt.Errorf("Tag must have at least one key-value pair"))
	}
	for k, v := range t {
		if !tagRegexp.MatchString(k) {
			errs = multierror.Append(errs, fmt.Errorf("Invalid tag key: %q", k))
		}
		if !tagRegexp.MatchString(v) {
			errs = multierror.Append(errs, fmt.Errorf("Invalid tag value: %q", v))
		}
	}
	return errs
}

// ValidateRouteRule checks routing rules
func ValidateRouteRule(msg proto.Message) error {
	value, ok := msg.(*proxyconfig.RouteRule)
	if !ok {
		return fmt.Errorf("Cannot cast to routing rule")
	}
	if value.GetDestination() == "" {
		return fmt.Errorf("RouteRule must have a destination service")
	}
	return nil
}

// ValidateDestination checks proxy policies
func ValidateDestination(msg proto.Message) error {
	value, ok := msg.(*proxyconfig.Destination)
	if !ok {
		return fmt.Errorf("Cannot cast to destination policy")
	}
	if value.GetDestination() == "" {
		return fmt.Errorf("Destination should have a valid service name in its destination field")
	}
	return nil
}
