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
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	aspenmeshconfig "github.com/aspenmesh/aspenmesh-crd/pkg/apis/config/v1alpha1"
	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
	multierror "github.com/hashicorp/go-multierror"

	authn "istio.io/api/authentication/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"
	networking "istio.io/api/networking/v1alpha3"
	rbac "istio.io/api/rbac/v1alpha1"
)

const (
	dns1123LabelMaxLength int    = 63
	dns1123LabelFmt       string = "[a-zA-Z0-9]([-a-z-A-Z0-9]*[a-zA-Z0-9])?"
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

// UnixAddressPrefix is the prefix used to indicate an address is for a Unix Domain socket. It is used in
// ServiceEntry.Endpoint.Address message.
const UnixAddressPrefix = "unix://"

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
	clusterMessages := make(map[string]bool)

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
		if v.ClusterScoped {
			if _, exists := clusterMessages[v.MessageName]; exists {
				errs = multierror.Append(errs, fmt.Errorf("duplicate message type: %q", v.MessageName))
			}
			clusterMessages[v.MessageName] = true
		} else {
			if _, exists := messages[v.MessageName]; exists {
				errs = multierror.Append(errs, fmt.Errorf("duplicate message type: %q", v.MessageName))
			}
			messages[v.MessageName] = true
		}
	}
	return errs
}

// Validate ensures that the service object is well-defined
func (s *Service) Validate() error {
	var errs error
	if len(s.Hostname) == 0 {
		errs = multierror.Append(errs, fmt.Errorf("invalid empty hostname"))
	}
	parts := strings.Split(s.Hostname.String(), ".")
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

// ValidateMixerService checks for validity of a service reference
func ValidateMixerService(svc *mccpb.IstioService) (errs error) {
	if svc.Name == "" && svc.Service == "" {
		errs = multierror.Append(errs, errors.New("name or service is mandatory for a service reference"))
	} else if svc.Service != "" && svc.Name != "" {
		errs = multierror.Append(errs, errors.New("specify either name or service, not both"))
	} else if svc.Service != "" {
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

// ValidatePercent checks that percent is in range
func ValidatePercent(val int32) error {
	if val < 0 || val > 100 {
		return fmt.Errorf("percentage %v is not in range 0..100", val)
	}
	return nil
}

// ValidateIPv4Subnet checks that a string is in "CIDR notation" or "Dot-decimal notation"
func ValidateIPv4Subnet(subnet string) error {
	// We expect a string in "CIDR notation" or "Dot-decimal notation"
	// E.g., a.b.c.d/xx form or just a.b.c.d
	if strings.Count(subnet, "/") == 1 {
		// We expect a string in "CIDR notation", i.e. a.b.c.d/xx form
		ip, _, err := net.ParseCIDR(subnet)
		if err != nil {
			return fmt.Errorf("%v is not a valid CIDR block", subnet)
		}
		// The current implementation only supports IP v4 addresses
		if ip.To4() == nil {
			return fmt.Errorf("%v is not a valid IPv4 address", subnet)
		}
		return nil
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

// ValidateUnixAddress validates that the string is a valid unix domain socket path.
func ValidateUnixAddress(addr string) error {
	if len(addr) == 0 {
		return errors.New("unix address must not be empty")
	}
	// Note that we use path, not path/filepath even though a domain socket path is a file path.  We don't want the
	// Pilot output to depend on which OS Pilot is run on, so we always use Unix-style forward slashes.
	if !path.IsAbs(addr) {
		return fmt.Errorf("%s is not an absolute path", addr)
	}
	return nil
}

// ValidateGateway checks gateway specifications
func ValidateGateway(name, namespace string, msg proto.Message) (errs error) {
	value, ok := msg.(*networking.Gateway)
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

	// Ensure unique port names
	portNames := make(map[string]bool)

	for _, s := range value.Servers {
		if portNames[s.Port.Name] {
			errs = appendErrors(errs, fmt.Errorf("port names in servers must be unique: duplicate name %s", s.Port.Name))
		}
		portNames[s.Port.Name] = true
	}

	return errs
}

func validateServer(server *networking.Server) (errs error) {
	if len(server.Hosts) == 0 {
		errs = appendErrors(errs, fmt.Errorf("server config must contain at least one host"))
	} else {
		for _, host := range server.Hosts {
			// short name hosts are not allowed in gateways
			if host != "*" && !strings.Contains(host, ".") {
				errs = appendErrors(errs, fmt.Errorf("short names (non FQDN) are not allowed in Gateway server hosts"))
			}
			if err := ValidateWildcardDomain(host); err != nil {
				errs = appendErrors(errs, err)
			}
		}
	}
	portErr := validateServerPort(server.Port)
	if portErr != nil {
		errs = appendErrors(errs, portErr)
	}
	errs = appendErrors(errs, validateTLSOptions(server.Tls))

	// If port is HTTPS or TLS, make sure that server has TLS options
	if portErr == nil {
		protocol := ParseProtocol(server.Port.Protocol)
		if protocol.IsTLS() && server.Tls == nil {
			errs = appendErrors(errs, fmt.Errorf("server must have TLS settings for HTTPS/TLS protocols"))
		} else if !protocol.IsTLS() && server.Tls != nil {
			// only tls redirect is allowed if this is a HTTP server
			if protocol.IsHTTP() {
				if server.Tls.Mode != networking.Server_TLSOptions_PASSTHROUGH ||
					server.Tls.CaCertificates != "" || server.Tls.PrivateKey != "" || server.Tls.ServerCertificate != "" {
					errs = appendErrors(errs, fmt.Errorf("server cannot have TLS settings for plain text HTTP ports"))
				}
			} else {
				errs = appendErrors(errs, fmt.Errorf("server cannot have TLS settings for non HTTPS/TLS ports"))
			}
		}
	}
	return errs
}

func validateServerPort(port *networking.Port) (errs error) {
	if port == nil {
		return appendErrors(errs, fmt.Errorf("port is required"))
	}
	if ParseProtocol(port.Protocol) == ProtocolUnsupported {
		errs = appendErrors(errs, fmt.Errorf("invalid protocol %q, supported protocols are HTTP, HTTP2, GRPC, MONGO, REDIS, TCP", port.Protocol))
	}
	if port.Number > 0 {
		errs = appendErrors(errs, ValidatePort(int(port.Number)))
	}

	if port.Name == "" {
		errs = appendErrors(errs, fmt.Errorf("port name must be set: %v", port))
	}
	return
}

func validateTLSOptions(tls *networking.Server_TLSOptions) (errs error) {
	if tls == nil {
		// no tls config at all is valid
		return
	}
	if tls.Mode == networking.Server_TLSOptions_SIMPLE {
		if tls.ServerCertificate == "" {
			errs = appendErrors(errs, fmt.Errorf("SIMPLE TLS requires a server certificate"))
		}
	} else if tls.Mode == networking.Server_TLSOptions_MUTUAL {
		if tls.ServerCertificate == "" {
			errs = appendErrors(errs, fmt.Errorf("MUTUAL TLS requires a server certificate"))
		}
		if tls.CaCertificates == "" {
			errs = appendErrors(errs, fmt.Errorf("MUTUAL TLS requires a client CA bundle"))
		}
	}
	return
}

// ValidateDestinationRule checks proxy policies
func ValidateDestinationRule(name, namespace string, msg proto.Message) (errs error) {
	rule, ok := msg.(*networking.DestinationRule)
	if !ok {
		return fmt.Errorf("cannot cast to destination rule")
	}

	errs = appendErrors(errs,
		ValidateWildcardDomain(rule.Host),
		validateTrafficPolicy(rule.TrafficPolicy))

	for _, subset := range rule.Subsets {
		errs = appendErrors(errs, validateSubset(subset))
	}

	return
}

// ValidateEnvoyFilter checks envoy filter config supplied by user
func ValidateEnvoyFilter(name, namespace string, msg proto.Message) (errs error) {
	rule, ok := msg.(*networking.EnvoyFilter)
	if !ok {
		return fmt.Errorf("cannot cast to envoy filter")
	}

	if len(rule.Filters) == 0 {
		return fmt.Errorf("envoy filter: missing filters")
	}

	for _, f := range rule.Filters {
		if f.InsertPosition != nil {
			if f.InsertPosition.Index == networking.EnvoyFilter_InsertPosition_BEFORE ||
				f.InsertPosition.Index == networking.EnvoyFilter_InsertPosition_AFTER {
				if f.InsertPosition.RelativeTo == "" {
					errs = appendErrors(errs, fmt.Errorf("envoy filter: missing relativeTo filter with BEFORE/AFTER index"))
				}
			}
		}
		if f.FilterType == networking.EnvoyFilter_Filter_INVALID {
			errs = appendErrors(errs, fmt.Errorf("envoy filter: missing filter type"))
		}
		if len(f.FilterName) == 0 {
			errs = appendErrors(errs, fmt.Errorf("envoy filter: missing filter name"))
		}

		if f.FilterConfig == nil {
			errs = appendErrors(errs, fmt.Errorf("envoy filter: missing filter config"))
		}
	}

	return
}

func validateTrafficPolicy(policy *networking.TrafficPolicy) error {
	if policy == nil {
		return nil
	}
	if policy.OutlierDetection == nil && policy.ConnectionPool == nil &&
		policy.LoadBalancer == nil && policy.Tls == nil && policy.PortLevelSettings == nil {
		return fmt.Errorf("traffic policy must have at least one field")
	}

	return appendErrors(validateOutlierDetection(policy.OutlierDetection),
		validateConnectionPool(policy.ConnectionPool),
		validateLoadBalancer(policy.LoadBalancer),
		validateTLS(policy.Tls), validatePortTrafficPolicies(policy.PortLevelSettings))
}

func validateOutlierDetection(outlier *networking.OutlierDetection) (errs error) {
	if outlier == nil {
		return
	}

	if outlier.BaseEjectionTime != nil {
		errs = appendErrors(errs, ValidateDurationGogo(outlier.BaseEjectionTime))
	}
	if outlier.ConsecutiveErrors < 0 {
		errs = appendErrors(errs, fmt.Errorf("outlier detection consecutive errors cannot be negative"))
	}
	if outlier.Interval != nil {
		errs = appendErrors(errs, ValidateDurationGogo(outlier.Interval))
	}
	errs = appendErrors(errs, ValidatePercent(outlier.MaxEjectionPercent))

	return
}

func validateConnectionPool(settings *networking.ConnectionPoolSettings) (errs error) {
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
			errs = appendErrors(errs, ValidateDurationGogo(tcp.ConnectTimeout))
		}
	}

	return
}

func validateLoadBalancer(settings *networking.LoadBalancerSettings) (errs error) {
	if settings == nil {
		return
	}

	// simple load balancing is always valid
	// TODO: settings.GetConsistentHash()

	return
}

func validateTLS(settings *networking.TLSSettings) (errs error) {
	if settings == nil {
		return
	}

	if settings.Mode == networking.TLSSettings_MUTUAL {
		if settings.ClientCertificate == "" {
			errs = appendErrors(errs, fmt.Errorf("client certificate required for mutual tls"))
		}
		if settings.PrivateKey == "" {
			errs = appendErrors(errs, fmt.Errorf("private key required for mutual tls"))
		}
	}

	return
}

func validateSubset(subset *networking.Subset) error {
	return appendErrors(validateSubsetName(subset.Name),
		Labels(subset.Labels).Validate(),
		validateTrafficPolicy(subset.TrafficPolicy))
}

func validatePortTrafficPolicies(pls []*networking.TrafficPolicy_PortTrafficPolicy) (errs error) {
	for _, t := range pls {
		if t.Port == nil {
			errs = appendErrors(errs, fmt.Errorf("portTrafficPolicy must have valid port"))
		}
		if t.OutlierDetection == nil && t.ConnectionPool == nil &&
			t.LoadBalancer == nil && t.Tls == nil {
			errs = appendErrors(errs, fmt.Errorf("port traffic policy must have at least one field"))
		} else {
			errs = appendErrors(errs, validateOutlierDetection(t.OutlierDetection),
				validateConnectionPool(t.ConnectionPool),
				validateLoadBalancer(t.LoadBalancer),
				validateTLS(t.Tls))
		}
	}
	return
}

// ValidateProxyAddress checks that a network address is well-formed
func ValidateProxyAddress(hostAddr string) error {
	host, p, err := net.SplitHostPort(hostAddr)
	if err != nil {
		return fmt.Errorf("unable to split %q: %v", hostAddr, err)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return fmt.Errorf("port (%s) is not a number: %v", p, err)
	}
	if err = ValidatePort(port); err != nil {
		return err
	}
	if err = ValidateFQDN(host); err != nil {
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("%q is not a valid hostname or an IP address", host)
		}
	}

	return nil
}

// ValidateDurationGogo checks that a gogo proto duration is well-formed
func ValidateDurationGogo(pd *types.Duration) error {
	dur, err := types.DurationFromProto(pd)
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
	if mesh.MixerCheckServer != "" {
		if err := ValidateProxyAddress(mesh.MixerCheckServer); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "invalid Policy Check Server address:"))
		}
	}

	if mesh.MixerReportServer != "" {
		if err := ValidateProxyAddress(mesh.MixerReportServer); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "invalid Telemetry Server address:"))
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
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid rds refresh delay:"))
	}

	if err := ValidateRefreshDelay(mesh.SdsRefreshDelay); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid sds refresh delay:"))
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
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("invalid statsd udp address %q:", config.StatsdUdpAddress)))
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
func ValidateHTTPAPISpec(name, namespace string, msg proto.Message) error {
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
func ValidateHTTPAPISpecBinding(name, namespace string, msg proto.Message) error {
	in, ok := msg.(*mccpb.HTTPAPISpecBinding)
	if !ok {
		return errors.New("cannot case to HTTPAPISpecBinding")
	}
	var errs error
	if len(in.Services) == 0 {
		errs = multierror.Append(errs, errors.New("at least one service must be specified"))
	}
	for _, service := range in.Services {
		if err := ValidateMixerService(service); err != nil {
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
func ValidateQuotaSpec(name, namespace string, msg proto.Message) error {
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
func ValidateQuotaSpecBinding(name, namespace string, msg proto.Message) error {
	in, ok := msg.(*mccpb.QuotaSpecBinding)
	if !ok {
		return errors.New("cannot case to HTTPAPISpecBinding")
	}
	var errs error
	if len(in.Services) == 0 {
		errs = multierror.Append(errs, errors.New("at least one service must be specified"))
	}
	for _, service := range in.Services {
		if err := ValidateMixerService(service); err != nil {
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

// ValidateAuthenticationPolicy checks that AuthenticationPolicy is well-formed.
func ValidateAuthenticationPolicy(name, namespace string, msg proto.Message) error {
	// Empty namespace indicate policy is from cluster-scoped CRD.
	clusterScoped := namespace == ""
	in, ok := msg.(*authn.Policy)
	if !ok {
		return errors.New("cannot cast to AuthenticationPolicy")
	}
	var errs error

	if !clusterScoped {
		if len(in.Targets) == 0 && name != DefaultAuthenticationPolicyName {
			errs = appendErrors(errs, fmt.Errorf("authentication policy with no target rules  must be named %q, found %q",
				DefaultAuthenticationPolicyName, name))
		}
		if len(in.Targets) > 0 && name == DefaultAuthenticationPolicyName {
			errs = appendErrors(errs, fmt.Errorf("authentication policy with name %q must not have any target rules", name))
		}
		for _, target := range in.Targets {
			errs = appendErrors(errs, validateAuthNPolicyTarget(target))
		}
	} else {
		if name != DefaultAuthenticationPolicyName {
			errs = appendErrors(errs, fmt.Errorf("cluster-scoped authentication policy name must be %q, found %q",
				DefaultAuthenticationPolicyName, name))
		}
		if len(in.Targets) > 0 {
			errs = appendErrors(errs, fmt.Errorf("cluster-scoped authentication policy must not have targets"))
		}
	}

	jwtIssuers := make(map[string]bool)
	for _, method := range in.Peers {
		if jwt := method.GetJwt(); jwt != nil {
			if _, jwtExist := jwtIssuers[jwt.Issuer]; jwtExist {
				errs = appendErrors(errs, fmt.Errorf("jwt with issuer %q already defined", jwt.Issuer))
			} else {
				jwtIssuers[jwt.Issuer] = true
			}
			errs = appendErrors(errs, validateJwt(jwt))
		}
	}
	for _, method := range in.Origins {
		if _, jwtExist := jwtIssuers[method.Jwt.Issuer]; jwtExist {
			errs = appendErrors(errs, fmt.Errorf("jwt with issuer %q already defined", method.Jwt.Issuer))
		} else {
			jwtIssuers[method.Jwt.Issuer] = true
		}
		errs = appendErrors(errs, validateJwt(method.Jwt))
	}

	return errs
}

// ValidateServiceRole checks that ServiceRole is well-formed.
func ValidateServiceRole(name, namespace string, msg proto.Message) error {
	in, ok := msg.(*rbac.ServiceRole)
	if !ok {
		return errors.New("cannot cast to ServiceRole")
	}
	var errs error
	if len(in.Rules) == 0 {
		errs = appendErrors(errs, fmt.Errorf("at least 1 rule must be specified"))
	}
	for i, rule := range in.Rules {
		if len(rule.Services) == 0 {
			errs = appendErrors(errs, fmt.Errorf("at least 1 service must be specified for rule %d", i))
		}
		if len(rule.Methods) == 0 {
			errs = appendErrors(errs, fmt.Errorf("at least 1 method must be specified for rule %d", i))
		}
		for j, constraint := range rule.Constraints {
			if len(constraint.Key) == 0 {
				errs = appendErrors(errs, fmt.Errorf("key cannot be empty for constraint %d in rule %d", j, i))
			}
			if len(constraint.Values) == 0 {
				errs = appendErrors(errs, fmt.Errorf("at least 1 value must be specified for constraint %d in rule %d", j, i))
			}
		}
	}
	return errs
}

// ValidateServiceRoleBinding checks that ServiceRoleBinding is well-formed.
func ValidateServiceRoleBinding(name, namespace string, msg proto.Message) error {
	in, ok := msg.(*rbac.ServiceRoleBinding)
	if !ok {
		return errors.New("cannot cast to ServiceRoleBinding")
	}
	var errs error
	if len(in.Subjects) == 0 {
		errs = appendErrors(errs, fmt.Errorf("at least 1 subject must be specified"))
	}
	for i, subject := range in.Subjects {
		if len(subject.User) == 0 && len(subject.Group) == 0 && len(subject.Properties) == 0 {
			errs = appendErrors(errs, fmt.Errorf("at least 1 of user, group or properties must be specified for subject %d", i))
		}
	}
	if in.RoleRef == nil {
		errs = appendErrors(errs, fmt.Errorf("roleRef must be specified"))
	} else {
		expectKind := "ServiceRole"
		if in.RoleRef.Kind != expectKind {
			errs = appendErrors(errs, fmt.Errorf("kind set to %q, currently the only supported value is %q",
				in.RoleRef.Kind, expectKind))
		}
		if len(in.RoleRef.Name) == 0 {
			errs = appendErrors(errs, fmt.Errorf("name cannot be empty"))
		}
	}
	return errs
}

// ValidateRbacConfig checks that RbacConfig is well-formed.
func ValidateRbacConfig(name, namespace string, msg proto.Message) error {
	in, ok := msg.(*rbac.RbacConfig)
	if !ok {
		return errors.New("cannot cast to RbacConfig")
	}

	if name != DefaultRbacConfigName {
		return fmt.Errorf("rbacConfig has invalid name(%s), name must be %s", name, DefaultRbacConfigName)
	}

	if in.Mode == rbac.RbacConfig_ON_WITH_INCLUSION && in.Inclusion == nil {
		return errors.New("inclusion cannot be null (use 'inclusion: {}' for none)")
	}

	if in.Mode == rbac.RbacConfig_ON_WITH_EXCLUSION && in.Exclusion == nil {
		return errors.New("exclusion cannot be null (use 'exclusion: {}' for none)")
	}

	return nil
}

func isExperimentValid(exp *aspenmeshconfig.ExperimentSpec) (bool, error) {
	token := exp.GetToken()
	id := exp.GetToken()
	spec := exp.GetSpec()
	if len(token) == 0 || len(id) == 0 || spec == nil {
		return false, errors.New("empty token, id or spec is not allowed in Aspen Mesh experiment")
	}
	if spec.GetServices() == nil {
		return false, errors.New("empty service list is not allowed for Aspen Mesh experiment")
	}
	for _, ss := range spec.GetServices() {
		os := ss.GetOriginal()
		ds := ss.GetExperiment()
		if os == nil || ds == nil {
			return false, errors.New("original and experiment services must be specified in Aspen Mesh experiment")
		}
		if len(os.GetName()) == 0 ||
			len(os.GetNamespace()) == 0 ||
			len(ds.GetName()) == 0 ||
			len(ds.GetNamespace()) == 0 {
			return false, errors.New("name and namespace must be specified in Aspen Mesh experiment")
		}
	}
	return true, nil
}

// ValidateExperiment checks that AspenMeshExperiment is well-formed.
func ValidateExperiment(name, namespace string, msg proto.Message) error {
	exp, ok := msg.(*aspenmeshconfig.ExperimentSpec)
	if !ok {
		return errors.New("cannot cast to Aspen Mesh Experiment")
	}
	valid, err := isExperimentValid(exp)
	if !valid {
		return err
	}
	return nil
}

func validateJwt(jwt *authn.Jwt) (errs error) {
	if jwt == nil {
		return nil
	}
	if jwt.Issuer == "" {
		errs = multierror.Append(errs, errors.New("issuer must be set"))
	}
	for _, audience := range jwt.Audiences {
		if audience == "" {
			errs = multierror.Append(errs, errors.New("audience must be non-empty string"))
		}
	}
	if jwt.JwksUri != "" {
		// TODO: do more extensive check (e.g try to fetch JwksUri)
		if _, _, _, err := ParseJwksURI(jwt.JwksUri); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	for _, location := range jwt.JwtHeaders {
		if location == "" {
			errs = multierror.Append(errs, errors.New("location header must be non-empty string"))
		}
	}

	for _, location := range jwt.JwtParams {
		if location == "" {
			errs = multierror.Append(errs, errors.New("location query must be non-empty string"))
		}
	}
	return
}

func validateAuthNPolicyTarget(target *authn.TargetSelector) (errs error) {
	if target == nil {
		return
	}

	// AuthN policy target (host)name must be a shortname
	if !IsDNS1123Label(target.Name) {
		errs = multierror.Append(errs, fmt.Errorf("target name %q must be a valid label", target.Name))
	}

	for _, port := range target.Ports {
		errs = appendErrors(errs, validateAuthNPortSelector(port))
	}

	return
}

// ValidateVirtualService checks that a v1alpha3 route rule is well-formed.
func ValidateVirtualService(name, namespace string, msg proto.Message) (errs error) {
	virtualService, ok := msg.(*networking.VirtualService)
	if !ok {
		return errors.New("cannot cast to virtual service")
	}

	appliesToMesh := false
	if len(virtualService.Gateways) == 0 {
		appliesToMesh = true
	}

	for _, gateway := range virtualService.Gateways {
		if err := ValidateFQDN(gateway); err != nil {
			errs = appendErrors(errs, err)
		}
		if gateway == IstioMeshGateway {
			appliesToMesh = true
		}
	}

	if len(virtualService.Hosts) == 0 {
		errs = appendErrors(errs, fmt.Errorf("virtual service must have at least one host"))
	}

	allHostsValid := true
	for _, host := range virtualService.Hosts {
		if err := ValidateWildcardDomain(host); err != nil {
			errs = appendErrors(errs, err)
			allHostsValid = false
		} else if appliesToMesh && host == "*" {
			errs = appendErrors(errs, fmt.Errorf("wildcard host * is not allowed for virtual services bound to the mesh gateway"))
			allHostsValid = false
		}
	}

	// Check for duplicate hosts
	// Duplicates include literal duplicates as well as wildcard duplicates
	// E.g., *.foo.com, and *.com are duplicates in the same virtual service
	if allHostsValid {
		for i := 0; i < len(virtualService.Hosts); i++ {
			hostI := Hostname(virtualService.Hosts[i])
			for j := i + 1; j < len(virtualService.Hosts); j++ {
				hostJ := Hostname(virtualService.Hosts[j])
				if hostI.Matches(hostJ) {
					errs = appendErrors(errs, fmt.Errorf("duplicate hosts in virtual service: %s & %s", hostI, hostJ))
				}
			}
		}
	}

	if len(virtualService.Http) == 0 && len(virtualService.Tcp) == 0 && len(virtualService.Tls) == 0 {
		errs = appendErrors(errs, errors.New("http, tcp or tls must be provided in virtual service"))
	}
	for _, httpRoute := range virtualService.Http {
		errs = appendErrors(errs, validateHTTPRoute(httpRoute))
	}
	for _, tlsRoute := range virtualService.Tls {
		errs = appendErrors(errs, validateTLSRoute(tlsRoute))
	}
	for _, tcpRoute := range virtualService.Tcp {
		errs = appendErrors(errs, validateTCPRoute(tcpRoute))
	}

	return
}

func validateTLSRoute(tls *networking.TLSRoute) (errs error) {
	if tls == nil {
		return nil
	}

	if len(tls.Match) == 0 {
		errs = appendErrors(errs, errors.New("TLS route must have at least one match condition"))
	}
	for _, match := range tls.Match {
		errs = appendErrors(errs, validateTLSMatch(match))
	}
	if len(tls.Route) != 1 {
		errs = appendErrors(errs, errors.New("TLS route must have exactly one destination"))
	}
	errs = appendErrors(errs, validateDestinationWeights(tls.Route))
	return
}

func validateTLSMatch(match *networking.TLSMatchAttributes) (errs error) {
	if len(match.SniHosts) == 0 {
		errs = appendErrors(errs, fmt.Errorf("TLS match must have at least one SNI host"))
	}
	for _, destinationSubnet := range match.DestinationSubnets {
		errs = appendErrors(errs, ValidateIPv4Subnet(destinationSubnet))
	}

	if match.Port != 0 {
		errs = appendErrors(errs, ValidatePort(int(match.Port)))
	}
	errs = appendErrors(errs, Labels(match.SourceLabels).Validate())
	errs = appendErrors(errs, validateGatewayNames(match.Gateways))
	return
}

func validateTCPRoute(tcp *networking.TCPRoute) (errs error) {
	if tcp == nil {
		return nil
	}
	for _, match := range tcp.Match {
		errs = appendErrors(errs, validateTCPMatch(match))
	}
	if len(tcp.Route) != 1 {
		errs = appendErrors(errs, errors.New("TCP route must have exactly one destination"))
	}
	errs = appendErrors(errs, validateDestinationWeights(tcp.Route))
	return
}

func validateTCPMatch(match *networking.L4MatchAttributes) (errs error) {
	for _, destinationSubnet := range match.DestinationSubnets {
		errs = appendErrors(errs, ValidateIPv4Subnet(destinationSubnet))
	}

	if len(match.SourceSubnet) > 0 {
		errs = appendErrors(errs, ValidateIPv4Subnet(match.SourceSubnet))
	}
	if match.Port != 0 {
		errs = appendErrors(errs, ValidatePort(int(match.Port)))
	}
	errs = appendErrors(errs, Labels(match.SourceLabels).Validate())
	errs = appendErrors(errs, validateGatewayNames(match.Gateways))
	return
}

func validateHTTPRoute(http *networking.HTTPRoute) (errs error) {
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
	} else if len(http.Route) == 0 {
		errs = appendErrors(errs, errors.New("HTTP route or redirect is required"))
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

		if match.Port != 0 {
			errs = appendErrors(errs, ValidatePort(int(match.Port)))
		}
		errs = appendErrors(errs, Labels(match.SourceLabels).Validate())
		errs = appendErrors(errs, validateGatewayNames(match.Gateways))
	}
	errs = appendErrors(errs, validateDestination(http.Mirror))
	errs = appendErrors(errs, validateHTTPRedirect(http.Redirect))
	errs = appendErrors(errs, validateHTTPRetry(http.Retries))
	errs = appendErrors(errs, validateHTTPRewrite(http.Rewrite))
	errs = appendErrors(errs, validateDestinationWeights(http.Route))
	if http.Timeout != nil {
		errs = appendErrors(errs, ValidateDurationGogo(http.Timeout))
	}

	return
}

func validateGatewayNames(gateways []string) (errs error) {
	for _, gateway := range gateways {
		if err := ValidateFQDN(gateway); err != nil {
			errs = appendErrors(errs, err)
		}
	}
	return
}

func validateDestinationWeights(weights []*networking.DestinationWeight) (errs error) {
	var totalWeight int32
	for _, weight := range weights {
		if weight.Destination == nil {
			errs = multierror.Append(errs, errors.New("destination is required"))
		}
		errs = appendErrors(errs, validateDestination(weight.Destination))
		errs = appendErrors(errs, ValidatePercent(weight.Weight))
		totalWeight += weight.Weight
	}
	if len(weights) > 1 && totalWeight != 100 {
		errs = appendErrors(errs, fmt.Errorf("total destination weight %v != 100", totalWeight))
	}
	return
}

func validateCORSPolicy(policy *networking.CorsPolicy) (errs error) {
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
		errs = appendErrors(errs, ValidateDurationGogo(policy.MaxAge))
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

func validateHTTPFaultInjection(fault *networking.HTTPFaultInjection) (errs error) {
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

func validateHTTPFaultInjectionAbort(abort *networking.HTTPFaultInjection_Abort) (errs error) {
	if abort == nil {
		return
	}

	errs = appendErrors(errs, ValidatePercent(abort.Percent))

	switch abort.ErrorType.(type) {
	case *networking.HTTPFaultInjection_Abort_GrpcStatus:
		// TODO: gRPC status validation
		errs = multierror.Append(errs, errors.New("gRPC abort fault injection not supported yet"))
	case *networking.HTTPFaultInjection_Abort_Http2Error:
		// TODO: HTTP2 error validation
		errs = multierror.Append(errs, errors.New("HTTP/2 abort fault injection not supported yet"))
	case *networking.HTTPFaultInjection_Abort_HttpStatus:
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

func validateHTTPFaultInjectionDelay(delay *networking.HTTPFaultInjection_Delay) (errs error) {
	if delay == nil {
		return
	}

	errs = appendErrors(errs, ValidatePercent(delay.Percent))
	switch v := delay.HttpDelayType.(type) {
	case *networking.HTTPFaultInjection_Delay_FixedDelay:
		errs = appendErrors(errs, ValidateDurationGogo(v.FixedDelay))
	case *networking.HTTPFaultInjection_Delay_ExponentialDelay:
		errs = appendErrors(errs, ValidateDurationGogo(v.ExponentialDelay))
		errs = multierror.Append(errs, fmt.Errorf("exponentialDelay not supported yet"))
	}
	return
}

func validateDestination(destination *networking.Destination) (errs error) {
	if destination == nil {
		return
	}

	errs = appendErrors(errs, ValidateWildcardDomain(destination.Host))
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

func validatePortSelector(selector *networking.PortSelector) error {
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

func validateAuthNPortSelector(selector *authn.PortSelector) error {
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

func validateHTTPRetry(retries *networking.HTTPRetry) (errs error) {
	if retries == nil {
		return
	}

	if retries.Attempts <= 0 {
		errs = multierror.Append(errs, errors.New("attempts must be positive"))
	}
	if retries.PerTryTimeout != nil {
		errs = appendErrors(errs, ValidateDurationGogo(retries.PerTryTimeout))
	}
	return
}

func validateHTTPRedirect(redirect *networking.HTTPRedirect) error {
	if redirect != nil && redirect.Uri == "" && redirect.Authority == "" {
		return errors.New("redirect must specify URI, authority, or both")
	}
	return nil
}

func validateHTTPRewrite(rewrite *networking.HTTPRewrite) error {
	if rewrite != nil && rewrite.Uri == "" && rewrite.Authority == "" {
		return errors.New("rewrite must specify URI, authority, or both")
	}
	return nil
}

// ValidateServiceEntry validates a service entry.
func ValidateServiceEntry(name, namespace string, config proto.Message) (errs error) {
	serviceEntry, ok := config.(*networking.ServiceEntry)
	if !ok {
		return fmt.Errorf("cannot cast to service entry")
	}

	if len(serviceEntry.Hosts) == 0 {
		errs = appendErrors(errs, fmt.Errorf("service entry must have at least one host"))
	}
	for _, host := range serviceEntry.Hosts {
		// Full wildcard or short names are not allowed in the service entry.
		if host == "*" || !strings.Contains(host, ".") {
			errs = appendErrors(errs, fmt.Errorf("invalid host %s", host))
		} else {
			errs = appendErrors(errs, ValidateWildcardDomain(host))
		}
	}

	cidrFound := false
	for _, address := range serviceEntry.Addresses {
		cidrFound = cidrFound || strings.Contains(address, "/")
		errs = appendErrors(errs, ValidateIPv4Subnet(address))
	}

	if cidrFound {
		if serviceEntry.Resolution != networking.ServiceEntry_NONE && serviceEntry.Resolution != networking.ServiceEntry_STATIC {
			errs = appendErrors(errs, fmt.Errorf("CIDR addresses are allowed only for NONE/STATIC resolution types"))
		}
	}

	servicePortNumbers := make(map[uint32]bool)
	servicePorts := make(map[string]bool, len(serviceEntry.Ports))
	for _, port := range serviceEntry.Ports {
		if servicePorts[port.Name] {
			errs = appendErrors(errs, fmt.Errorf("service entry port name %q already defined", port.Name))
		}
		servicePorts[port.Name] = true
		if servicePortNumbers[port.Number] {
			errs = appendErrors(errs, fmt.Errorf("service entry port %d already defined", port.Number))
		}
		servicePortNumbers[port.Number] = true
	}

	switch serviceEntry.Resolution {
	case networking.ServiceEntry_NONE:
		if len(serviceEntry.Endpoints) != 0 {
			errs = appendErrors(errs, fmt.Errorf("no endpoints should be provided for discovery type none"))
		}
	case networking.ServiceEntry_STATIC:
		if len(serviceEntry.Endpoints) == 0 {
			errs = appendErrors(errs,
				fmt.Errorf("endpoints must be provided if service entry discovery mode is static"))
		}

		unixEndpoint := false
		for _, endpoint := range serviceEntry.Endpoints {
			addr := endpoint.GetAddress()
			if strings.HasPrefix(addr, UnixAddressPrefix) {
				unixEndpoint = true
				errs = appendErrors(errs, ValidateUnixAddress(strings.TrimPrefix(addr, UnixAddressPrefix)))
				if len(endpoint.Ports) != 0 {
					errs = appendErrors(errs, fmt.Errorf("unix endpoint %s must not include ports", addr))
				}
			} else {
				errs = appendErrors(errs, ValidateIPv4Address(addr))

				for name, port := range endpoint.Ports {
					if !servicePorts[name] {
						errs = appendErrors(errs, fmt.Errorf("endpoint port %v is not defined by the service entry", port))
					}
				}
			}
			errs = appendErrors(errs, Labels(endpoint.Labels).Validate())

		}
		if unixEndpoint && len(serviceEntry.Ports) != 1 {
			errs = appendErrors(errs, errors.New("exactly 1 service port required for unix endpoints"))
		}
	case networking.ServiceEntry_DNS:
		if len(serviceEntry.Endpoints) == 0 {
			for _, host := range serviceEntry.Hosts {
				if err := ValidateFQDN(host); err != nil {
					errs = appendErrors(errs,
						fmt.Errorf("hosts must be FQDN if no endpoints are provided for discovery mode DNS"))
				}
			}
		}

		for _, endpoint := range serviceEntry.Endpoints {
			errs = appendErrors(errs,
				ValidateFQDN(endpoint.Address),
				Labels(endpoint.Labels).Validate())

			for name, port := range endpoint.Ports {
				if !servicePorts[name] {
					errs = appendErrors(errs, fmt.Errorf("endpoint port %v is not defined by the service entry", port))
				}
				errs = appendErrors(errs,
					validatePortName(name),
					ValidatePort(int(port)))
			}
		}
	default:
		errs = appendErrors(errs, fmt.Errorf("unsupported resolution type %s",
			networking.ServiceEntry_Resolution_name[int32(serviceEntry.Resolution)]))
	}

	for _, port := range serviceEntry.Ports {
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
	if ParseProtocol(protocol) == ProtocolUnsupported {
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

// ValidateNetworkEndpointAddress checks the Address field of a NetworkEndpoint. If the family is TCP, it checks the
// address is a valid IP address. If the family is Unix, it checks the address is a valid socket file path.
func ValidateNetworkEndpointAddress(n *NetworkEndpoint) error {
	switch n.Family {
	case AddressFamilyTCP:
		ipAddr := net.ParseIP(n.Address)
		if ipAddr == nil {
			return errors.New("invalid IP address " + n.Address)
		}
	case AddressFamilyUnix:
		return ValidateUnixAddress(n.Address)
	default:
		panic(fmt.Sprintf("unhandled Family %v", n.Family))
	}
	return nil
}
