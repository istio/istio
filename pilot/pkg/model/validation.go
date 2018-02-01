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
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
	multierror "github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"
	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"
	routing "istio.io/api/routing/v1alpha1"
	routingv2 "istio.io/api/routing/v1alpha2"
)

const (
	dns1123LabelMaxLength int    = 63
	dns1123LabelFmt       string = "[a-z0-9]([-a-z0-9]*[a-z0-9])?"
	// a wild-card prefix is an '*', a normal DNS1123 label with a leading '*' or '*-', or a normal DNS1123 label
	wildcardPrefix string = `\*|(\*|\*-)?(` + dns1123LabelFmt + `)`

	// TODO: there is a stricter regex for the labels from validation.go in k8s
	qualifiedNameFmt string = "[-A-Za-z0-9_./]*"
)

// Constants for duration fields
const (
	discoveryRefreshDelayMax = time.Minute * 10
	discoveryRefreshDelayMin = time.Second

	connectTimeoutMax = time.Second * 30
	connectTimeoutMin = time.Millisecond

	drainTimeMax          = time.Hour
	parentShutdownTimeMax = time.Hour
)

var (
	dns1123LabelRegexp   = regexp.MustCompile("^" + dns1123LabelFmt + "$")
	tagRegexp            = regexp.MustCompile("^" + qualifiedNameFmt + "$")
	wildcardPrefixRegexp = regexp.MustCompile("^" + wildcardPrefix + "$")
)

// golang supported methods: https://golang.org/src/net/http/method.go
var supportedMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodPatch:   true,
	http.MethodDelete:  true,
	http.MethodConnect: true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
}

// ValidatePort checks that the network port is in range
func ValidatePort(port int) error {
	if 1 <= port && port <= 65535 {
		return nil
	}
	return fmt.Errorf("port number %d must be in the range 1..65535", port)
}

// Validate checks that each name conforms to the spec and has a ProtoMessage
func (descriptor ConfigDescriptor) Validate() error {
	var errs error
	descriptorTypes := make(map[string]bool)
	messages := make(map[string]bool)

	for _, v := range descriptor {
		if !IsDNS1123Label(v.Type) {
			errs = multierror.Append(errs, fmt.Errorf("invalid type: %q", v.Type))
		}
		if !IsDNS1123Label(v.Plural) {
			errs = multierror.Append(errs, fmt.Errorf("invalid plural: %q", v.Type))
		}
		if proto.MessageType(v.MessageName) == nil && gogoproto.MessageType(v.MessageName) == nil {
			errs = multierror.Append(errs, fmt.Errorf("cannot discover proto message type: %q", v.MessageName))
		}
		if _, exists := descriptorTypes[v.Type]; exists {
			errs = multierror.Append(errs, fmt.Errorf("duplicate type: %q", v.Type))
		}
		descriptorTypes[v.Type] = true
		if _, exists := messages[v.MessageName]; exists {
			errs = multierror.Append(errs, fmt.Errorf("duplicate message type: %q", v.MessageName))
		}
		messages[v.MessageName] = true
	}
	return errs
}

// ValidateConfig ensures that the config object is well-defined
// TODO: also check name and namespace
func (descriptor ConfigDescriptor) ValidateConfig(typ string, obj interface{}) error {
	if obj == nil {
		return fmt.Errorf("invalid nil configuration object")
	}

	t, ok := descriptor.GetByType(typ)
	if !ok {
		return fmt.Errorf("undeclared type: %q", typ)
	}

	v, ok := obj.(proto.Message)
	if !ok {
		return fmt.Errorf("cannot cast to a proto message")
	}

	if proto.MessageName(v) != t.MessageName {
		return fmt.Errorf("mismatched message type %q and type %q",
			proto.MessageName(v), t.MessageName)
	}

	return t.Validate(v)
}

// Validate ensures that the service object is well-defined
func (s *Service) Validate() error {
	var errs error
	if len(s.Hostname) == 0 {
		errs = multierror.Append(errs, fmt.Errorf("invalid empty hostname"))
	}
	parts := strings.Split(s.Hostname, ".")
	for _, part := range parts {
		if !IsDNS1123Label(part) {
			errs = multierror.Append(errs, fmt.Errorf("invalid hostname part: %q", part))
		}
	}

	// Require at least one port
	if len(s.Ports) == 0 {
		errs = multierror.Append(errs, fmt.Errorf("service must have at least one declared port"))
	}

	// Port names can be empty if there exists only one port
	for _, port := range s.Ports {
		if port.Name == "" {
			if len(s.Ports) > 1 {
				errs = multierror.Append(errs,
					fmt.Errorf("empty port names are not allowed for services with multiple ports"))
			}
		} else if !IsDNS1123Label(port.Name) {
			errs = multierror.Append(errs, fmt.Errorf("invalid name: %q", port.Name))
		}
		if err := ValidatePort(port.Port); err != nil {
			errs = multierror.Append(errs,
				fmt.Errorf("invalid service port value %d for %q: %v", port.Port, port.Name, err))
		}
	}
	return errs
}

// Validate ensures that the service instance is well-defined
func (instance *ServiceInstance) Validate() error {
	var errs error
	if instance.Service == nil {
		errs = multierror.Append(errs, fmt.Errorf("missing service in the instance"))
	} else if err := instance.Service.Validate(); err != nil {
		errs = multierror.Append(errs, err)
	}

	if err := instance.Labels.Validate(); err != nil {
		errs = multierror.Append(errs, err)
	}

	if err := ValidatePort(instance.Endpoint.Port); err != nil {
		errs = multierror.Append(errs, err)
	}

	port := instance.Endpoint.ServicePort
	if port == nil {
		errs = multierror.Append(errs, fmt.Errorf("missing service port"))
	} else if instance.Service != nil {
		expected, ok := instance.Service.Ports.Get(port.Name)
		if !ok {
			errs = multierror.Append(errs, fmt.Errorf("missing service port %q", port.Name))
		} else {
			if expected.Port != port.Port {
				errs = multierror.Append(errs,
					fmt.Errorf("unexpected service port value %d, expected %d", port.Port, expected.Port))
			}
			if expected.Protocol != port.Protocol {
				errs = multierror.Append(errs,
					fmt.Errorf("unexpected service protocol %s, expected %s", port.Protocol, expected.Protocol))
			}
		}
	}

	return errs
}

// Validate ensures tag is well-formed
func (l Labels) Validate() error {
	var errs error
	for k, v := range l {
		if !tagRegexp.MatchString(k) {
			errs = multierror.Append(errs, fmt.Errorf("invalid tag key: %q", k))
		}
		if !tagRegexp.MatchString(v) {
			errs = multierror.Append(errs, fmt.Errorf("invalid tag value: %q", v))
		}
	}
	return errs
}

// ValidateFQDN checks a fully-qualified domain name
func ValidateFQDN(fqdn string) error {
	return appendErrors(checkDNS1123Preconditions(fqdn), validateDNS1123Labels(fqdn))
}

// ValidateWildcardDomain checks that a domain is a valid FQDN, but also allows wildcard prefixes.
func ValidateWildcardDomain(domain string) error {
	if err := checkDNS1123Preconditions(domain); err != nil {
		return err
	}
	// We only allow wildcards in the first label; split off the first label (parts[0]) from the rest of the host (parts[1])
	parts := strings.SplitN(domain, ".", 2)
	if !IsWildcardDNS1123Label(parts[0]) {
		return fmt.Errorf("domain name %q invalid (label %q invalid)", domain, parts[0])
	} else if len(parts) > 1 {
		return validateDNS1123Labels(parts[1])
	}
	return nil
}

// encapsulates DNS 1123 checks common to both wildcarded hosts and FQDNs
func checkDNS1123Preconditions(name string) error {
	if len(name) > 255 {
		return fmt.Errorf("domain name %q too long (max 255)", name)
	}
	if len(name) == 0 {
		return fmt.Errorf("empty domain name not allowed")
	}
	return nil
}

func validateDNS1123Labels(domain string) error {
	for _, label := range strings.Split(domain, ".") {
		if !IsDNS1123Label(label) {
			return fmt.Errorf("domain name %q invalid (label %q invalid)", domain, label)
		}
	}
	return nil
}

// IsDNS1123Label tests for a string that conforms to the definition of a label in
// DNS (RFC 1123).
func IsDNS1123Label(value string) bool {
	return len(value) <= dns1123LabelMaxLength && dns1123LabelRegexp.MatchString(value)
}

// IsWildcardDNS1123Label tests for a string that conforms to the definition of a label in DNS (RFC 1123), but allows
// the wildcard label (`*`), and typical labels with a leading astrisk instead of alphabetic character (e.g. "*-foo")
func IsWildcardDNS1123Label(value string) bool {
	return len(value) <= dns1123LabelMaxLength && wildcardPrefixRegexp.MatchString(value)
}

// ValidateIstioService checks for validity of a service reference
func ValidateIstioService(svc *routing.IstioService) (errs error) {
	if svc.Name == "" && svc.Service == "" {
		errs = multierror.Append(errs, errors.New("name or service is mandatory for a service reference"))
	} else if svc.Service != "" && svc.Name != "" {
		errs = multierror.Append(errs, errors.New("specify either name or service, not both"))
	} else if svc.Service != "" {
		if err := ValidateEgressRuleService(svc.Service); err != nil {
			errs = multierror.Append(errs, err)
		}
		if svc.Namespace != "" {
			errs = multierror.Append(errs, errors.New("namespace is not valid when service is provided"))
		}
		if svc.Domain != "" {
			errs = multierror.Append(errs, errors.New("domain is not valid when service is provided"))
		}
	} else if svc.Name != "" {
		if !IsDNS1123Label(svc.Name) {
			errs = multierror.Append(errs, fmt.Errorf("name %q must be a valid label", svc.Name))
		}
	}

	if svc.Namespace != "" && !IsDNS1123Label(svc.Namespace) {
		errs = multierror.Append(errs, fmt.Errorf("namespace %q must be a valid label", svc.Namespace))
	}

	if svc.Domain != "" {
		if err := ValidateFQDN(svc.Domain); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if err := Labels(svc.Labels).Validate(); err != nil {
		errs = multierror.Append(errs, err)
	}

	return
}

// ValidateMatchCondition validates a match condition
func ValidateMatchCondition(mc *routing.MatchCondition) (errs error) {
	if mc.Source != nil {
		if err := ValidateIstioService(mc.Source); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if mc.Tcp != nil {
		if err := ValidateL4MatchAttributes(mc.Tcp); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if mc.Udp != nil {
		if err := ValidateL4MatchAttributes(mc.Udp); err != nil {
			errs = multierror.Append(errs, err)
		}
		// nolint: golint
		errs = multierror.Append(errs, fmt.Errorf("UDP protocol not supported yet"))
	}

	if mc.Request != nil {
		for name, value := range mc.Request.Headers {
			if err := ValidateHTTPHeaderName(name); err != nil {
				errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("header name %q invalid: ", name)))
			}
			if err := ValidateStringMatch(value); err != nil {
				errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("header %q value invalid: ", name)))
			}

			// validate special `uri` header:
			// absolute path must be non-empty (https://www.w3.org/Protocols/rfc2616/rfc2616-sec5.html#sec5.1.2)
			if name == HeaderURI {
				switch m := value.MatchType.(type) {
				case *routing.StringMatch_Exact:
					if m.Exact == "" {
						errs = multierror.Append(errs, fmt.Errorf("exact header value for %q must be non-empty", HeaderURI))
					}
				case *routing.StringMatch_Prefix:
					if m.Prefix == "" {
						errs = multierror.Append(errs, fmt.Errorf("prefix header value for %q must be non-empty", HeaderURI))
					}
				case *routing.StringMatch_Regex:
					if m.Regex == "" {
						errs = multierror.Append(errs, fmt.Errorf("regex header value for %q must be non-empty", HeaderURI))
					}
				}
			}

			// TODO validate authority special header
		}
	}

	return
}

// ValidateHTTPHeaderName checks that the name is lower-case
func ValidateHTTPHeaderName(name string) error {
	if name == "" {
		return fmt.Errorf("header name cannot be empty")
	}
	if strings.ToLower(name) != name {
		return fmt.Errorf("must be in lower case")
	}
	return nil
}

// ValidateStringMatch checks that the match types are correct
func ValidateStringMatch(match *routing.StringMatch) error {
	switch match.MatchType.(type) {
	case *routing.StringMatch_Exact, *routing.StringMatch_Prefix, *routing.StringMatch_Regex:
	default:
		return fmt.Errorf("unrecognized string match %q", match)
	}
	return nil
}

// ValidateL4MatchAttributes validates L4 Match Attributes
func ValidateL4MatchAttributes(ma *routing.L4MatchAttributes) (errs error) {
	for _, subnet := range ma.SourceSubnet {
		if err := ValidateSubnet(subnet); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	for _, subnet := range ma.DestinationSubnet {
		if err := ValidateSubnet(subnet); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return
}

// ValidatePercent checks that percent is in range
func ValidatePercent(val int32) error {
	if val < 0 || val > 100 {
		return fmt.Errorf("percentage %v is not in range 0..100", val)
	}
	return nil
}

// ValidateFloatPercent checks that percent is in range
func ValidateFloatPercent(val float32) error {
	if val < 0.0 || val > 100.0 {
		return fmt.Errorf("percentage %v is not in range 0..100", val)
	}
	return nil
}

// ValidateDestinationWeight validates DestinationWeight
func ValidateDestinationWeight(dw *routing.DestinationWeight) (errs error) {
	// TODO: fix destination in destination weight to be an istio service

	if err := Labels(dw.Labels).Validate(); err != nil {
		errs = multierror.Append(errs, err)
	}

	if err := ValidatePercent(dw.Weight); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "weight invalid: "))
	}

	return
}

// ValidateHTTPTimeout validates HTTP Timeout
func ValidateHTTPTimeout(timeout *routing.HTTPTimeout) (errs error) {
	if simple := timeout.GetSimpleTimeout(); simple != nil {
		if err := ValidateDuration(simple.Timeout); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "httpTimeout invalid: "))
		}

		// TODO validate override_header_name?
	}

	return
}

// ValidateHTTPRetries validates HTTP Retries
func ValidateHTTPRetries(retry *routing.HTTPRetry) (errs error) {
	if simple := retry.GetSimpleRetry(); simple != nil {
		if simple.Attempts < 0 {
			errs = multierror.Append(errs, fmt.Errorf("attempts must be in range [0..]"))
		}

		if err := ValidateDuration(simple.PerTryTimeout); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "perTryTimeout invalid: "))
		}
		// We ignore override_header_name
	}

	return
}

// ValidateHTTPFault validates HTTP Fault
func ValidateHTTPFault(fault *routing.HTTPFaultInjection) (errs error) {
	if fault.GetDelay() != nil {
		if err := ValidateDelay(fault.GetDelay()); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if fault.GetAbort() != nil {
		if err := ValidateAbort(fault.GetAbort()); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return
}

// ValidateL4Fault validates L4 Fault
func ValidateL4Fault(fault *routing.L4FaultInjection) (errs error) {
	if fault.GetTerminate() != nil {
		if err := ValidateTerminate(fault.GetTerminate()); err != nil {
			errs = multierror.Append(errs, err)
		}
		errs = multierror.Append(errs, fmt.Errorf("the terminate fault not supported yet"))
	}

	if fault.GetThrottle() != nil {
		if err := ValidateThrottle(fault.GetThrottle()); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return
}

// ValidateSubnet checks that IPv4 subnet form
func ValidateSubnet(subnet string) error {
	// The current implementation only supports IP v4 addresses
	return ValidateIPv4Subnet(subnet)
}

// validateCIDR checks that a string is in "CIDR notation"
func validateCIDR(cidr string) error {
	// We expect a string in "CIDR notation", i.e. a.b.c.d/xx form
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("%v is not a valid CIDR block", cidr)
	}
	// The current implementation only supports IP v4 addresses
	if ip.To4() == nil {
		return fmt.Errorf("%v is not a valid IPv4 address", cidr)
	}

	return nil
}

// ValidateIPv4Subnet checks that a string is in "CIDR notation" or "Dot-decimal notation"
func ValidateIPv4Subnet(subnet string) error {
	// We expect a string in "CIDR notation" or "Dot-decimal notation"
	// E.g., a.b.c.d/xx form or just a.b.c.d
	if strings.Count(subnet, "/") == 1 {
		return validateCIDR(subnet)
	}
	return ValidateIPv4Address(subnet)
}

// ValidateIPv4Address validates that a string in "CIDR notation" or "Dot-decimal notation"
func ValidateIPv4Address(addr string) error {
	ip := net.ParseIP(addr)
	if ip == nil {
		return fmt.Errorf("%v is not a valid IP", addr)
	}

	// The current implementation only supports IP v4 addresses
	if ip.To4() == nil {
		return fmt.Errorf("%v is not a valid IPv4 address", addr)
	}

	return nil
}

// ValidateDelay checks that fault injection delay is well-formed
func ValidateDelay(delay *routing.HTTPFaultInjection_Delay) (errs error) {
	if err := ValidateFloatPercent(delay.Percent); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "percent invalid: "))
	}
	if err := ValidateDuration(delay.GetFixedDelay()); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "fixedDelay invalid:"))
	}

	if delay.GetExponentialDelay() != nil {
		if err := ValidateDuration(delay.GetExponentialDelay()); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "exponentialDelay invalid: "))
		}
		errs = multierror.Append(errs, fmt.Errorf("exponentialDelay not supported yet"))
	}

	return
}

// ValidateAbortHTTPStatus checks that fault injection abort HTTP status is valid
func ValidateAbortHTTPStatus(httpStatus *routing.HTTPFaultInjection_Abort_HttpStatus) (errs error) {
	if httpStatus.HttpStatus < 0 || httpStatus.HttpStatus > 600 {
		errs = multierror.Append(errs, fmt.Errorf("invalid abort http status %v", httpStatus.HttpStatus))
	}

	return
}

// ValidateAbort checks that fault injection abort is well-formed
func ValidateAbort(abort *routing.HTTPFaultInjection_Abort) (errs error) {
	if err := ValidateFloatPercent(abort.Percent); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "percent invalid: "))
	}

	switch abort.ErrorType.(type) {
	case *routing.HTTPFaultInjection_Abort_GrpcStatus:
		// TODO No validation yet for grpc_status / http2_error / http_status
		errs = multierror.Append(errs, fmt.Errorf("gRPC fault injection not supported yet"))
	case *routing.HTTPFaultInjection_Abort_Http2Error:
	// TODO No validation yet for grpc_status / http2_error / http_status
	case *routing.HTTPFaultInjection_Abort_HttpStatus:
		if err := ValidateAbortHTTPStatus(abort.ErrorType.(*routing.HTTPFaultInjection_Abort_HttpStatus)); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	// No validation yet for override_header_name

	return
}

// ValidateTerminate checks that fault injection terminate is well-formed
func ValidateTerminate(terminate *routing.L4FaultInjection_Terminate) (errs error) {
	if err := ValidateFloatPercent(terminate.Percent); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "terminate percent invalid: "))
	}
	return
}

// ValidateThrottle checks that fault injections throttle is well-formed
func ValidateThrottle(throttle *routing.L4FaultInjection_Throttle) (errs error) {
	if err := ValidateFloatPercent(throttle.Percent); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "throttle percent invalid: "))
	}

	if throttle.DownstreamLimitBps < 0 {
		errs = multierror.Append(errs, fmt.Errorf("downstreamLimitBps invalid"))
	}

	if throttle.UpstreamLimitBps < 0 {
		errs = multierror.Append(errs, fmt.Errorf("upstreamLimitBps invalid"))
	}

	err := ValidateDuration(throttle.GetThrottleAfterPeriod())
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("throttleAfterPeriod invalid"))
	}

	if throttle.GetThrottleAfterBytes() < 0 {
		errs = multierror.Append(errs, fmt.Errorf("throttleAfterBytes invalid"))
	}

	// TODO Check DoubleValue throttle.GetThrottleForSeconds()

	return
}

// ValidateLoadBalancing validates Load Balancing
func ValidateLoadBalancing(lb *routing.LoadBalancing) (errs error) {
	if lb.LbPolicy == nil {
		errs = multierror.Append(errs, errors.New("must set load balancing if specified"))
	}
	// Currently the policy is just a name, and we don't validate it
	return
}

// ValidateCircuitBreaker validates Circuit Breaker
func ValidateCircuitBreaker(cb *routing.CircuitBreaker) (errs error) {
	if simple := cb.GetSimpleCb(); simple != nil {
		if simple.MaxConnections < 0 {
			errs = multierror.Append(errs,
				fmt.Errorf("circuitBreak maxConnections must be in range [0..]"))
		}
		if simple.HttpMaxPendingRequests < 0 {
			errs = multierror.Append(errs,
				fmt.Errorf("circuitBreaker maxPendingRequests must be in range [0..]"))
		}
		if simple.HttpMaxRequests < 0 {
			errs = multierror.Append(errs,
				fmt.Errorf("circuitBreaker maxRequests must be in range [0..]"))
		}

		err := ValidateDuration(simple.SleepWindow)
		if err != nil {
			errs = multierror.Append(errs,
				fmt.Errorf("circuitBreaker sleepWindow must be in range [0..]"))
		}

		if simple.HttpConsecutiveErrors < 0 {
			errs = multierror.Append(errs,
				fmt.Errorf("circuitBreaker httpConsecutiveErrors must be in range [0..]"))
		}

		err = ValidateDuration(simple.HttpDetectionInterval)
		if err != nil {
			errs = multierror.Append(errs,
				fmt.Errorf("circuitBreaker httpDetectionInterval must be in range [0..]"))
		}

		if simple.HttpMaxRequestsPerConnection < 0 {
			errs = multierror.Append(errs,
				fmt.Errorf("circuitBreaker httpMaxRequestsPerConnection must be in range [0..]"))
		}
		if err := ValidatePercent(simple.HttpMaxEjectionPercent); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "circuitBreaker httpMaxEjectionPercent invalid: "))
		}
	}

	return
}

// ValidateWeights checks that destination weights sum to 100
func ValidateWeights(routes []*routing.DestinationWeight) (errs error) {
	// Sum weights
	sum := 0
	for _, destWeight := range routes {
		sum = sum + int(destWeight.Weight)
	}

	// From cfg.proto "If there is only [one] destination in a rule, the weight value is assumed to be 100."
	if len(routes) == 1 && sum == 0 {
		return
	}

	if sum != 100 {
		errs = multierror.Append(errs,
			fmt.Errorf("route weights total %v (must total 100)", sum))
	}

	return
}

// ValidateRouteRule checks routing rules
func ValidateRouteRule(msg proto.Message) error {
	value, ok := msg.(*routing.RouteRule)
	if !ok {
		return fmt.Errorf("cannot cast to routing rule")
	}

	var errs error
	if value.Destination == nil {
		errs = multierror.Append(errs, fmt.Errorf("route rule must have a destination service"))
	} else {
		if err := ValidateIstioService(value.Destination); err != nil {
			errs = multierror.Append(errs, err)
		}
		if len(value.Destination.Labels) > 0 {
			errs = multierror.Append(errs, errors.New("route rule destination labels must be empty"))
		}
	}

	// We don't validate precedence because any int32 is legal
	if value.Match != nil {
		if err := ValidateMatchCondition(value.Match); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if value.Rewrite != nil {
		if value.Rewrite.GetUri() == "" && value.Rewrite.GetAuthority() == "" {
			errs = multierror.Append(errs, errors.New("rewrite must specify path, host, or both"))
		}
	}

	if value.Redirect != nil {
		if len(value.Route) > 0 {
			errs = multierror.Append(errs, errors.New("rule cannot contain both route and redirect"))
		}

		if value.HttpFault != nil {
			errs = multierror.Append(errs, errors.New("rule cannot contain both fault and redirect"))
		}

		if value.Redirect.GetAuthority() == "" && value.Redirect.GetUri() == "" {
			errs = multierror.Append(errs, errors.New("redirect must specify path, host, or both"))
		}

		if value.WebsocketUpgrade {
			// nolint: golint
			errs = multierror.Append(errs, errors.New("WebSocket upgrade is not allowed on redirect rules"))
		}
	}

	if value.Redirect != nil && value.Rewrite != nil {
		errs = multierror.Append(errs, errors.New("rule cannot contain both rewrite and redirect"))
	}

	if value.Route != nil {
		for _, destWeight := range value.Route {
			if err := ValidateDestinationWeight(destWeight); err != nil {
				errs = multierror.Append(errs, err)
			}
		}
		if err := ValidateWeights(value.Route); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if value.Mirror != nil {
		if err := ValidateIstioService(value.Mirror); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	for name, val := range value.AppendHeaders {
		if err := ValidateHTTPHeaderName(name); err != nil {
			errs = multierror.Append(errs, err)
		}
		if val == "" {
			errs = multierror.Append(errs,
				fmt.Errorf("appended header %q must have a non-empty value", name))
		}
	}

	if value.CorsPolicy != nil {
		if value.CorsPolicy.MaxAge != nil {
			if err := ValidateDuration(value.CorsPolicy.MaxAge); err != nil {
				errs = multierror.Append(errs, err)
			}
			if value.CorsPolicy.MaxAge.Nanos > 0 {
				errs = multierror.Append(errs,
					errors.New("max_age duration is accurate only to seconds precision"))
			}
		}

		for _, name := range value.CorsPolicy.AllowHeaders {
			if err := ValidateHTTPHeaderName(name); err != nil {
				errs = multierror.Append(errs, err)
			}
		}

		for _, name := range value.CorsPolicy.ExposeHeaders {
			if err := ValidateHTTPHeaderName(name); err != nil {
				errs = multierror.Append(errs, err)
			}
		}

		for _, method := range value.CorsPolicy.AllowMethods {
			if !supportedMethods[method] {
				errs = multierror.Append(errs, fmt.Errorf("%q is not a supported HTTP method", method))
			}
		}
	}

	if value.HttpReqTimeout != nil {
		if err := ValidateHTTPTimeout(value.HttpReqTimeout); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if value.HttpReqRetries != nil {
		if err := ValidateHTTPRetries(value.HttpReqRetries); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if value.HttpFault != nil {
		if err := ValidateHTTPFault(value.HttpFault); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if value.L4Fault != nil {
		if err := ValidateL4Fault(value.L4Fault); err != nil {
			errs = multierror.Append(errs, err)
		}
		// nolint: golint
		errs = multierror.Append(errs, fmt.Errorf("L4 faults are not implemented"))
	}

	return errs
}

// ValidateIngressRule checks ingress rules
func ValidateIngressRule(msg proto.Message) error {
	value, ok := msg.(*routing.IngressRule)
	if !ok {
		return fmt.Errorf("cannot cast to ingress rule")
	}

	var errs error
	if value.Destination == nil {
		errs = multierror.Append(errs, fmt.Errorf("ingress rule must have a destination service"))
	} else {
		if err := ValidateIstioService(value.Destination); err != nil {
			errs = multierror.Append(errs, err)
		}
		if len(value.Destination.Labels) > 0 {
			errs = multierror.Append(errs, errors.New("ingress rule destination labels must be empty"))
		}
	}

	// TODO: complete validation for ingress
	return errs
}

// ValidateGateway checks gateway specifications
func ValidateGateway(msg proto.Message) (errs error) {
	value, ok := msg.(*routingv2.Gateway)
	if !ok {
		errs = appendErrors(errs, fmt.Errorf("cannot cast to gateway: %#v", msg))
		return
	}

	if len(value.Servers) == 0 {
		errs = appendErrors(errs, fmt.Errorf("gateway must have at least one server"))
	} else {
		for _, server := range value.Servers {
			errs = appendErrors(errs, validateServer(server))
		}
	}
	return errs
}

func validateServer(server *routingv2.Server) (errs error) {
	if len(server.Hosts) == 0 {
		errs = appendErrors(errs, fmt.Errorf("server config must contain at least one host"))
	} else {
		for _, host := range server.Hosts {
			// We check if its a valid wildcard domain first; if not then we check if its a valid IPv4 address
			// (including CIDR addresses). If it's neither, we report both errors.
			if err := ValidateWildcardDomain(host); err != nil {
				if err2 := ValidateIPv4Subnet(host); err2 != nil {
					errs = appendErrors(errs, err, err2)
				}
			}
		}
	}
	return appendErrors(errs, validateTLSOptions(server.Tls), validateServerPort(server.Port))
}

func validateServerPort(port *routingv2.Port) (errs error) {
	if port == nil {
		return appendErrors(errs, fmt.Errorf("port is required"))
	}
	if ConvertCaseInsensitiveStringToProtocol(port.Protocol) == ProtocolUnsupported {
		errs = appendErrors(errs, fmt.Errorf("invalid protocol %q, supported protocols are HTTP, HTTP2, GRPC, MONGO, REDIS, TCP", port.Protocol))
	}
	if port.Number > 0 {
		errs = appendErrors(errs, ValidatePort(int(port.Number)))
	} else if port.Name == "" {
		errs = appendErrors(errs, fmt.Errorf("either port number or name must be set: %v", port))
	}
	return
}

func validateTLSOptions(tls *routingv2.Server_TLSOptions) (errs error) {
	if tls == nil {
		// no tls config at all is valid
		return
	}
	if tls.Mode == routingv2.Server_TLSOptions_SIMPLE {
		if tls.ServerCertificate == "" {
			errs = appendErrors(errs, fmt.Errorf("SIMPLE TLS requires a server certificate"))
		}
	} else if tls.Mode == routingv2.Server_TLSOptions_MUTUAL {
		if tls.ServerCertificate == "" {
			errs = appendErrors(errs, fmt.Errorf("MUTUAL TLS requires a server certificate"))
		}
		if tls.CaCertificates == "" {
			errs = appendErrors(errs, fmt.Errorf("MUTUAL TLS requires a client CA bundle"))
		}
	}
	return
}

// ValidateEgressRule checks egress rules
func ValidateEgressRule(msg proto.Message) error {
	rule, ok := msg.(*routing.EgressRule)
	if !ok {
		return fmt.Errorf("cannot cast to egress rule")
	}

	var errs error
	destination := rule.Destination

	if err := ValidateEgressRuleDestination(destination); err != nil {
		errs = multierror.Append(errs, err)
	}

	if len(rule.Ports) == 0 {
		errs = multierror.Append(errs, fmt.Errorf("egress rule must have a ports list"))
	}

	cidrDestinationService := destination != nil && strings.Count(destination.Service, "/") == 1

	ports := make(map[int32]bool)
	for _, port := range rule.Ports {
		if _, exists := ports[port.Port]; exists {
			errs = multierror.Append(errs, fmt.Errorf("duplicate port: %d", port.Port))
		}
		ports[port.Port] = true

		if err := ValidateEgressRulePort(port); err != nil {
			errs = multierror.Append(errs, err)
		}

		if cidrDestinationService &&
			!IsEgressRulesSupportedTCPProtocol(ConvertCaseInsensitiveStringToProtocol(port.Protocol)) {
			errs = multierror.Append(errs, fmt.Errorf("Only the following protocols can be defined for "+
				"CIDR destination service notation: %s. "+
				"This rule - port: %d protocol: %s destination.service: %s",
				egressRulesSupportedTCPProtocols(), port.Port, port.Protocol, destination.Service))
		}
	}

	if rule.UseEgressProxy {
		errs = multierror.Append(errs, fmt.Errorf("directing traffic through egress proxy is not implemented yet"))
	}

	return errs
}

//ValidateEgressRuleDestination checks that valid destination is used for an egress-rule
// only service field is allowed, all other fields are forbidden
func ValidateEgressRuleDestination(destination *routing.IstioService) error {
	if destination == nil {
		return fmt.Errorf("destination of egress rule must have destination field")
	}

	var errs error
	if destination.Name != "" {
		errs = multierror.Append(errs, fmt.Errorf("destination of egress rule must not have name field"))
	}

	if destination.Namespace != "" {
		errs = multierror.Append(errs,
			fmt.Errorf("destination of egress rule must not have namespace field"))
	}

	if destination.Domain != "" {
		errs = multierror.Append(errs, fmt.Errorf("destination of egress rule must not have domain field"))
	}

	if len(destination.Labels) > 0 {
		errs = multierror.Append(errs, fmt.Errorf("destination of egress rule must not have labels field"))
	}

	if err := ValidateEgressRuleService(destination.Service); err != nil {
		errs = multierror.Append(errs, err)
	}
	return errs
}

// ValidateEgressRuleService validates service field of egress rules. Service field of egress rule contains either
// domain, according to the definition of Envoy's domain of virtual hosts, or CIDR, according to the definition of
// destination_ip_list of a route in Envoy's TCP Proxy filter.
func ValidateEgressRuleService(service string) error {
	if strings.Count(service, "/") == 1 {
		return validateCIDR(service)
	}
	return ValidateEgressRuleDomain(service)
}

// ValidateEgressRuleDomain validates domains in the egress rules
// domains are according to the definion of Envoy's domain of virtual hosts.
//
// Wildcard hosts are supported in the form of “*.foo.com” or “*-bar.foo.com”.
// Note that the wildcard will not match the empty string. e.g. “*-bar.foo.com” will match “baz-bar.foo.com”
// but not “-bar.foo.com”.  Additionally, a special entry “*” is allowed which will match any host/authority header.
func ValidateEgressRuleDomain(domain string) error {
	if len(domain) < 1 {
		return fmt.Errorf("domain must not be empty string")
	}

	if domain[0] == '*' {
		domain = domain[1:]   // wildcard * is allowed only at the first position
		if len(domain) == 0 { // the domain was just * and it is OK
			return nil
		}
		if domain[0] == '.' || domain[0] == '-' {
			// the domain started with '*.' or '*-' - the rest of the domain should be validate FDQN
			domain = domain[1:]
		}
	}
	return ValidateFQDN(domain)
}

// ValidateEgressRulePort checks the port of the egress rule (communication port and protocol)
func ValidateEgressRulePort(port *routing.EgressRule_Port) error {
	if err := ValidatePort(int(port.Port)); err != nil {
		return err
	}

	if !IsEgressRulesSupportedProtocol(ConvertCaseInsensitiveStringToProtocol(port.Protocol)) {
		return fmt.Errorf("egress rule support is available only for the following protocols: %s",
			egressRulesSupportedProtocols())
	}
	return nil
}

// ValidateDestinationRule checks proxy policies
func ValidateDestinationRule(msg proto.Message) (errs error) {
	rule, ok := msg.(*routingv2.DestinationRule)
	if !ok {
		return fmt.Errorf("cannot cast to destination rule")
	}

	errs = appendErrors(errs,
		validateHost(rule.Name),
		validateTrafficPolicy(rule.TrafficPolicy))

	for _, subset := range rule.Subsets {
		errs = appendErrors(errs, validateSubset(subset))
	}

	return
}

func validateTrafficPolicy(policy *routingv2.TrafficPolicy) error {
	if policy == nil {
		return nil
	}
	if policy.OutlierDetection == nil && policy.ConnectionPool == nil && policy.LoadBalancer == nil {
		return fmt.Errorf("traffic policy must have at least one field")
	}

	return appendErrors(validateOutlierDetection(policy.OutlierDetection),
		validateConnectionPool(policy.ConnectionPool),
		validateLoadBalancer(policy.LoadBalancer))
}

func validateOutlierDetection(outlier *routingv2.OutlierDetection) (errs error) {
	if outlier == nil {
		return
	}
	if outlier.Http == nil {
		return fmt.Errorf("outlier detection must have at least one field")
	}

	http := outlier.Http
	if http.BaseEjectionTime != nil {
		errs = appendErrors(errs, ValidateDuration(http.BaseEjectionTime))
	}
	if http.ConsecutiveErrors < 0 {
		errs = appendErrors(errs, fmt.Errorf("outlier detection consecutive errors cannot be negative"))
	}
	if http.Interval != nil {
		errs = appendErrors(errs, ValidateDuration(http.Interval))
	}
	errs = appendErrors(errs, ValidatePercent(http.MaxEjectionPercent))

	return
}

func validateConnectionPool(settings *routingv2.ConnectionPoolSettings) (errs error) {
	if settings == nil {
		return
	}
	if settings.Http == nil && settings.Tcp == nil {
		return fmt.Errorf("connection pool must have at least one field")
	}

	if http := settings.Http; http != nil {
		if http.Http1MaxPendingRequests < 0 {
			errs = appendErrors(errs, fmt.Errorf("http1 max pending requests must be non-negative"))
		}
		if http.Http2MaxRequests < 0 {
			errs = appendErrors(errs, fmt.Errorf("http2 max requests must be non-negative"))
		}
		if http.MaxRequestsPerConnection < 0 {
			errs = appendErrors(errs, fmt.Errorf("max requests per connection must be non-negative"))
		}
		if http.MaxRetries < 0 {
			errs = appendErrors(errs, fmt.Errorf("max retries must be non-negative"))
		}
	}

	if tcp := settings.Tcp; tcp != nil {
		if tcp.MaxConnections < 0 {
			errs = appendErrors(errs, fmt.Errorf("max connections must be non-negative"))
		}
		if tcp.ConnectTimeout != nil {
			errs = appendErrors(errs, ValidateDuration(tcp.ConnectTimeout))
		}
	}

	return
}

func validateLoadBalancer(settings *routingv2.LoadBalancerSettings) (errs error) {
	if settings == nil {
		return
	}

	// simple load balancing is always valid
	// TODO: settings.GetConsistentHash()

	return
}

func validateSubset(subset *routingv2.Subset) error {
	return appendErrors(validateSubsetName(subset.Name),
		Labels(subset.Labels).Validate(),
		validateTrafficPolicy(subset.TrafficPolicy))
}

// ValidateDestinationPolicy checks proxy policies
func ValidateDestinationPolicy(msg proto.Message) error {
	policy, ok := msg.(*routing.DestinationPolicy)
	if !ok {
		return fmt.Errorf("cannot cast to destination policy")
	}

	var errs error
	if policy.Destination == nil {
		errs = multierror.Append(errs, errors.New("destination is required in the destination policy"))
	} else if err := ValidateIstioService(policy.Destination); err != nil {
		errs = multierror.Append(errs, err)
	}

	if policy.Source != nil {
		if err := ValidateIstioService(policy.Source); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if policy.LoadBalancing != nil {
		if err := ValidateLoadBalancing(policy.LoadBalancing); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if policy.CircuitBreaker != nil {
		if err := ValidateCircuitBreaker(policy.CircuitBreaker); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	return errs
}

// ValidateProxyAddress checks that a network address is well-formed
func ValidateProxyAddress(hostAddr string) error {
	colon := strings.Index(hostAddr, ":")
	if colon < 0 {
		return fmt.Errorf("':' separator not found in %q, host address must be of the form <DNS name>:<port> or <IP>:<port>",
			hostAddr)
	}
	port, err := strconv.Atoi(hostAddr[colon+1:])
	if err != nil {
		return err
	}
	if err = ValidatePort(port); err != nil {
		return err
	}
	host := hostAddr[:colon]
	if err = ValidateFQDN(host); err != nil {
		if err = ValidateIPv4Address(host); err != nil {
			return fmt.Errorf("%q is not a valid hostname or an IPv4 address", host)
		}
	}

	return nil
}

// ValidateDuration checks that a proto duration is well-formed
func ValidateDuration(pd *duration.Duration) error {
	dur, err := ptypes.Duration(pd)
	if err != nil {
		return err
	}
	if dur < time.Millisecond {
		return errors.New("duration must be greater than 1ms")
	}
	if dur%time.Millisecond != 0 {
		return errors.New("only durations to ms precision are supported")
	}
	return nil
}

// ValidateGogoDuration validates the gogoproto variant of duration.
func ValidateGogoDuration(in *types.Duration) error {
	return ValidateDuration(&duration.Duration{
		Seconds: in.Seconds,
		Nanos:   in.Nanos,
	})
}

// ValidateDurationRange verifies range is in specified duration
func ValidateDurationRange(dur, min, max time.Duration) error {
	if dur > max || dur < min {
		return fmt.Errorf("time %v must be >%v and <%v", dur.String(), min.String(), max.String())
	}

	return nil
}

// ValidateParentAndDrain checks that parent and drain durations are valid
func ValidateParentAndDrain(drainTime, parentShutdown *duration.Duration) (errs error) {
	if err := ValidateDuration(drainTime); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid drain duration:"))
	}
	if err := ValidateDuration(parentShutdown); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid parent shutdown duration:"))
	}
	if errs != nil {
		return
	}

	drainDuration, _ := ptypes.Duration(drainTime)
	parentShutdownDuration, _ := ptypes.Duration(parentShutdown)

	if drainDuration%time.Second != 0 {
		errs = multierror.Append(errs,
			errors.New("drain time only supports durations to seconds precision"))
	}
	if parentShutdownDuration%time.Second != 0 {
		errs = multierror.Append(errs,
			errors.New("parent shutdown time only supports durations to seconds precision"))
	}
	if parentShutdownDuration <= drainDuration {
		errs = multierror.Append(errs,
			fmt.Errorf("parent shutdown time %v must be greater than drain time %v",
				parentShutdownDuration.String(), drainDuration.String()))
	}

	if drainDuration > drainTimeMax {
		errs = multierror.Append(errs,
			fmt.Errorf("drain time %v must be <%v", drainDuration.String(), drainTimeMax.String()))
	}

	if parentShutdownDuration > parentShutdownTimeMax {
		errs = multierror.Append(errs,
			fmt.Errorf("parent shutdown time %v must be <%v",
				parentShutdownDuration.String(), parentShutdownTimeMax.String()))
	}

	return
}

// ValidateRefreshDelay validates the discovery refresh delay time
func ValidateRefreshDelay(refresh *duration.Duration) error {
	if err := ValidateDuration(refresh); err != nil {
		return err
	}

	refreshDuration, _ := ptypes.Duration(refresh)
	err := ValidateDurationRange(refreshDuration, discoveryRefreshDelayMin, discoveryRefreshDelayMax)
	return err
}

// ValidateConnectTimeout validates the envoy conncection timeout
func ValidateConnectTimeout(timeout *duration.Duration) error {
	if err := ValidateDuration(timeout); err != nil {
		return err
	}

	timeoutDuration, _ := ptypes.Duration(timeout)
	err := ValidateDurationRange(timeoutDuration, connectTimeoutMin, connectTimeoutMax)
	return err
}

// ValidateMeshConfig checks that the mesh config is well-formed
func ValidateMeshConfig(mesh *meshconfig.MeshConfig) (errs error) {
	if mesh.EgressProxyAddress != "" {
		if err := ValidateProxyAddress(mesh.EgressProxyAddress); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "invalid egress proxy address:"))
		}
	}

	if mesh.MixerAddress != "" {
		if err := ValidateProxyAddress(mesh.MixerAddress); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "invalid Mixer address:"))
		}
	}

	if err := ValidatePort(int(mesh.ProxyListenPort)); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid proxy listen port:"))
	}

	if err := ValidateConnectTimeout(mesh.ConnectTimeout); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid connect timeout:"))
	}

	switch mesh.AuthPolicy {
	case meshconfig.MeshConfig_NONE, meshconfig.MeshConfig_MUTUAL_TLS:
	default:
		errs = multierror.Append(errs, fmt.Errorf("unrecognized auth policy %q", mesh.AuthPolicy))
	}

	if err := ValidateRefreshDelay(mesh.RdsRefreshDelay); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid refresh delay:"))
	}

	if mesh.DefaultConfig == nil {
		errs = multierror.Append(errs, errors.New("missing default config"))
	} else if err := ValidateProxyConfig(mesh.DefaultConfig); err != nil {
		errs = multierror.Append(errs, err)
	}

	return
}

// ValidateProxyConfig checks that the mesh config is well-formed
func ValidateProxyConfig(config *meshconfig.ProxyConfig) (errs error) {
	if config.ConfigPath == "" {
		errs = multierror.Append(errs, errors.New("config path must be set"))
	}

	if config.BinaryPath == "" {
		errs = multierror.Append(errs, errors.New("binary path must be set"))
	}

	if config.ServiceCluster == "" {
		errs = multierror.Append(errs, errors.New("service cluster must be set"))
	}

	if err := ValidateParentAndDrain(config.DrainDuration, config.ParentShutdownDuration); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid parent and drain time combination"))
	}

	if err := ValidateRefreshDelay(config.DiscoveryRefreshDelay); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid refresh delay:"))
	}

	// discovery address is mandatory since mutual TLS relies on CDS.
	// strictly speaking, proxies can operate without RDS/CDS and with hot restarts
	// but that requires additional test validation
	if config.DiscoveryAddress == "" {
		errs = multierror.Append(errs, errors.New("discovery address must be set to the proxy discovery service"))
	} else if err := ValidateProxyAddress(config.DiscoveryAddress); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid discovery address:"))
	}

	if config.ZipkinAddress != "" {
		if err := ValidateProxyAddress(config.ZipkinAddress); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "invalid zipkin address:"))
		}
	}

	if err := ValidateConnectTimeout(config.ConnectTimeout); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid connect timeout:"))
	}

	if config.StatsdUdpAddress != "" {
		if err := ValidateProxyAddress(config.StatsdUdpAddress); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "invalid statsd udp address:"))
		}
	}

	if err := ValidatePort(int(config.ProxyAdminPort)); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid proxy admin port:"))
	}

	switch config.ControlPlaneAuthPolicy {
	case meshconfig.AuthenticationPolicy_NONE, meshconfig.AuthenticationPolicy_MUTUAL_TLS:
	default:
		errs = multierror.Append(errs,
			fmt.Errorf("unrecognized control plane auth policy %q", config.ControlPlaneAuthPolicy))
	}

	return
}

// ValidateMixerAttributes checks that Mixer attributes is
// well-formed.
func ValidateMixerAttributes(msg proto.Message) error {
	in, ok := msg.(*mpb.Attributes)
	if !ok {
		return errors.New("cannot case to attributes")
	}
	if in == nil || len(in.Attributes) == 0 {
		return errors.New("list of attributes is nil/empty")
	}
	var errs error
	for k, v := range in.Attributes {
		switch val := v.Value.(type) {
		case *mpb.Attributes_AttributeValue_StringValue:
			if val.StringValue == "" {
				errs = multierror.Append(errs,
					fmt.Errorf("string attribute for %q should not be empty", k))
			}
		case *mpb.Attributes_AttributeValue_DurationValue:
			if val.DurationValue == nil {
				errs = multierror.Append(errs,
					fmt.Errorf("duration attribute for %q should not be nil", k))
			}
			if err := ValidateGogoDuration(val.DurationValue); err != nil {
				errs = multierror.Append(errs, err)
			}
		case *mpb.Attributes_AttributeValue_BytesValue:
			if len(val.BytesValue) == 0 {
				errs = multierror.Append(errs,
					fmt.Errorf("bytes attribute for %q should not be ", k))
			}
		case *mpb.Attributes_AttributeValue_TimestampValue:
			if val.TimestampValue == nil {
				errs = multierror.Append(errs,
					fmt.Errorf("timestamp attribute for %q should not be nil", k))
			}
			if _, err := types.TimestampFromProto(val.TimestampValue); err != nil {
				errs = multierror.Append(errs, err)
			}
		case *mpb.Attributes_AttributeValue_StringMapValue:
			if val.StringMapValue == nil || val.StringMapValue.Entries == nil {
				errs = multierror.Append(errs,
					fmt.Errorf("stringmap attribute for %q should not be nil", k))
			}
		}
	}
	return errs
}

// ValidateHTTPAPISpec checks that HTTPAPISpec is well-formed.
func ValidateHTTPAPISpec(msg proto.Message) error {
	in, ok := msg.(*mccpb.HTTPAPISpec)
	if !ok {
		return errors.New("cannot case to HTTPAPISpec")
	}
	var errs error
	// top-level list of attributes is optional
	if in.Attributes != nil {
		if err := ValidateMixerAttributes(in.Attributes); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	if len(in.Patterns) == 0 {
		errs = multierror.Append(errs, errors.New("at least one pattern must be specified"))
	}
	for _, pattern := range in.Patterns {
		if err := ValidateMixerAttributes(in.Attributes); err != nil {
			errs = multierror.Append(errs, err)
		}
		if pattern.HttpMethod == "" {
			errs = multierror.Append(errs, errors.New("http_method cannot be empty"))
		}
		switch m := pattern.Pattern.(type) {
		case *mccpb.HTTPAPISpecPattern_UriTemplate:
			if m.UriTemplate == "" {
				errs = multierror.Append(errs, errors.New("uri_template cannot be empty"))
			}
		case *mccpb.HTTPAPISpecPattern_Regex:
			if m.Regex == "" {
				errs = multierror.Append(errs, errors.New("regex cannot be empty"))
			}
		}
	}
	for _, key := range in.ApiKeys {
		switch m := key.Key.(type) {
		case *mccpb.APIKey_Query:
			if m.Query == "" {
				errs = multierror.Append(errs, errors.New("query cannot be empty"))
			}
		case *mccpb.APIKey_Header:
			if m.Header == "" {
				errs = multierror.Append(errs, errors.New("header cannot be empty"))
			}
		case *mccpb.APIKey_Cookie:
			if m.Cookie == "" {
				errs = multierror.Append(errs, errors.New("cookie cannot be empty"))
			}
		}
	}
	return errs
}

// ValidateHTTPAPISpecBinding checks that HTTPAPISpecBinding is well-formed.
func ValidateHTTPAPISpecBinding(msg proto.Message) error {
	in, ok := msg.(*mccpb.HTTPAPISpecBinding)
	if !ok {
		return errors.New("cannot case to HTTPAPISpecBinding")
	}
	var errs error
	if len(in.Services) == 0 {
		errs = multierror.Append(errs, errors.New("at least one service must be specified"))
	}
	for _, service := range in.Services {
		if err := ValidateIstioService(mixerToProxyIstioService(service)); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	if len(in.ApiSpecs) == 0 {
		errs = multierror.Append(errs, errors.New("at least one spec must be specified"))
	}
	for _, spec := range in.ApiSpecs {
		if spec.Name == "" {
			errs = multierror.Append(errs, errors.New("name is mandatory for HTTPAPISpecReference"))
		}
		if spec.Namespace != "" && !IsDNS1123Label(spec.Namespace) {
			errs = multierror.Append(errs, fmt.Errorf("namespace %q must be a valid label", spec.Namespace))
		}
	}
	return errs
}

// ValidateQuotaSpec checks that Quota is well-formed.
func ValidateQuotaSpec(msg proto.Message) error {
	in, ok := msg.(*mccpb.QuotaSpec)
	if !ok {
		return errors.New("cannot case to HTTPAPISpecBinding")
	}
	var errs error
	if len(in.Rules) == 0 {
		errs = multierror.Append(errs, errors.New("a least one rule must be specified"))
	}
	for _, rule := range in.Rules {
		for _, match := range rule.Match {
			for name, clause := range match.Clause {
				switch matchType := clause.MatchType.(type) {
				case *mccpb.StringMatch_Exact:
					if matchType.Exact == "" {
						errs = multierror.Append(errs,
							fmt.Errorf("StringMatch_Exact for attribute %q cannot be empty", name)) // nolint: golint
					}
				case *mccpb.StringMatch_Prefix:
					if matchType.Prefix == "" {
						errs = multierror.Append(errs,
							fmt.Errorf("StringMatch_Prefix for attribute %q cannot be empty", name)) // nolint: golint
					}
				case *mccpb.StringMatch_Regex:
					if matchType.Regex == "" {
						errs = multierror.Append(errs,
							fmt.Errorf("StringMatch_Regex for attribute %q cannot be empty", name)) // nolint: golint
					}
				}
			}
		}
		if len(rule.Quotas) == 0 {
			errs = multierror.Append(errs, errors.New("a least one quota must be specified"))
		}
		for _, quota := range rule.Quotas {
			if quota.Quota == "" {
				errs = multierror.Append(errs, errors.New("quota name cannot be empty"))
			}
			if quota.Charge <= 0 {
				errs = multierror.Append(errs, errors.New("quota charge amount must be positive"))
			}
		}
	}
	return errs
}

// ValidateQuotaSpecBinding checks that QuotaSpecBinding is well-formed.
func ValidateQuotaSpecBinding(msg proto.Message) error {
	in, ok := msg.(*mccpb.QuotaSpecBinding)
	if !ok {
		return errors.New("cannot case to HTTPAPISpecBinding")
	}
	var errs error
	if len(in.Services) == 0 {
		errs = multierror.Append(errs, errors.New("at least one service must be specified"))
	}
	for _, service := range in.Services {
		if err := ValidateIstioService(mixerToProxyIstioService(service)); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	if len(in.QuotaSpecs) == 0 {
		errs = multierror.Append(errs, errors.New("at least one spec must be specified"))
	}
	for _, spec := range in.QuotaSpecs {
		if spec.Name == "" {
			errs = multierror.Append(errs, errors.New("name is mandatory for QuotaSpecReference"))
		}
		if spec.Namespace != "" && !IsDNS1123Label(spec.Namespace) {
			errs = multierror.Append(errs, fmt.Errorf("namespace %q must be a valid label", spec.Namespace))
		}
	}
	return errs
}

// ValidateEndUserAuthenticationPolicySpec checks that EndUserAuthenticationPolicySpec is well-formed.
func ValidateEndUserAuthenticationPolicySpec(msg proto.Message) error {
	in, ok := msg.(*mccpb.EndUserAuthenticationPolicySpec)
	if !ok {
		return errors.New("cannot case to EndUserAuthenticationPolicySpec")
	}
	var errs error
	if len(in.Jwts) == 0 {
		errs = multierror.Append(errs, errors.New("at least one JWT must be specified"))
	}
	for _, jwt := range in.Jwts {
		if jwt.Issuer == "" {
			errs = multierror.Append(errs, errors.New("issuer must be set"))
		}
		for _, audience := range jwt.Audiences {
			if audience == "" {
				errs = multierror.Append(errs, errors.New("audience must be non-empty string"))
			}
		}
		if jwt.JwksUri == "" {
			errs = multierror.Append(errs, errors.New("jwks_uri must be set"))
		}
		if !strings.HasPrefix(jwt.JwksUri, "http://") && !strings.HasPrefix(jwt.JwksUri, "https://") {
			errs = multierror.Append(errs, errors.New("jwks_uri must have http:// or https:// scheme"))
		}
		if _, err := url.Parse(jwt.JwksUri); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("%q is not a valid url: %v", jwt.JwksUri, err))
		}

		for _, location := range jwt.Locations {
			switch l := location.Scheme.(type) {
			case *mccpb.JWT_Location_Header:
				if l.Header == "" {
					errs = multierror.Append(errs, errors.New("location header must be non-empty string"))
				}
			case *mccpb.JWT_Location_Query:
				if l.Query == "" {
					errs = multierror.Append(errs, errors.New("location query must be non-empty string"))
				}
			}
		}
		if jwt.PublicKeyCacheDuration != nil {
			if err := ValidateGogoDuration(jwt.PublicKeyCacheDuration); err != nil {
				errs = multierror.Append(errs, err)
			}
		}
	}
	return errs
}

// ValidateEndUserAuthenticationPolicySpecBinding checks that EndUserAuthenticationPolicySpecBinding is well-formed.
func ValidateEndUserAuthenticationPolicySpecBinding(msg proto.Message) error {
	in, ok := msg.(*mccpb.EndUserAuthenticationPolicySpecBinding)
	if !ok {
		return errors.New("cannot case to EndUserAuthenticationPolicySpecBinding")
	}
	var errs error
	if len(in.Services) == 0 {
		errs = multierror.Append(errs, errors.New("at least one service must be specified"))
	}
	for _, service := range in.Services {
		if err := ValidateIstioService(mixerToProxyIstioService(service)); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	if len(in.Policies) == 0 {
		errs = multierror.Append(errs, errors.New("at least one policy must be specified"))
	}
	for _, policy := range in.Policies {
		if policy.Name == "" {
			errs = multierror.Append(errs, errors.New("name is mandatory for EndUserAuthenticationPolicySpecReference"))
		}
		if policy.Namespace != "" && !IsDNS1123Label(policy.Namespace) {
			errs = multierror.Append(errs, fmt.Errorf("namespace %q must be a valid label", policy.Namespace))
		}
	}
	return errs
}

// ValidateRouteRuleV2 checks that a v1alpha2 route rule is well-formed.
func ValidateRouteRuleV2(msg proto.Message) (errs error) {
	routeRule, ok := msg.(*routingv2.RouteRule)
	if !ok {
		return errors.New("cannot cast to v1alpha2 routing rule")
	}

	// TODO: routeRule.Gateways

	if len(routeRule.Hosts) == 0 {
		errs = appendErrors(errs, fmt.Errorf("route rule must have at least one host"))
	}
	for _, host := range routeRule.Hosts {
		errs = appendErrors(errs, validateHost(host))
	}

	for _, httpRoute := range routeRule.Http {
		errs = appendErrors(validateHTTPRoute(httpRoute))
	}

	// TODO: validate once implemented
	if len(routeRule.Tcp) > 0 {
		errs = appendErrors(errs, errors.New("TCP route rules have not been implemented"))
	}

	return
}

func validateHost(host string) error {
	// We check if its a valid wildcard domain first; if not then we check if its a valid IPv4 address
	// (including CIDR addresses). If it's neither, we report both errors.
	if err := ValidateWildcardDomain(host); err != nil {
		if err2 := ValidateIPv4Subnet(host); err2 != nil {
			return appendErrors(err, err2)
		}
	}
	return nil
}

func validateHTTPRoute(http *routingv2.HTTPRoute) (errs error) {
	// check for conflicts
	if http.Redirect != nil {
		if len(http.Route) > 0 {
			errs = appendErrors(errs, errors.New("HTTP route cannot contain both route and redirect"))
		}

		if http.Fault != nil {
			errs = appendErrors(errs, errors.New("HTTP route cannot contain both fault and redirect"))
		}

		if http.Rewrite != nil {
			errs = appendErrors(errs, errors.New("HTTP route rule cannot contain both rewrite and redirect"))
		}

		if http.WebsocketUpgrade {
			errs = appendErrors(errs, errors.New("WebSocket upgrade is not allowed on redirect rules")) // nolint: golint
		}
	}

	for name := range http.AppendHeaders {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}
	errs = appendErrors(errs, validateCORSPolicy(http.CorsPolicy))
	errs = appendErrors(errs, validateHTTPFaultInjection(http.Fault))

	for _, match := range http.Match {
		for name := range match.Headers {
			errs = appendErrors(errs, ValidateHTTPHeaderName(name))
		}

		// TODO: validate once implemented
		if match.Port != nil {
			errs = appendErrors(errs, errors.New("HTTP match port has not been implemented"))
		}

		errs = appendErrors(errs, Labels(match.SourceLabels).Validate())
	}
	errs = appendErrors(errs, validateDestination(http.Mirror))
	errs = appendErrors(errs, validateHTTPRedirect(http.Redirect))
	errs = appendErrors(errs, validateHTTPRetry(http.Retries))
	errs = appendErrors(errs, validateHTTPRewrite(http.Rewrite))
	for _, route := range http.Route {
		if route.Destination == nil {
			errs = multierror.Append(errs, errors.New("destination is required"))
		}
		errs = appendErrors(errs, validateDestination(route.Destination))
		errs = appendErrors(errs, ValidatePercent(route.Weight))
	}
	if http.Timeout != nil {
		errs = appendErrors(errs, ValidateDuration(http.Timeout))
	}

	return
}

func validateCORSPolicy(policy *routingv2.CorsPolicy) (errs error) {
	if policy == nil {
		return
	}

	// TODO: additional validation for AllowOrigin?

	for _, method := range policy.AllowMethods {
		errs = appendErrors(errs, validateHTTPMethod(method))
	}

	for _, name := range policy.AllowHeaders {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}

	for _, name := range policy.ExposeHeaders {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}

	if policy.MaxAge != nil {
		errs = appendErrors(errs, ValidateDuration(policy.MaxAge))
		if policy.MaxAge.Nanos > 0 {
			errs = multierror.Append(errs, errors.New("max_age duration is accurate only to seconds precision"))
		}
	}

	// TODO: additional validation for AllowCredentials?

	return
}

func validateHTTPMethod(method string) error {
	if !supportedMethods[method] {
		return fmt.Errorf("%q is not a supported HTTP method", method)
	}
	return nil
}

func validateHTTPFaultInjection(fault *routingv2.HTTPFaultInjection) (errs error) {
	if fault == nil {
		return
	}

	if fault.Abort == nil && fault.Delay == nil {
		errs = multierror.Append(errs, errors.New("HTTP fault injection must have an abort and/or a delay"))
	}

	errs = appendErrors(errs, validateHTTPFaultInjectionAbort(fault.Abort))
	errs = appendErrors(errs, validateHTTPFaultInjectionDelay(fault.Delay))

	return
}

func validateHTTPFaultInjectionAbort(abort *routingv2.HTTPFaultInjection_Abort) (errs error) {
	if abort == nil {
		return
	}

	errs = appendErrors(errs, ValidatePercent(abort.Percent))

	switch abort.ErrorType.(type) {
	case *routingv2.HTTPFaultInjection_Abort_GrpcStatus:
		// TODO: gRPC status validation
		errs = multierror.Append(errs, errors.New("gRPC abort fault injection not supported yet"))
	case *routingv2.HTTPFaultInjection_Abort_Http2Error:
		// TODO: HTTP2 error validation
		errs = multierror.Append(errs, errors.New("HTTP/2 abort fault injection not supported yet"))
	case *routingv2.HTTPFaultInjection_Abort_HttpStatus:
		errs = appendErrors(errs, validateHTTPStatus(abort.GetHttpStatus()))
	}

	return
}

func validateHTTPStatus(status int32) error {
	if status < 0 || status > 600 {
		return fmt.Errorf("HTTP status %d is not in range 0-600", status)
	}
	return nil
}

func validateHTTPFaultInjectionDelay(delay *routingv2.HTTPFaultInjection_Delay) (errs error) {
	if delay == nil {
		return
	}

	errs = appendErrors(errs, ValidatePercent(delay.Percent))
	switch v := delay.HttpDelayType.(type) {
	case *routingv2.HTTPFaultInjection_Delay_FixedDelay:
		errs = appendErrors(errs, ValidateDuration(v.FixedDelay))
	case *routingv2.HTTPFaultInjection_Delay_ExponentialDelay:
		errs = appendErrors(errs, ValidateDuration(v.ExponentialDelay))
		errs = multierror.Append(errs, fmt.Errorf("exponentialDelay not supported yet"))
	}
	return
}

func validateDestination(destination *routingv2.Destination) (errs error) {
	if destination == nil {
		return
	}

	errs = appendErrors(errs, validateHost(destination.Name))
	if destination.Subset != "" {
		errs = appendErrors(errs, validateSubsetName(destination.Subset))
	}
	if destination.Port != nil {
		errs = appendErrors(errs, validatePortSelector(destination.Port))
	}

	return
}

func validateSubsetName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("subset name cannot be empty")
	}
	if !IsDNS1123Label(name) {
		return fmt.Errorf("subnet name is invalid: %s", name)
	}
	return nil
}

func validatePortSelector(selector *routingv2.PortSelector) error {
	if selector == nil {
		return nil
	}

	// port selector is either a name or a number
	name := selector.GetName()
	number := int(selector.GetNumber())
	if name == "" && number == 0 {
		// an unset value is indistinguishable from a zero value, so return both errors
		return appendErrors(validateSubsetName(name), ValidatePort(number))
	} else if number != 0 {
		return ValidatePort(number)
	}
	return validateSubsetName(name)
}

func validateHTTPRetry(retries *routingv2.HTTPRetry) (errs error) {
	if retries == nil {
		return
	}

	if retries.Attempts <= 0 {
		errs = multierror.Append(errs, errors.New("attempts must be positive"))
	}
	if retries.PerTryTimeout != nil {
		errs = appendErrors(errs, ValidateDuration(retries.PerTryTimeout))
	}
	return
}

func validateHTTPRedirect(redirect *routingv2.HTTPRedirect) error {
	if redirect != nil && redirect.Uri == "" && redirect.Authority == "" {
		return errors.New("redirect must specify URI, authority, or both")
	}
	return nil
}

func validateHTTPRewrite(rewrite *routingv2.HTTPRewrite) error {
	if rewrite != nil && rewrite.Uri == "" && rewrite.Authority == "" {
		return errors.New("rewrite must specify URI, authority, or both")
	}
	return nil
}

// ValidateExternalService validates a external service.
func ValidateExternalService(config proto.Message) (errs error) {
	externalService, ok := config.(*routingv2.ExternalService)
	if !ok {
		return fmt.Errorf("cannot cast to external service")
	}

	if len(externalService.Hosts) == 0 {
		errs = appendErrors(errs, fmt.Errorf("external service must have at least one host"))
	}
	for _, host := range externalService.Hosts {
		errs = appendErrors(errs, validateHost(host))
	}

	servicePortNumbers := make(map[uint32]bool)
	servicePorts := make(map[string]bool, len(externalService.Ports))
	for _, port := range externalService.Ports {
		if servicePorts[port.Name] {
			errs = appendErrors(errs, fmt.Errorf("external service port name %q already defined", port.Name))
		}
		servicePorts[port.Name] = true
		if servicePortNumbers[port.Number] {
			errs = appendErrors(errs, fmt.Errorf("external service port %d already defined", port.Number))
		}
		servicePortNumbers[port.Number] = true
	}

	switch externalService.Discovery {
	case routingv2.ExternalService_NONE:
		if len(externalService.Endpoints) != 0 {
			errs = appendErrors(errs, fmt.Errorf("no endpoints should be provided for discovery type none"))
		}
	case routingv2.ExternalService_STATIC:
		if len(externalService.Endpoints) == 0 {
			errs = appendErrors(errs,
				fmt.Errorf("endpoints must be provided if external service discovery mode is static"))
		}

		for _, endpoint := range externalService.Endpoints {
			errs = appendErrors(errs,
				ValidateIPv4Address(endpoint.Address),
				Labels(endpoint.Labels).Validate())

			for name, port := range endpoint.Ports {
				if !servicePorts[name] {
					errs = appendErrors(errs, fmt.Errorf("endpoint port %v is not defined by the external service", port))
				}
				errs = appendErrors(errs,
					validatePortName(name),
					ValidatePort(int(port)))
			}
		}
	case routingv2.ExternalService_DNS:
		if len(externalService.Endpoints) == 0 {
			for _, host := range externalService.Hosts {
				if err := ValidateFQDN(host); err != nil {
					errs = appendErrors(errs,
						fmt.Errorf("hosts must be FQDN if no endpoints are provided for discovery mode DNS"))
				}
			}
		}

		for _, endpoint := range externalService.Endpoints {
			errs = appendErrors(errs,
				ValidateFQDN(endpoint.Address),
				Labels(endpoint.Labels).Validate())

			for name, port := range endpoint.Ports {
				if !servicePorts[name] {
					errs = appendErrors(errs, fmt.Errorf("endpoint port %v is not defined by the external service", port))
				}
				errs = appendErrors(errs,
					validatePortName(name),
					ValidatePort(int(port)))
			}
		}
	default:
		errs = appendErrors(errs, fmt.Errorf("unsupported discovery type %s",
			routingv2.ExternalService_Discovery_name[int32(externalService.Discovery)]))
	}

	for _, port := range externalService.Ports {
		errs = appendErrors(errs,
			validatePortName(port.Name),
			validateProtocol(port.Protocol),
			ValidatePort(int(port.Number)))
	}

	return
}

func validatePortName(name string) error {
	if !IsDNS1123Label(name) {
		return fmt.Errorf("invalid port name: %s", name)
	}
	return nil
}

func validateProtocol(protocol string) error {
	if ConvertCaseInsensitiveStringToProtocol(protocol) == ProtocolUnsupported {
		return fmt.Errorf("unsupported protocol: %s", protocol)
	}
	return nil
}

// wrapper around multierror.Append that enforces the invariant that if all input errors are nil, the output
// error is nil (allowing validation without branching).
func appendErrors(err error, errs ...error) error {
	appendError := func(err, err2 error) error {
		if err == nil {
			return err2
		} else if err2 == nil {
			return err
		}
		return multierror.Append(err, err2)
	}

	for _, err2 := range errs {
		err = appendError(err, err2)
	}
	return err
}
