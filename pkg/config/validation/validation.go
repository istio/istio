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

package validation

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	udpaa "github.com/cncf/xds/go/udpa/annotations"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/hashicorp/go-multierror"
	"github.com/lestrrat-go/jwx/jwk"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	any "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"

	"istio.io/api/annotation"
	extensions "istio.io/api/extensions/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	networkingv1beta1 "istio.io/api/networking/v1beta1"
	security_beta "istio.io/api/security/v1beta1"
	telemetry "istio.io/api/telemetry/v1alpha1"
	type_beta "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/util/constant"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/gateway"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/security"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/config/xds"
	"istio.io/istio/pkg/kube/apimirror"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/log"
)

// Constants for duration fields
const (
	// nolint: revive
	connectTimeoutMax = time.Second * 30
	// nolint: revive
	connectTimeoutMin = time.Millisecond

	drainTimeMax          = time.Hour
	parentShutdownTimeMax = time.Hour

	// UnixAddressPrefix is the prefix used to indicate an address is for a Unix Domain socket. It is used in
	// ServiceEntry.Endpoint.Address message.
	UnixAddressPrefix = "unix://"

	matchExact  = "exact:"
	matchPrefix = "prefix:"
)

const (
	regionIndex int = iota
	zoneIndex
	subZoneIndex
)

var (
	// envoy supported retry on header values
	supportedRetryOnPolicies = map[string]bool{
		// 'x-envoy-retry-on' supported policies:
		// https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/router_filter.html#x-envoy-retry-on
		"5xx":                    true,
		"gateway-error":          true,
		"reset":                  true,
		"connect-failure":        true,
		"retriable-4xx":          true,
		"refused-stream":         true,
		"retriable-status-codes": true,
		"retriable-headers":      true,
		"envoy-ratelimited":      true,

		// 'x-envoy-retry-grpc-on' supported policies:
		// https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/router_filter#x-envoy-retry-grpc-on
		"cancelled":          true,
		"deadline-exceeded":  true,
		"internal":           true,
		"resource-exhausted": true,
		"unavailable":        true,
	}

	// golang supported methods: https://golang.org/src/net/http/method.go
	supportedMethods = map[string]bool{
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

	scope = log.RegisterScope("validation", "CRD validation debugging", 0)

	// EmptyValidate is a Validate that does nothing and returns no error.
	EmptyValidate = registerValidateFunc("EmptyValidate",
		func(config.Config) (Warning, error) {
			return nil, nil
		})

	validateFuncs = make(map[string]ValidateFunc)
)

type Warning error

// Validation holds errors and warnings. They can be joined with additional errors by called appendValidation
type Validation struct {
	Err     error
	Warning Warning
}

type AnalysisAwareError struct {
	Type       string
	Msg        string
	Parameters []interface{}
}

// OverlappingMatchValidationForHTTPRoute holds necessary information from virtualservice
// to do such overlapping match validation
type OverlappingMatchValidationForHTTPRoute struct {
	RouteStr         string
	MatchStr         string
	Prefix           string
	MatchPort        uint32
	MatchMethod      string
	MatchAuthority   string
	MatchHeaders     map[string]string
	MatchQueryParams map[string]string
	MatchNonHeaders  map[string]string
}

var _ error = Validation{}

// WrapError turns an error into a Validation
func WrapError(e error) Validation {
	return Validation{Err: e}
}

// WrapWarning turns an error into a Validation as a warning
func WrapWarning(e error) Validation {
	return Validation{Warning: e}
}

// Warningf formats according to a format specifier and returns the string as a
// value that satisfies error. Like Errorf, but for warnings.
func Warningf(format string, a ...interface{}) Validation {
	return WrapWarning(fmt.Errorf(format, a...))
}

func (v Validation) Unwrap() (Warning, error) {
	return v.Warning, v.Err
}

func (v Validation) Error() string {
	if v.Err == nil {
		return ""
	}
	return v.Err.Error()
}

// ValidateFunc defines a validation func for an API proto.
type ValidateFunc func(config config.Config) (Warning, error)

// IsValidateFunc indicates whether there is a validation function with the given name.
func IsValidateFunc(name string) bool {
	return GetValidateFunc(name) != nil
}

// GetValidateFunc returns the validation function with the given name, or null if it does not exist.
func GetValidateFunc(name string) ValidateFunc {
	return validateFuncs[name]
}

func registerValidateFunc(name string, f ValidateFunc) ValidateFunc {
	// Wrap the original validate function with an extra validate function for the annotation "istio.io/dry-run".
	validate := validateAnnotationDryRun(f)
	validateFuncs[name] = validate
	return validate
}

func validateAnnotationDryRun(f ValidateFunc) ValidateFunc {
	return func(config config.Config) (Warning, error) {
		_, isAuthz := config.Spec.(*security_beta.AuthorizationPolicy)
		// Only the AuthorizationPolicy supports the annotation "istio.io/dry-run".
		if err := checkDryRunAnnotation(config, isAuthz); err != nil {
			return nil, err
		}
		return f(config)
	}
}

func checkDryRunAnnotation(cfg config.Config, allowed bool) error {
	if val, found := cfg.Annotations[annotation.IoIstioDryRun.Name]; found {
		if !allowed {
			return fmt.Errorf("%s/%s has unsupported annotation %s, please remove the annotation", cfg.Namespace, cfg.Name, annotation.IoIstioDryRun.Name)
		}
		if spec, ok := cfg.Spec.(*security_beta.AuthorizationPolicy); ok {
			switch spec.Action {
			case security_beta.AuthorizationPolicy_ALLOW, security_beta.AuthorizationPolicy_DENY:
				if _, err := strconv.ParseBool(val); err != nil {
					return fmt.Errorf("%s/%s has annotation %s with invalid value (%s): %v", cfg.Namespace, cfg.Name, annotation.IoIstioDryRun.Name, val, err)
				}
			default:
				return fmt.Errorf("the annotation %s currently only supports action ALLOW/DENY, found action %v in %s/%s",
					annotation.IoIstioDryRun.Name, spec.Action, cfg.Namespace, cfg.Name)
			}
		}
	}
	return nil
}

// ValidatePort checks that the network port is in range
func ValidatePort(port int) error {
	if 1 <= port && port <= 65535 {
		return nil
	}
	return fmt.Errorf("port number %d must be in the range 1..65535", port)
}

// ValidateFQDN checks a fully-qualified domain name
func ValidateFQDN(fqdn string) error {
	if err := checkDNS1123Preconditions(fqdn); err != nil {
		return err
	}
	return validateDNS1123Labels(fqdn)
}

// ValidateWildcardDomain checks that a domain is a valid FQDN, but also allows wildcard prefixes.
func ValidateWildcardDomain(domain string) error {
	if err := checkDNS1123Preconditions(domain); err != nil {
		return err
	}
	// We only allow wildcards in the first label; split off the first label (parts[0]) from the rest of the host (parts[1])
	parts := strings.SplitN(domain, ".", 2)
	if !labels.IsWildcardDNS1123Label(parts[0]) {
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
	parts := strings.Split(domain, ".")
	topLevelDomain := parts[len(parts)-1]
	if _, err := strconv.Atoi(topLevelDomain); err == nil {
		return fmt.Errorf("domain name %q invalid (top level domain %q cannot be all-numeric)", domain, topLevelDomain)
	}
	for i, label := range parts {
		// Allow the last part to be empty, for unambiguous names like `istio.io.`
		if i == len(parts)-1 && label == "" {
			return nil
		}
		if !labels.IsDNS1123Label(label) {
			return fmt.Errorf("domain name %q invalid (label %q invalid)", domain, label)
		}
	}
	return nil
}

// validate the trust domain format
func ValidateTrustDomain(domain string) error {
	if len(domain) == 0 {
		return fmt.Errorf("empty domain name not allowed")
	}
	parts := strings.Split(domain, ".")
	for i, label := range parts {
		// Allow the last part to be empty, for unambiguous names like `istio.io.`
		if i == len(parts)-1 && label == "" {
			return nil
		}
		if !labels.IsDNS1123Label(label) {
			return fmt.Errorf("trust domain name %q invalid", domain)
		}
	}
	return nil
}

// ValidateHTTPHeaderName validates a header name
func ValidateHTTPHeaderName(name string) error {
	if name == "" {
		return fmt.Errorf("header name cannot be empty")
	}
	return nil
}

// ValidateHTTPHeaderWithAuthorityOperationName validates a header name when used to add/set in request.
func ValidateHTTPHeaderWithAuthorityOperationName(name string) error {
	if name == "" {
		return fmt.Errorf("header name cannot be empty")
	}
	// Authority header is validated later
	if isInternalHeader(name) && !isAuthorityHeader(name) {
		return fmt.Errorf(`invalid header %q: header cannot have ":" prefix`, name)
	}
	return nil
}

// ValidateHTTPHeaderWithHostOperationName validates a header name when used to destination specific add/set in request.
// TODO(https://github.com/envoyproxy/envoy/issues/16775) merge with ValidateHTTPHeaderWithAuthorityOperationName
func ValidateHTTPHeaderWithHostOperationName(name string) error {
	if name == "" {
		return fmt.Errorf("header name cannot be empty")
	}
	// Authority header is validated later
	if isInternalHeader(name) && !strings.EqualFold(name, "host") {
		return fmt.Errorf(`invalid header %q: header cannot have ":" prefix`, name)
	}
	return nil
}

// ValidateHTTPHeaderOperationName validates a header name when used to remove from request or modify response.
func ValidateHTTPHeaderOperationName(name string) error {
	if name == "" {
		return fmt.Errorf("header name cannot be empty")
	}
	if strings.EqualFold(name, "host") {
		return fmt.Errorf(`invalid header %q: cannot set Host header`, name)
	}
	if isInternalHeader(name) {
		return fmt.Errorf(`invalid header %q: header cannot have ":" prefix`, name)
	}
	return nil
}

// ValidateHTTPHeaderValue validates a header value for Envoy
// Valid: "foo", "%HOSTNAME%", "100%%", "prefix %HOSTNAME% suffix"
// Invalid: "abc%123"
// We don't try to check that what is inside the %% is one of Envoy recognized values, we just prevent invalid config.
// See: https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/headers.html#custom-request-response-headers
func ValidateHTTPHeaderValue(value string) error {
	if strings.Count(value, "%")%2 != 0 {
		return errors.New("single % not allowed.  Escape by doubling to %% or encase Envoy variable name in pair of %")
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

// validatePercentage checks if the specified fractional percentage is valid.
func validatePercentage(percentage *networking.Percent) error {
	if percentage != nil {
		if percentage.Value < 0.0 || percentage.Value > 100.0 || (percentage.Value > 0.0 && percentage.Value < 0.0001) {
			return fmt.Errorf("percentage %v is neither 0.0, nor in range [0.0001, 100.0]", percentage.Value)
		}
	}
	return nil
}

// ValidateIPSubnet checks that a string is in "CIDR notation" or "Dot-decimal notation"
func ValidateIPSubnet(subnet string) error {
	// We expect a string in "CIDR notation" or "Dot-decimal notation"
	// E.g., a.b.c.d/xx form or just a.b.c.d or 2001:1::1/64
	if strings.Count(subnet, "/") == 1 {
		// We expect a string in "CIDR notation", i.e. a.b.c.d/xx or 2001:1::1/64 form
		ip, _, err := net.ParseCIDR(subnet)
		if err != nil {
			return fmt.Errorf("%v is not a valid CIDR block", subnet)
		}
		if ip.To4() == nil && ip.To16() == nil {
			return fmt.Errorf("%v is not a valid IPv4 or IPv6 address", subnet)
		}
		return nil
	}
	return ValidateIPAddress(subnet)
}

// ValidateIPAddress validates that a string in "CIDR notation" or "Dot-decimal notation"
func ValidateIPAddress(addr string) error {
	ip := net.ParseIP(addr)
	if ip == nil {
		return fmt.Errorf("%v is not a valid IP", addr)
	}

	return nil
}

// ValidateUnixAddress validates that the string is a valid unix domain socket path.
func ValidateUnixAddress(addr string) error {
	if len(addr) == 0 {
		return errors.New("unix address must not be empty")
	}

	// Allow unix abstract domain sockets whose names start with @
	if strings.HasPrefix(addr, "@") {
		return nil
	}

	// Note that we use path, not path/filepath even though a domain socket path is a file path.  We don't want the
	// Pilot output to depend on which OS Pilot is run on, so we always use Unix-style forward slashes.
	if !path.IsAbs(addr) || strings.HasSuffix(addr, "/") {
		return fmt.Errorf("%s is not an absolute path to a file", addr)
	}
	return nil
}

// ValidateGateway checks gateway specifications
var ValidateGateway = registerValidateFunc("ValidateGateway",
	func(cfg config.Config) (Warning, error) {
		name := cfg.Name
		v := Validation{}
		// Gateway name must conform to the DNS label format (no dots)
		if !labels.IsDNS1123Label(name) {
			v = appendValidation(v, fmt.Errorf("invalid gateway name: %q", name))
		}
		value, ok := cfg.Spec.(*networking.Gateway)
		if !ok {
			v = appendValidation(v, fmt.Errorf("cannot cast to gateway: %#v", cfg.Spec))
			return v.Unwrap()
		}

		if len(value.Servers) == 0 {
			v = appendValidation(v, fmt.Errorf("gateway must have at least one server"))
		} else {
			for _, server := range value.Servers {
				v = appendValidation(v, validateServer(server))
			}
		}

		// Ensure unique port names
		portNames := make(map[string]bool)

		for _, s := range value.Servers {
			if s == nil {
				v = appendValidation(v, fmt.Errorf("server may not be nil"))
				continue
			}
			if s.Port != nil {
				if portNames[s.Port.Name] {
					v = appendValidation(v, fmt.Errorf("port names in servers must be unique: duplicate name %s", s.Port.Name))
				}
				portNames[s.Port.Name] = true
				if !protocol.Parse(s.Port.Protocol).IsHTTP() && s.GetTls().GetHttpsRedirect() {
					v = appendValidation(v, WrapWarning(fmt.Errorf("tls.httpsRedirect should only be used with http servers")))
				}
			}
		}

		return v.Unwrap()
	})

func validateServer(server *networking.Server) (v Validation) {
	if server == nil {
		return WrapError(fmt.Errorf("cannot have nil server"))
	}
	if len(server.Hosts) == 0 {
		v = appendValidation(v, fmt.Errorf("server config must contain at least one host"))
	} else {
		for _, hostname := range server.Hosts {
			v = appendValidation(v, validateNamespaceSlashWildcardHostname(hostname, true))
		}
	}
	portErr := validateServerPort(server.Port)
	if portErr != nil {
		v = appendValidation(v, portErr)
	}
	v = appendValidation(v, validateServerBind(server.Port, server.Bind))
	v = appendValidation(v, validateTLSOptions(server.Tls))

	// If port is HTTPS or TLS, make sure that server has TLS options
	if portErr == nil {
		p := protocol.Parse(server.Port.Protocol)
		if p.IsTLS() && server.Tls == nil {
			v = appendValidation(v, fmt.Errorf("server must have TLS settings for HTTPS/TLS protocols"))
		} else if !p.IsTLS() && server.Tls != nil {
			// only tls redirect is allowed if this is a HTTP server
			if p.IsHTTP() {
				if !gateway.IsPassThroughServer(server) ||
					server.Tls.CaCertificates != "" || server.Tls.PrivateKey != "" || server.Tls.ServerCertificate != "" {
					v = appendValidation(v, fmt.Errorf("server cannot have TLS settings for plain text HTTP ports"))
				}
			} else {
				v = appendValidation(v, fmt.Errorf("server cannot have TLS settings for non HTTPS/TLS ports"))
			}
		}
	}
	return v
}

func validateServerPort(port *networking.Port) (errs error) {
	if port == nil {
		return appendErrors(errs, fmt.Errorf("port is required"))
	}
	if protocol.Parse(port.Protocol) == protocol.Unsupported {
		errs = appendErrors(errs, fmt.Errorf("invalid protocol %q, supported protocols are HTTP, HTTP2, GRPC, GRPC-WEB, MONGO, REDIS, MYSQL, TCP", port.Protocol))
	}
	if port.Number > 0 {
		errs = appendErrors(errs, ValidatePort(int(port.Number)))
	}

	if port.Name == "" {
		errs = appendErrors(errs, fmt.Errorf("port name must be set: %v", port))
	}
	return
}

func validateServerBind(port *networking.Port, bind string) (errs error) {
	if strings.HasPrefix(bind, UnixAddressPrefix) {
		errs = appendErrors(errs, ValidateUnixAddress(strings.TrimPrefix(bind, UnixAddressPrefix)))
		if port != nil && port.Number != 0 {
			errs = appendErrors(errs, fmt.Errorf("port number must be 0 for unix domain socket: %v", port))
		}
	} else if len(bind) != 0 {
		errs = appendErrors(errs, ValidateIPAddress(bind))
	}
	return
}

func validateTLSOptions(tls *networking.ServerTLSSettings) (v Validation) {
	if tls == nil {
		// no tls config at all is valid
		return
	}

	invalidCiphers := sets.New()
	validCiphers := sets.New()
	duplicateCiphers := sets.New()
	for _, cs := range tls.CipherSuites {
		if !security.IsValidCipherSuite(cs) {
			invalidCiphers.Insert(cs)
		} else {
			if !validCiphers.Contains(cs) {
				validCiphers.Insert(cs)
			} else {
				duplicateCiphers.Insert(cs)
			}
		}
	}

	if len(invalidCiphers) > 0 {
		v = appendWarningf(v, "ignoring invalid cipher suites: %v", invalidCiphers.SortedList())
	}

	if len(duplicateCiphers) > 0 {
		v = appendWarningf(v, "ignoring duplicate cipher suites: %v", duplicateCiphers.SortedList())
	}

	if tls.Mode == networking.ServerTLSSettings_ISTIO_MUTUAL {
		// ISTIO_MUTUAL TLS mode uses either SDS or default certificate mount paths
		// therefore, we should fail validation if other TLS fields are set
		if tls.ServerCertificate != "" {
			v = appendValidation(v, fmt.Errorf("ISTIO_MUTUAL TLS cannot have associated server certificate"))
		}
		if tls.PrivateKey != "" {
			v = appendValidation(v, fmt.Errorf("ISTIO_MUTUAL TLS cannot have associated private key"))
		}
		if tls.CaCertificates != "" {
			v = appendValidation(v, fmt.Errorf("ISTIO_MUTUAL TLS cannot have associated CA bundle"))
		}
		if tls.CredentialName != "" {
			if features.EnableLegacyIstioMutualCredentialName {
				// Legacy mode enabled, just warn
				v = appendWarningf(v, "ISTIO_MUTUAL TLS cannot have associated credentialName")
			} else {
				v = appendValidation(v, fmt.Errorf("ISTIO_MUTUAL TLS cannot have associated credentialName"))
			}
		}
		return
	}

	if tls.Mode == networking.ServerTLSSettings_PASSTHROUGH || tls.Mode == networking.ServerTLSSettings_AUTO_PASSTHROUGH {
		if tls.ServerCertificate != "" || tls.PrivateKey != "" || tls.CaCertificates != "" || tls.CredentialName != "" {
			// Warn for backwards compatibility
			v = appendWarningf(v, "%v mode does not use certificates, they will be ignored", tls.Mode)
		}
	}

	if (tls.Mode == networking.ServerTLSSettings_SIMPLE || tls.Mode == networking.ServerTLSSettings_MUTUAL) && tls.CredentialName != "" {
		// If tls mode is SIMPLE or MUTUAL, and CredentialName is specified, credentials are fetched
		// remotely. ServerCertificate and CaCertificates fields are not required.
		return
	}
	if tls.Mode == networking.ServerTLSSettings_SIMPLE {
		if tls.ServerCertificate == "" {
			v = appendValidation(v, fmt.Errorf("SIMPLE TLS requires a server certificate"))
		}
		if tls.PrivateKey == "" {
			v = appendValidation(v, fmt.Errorf("SIMPLE TLS requires a private key"))
		}
	} else if tls.Mode == networking.ServerTLSSettings_MUTUAL {
		if tls.ServerCertificate == "" {
			v = appendValidation(v, fmt.Errorf("MUTUAL TLS requires a server certificate"))
		}
		if tls.PrivateKey == "" {
			v = appendValidation(v, fmt.Errorf("MUTUAL TLS requires a private key"))
		}
		if tls.CaCertificates == "" {
			v = appendValidation(v, fmt.Errorf("MUTUAL TLS requires a client CA bundle"))
		}
	}
	return
}

// ValidateDestinationRule checks proxy policies
var ValidateDestinationRule = registerValidateFunc("ValidateDestinationRule",
	func(cfg config.Config) (Warning, error) {
		rule, ok := cfg.Spec.(*networking.DestinationRule)
		if !ok {
			return nil, fmt.Errorf("cannot cast to destination rule")
		}
		v := Validation{}
		if features.EnableDestinationRuleInheritance {
			if rule.Host == "" {
				if rule.GetWorkloadSelector() != nil {
					v = appendValidation(v,
						fmt.Errorf("mesh/namespace destination rule cannot have workloadSelector configured"))
				}
				if len(rule.Subsets) != 0 {
					v = appendValidation(v,
						fmt.Errorf("mesh/namespace destination rule cannot have subsets"))
				}
				if len(rule.ExportTo) != 0 {
					v = appendValidation(v,
						fmt.Errorf("mesh/namespace destination rule cannot have exportTo configured"))
				}
				if rule.TrafficPolicy != nil && len(rule.TrafficPolicy.PortLevelSettings) != 0 {
					v = appendValidation(v,
						fmt.Errorf("mesh/namespace destination rule cannot have portLevelSettings configured"))
				}
			} else {
				v = appendValidation(v, ValidateWildcardDomain(rule.Host))
			}
		} else {
			v = appendValidation(v, ValidateWildcardDomain(rule.Host))
		}

		v = appendValidation(v, validateTrafficPolicy(rule.TrafficPolicy))

		for _, subset := range rule.Subsets {
			if subset == nil {
				v = appendValidation(v, errors.New("subset may not be null"))
				continue
			}
			v = appendValidation(v, validateSubset(subset))
		}
		v = appendValidation(v,
			validateExportTo(cfg.Namespace, rule.ExportTo, false, rule.GetWorkloadSelector() != nil))

		v = appendValidation(v, validateWorkloadSelector(rule.GetWorkloadSelector()))

		return v.Unwrap()
	})

func validateExportTo(namespace string, exportTo []string, isServiceEntry bool, isDestinationRuleWithSelector bool) (errs error) {
	if len(exportTo) > 0 {
		// Make sure there are no duplicates
		exportToSet := sets.New()
		for _, e := range exportTo {
			key := e
			if visibility.Instance(e) == visibility.Private {
				// substitute this with the current namespace so that we
				// can check for duplicates like ., namespace
				key = namespace
			}
			if exportToSet.Contains(key) {
				if key != e {
					errs = appendErrors(errs, fmt.Errorf("duplicate entries in exportTo: . and current namespace %s", namespace))
				} else {
					errs = appendErrors(errs, fmt.Errorf("duplicate entries in exportTo for entry %s", e))
				}
			} else {
				// if this is a serviceEntry, allow ~ in exportTo as it can be used to create
				// a service that is not even visible within the local namespace to anyone other
				// than the proxies of that service.
				if isServiceEntry && visibility.Instance(e) == visibility.None {
					exportToSet.Insert(key)
				} else {
					if err := visibility.Instance(key).Validate(); err != nil {
						errs = appendErrors(errs, err)
					} else {
						exportToSet.Insert(key)
					}
				}
			}
		}

		// Make sure workloadSelector based destination rule does not use exportTo other than current namespace
		if isDestinationRuleWithSelector && !exportToSet.IsEmpty() {
			if exportToSet.Contains(namespace) {
				if len(exportToSet) > 1 {
					errs = appendErrors(errs, fmt.Errorf("destination rule with workload selector cannot have multiple entries in exportTo"))
				}
			} else {
				errs = appendErrors(errs, fmt.Errorf("destination rule with workload selector cannot have exportTo beyond current namespace"))
			}
		}

		// Make sure we have only one of . or *
		if exportToSet.Contains(string(visibility.Public)) {
			// make sure that there are no other entries in the exportTo
			// i.e. no point in saying ns1,ns2,*. Might as well say *
			if len(exportTo) > 1 {
				errs = appendErrors(errs, fmt.Errorf("cannot have both public (*) and non-public exportTo values for a resource"))
			}
		}

		// if this is a service entry, then we need to disallow * and ~ together. Or ~ and other namespaces
		if exportToSet.Contains(string(visibility.None)) {
			if len(exportTo) > 1 {
				errs = appendErrors(errs, fmt.Errorf("cannot export service entry to no one (~) and someone"))
			}
		}
	}

	return
}

func validateAlphaWorkloadSelector(selector *networking.WorkloadSelector) error {
	var errs error
	if selector != nil {
		for k, v := range selector.Labels {
			if k == "" {
				errs = appendErrors(errs,
					fmt.Errorf("empty key is not supported in selector: %q", fmt.Sprintf("%s=%s", k, v)))
			}
			if strings.Contains(k, "*") || strings.Contains(v, "*") {
				errs = appendErrors(errs,
					fmt.Errorf("wildcard is not supported in selector: %q", fmt.Sprintf("%s=%s", k, v)))
			}
		}
	}

	return errs
}

// ValidateEnvoyFilter checks envoy filter config supplied by user
var ValidateEnvoyFilter = registerValidateFunc("ValidateEnvoyFilter",
	func(cfg config.Config) (Warning, error) {
		errs := Validation{}
		rule, ok := cfg.Spec.(*networking.EnvoyFilter)
		if !ok {
			return nil, fmt.Errorf("cannot cast to Envoy filter")
		}

		if err := validateAlphaWorkloadSelector(rule.WorkloadSelector); err != nil {
			return nil, err
		}

		for _, cp := range rule.ConfigPatches {
			if cp == nil {
				errs = appendValidation(errs, fmt.Errorf("Envoy filter: null config patch")) // nolint: golint,stylecheck
				continue
			}
			if cp.ApplyTo == networking.EnvoyFilter_INVALID {
				errs = appendValidation(errs, fmt.Errorf("Envoy filter: missing applyTo")) // nolint: golint,stylecheck
				continue
			}
			if cp.Patch == nil {
				errs = appendValidation(errs, fmt.Errorf("Envoy filter: missing patch")) // nolint: golint,stylecheck
				continue
			}
			if cp.Patch.Operation == networking.EnvoyFilter_Patch_INVALID {
				errs = appendValidation(errs, fmt.Errorf("Envoy filter: missing patch operation")) // nolint: golint,stylecheck
				continue
			}
			if cp.Patch.Operation != networking.EnvoyFilter_Patch_REMOVE && cp.Patch.Value == nil {
				errs = appendValidation(errs, fmt.Errorf("Envoy filter: missing patch value for non-remove operation")) // nolint: golint,stylecheck
				continue
			}

			// ensure that the supplied regex for proxy version compiles
			if cp.Match != nil && cp.Match.Proxy != nil && cp.Match.Proxy.ProxyVersion != "" {
				if _, err := regexp.Compile(cp.Match.Proxy.ProxyVersion); err != nil {
					errs = appendValidation(errs, fmt.Errorf("Envoy filter: invalid regex for proxy version, [%v]", err)) // nolint: golint,stylecheck
					continue
				}
			}
			// ensure that applyTo, match and patch all line up
			switch cp.ApplyTo {
			case networking.EnvoyFilter_LISTENER,
				networking.EnvoyFilter_FILTER_CHAIN,
				networking.EnvoyFilter_NETWORK_FILTER,
				networking.EnvoyFilter_HTTP_FILTER:
				if cp.Match != nil && cp.Match.ObjectTypes != nil {
					if cp.Match.GetListener() == nil {
						errs = appendValidation(errs, fmt.Errorf("Envoy filter: applyTo for listener class objects cannot have non listener match")) // nolint: golint,stylecheck
						continue
					}
					listenerMatch := cp.Match.GetListener()
					if listenerMatch.FilterChain != nil {
						if listenerMatch.FilterChain.Filter != nil {
							if cp.ApplyTo == networking.EnvoyFilter_LISTENER || cp.ApplyTo == networking.EnvoyFilter_FILTER_CHAIN {
								// This would be an error but is a warning for backwards compatibility
								errs = appendValidation(errs, WrapWarning(
									fmt.Errorf("Envoy filter: filter match has no effect when used with %v", cp.ApplyTo))) // nolint: golint,stylecheck
							}
							// filter names are required if network filter matches are being made
							if listenerMatch.FilterChain.Filter.Name == "" {
								errs = appendValidation(errs, fmt.Errorf("Envoy filter: filter match has no name to match on")) // nolint: golint,stylecheck
								continue
							} else if listenerMatch.FilterChain.Filter.SubFilter != nil {
								// sub filter match is supported only for applyTo HTTP_FILTER
								if cp.ApplyTo != networking.EnvoyFilter_HTTP_FILTER {
									errs = appendValidation(errs, fmt.Errorf("Envoy filter: subfilter match can be used with applyTo HTTP_FILTER only")) // nolint: golint,stylecheck
									continue
								}
								// sub filter match requires the network filter to match to envoy http connection manager
								if listenerMatch.FilterChain.Filter.Name != wellknown.HTTPConnectionManager &&
									listenerMatch.FilterChain.Filter.Name != "envoy.http_connection_manager" {
									errs = appendValidation(errs, fmt.Errorf("Envoy filter: subfilter match requires filter match with %s", // nolint: golint,stylecheck
										wellknown.HTTPConnectionManager))
									continue
								}
								if listenerMatch.FilterChain.Filter.SubFilter.Name == "" {
									errs = appendValidation(errs, fmt.Errorf("Envoy filter: subfilter match has no name to match on")) // nolint: golint,stylecheck
									continue
								}
							}

							errs = appendValidation(errs, validateListenerMatchName(listenerMatch.FilterChain.Filter.GetName()))
							errs = appendValidation(errs, validateListenerMatchName(listenerMatch.FilterChain.Filter.GetSubFilter().GetName()))
						}
					}
				}
			case networking.EnvoyFilter_ROUTE_CONFIGURATION, networking.EnvoyFilter_VIRTUAL_HOST, networking.EnvoyFilter_HTTP_ROUTE:
				if cp.Match != nil && cp.Match.ObjectTypes != nil {
					if cp.Match.GetRouteConfiguration() == nil {
						errs = appendValidation(errs,
							fmt.Errorf("Envoy filter: applyTo for http route class objects cannot have non route configuration match")) // nolint: golint,stylecheck
					}
				}

			case networking.EnvoyFilter_CLUSTER:
				if cp.Match != nil && cp.Match.ObjectTypes != nil {
					if cp.Match.GetCluster() == nil {
						errs = appendValidation(errs, fmt.Errorf("Envoy filter: applyTo for cluster class objects cannot have non cluster match")) // nolint: golint,stylecheck
					}
				}
			}
			// ensure that the struct is valid
			if _, err := xds.BuildXDSObjectFromStruct(cp.ApplyTo, cp.Patch.Value, false); err != nil {
				errs = appendValidation(errs, err)
			} else {
				// Run with strict validation, and emit warnings. This helps capture cases like unknown fields
				// We do not want to reject in case the proto is valid but our libraries are outdated
				obj, err := xds.BuildXDSObjectFromStruct(cp.ApplyTo, cp.Patch.Value, true)
				if err != nil {
					errs = appendValidation(errs, WrapWarning(err))
				}

				// Append any deprecation notices
				if obj != nil {
					errs = appendValidation(errs, validateDeprecatedFilterTypes(obj))
				}
			}
		}

		return errs.Unwrap()
	})

func validateListenerMatchName(name string) error {
	if newName, f := xds.ReverseDeprecatedFilterNames[name]; f {
		return WrapWarning(fmt.Errorf("using deprecated filter name %q; use %q instead", name, newName))
	}
	return nil
}

func recurseDeprecatedTypes(message protoreflect.Message) ([]string, error) {
	var topError error
	var deprecatedTypes []string
	if message == nil {
		return nil, nil
	}
	message.Range(func(descriptor protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		m, isMessage := value.Interface().(protoreflect.Message)
		if isMessage {
			anyMessage, isAny := m.Interface().(*any.Any)
			if isAny {
				mt, err := protoregistry.GlobalTypes.FindMessageByURL(anyMessage.TypeUrl)
				if err != nil {
					topError = err
					return false
				}
				var fileOpts proto.Message = mt.Descriptor().ParentFile().Options().(*descriptorpb.FileOptions)
				if proto.HasExtension(fileOpts, udpaa.E_FileStatus) {
					ext := proto.GetExtension(fileOpts, udpaa.E_FileStatus)
					udpaext, ok := ext.(*udpaa.StatusAnnotation)
					if !ok {
						topError = fmt.Errorf("extension was of wrong type: %T", ext)
						return false
					}
					if udpaext.PackageVersionStatus == udpaa.PackageVersionStatus_FROZEN {
						deprecatedTypes = append(deprecatedTypes, anyMessage.TypeUrl)
					}
				}
			}
			newTypes, err := recurseDeprecatedTypes(m)
			if err != nil {
				topError = err
				return false
			}
			deprecatedTypes = append(deprecatedTypes, newTypes...)
		}
		return true
	})
	return deprecatedTypes, topError
}

func validateDeprecatedFilterTypes(obj proto.Message) error {
	deprecated, err := recurseDeprecatedTypes(obj.ProtoReflect())
	if err != nil {
		return fmt.Errorf("failed to find deprecated types: %v", err)
	}
	if len(deprecated) > 0 {
		return WrapWarning(fmt.Errorf("using deprecated type_url(s); %v", strings.Join(deprecated, ", ")))
	}
	return nil
}

// validates that hostname in ns/<hostname> is a valid hostname according to
// API specs
func validateSidecarOrGatewayHostnamePart(hostname string, isGateway bool) (errs error) {
	// short name hosts are not allowed
	if hostname != "*" && !strings.Contains(hostname, ".") {
		errs = appendErrors(errs, fmt.Errorf("short names (non FQDN) are not allowed"))
	}

	if err := ValidateWildcardDomain(hostname); err != nil {
		if !isGateway {
			errs = appendErrors(errs, err)
		}

		// Gateway allows IP as the host string, as well
		ipAddr := net.ParseIP(hostname)
		if ipAddr == nil {
			errs = appendErrors(errs, err)
		}
	}
	return
}

func validateNamespaceSlashWildcardHostname(hostname string, isGateway bool) (errs error) {
	parts := strings.SplitN(hostname, "/", 2)
	if len(parts) != 2 {
		if isGateway {
			// Old style host in the gateway
			return validateSidecarOrGatewayHostnamePart(hostname, true)
		}
		errs = appendErrors(errs, fmt.Errorf("host must be of form namespace/dnsName"))
		return
	}

	if len(parts[0]) == 0 || len(parts[1]) == 0 {
		errs = appendErrors(errs, fmt.Errorf("config namespace and dnsName in host entry cannot be empty"))
	}

	if !isGateway {
		// namespace can be * or . or ~ or a valid DNS label in sidecars
		if parts[0] != "*" && parts[0] != "." && parts[0] != "~" {
			if !labels.IsDNS1123Label(parts[0]) {
				errs = appendErrors(errs, fmt.Errorf("invalid namespace value %q in sidecar", parts[0]))
			}
		}
	} else {
		// namespace can be * or . or a valid DNS label in gateways
		if parts[0] != "*" && parts[0] != "." {
			if !labels.IsDNS1123Label(parts[0]) {
				errs = appendErrors(errs, fmt.Errorf("invalid namespace value %q in gateway", parts[0]))
			}
		}
	}
	errs = appendErrors(errs, validateSidecarOrGatewayHostnamePart(parts[1], isGateway))
	return
}

// ValidateSidecar checks sidecar config supplied by user
var ValidateSidecar = registerValidateFunc("ValidateSidecar",
	func(cfg config.Config) (Warning, error) {
		errs := Validation{}
		rule, ok := cfg.Spec.(*networking.Sidecar)
		if !ok {
			return nil, fmt.Errorf("cannot cast to Sidecar")
		}

		if err := validateAlphaWorkloadSelector(rule.WorkloadSelector); err != nil {
			return nil, err
		}

		if len(rule.Egress) == 0 && len(rule.Ingress) == 0 && rule.OutboundTrafficPolicy == nil {
			return nil, fmt.Errorf("sidecar: empty configuration provided")
		}

		portMap := make(map[uint32]struct{})
		for _, i := range rule.Ingress {
			if i == nil {
				errs = appendValidation(errs, fmt.Errorf("sidecar: ingress may not be null"))
				continue
			}
			if i.Port == nil {
				errs = appendValidation(errs, fmt.Errorf("sidecar: port is required for ingress listeners"))
				continue
			}

			bind := i.GetBind()
			errs = appendValidation(errs, validateSidecarIngressPortAndBind(i.Port, bind))

			if _, found := portMap[i.Port.Number]; found {
				errs = appendValidation(errs, fmt.Errorf("sidecar: ports on IP bound listeners must be unique"))
			}
			portMap[i.Port.Number] = struct{}{}

			if len(i.DefaultEndpoint) != 0 {
				if strings.HasPrefix(i.DefaultEndpoint, UnixAddressPrefix) {
					errs = appendValidation(errs, ValidateUnixAddress(strings.TrimPrefix(i.DefaultEndpoint, UnixAddressPrefix)))
				} else {
					// format should be 127.0.0.1:port or :port
					parts := strings.Split(i.DefaultEndpoint, ":")
					if len(parts) < 2 {
						errs = appendValidation(errs, fmt.Errorf("sidecar: defaultEndpoint must be of form 127.0.0.1:<port>, 0.0.0.0:<port>, unix://filepath, or unset"))
					} else {
						if len(parts[0]) > 0 && parts[0] != "127.0.0.1" && parts[0] != "0.0.0.0" {
							errs = appendValidation(errs, fmt.Errorf("sidecar: defaultEndpoint must be of form 127.0.0.1:<port>, 0.0.0.0:<port>, unix://filepath, or unset"))
						}

						port, err := strconv.Atoi(parts[1])
						if err != nil {
							errs = appendValidation(errs, fmt.Errorf("sidecar: defaultEndpoint port (%s) is not a number: %v", parts[1], err))
						} else {
							errs = appendValidation(errs, ValidatePort(port))
						}
					}
				}
			}

			if i.Tls != nil {
				if len(i.Tls.SubjectAltNames) > 0 {
					errs = appendValidation(errs, fmt.Errorf("sidecar: subjectAltNames is not supported in ingress tls"))
				}
				if i.Tls.HttpsRedirect {
					errs = appendValidation(errs, fmt.Errorf("sidecar: httpsRedirect is not supported"))
				}
				if i.Tls.CredentialName != "" {
					errs = appendValidation(errs, fmt.Errorf("sidecar: credentialName is not currently supported"))
				}
				if i.Tls.Mode == networking.ServerTLSSettings_ISTIO_MUTUAL || i.Tls.Mode == networking.ServerTLSSettings_AUTO_PASSTHROUGH {
					errs = appendValidation(errs, fmt.Errorf("configuration is invalid: cannot set mode to %s in sidecar ingress tls", i.Tls.Mode.String()))
				}
				protocol := protocol.Parse(i.Port.Protocol)
				if !protocol.IsTLS() {
					errs = appendValidation(errs, fmt.Errorf("server cannot have TLS settings for non HTTPS/TLS ports"))
				}
				errs = appendValidation(errs, validateTLSOptions(i.Tls))
			}
		}

		portMap = make(map[uint32]struct{})
		udsMap := make(map[string]struct{})
		catchAllEgressListenerFound := false
		for index, egress := range rule.Egress {
			if egress == nil {
				errs = appendValidation(errs, errors.New("egress listener may not be null"))
				continue
			}
			// there can be only one catch all egress listener with empty port, and it should be the last listener.
			if egress.Port == nil {
				if !catchAllEgressListenerFound {
					if index == len(rule.Egress)-1 {
						catchAllEgressListenerFound = true
					} else {
						errs = appendValidation(errs, fmt.Errorf("sidecar: the egress listener with empty port should be the last listener in the list"))
					}
				} else {
					errs = appendValidation(errs, fmt.Errorf("sidecar: egress can have only one listener with empty port"))
					continue
				}
			} else {
				bind := egress.GetBind()
				captureMode := egress.GetCaptureMode()
				errs = appendValidation(errs, validateSidecarEgressPortBindAndCaptureMode(egress.Port, bind, captureMode))

				if egress.Port.Number == 0 {
					if _, found := udsMap[bind]; found {
						errs = appendValidation(errs, fmt.Errorf("sidecar: unix domain socket values for listeners must be unique"))
					}
					udsMap[bind] = struct{}{}
				} else {
					if _, found := portMap[egress.Port.Number]; found {
						errs = appendValidation(errs, fmt.Errorf("sidecar: ports on IP bound listeners must be unique"))
					}
					portMap[egress.Port.Number] = struct{}{}
				}
			}

			// validate that the hosts field is a slash separated value
			// of form ns1/host, or */host, or */*, or ns1/*, or ns1/*.example.com
			if len(egress.Hosts) == 0 {
				errs = appendValidation(errs, fmt.Errorf("sidecar: egress listener must contain at least one host"))
			} else {
				nssSvcs := map[string]map[string]bool{}
				for _, hostname := range egress.Hosts {
					parts := strings.SplitN(hostname, "/", 2)
					if len(parts) == 2 {
						ns := parts[0]
						svc := parts[1]
						if ns == "." {
							ns = cfg.Namespace
						}
						if _, ok := nssSvcs[ns]; !ok {
							nssSvcs[ns] = map[string]bool{}
						}

						// test/a
						// test/a
						// test/*
						if svc != "*" {
							if _, ok := nssSvcs[ns][svc]; ok || nssSvcs[ns]["*"] {
								// already exists
								// TODO: prevent this invalid setting, maybe in 1.12+
								errs = appendValidation(errs, WrapWarning(fmt.Errorf("duplicated egress host: %s", hostname)))
							}
						} else {
							if len(nssSvcs[ns]) != 0 {
								errs = appendValidation(errs, WrapWarning(fmt.Errorf("duplicated egress host: %s", hostname)))
							}
						}
						nssSvcs[ns][svc] = true
					}
					errs = appendValidation(errs, validateNamespaceSlashWildcardHostname(hostname, false))
				}
				// */*
				// test/a
				if nssSvcs["*"]["*"] && len(nssSvcs) != 1 {
					errs = appendValidation(errs, WrapWarning(fmt.Errorf("`*/*` host select all resources, no other hosts can be added")))
				}
			}
		}

		errs = appendValidation(errs, validateSidecarOutboundTrafficPolicy(rule.OutboundTrafficPolicy))

		return errs.Unwrap()
	})

func validateSidecarOutboundTrafficPolicy(tp *networking.OutboundTrafficPolicy) (errs error) {
	if tp == nil {
		return
	}
	mode := tp.GetMode()
	if tp.EgressProxy != nil {
		if mode != networking.OutboundTrafficPolicy_ALLOW_ANY {
			errs = appendErrors(errs, fmt.Errorf("sidecar: egress_proxy must be set only with ALLOW_ANY outbound_traffic_policy mode"))
			return
		}

		errs = appendErrors(errs, ValidateFQDN(tp.EgressProxy.GetHost()))

		if tp.EgressProxy.Port == nil {
			errs = appendErrors(errs, fmt.Errorf("sidecar: egress_proxy port must be non-nil"))
			return
		}
		errs = appendErrors(errs, validateDestination(tp.EgressProxy))
	}
	return
}

func validateSidecarEgressPortBindAndCaptureMode(port *networking.Port, bind string,
	captureMode networking.CaptureMode) (errs error) {
	// Port name is optional. Validate if exists.
	if len(port.Name) > 0 {
		errs = appendErrors(errs, ValidatePortName(port.Name))
	}

	// Handle Unix domain sockets
	if port.Number == 0 {
		// require bind to be a unix domain socket
		errs = appendErrors(errs,
			ValidateProtocol(port.Protocol))

		if !strings.HasPrefix(bind, UnixAddressPrefix) {
			errs = appendErrors(errs, fmt.Errorf("sidecar: ports with 0 value must have a unix domain socket bind address"))
		} else {
			errs = appendErrors(errs, ValidateUnixAddress(strings.TrimPrefix(bind, UnixAddressPrefix)))
		}

		if captureMode != networking.CaptureMode_DEFAULT && captureMode != networking.CaptureMode_NONE {
			errs = appendErrors(errs, fmt.Errorf("sidecar: captureMode must be DEFAULT/NONE for unix domain socket listeners"))
		}
	} else {
		errs = appendErrors(errs,
			ValidateProtocol(port.Protocol),
			ValidatePort(int(port.Number)))

		if len(bind) != 0 {
			errs = appendErrors(errs, ValidateIPAddress(bind))
		}
	}

	return
}

func validateSidecarIngressPortAndBind(port *networking.Port, bind string) (errs error) {
	// Port name is optional. Validate if exists.
	if len(port.Name) > 0 {
		errs = appendErrors(errs, ValidatePortName(port.Name))
	}

	errs = appendErrors(errs,
		ValidateProtocol(port.Protocol),
		ValidatePort(int(port.Number)))

	if len(bind) != 0 {
		errs = appendErrors(errs, ValidateIPAddress(bind))
	}

	return
}

func validateTrafficPolicy(policy *networking.TrafficPolicy) Validation {
	if policy == nil {
		return Validation{}
	}
	if policy.OutlierDetection == nil && policy.ConnectionPool == nil &&
		policy.LoadBalancer == nil && policy.Tls == nil && policy.PortLevelSettings == nil {
		return WrapError(fmt.Errorf("traffic policy must have at least one field"))
	}

	return appendValidation(validateOutlierDetection(policy.OutlierDetection),
		validateConnectionPool(policy.ConnectionPool),
		validateLoadBalancer(policy.LoadBalancer),
		validateTLS(policy.Tls),
		validatePortTrafficPolicies(policy.PortLevelSettings))
}

func validateOutlierDetection(outlier *networking.OutlierDetection) (errs Validation) {
	if outlier == nil {
		return
	}

	if outlier.BaseEjectionTime != nil {
		errs = appendValidation(errs, ValidateDuration(outlier.BaseEjectionTime))
	}
	// nolint: staticcheck
	if outlier.ConsecutiveErrors != 0 {
		warn := "outlier detection consecutive errors is deprecated, use consecutiveGatewayErrors or consecutive5xxErrors instead"
		scope.Warnf(warn)
		errs = appendValidation(errs, WrapWarning(errors.New(warn)))
	}
	if !outlier.SplitExternalLocalOriginErrors && outlier.ConsecutiveLocalOriginFailures.GetValue() > 0 {
		err := "outlier detection consecutive local origin failures is specified, but split external local origin errors is set to false"
		errs = appendValidation(errs, errors.New(err))
	}
	if outlier.Interval != nil {
		errs = appendValidation(errs, ValidateDuration(outlier.Interval))
	}
	errs = appendValidation(errs, ValidatePercent(outlier.MaxEjectionPercent), ValidatePercent(outlier.MinHealthPercent))

	return
}

func validateConnectionPool(settings *networking.ConnectionPoolSettings) (errs error) {
	if settings == nil {
		return
	}
	if settings.Http == nil && settings.Tcp == nil {
		return fmt.Errorf("connection pool must have at least one field")
	}

	if httpSettings := settings.Http; httpSettings != nil {
		if httpSettings.Http1MaxPendingRequests < 0 {
			errs = appendErrors(errs, fmt.Errorf("http1 max pending requests must be non-negative"))
		}
		if httpSettings.Http2MaxRequests < 0 {
			errs = appendErrors(errs, fmt.Errorf("http2 max requests must be non-negative"))
		}
		if httpSettings.MaxRequestsPerConnection < 0 {
			errs = appendErrors(errs, fmt.Errorf("max requests per connection must be non-negative"))
		}
		if httpSettings.MaxRetries < 0 {
			errs = appendErrors(errs, fmt.Errorf("max retries must be non-negative"))
		}
		if httpSettings.IdleTimeout != nil {
			errs = appendErrors(errs, ValidateDuration(httpSettings.IdleTimeout))
		}
		if httpSettings.H2UpgradePolicy == networking.ConnectionPoolSettings_HTTPSettings_UPGRADE && httpSettings.UseClientProtocol {
			errs = appendErrors(errs, fmt.Errorf("use client protocol must not be true when H2UpgradePolicy is UPGRADE"))
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

func validateLoadBalancer(settings *networking.LoadBalancerSettings) (errs error) {
	if settings == nil {
		return
	}

	// simple load balancing is always valid

	consistentHash := settings.GetConsistentHash()
	if consistentHash != nil {
		httpCookie := consistentHash.GetHttpCookie()
		if httpCookie != nil {
			if httpCookie.Name == "" {
				errs = appendErrors(errs, fmt.Errorf("name required for HttpCookie"))
			}
			if httpCookie.Ttl == nil {
				errs = appendErrors(errs, fmt.Errorf("ttl required for HttpCookie"))
			}
		}
	}
	if err := validateLocalityLbSetting(settings.LocalityLbSetting); err != nil {
		errs = multierror.Append(errs, err)
	}
	return
}

func validateTLS(settings *networking.ClientTLSSettings) (errs error) {
	if settings == nil {
		return
	}

	if (settings.Mode == networking.ClientTLSSettings_SIMPLE || settings.Mode == networking.ClientTLSSettings_MUTUAL) &&
		settings.CredentialName != "" {
		if settings.ClientCertificate != "" || settings.CaCertificates != "" || settings.PrivateKey != "" {
			errs = appendErrors(errs,
				fmt.Errorf("cannot specify client certificates or CA certificate If credentialName is set"))
		}

		// If tls mode is SIMPLE or MUTUAL, and CredentialName is specified, credentials are fetched
		// remotely. ServerCertificate and CaCertificates fields are not required.
		return
	}

	if settings.Mode == networking.ClientTLSSettings_MUTUAL {
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
		labels.Instance(subset.Labels).Validate(),
		validateTrafficPolicy(subset.TrafficPolicy))
}

func validatePortTrafficPolicies(pls []*networking.TrafficPolicy_PortTrafficPolicy) (errs error) {
	for _, t := range pls {
		if t == nil {
			errs = appendErrors(errs, fmt.Errorf("traffic policy may not be null"))
			continue
		}
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
	hostname, p, err := net.SplitHostPort(hostAddr)
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
	if err = ValidateFQDN(hostname); err != nil {
		ip := net.ParseIP(hostname)
		if ip == nil {
			return fmt.Errorf("%q is not a valid hostname or an IP address", hostname)
		}
	}

	return nil
}

// ValidateDuration checks that a proto duration is well-formed
func ValidateDuration(pd *durationpb.Duration) error {
	dur := pd.AsDuration()
	if dur < time.Millisecond {
		return errors.New("duration must be greater than 1ms")
	}
	if dur%time.Millisecond != 0 {
		return errors.New("only durations to ms precision are supported")
	}
	return nil
}

// ValidateDurationRange verifies range is in specified duration
func ValidateDurationRange(dur, min, max time.Duration) error {
	if dur > max || dur < min {
		return fmt.Errorf("time %v must be >%v and <%v", dur.String(), min.String(), max.String())
	}

	return nil
}

// ValidateParentAndDrain checks that parent and drain durations are valid
func ValidateParentAndDrain(drainTime, parentShutdown *durationpb.Duration) (errs error) {
	if err := ValidateDuration(drainTime); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid drain duration:"))
	}
	if err := ValidateDuration(parentShutdown); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid parent shutdown duration:"))
	}
	if errs != nil {
		return
	}

	drainDuration := drainTime.AsDuration()
	parentShutdownDuration := parentShutdown.AsDuration()

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

// ValidateLightstepCollector validates the configuration for sending envoy spans to LightStep
func ValidateLightstepCollector(ls *meshconfig.Tracing_Lightstep) error {
	var errs error
	if ls.GetAddress() == "" {
		errs = multierror.Append(errs, errors.New("address is required"))
	}
	if err := ValidateProxyAddress(ls.GetAddress()); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid lightstep address:"))
	}
	if ls.GetAccessToken() == "" {
		errs = multierror.Append(errs, errors.New("access token is required"))
	}
	return errs
}

// validateCustomTags validates that tracing CustomTags map does not contain any nil items
func validateCustomTags(tags map[string]*meshconfig.Tracing_CustomTag) error {
	for tagName, tagVal := range tags {
		if tagVal == nil {
			return fmt.Errorf("encountered nil value for custom tag: %s", tagName)
		}
	}
	return nil
}

// ValidateZipkinCollector validates the configuration for sending envoy spans to Zipkin
func ValidateZipkinCollector(z *meshconfig.Tracing_Zipkin) error {
	return ValidateProxyAddress(strings.Replace(z.GetAddress(), "$(HOST_IP)", "127.0.0.1", 1))
}

// ValidateDatadogCollector validates the configuration for sending envoy spans to Datadog
func ValidateDatadogCollector(d *meshconfig.Tracing_Datadog) error {
	// If the address contains $(HOST_IP), replace it with a valid IP before validation.
	return ValidateProxyAddress(strings.Replace(d.GetAddress(), "$(HOST_IP)", "127.0.0.1", 1))
}

// ValidateConnectTimeout validates the envoy connection timeout
func ValidateConnectTimeout(timeout *durationpb.Duration) error {
	if err := ValidateDuration(timeout); err != nil {
		return err
	}

	err := ValidateDurationRange(timeout.AsDuration(), connectTimeoutMin, connectTimeoutMax)
	return err
}

// ValidateProtocolDetectionTimeout validates the envoy protocol detection timeout
func ValidateProtocolDetectionTimeout(timeout *durationpb.Duration) error {
	dur := timeout.AsDuration()
	// 0s is a valid value if trying to disable protocol detection timeout
	if dur == time.Second*0 {
		return nil
	}
	if dur%time.Millisecond != 0 {
		return errors.New("only durations to ms precision are supported")
	}

	return nil
}

// ValidateMaxServerConnectionAge validate negative duration
func ValidateMaxServerConnectionAge(in time.Duration) error {
	if err := IsNegativeDuration(in); err != nil {
		return fmt.Errorf("%v: --keepaliveMaxServerConnectionAge only accepts positive duration eg: 30m", err)
	}
	return nil
}

// IsNegativeDuration check if the duration is negative
func IsNegativeDuration(in time.Duration) error {
	if in < 0 {
		return fmt.Errorf("invalid duration: %s", in.String())
	}
	return nil
}

// ValidateMeshConfig checks that the mesh config is well-formed
func ValidateMeshConfig(mesh *meshconfig.MeshConfig) (errs error) {
	if err := ValidatePort(int(mesh.ProxyListenPort)); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid proxy listen port:"))
	}

	if err := ValidateConnectTimeout(mesh.ConnectTimeout); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid connect timeout:"))
	}

	if err := ValidateProtocolDetectionTimeout(mesh.ProtocolDetectionTimeout); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid protocol detection timeout:"))
	}

	if mesh.DefaultConfig == nil {
		errs = multierror.Append(errs, errors.New("missing default config"))
	} else if err := ValidateMeshConfigProxyConfig(mesh.DefaultConfig); err != nil {
		errs = multierror.Append(errs, err)
	}

	if err := validateLocalityLbSetting(mesh.LocalityLbSetting); err != nil {
		errs = multierror.Append(errs, err)
	}

	if err := validateServiceSettings(mesh); err != nil {
		errs = multierror.Append(errs, err)
	}

	if err := validateTrustDomainConfig(mesh); err != nil {
		errs = multierror.Append(errs, err)
	}

	if err := validateExtensionProvider(mesh); err != nil {
		scope.Warnf("found invalid extension provider (can be ignored if the given extension provider is not used): %v", err)
	}

	return
}

func validateTrustDomainConfig(config *meshconfig.MeshConfig) (errs error) {
	if err := ValidateTrustDomain(config.TrustDomain); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("trustDomain: %v", err))
	}
	for i, tda := range config.TrustDomainAliases {
		if err := ValidateTrustDomain(tda); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("trustDomainAliases[%d], domain `%s` : %v", i, tda, err))
		}
	}
	return
}

func validateServiceSettings(config *meshconfig.MeshConfig) (errs error) {
	for sIndex, s := range config.ServiceSettings {
		for _, h := range s.Hosts {
			if err := ValidateWildcardDomain(h); err != nil {
				errs = multierror.Append(errs, fmt.Errorf("serviceSettings[%d], host `%s`: %v", sIndex, h, err))
			}
		}
	}
	return
}

func validatePrivateKeyProvider(pkpConf *meshconfig.PrivateKeyProvider) error {
	var errs error
	if pkpConf.GetProvider() == nil {
		errs = multierror.Append(errs, errors.New("private key provider confguration is required"))
	}

	switch pkpConf.GetProvider().(type) {
	case *meshconfig.PrivateKeyProvider_Cryptomb:
		cryptomb := pkpConf.GetCryptomb()
		if cryptomb == nil {
			errs = multierror.Append(errs, errors.New("cryptomb confguration is required"))
		} else {
			pollDelay := cryptomb.GetPollDelay()
			if pollDelay == nil {
				errs = multierror.Append(errs, errors.New("pollDelay is required"))
			} else if pollDelay.GetSeconds() == 0 && pollDelay.GetNanos() == 0 {
				errs = multierror.Append(errs, errors.New("pollDelay must be non zero"))
			}
		}
	default:
		errs = multierror.Append(errs, errors.New("unknown private key provider"))
	}

	return errs
}

// ValidateMeshConfigProxyConfig checks that the mesh config is well-formed
func ValidateMeshConfigProxyConfig(config *meshconfig.ProxyConfig) (errs error) {
	if config.ConfigPath == "" {
		errs = multierror.Append(errs, errors.New("config path must be set"))
	}

	if config.BinaryPath == "" {
		errs = multierror.Append(errs, errors.New("binary path must be set"))
	}

	clusterName := config.GetClusterName()
	switch naming := clusterName.(type) {
	case *meshconfig.ProxyConfig_ServiceCluster:
		if naming.ServiceCluster == "" {
			errs = multierror.Append(errs, errors.New("service cluster must be specified"))
		}
	case *meshconfig.ProxyConfig_TracingServiceName_: // intentionally left empty for now
	default:
		errs = multierror.Append(errs, errors.New("oneof service cluster or tracing service name must be specified"))
	}

	if err := ValidateParentAndDrain(config.DrainDuration, config.ParentShutdownDuration); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid parent and drain time combination"))
	}

	// discovery address is mandatory since mutual TLS relies on CDS.
	// strictly speaking, proxies can operate without RDS/CDS and with hot restarts
	// but that requires additional test validation
	if config.DiscoveryAddress == "" {
		errs = multierror.Append(errs, errors.New("discovery address must be set to the proxy discovery service"))
	} else if err := ValidateProxyAddress(config.DiscoveryAddress); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid discovery address:"))
	}

	if tracer := config.GetTracing().GetLightstep(); tracer != nil {
		if err := ValidateLightstepCollector(tracer); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "invalid lightstep config:"))
		}
	}

	if tracer := config.GetTracing().GetZipkin(); tracer != nil {
		if err := ValidateZipkinCollector(tracer); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "invalid zipkin config:"))
		}
	}

	if tracer := config.GetTracing().GetDatadog(); tracer != nil {
		if err := ValidateDatadogCollector(tracer); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "invalid datadog config:"))
		}
	}

	if tracer := config.GetTracing().GetTlsSettings(); tracer != nil {
		if err := validateTLS(tracer); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "invalid tracing TLS config:"))
		}
	}

	if tracerCustomTags := config.GetTracing().GetCustomTags(); tracerCustomTags != nil {
		if err := validateCustomTags(tracerCustomTags); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "invalid tracing custom tags:"))
		}
	}

	if config.StatsdUdpAddress != "" {
		if err := ValidateProxyAddress(config.StatsdUdpAddress); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("invalid statsd udp address %q:", config.StatsdUdpAddress)))
		}
	}

	// nolint: staticcheck
	if config.EnvoyMetricsServiceAddress != "" {
		if err := ValidateProxyAddress(config.EnvoyMetricsServiceAddress); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("invalid envoy metrics service address %q:", config.EnvoyMetricsServiceAddress)))
		} else {
			scope.Warnf("EnvoyMetricsServiceAddress is deprecated, use EnvoyMetricsService instead.") // nolint: golint,stylecheck
		}
	}

	if config.EnvoyMetricsService != nil && config.EnvoyMetricsService.Address != "" {
		if err := ValidateProxyAddress(config.EnvoyMetricsService.Address); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("invalid envoy metrics service address %q:", config.EnvoyMetricsService.Address)))
		}
	}

	if config.EnvoyAccessLogService != nil && config.EnvoyAccessLogService.Address != "" {
		if err := ValidateProxyAddress(config.EnvoyAccessLogService.Address); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("invalid envoy access log service address %q:", config.EnvoyAccessLogService.Address)))
		}
	}

	if err := ValidatePort(int(config.ProxyAdminPort)); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid proxy admin port:"))
	}

	if err := ValidateControlPlaneAuthPolicy(config.ControlPlaneAuthPolicy); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid authentication policy:"))
	}

	if err := ValidatePort(int(config.StatusPort)); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid status port:"))
	}

	if pkpConf := config.GetPrivateKeyProvider(); pkpConf != nil {
		if err := validatePrivateKeyProvider(pkpConf); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, "invalid private key provider confguration:"))
		}
	}

	return
}

func ValidateControlPlaneAuthPolicy(policy meshconfig.AuthenticationPolicy) error {
	if policy == meshconfig.AuthenticationPolicy_NONE || policy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
		return nil
	}
	return fmt.Errorf("unrecognized control plane auth policy %q", policy)
}

func validateWorkloadSelector(selector *type_beta.WorkloadSelector) error {
	var errs error
	if selector != nil {
		for k, v := range selector.MatchLabels {
			if k == "" {
				errs = appendErrors(errs,
					fmt.Errorf("empty key is not supported in selector: %q", fmt.Sprintf("%s=%s", k, v)))
			}
			if strings.Contains(k, "*") || strings.Contains(v, "*") {
				errs = appendErrors(errs,
					fmt.Errorf("wildcard is not supported in selector: %q", fmt.Sprintf("%s=%s", k, v)))
			}
		}
	}

	return errs
}

// ValidateAuthorizationPolicy checks that AuthorizationPolicy is well-formed.
var ValidateAuthorizationPolicy = registerValidateFunc("ValidateAuthorizationPolicy",
	func(cfg config.Config) (Warning, error) {
		in, ok := cfg.Spec.(*security_beta.AuthorizationPolicy)
		if !ok {
			return nil, fmt.Errorf("cannot cast to AuthorizationPolicy")
		}

		var errs error
		if err := validateWorkloadSelector(in.Selector); err != nil {
			errs = appendErrors(errs, err)
		}

		if in.Action == security_beta.AuthorizationPolicy_CUSTOM {
			if in.Rules == nil {
				errs = appendErrors(errs, fmt.Errorf("CUSTOM action without `rules` is meaningless as it will never be triggered, "+
					"add an empty rule `{}` if you want it be triggered for every request"))
			} else {
				if in.GetProvider() == nil || in.GetProvider().GetName() == "" {
					errs = appendErrors(errs, fmt.Errorf("`provider.name` must not be empty"))
				}
			}
			// TODO(yangminzhu): Add support for more matching rules.
			for _, rule := range in.GetRules() {
				check := func(invalid bool, name string) error {
					if invalid {
						return fmt.Errorf("%s is currently not supported with CUSTOM action", name)
					}
					return nil
				}
				for _, from := range rule.GetFrom() {
					if src := from.GetSource(); src != nil {
						errs = appendErrors(errs, check(len(src.Namespaces) != 0, "From.Namespaces"))
						errs = appendErrors(errs, check(len(src.NotNamespaces) != 0, "From.NotNamespaces"))
						errs = appendErrors(errs, check(len(src.Principals) != 0, "From.Principals"))
						errs = appendErrors(errs, check(len(src.NotPrincipals) != 0, "From.NotPrincipals"))
						errs = appendErrors(errs, check(len(src.RequestPrincipals) != 0, "From.RequestPrincipals"))
						errs = appendErrors(errs, check(len(src.NotRequestPrincipals) != 0, "From.NotRequestPrincipals"))
					}
				}
				for _, when := range rule.GetWhen() {
					errs = appendErrors(errs, check(when.Key == "source.namespace", when.Key))
					errs = appendErrors(errs, check(when.Key == "source.principal", when.Key))
					errs = appendErrors(errs, check(strings.HasPrefix(when.Key, "request.auth."), when.Key))
				}
			}
		}
		if in.GetProvider() != nil && in.Action != security_beta.AuthorizationPolicy_CUSTOM {
			errs = appendErrors(errs, fmt.Errorf("`provider` must not be with non CUSTOM action, found %s", in.Action))
		}

		if in.Action == security_beta.AuthorizationPolicy_DENY && in.Rules == nil {
			errs = appendErrors(errs, fmt.Errorf("DENY action without `rules` is meaningless as it will never be triggered, "+
				"add an empty rule `{}` if you want it be triggered for every request"))
		}

		for i, rule := range in.GetRules() {
			if rule == nil {
				errs = appendErrors(errs, fmt.Errorf("`rule` must not be nil, found at rule %d", i))
				continue
			}
			if rule.From != nil && len(rule.From) == 0 {
				errs = appendErrors(errs, fmt.Errorf("`from` must not be empty, found at rule %d", i))
			}
			for _, from := range rule.From {
				if from == nil {
					errs = appendErrors(errs, fmt.Errorf("`from` must not be nil, found at rule %d", i))
					continue
				}
				if from.Source == nil {
					errs = appendErrors(errs, fmt.Errorf("`from.source` must not be nil, found at rule %d", i))
				} else {
					src := from.Source
					if len(src.Principals) == 0 && len(src.RequestPrincipals) == 0 && len(src.Namespaces) == 0 && len(src.IpBlocks) == 0 &&
						len(src.RemoteIpBlocks) == 0 && len(src.NotPrincipals) == 0 && len(src.NotRequestPrincipals) == 0 && len(src.NotNamespaces) == 0 &&
						len(src.NotIpBlocks) == 0 && len(src.NotRemoteIpBlocks) == 0 {
						errs = appendErrors(errs, fmt.Errorf("`from.source` must not be empty, found at rule %d", i))
					}
					errs = appendErrors(errs, security.ValidateIPs(from.Source.GetIpBlocks()))
					errs = appendErrors(errs, security.ValidateIPs(from.Source.GetNotIpBlocks()))
					errs = appendErrors(errs, security.ValidateIPs(from.Source.GetRemoteIpBlocks()))
					errs = appendErrors(errs, security.ValidateIPs(from.Source.GetNotRemoteIpBlocks()))
					errs = appendErrors(errs, security.CheckEmptyValues("Principals", src.Principals))
					errs = appendErrors(errs, security.CheckEmptyValues("RequestPrincipals", src.RequestPrincipals))
					errs = appendErrors(errs, security.CheckEmptyValues("Namespaces", src.Namespaces))
					errs = appendErrors(errs, security.CheckEmptyValues("IpBlocks", src.IpBlocks))
					errs = appendErrors(errs, security.CheckEmptyValues("RemoteIpBlocks", src.RemoteIpBlocks))
					errs = appendErrors(errs, security.CheckEmptyValues("NotPrincipals", src.NotPrincipals))
					errs = appendErrors(errs, security.CheckEmptyValues("NotRequestPrincipals", src.NotRequestPrincipals))
					errs = appendErrors(errs, security.CheckEmptyValues("NotNamespaces", src.NotNamespaces))
					errs = appendErrors(errs, security.CheckEmptyValues("NotIpBlocks", src.NotIpBlocks))
					errs = appendErrors(errs, security.CheckEmptyValues("NotRemoteIpBlocks", src.NotRemoteIpBlocks))
				}
			}
			if rule.To != nil && len(rule.To) == 0 {
				errs = appendErrors(errs, fmt.Errorf("`to` must not be empty, found at rule %d", i))
			}
			for _, to := range rule.To {
				if to == nil {
					errs = appendErrors(errs, fmt.Errorf("`to` must not be nil, found at rule %d", i))
					continue
				}
				if to.Operation == nil {
					errs = appendErrors(errs, fmt.Errorf("`to.operation` must not be nil, found at rule %d", i))
				} else {
					op := to.Operation
					if len(op.Ports) == 0 && len(op.Methods) == 0 && len(op.Paths) == 0 && len(op.Hosts) == 0 &&
						len(op.NotPorts) == 0 && len(op.NotMethods) == 0 && len(op.NotPaths) == 0 && len(op.NotHosts) == 0 {
						errs = appendErrors(errs, fmt.Errorf("`to.operation` must not be empty, found at rule %d", i))
					}
					errs = appendErrors(errs, security.ValidatePorts(to.Operation.GetPorts()))
					errs = appendErrors(errs, security.ValidatePorts(to.Operation.GetNotPorts()))
					errs = appendErrors(errs, security.CheckEmptyValues("Ports", op.Ports))
					errs = appendErrors(errs, security.CheckEmptyValues("Methods", op.Methods))
					errs = appendErrors(errs, security.CheckEmptyValues("Paths", op.Paths))
					errs = appendErrors(errs, security.CheckEmptyValues("Hosts", op.Hosts))
					errs = appendErrors(errs, security.CheckEmptyValues("NotPorts", op.NotPorts))
					errs = appendErrors(errs, security.CheckEmptyValues("NotMethods", op.NotMethods))
					errs = appendErrors(errs, security.CheckEmptyValues("NotPaths", op.NotPaths))
					errs = appendErrors(errs, security.CheckEmptyValues("NotHosts", op.NotHosts))
				}
			}
			for _, condition := range rule.GetWhen() {
				key := condition.GetKey()
				if key == "" {
					errs = appendErrors(errs, fmt.Errorf("`key` must not be empty"))
				} else {
					if len(condition.GetValues()) == 0 && len(condition.GetNotValues()) == 0 {
						errs = appendErrors(errs, fmt.Errorf("at least one of `values` or `notValues` must be set for key %s",
							key))
					} else {
						if err := security.ValidateAttribute(key, condition.GetValues()); err != nil {
							errs = appendErrors(errs, fmt.Errorf("invalid `value` for `key` %s: %v", key, err))
						}
						if err := security.ValidateAttribute(key, condition.GetNotValues()); err != nil {
							errs = appendErrors(errs, fmt.Errorf("invalid `notValue` for `key` %s: %v", key, err))
						}
					}
				}
			}
		}
		return nil, multierror.Prefix(errs, fmt.Sprintf("invalid policy %s.%s:", cfg.Name, cfg.Namespace))
	})

// ValidateRequestAuthentication checks that request authentication spec is well-formed.
var ValidateRequestAuthentication = registerValidateFunc("ValidateRequestAuthentication",
	func(cfg config.Config) (Warning, error) {
		in, ok := cfg.Spec.(*security_beta.RequestAuthentication)
		if !ok {
			return nil, errors.New("cannot cast to RequestAuthentication")
		}

		var errs error
		errs = appendErrors(errs, validateWorkloadSelector(in.Selector))

		for _, rule := range in.JwtRules {
			errs = appendErrors(errs, validateJwtRule(rule))
		}
		return nil, errs
	})

func validateJwtRule(rule *security_beta.JWTRule) (errs error) {
	if rule == nil {
		return nil
	}
	if len(rule.Issuer) == 0 {
		errs = multierror.Append(errs, errors.New("issuer must be set"))
	}
	for _, audience := range rule.Audiences {
		if len(audience) == 0 {
			errs = multierror.Append(errs, errors.New("audience must be non-empty string"))
		}
	}

	if len(rule.JwksUri) != 0 {
		if _, err := security.ParseJwksURI(rule.JwksUri); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if rule.Jwks != "" {
		_, err := jwk.Parse([]byte(rule.Jwks))
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("jwks parse error: %v", err))
		}
	}

	for _, location := range rule.FromHeaders {
		if location == nil {
			errs = multierror.Append(errs, errors.New("location header name must be non-null"))
			continue
		}
		if len(location.Name) == 0 {
			errs = multierror.Append(errs, errors.New("location header name must be non-empty string"))
		}
	}

	for _, location := range rule.FromParams {
		if len(location) == 0 {
			errs = multierror.Append(errs, errors.New("location query must be non-empty string"))
		}
	}
	return
}

// ValidatePeerAuthentication checks that peer authentication spec is well-formed.
var ValidatePeerAuthentication = registerValidateFunc("ValidatePeerAuthentication",
	func(cfg config.Config) (Warning, error) {
		in, ok := cfg.Spec.(*security_beta.PeerAuthentication)
		if !ok {
			return nil, errors.New("cannot cast to PeerAuthentication")
		}

		var errs error
		emptySelector := in.Selector == nil || len(in.Selector.MatchLabels) == 0

		if emptySelector && len(in.PortLevelMtls) != 0 {
			errs = appendErrors(errs,
				fmt.Errorf("mesh/namespace peer authentication cannot have port level mTLS"))
		}

		if in.PortLevelMtls != nil && len(in.PortLevelMtls) == 0 {
			errs = appendErrors(errs,
				fmt.Errorf("port level mTLS, if defined, must have at least one element"))
		}

		for port := range in.PortLevelMtls {
			if port == 0 {
				errs = appendErrors(errs, fmt.Errorf("port cannot be 0"))
			}
		}

		errs = appendErrors(errs, validateWorkloadSelector(in.Selector))

		return nil, errs
	})

// ValidateVirtualService checks that a v1alpha3 route rule is well-formed.
var ValidateVirtualService = registerValidateFunc("ValidateVirtualService",
	func(cfg config.Config) (Warning, error) {
		virtualService, ok := cfg.Spec.(*networking.VirtualService)
		if !ok {
			return nil, errors.New("cannot cast to virtual service")
		}
		errs := Validation{}
		if len(virtualService.Hosts) == 0 {
			// This must be delegate - enforce delegate validations.
			if len(virtualService.Gateways) != 0 {
				// meaningless to specify gateways in delegate
				errs = appendValidation(errs, fmt.Errorf("delegate virtual service must have no gateways specified"))
			}
			if len(virtualService.Tls) != 0 {
				// meaningless to specify tls in delegate, we donot support tls delegate
				errs = appendValidation(errs, fmt.Errorf("delegate virtual service must have no tls route specified"))
			}
			if len(virtualService.Tcp) != 0 {
				// meaningless to specify tls in delegate, we donot support tcp delegate
				errs = appendValidation(errs, fmt.Errorf("delegate virtual service must have no tcp route specified"))
			}
		}

		appliesToMesh := false
		appliesToGateway := false
		if len(virtualService.Gateways) == 0 {
			appliesToMesh = true
		} else {
			errs = appendValidation(errs, validateGatewayNames(virtualService.Gateways))
			for _, gatewayName := range virtualService.Gateways {
				if gatewayName == constants.IstioMeshGateway {
					appliesToMesh = true
				} else {
					appliesToGateway = true
				}
			}
		}

		if !appliesToGateway {
			validateJWTClaimRoute := func(headers map[string]*networking.StringMatch) {
				for key := range headers {
					if strings.HasPrefix(key, constant.HeaderJWTClaim) {
						msg := fmt.Sprintf("JWT claim based routing (key: %s) is only supported for gateway, found no gateways: %v", key, virtualService.Gateways)
						errs = appendValidation(errs, errors.New(msg))
					}
				}
			}
			for _, http := range virtualService.GetHttp() {
				for _, m := range http.GetMatch() {
					validateJWTClaimRoute(m.GetHeaders())
					validateJWTClaimRoute(m.GetWithoutHeaders())
				}
			}
		}

		allHostsValid := true
		for _, virtualHost := range virtualService.Hosts {
			if err := ValidateWildcardDomain(virtualHost); err != nil {
				ipAddr := net.ParseIP(virtualHost) // Could also be an IP
				if ipAddr == nil {
					errs = appendValidation(errs, err)
					allHostsValid = false
				}
			} else if appliesToMesh && virtualHost == "*" {
				errs = appendValidation(errs, fmt.Errorf("wildcard host * is not allowed for virtual services bound to the mesh gateway"))
				allHostsValid = false
			}
		}

		// Check for duplicate hosts
		// Duplicates include literal duplicates as well as wildcard duplicates
		// E.g., *.foo.com, and *.com are duplicates in the same virtual service
		if allHostsValid {
			for i := 0; i < len(virtualService.Hosts); i++ {
				hostI := host.Name(virtualService.Hosts[i])
				for j := i + 1; j < len(virtualService.Hosts); j++ {
					hostJ := host.Name(virtualService.Hosts[j])
					if hostI.Matches(hostJ) {
						errs = appendValidation(errs, fmt.Errorf("duplicate hosts in virtual service: %s & %s", hostI, hostJ))
					}
				}
			}
		}

		if len(virtualService.Http) == 0 && len(virtualService.Tcp) == 0 && len(virtualService.Tls) == 0 {
			errs = appendValidation(errs, errors.New("http, tcp or tls must be provided in virtual service"))
		}
		for _, httpRoute := range virtualService.Http {
			if httpRoute == nil {
				errs = appendValidation(errs, errors.New("http route may not be null"))
				continue
			}
			errs = appendValidation(errs, validateHTTPRoute(httpRoute, len(virtualService.Hosts) == 0))
		}
		for _, tlsRoute := range virtualService.Tls {
			errs = appendValidation(errs, validateTLSRoute(tlsRoute, virtualService))
		}
		for _, tcpRoute := range virtualService.Tcp {
			errs = appendValidation(errs, validateTCPRoute(tcpRoute))
		}

		errs = appendValidation(errs, validateExportTo(cfg.Namespace, virtualService.ExportTo, false, false))

		warnUnused := func(ruleno, reason string) {
			errs = appendValidation(errs, WrapWarning(&AnalysisAwareError{
				Type:       "VirtualServiceUnreachableRule",
				Msg:        fmt.Sprintf("virtualService rule %v not used (%s)", ruleno, reason),
				Parameters: []interface{}{ruleno, reason},
			}))
		}
		warnIneffective := func(ruleno, matchno, dupno string) {
			errs = appendValidation(errs, WrapWarning(&AnalysisAwareError{
				Type:       "VirtualServiceIneffectiveMatch",
				Msg:        fmt.Sprintf("virtualService rule %v match %v is not used (duplicate/overlapping match in rule %v)", ruleno, matchno, dupno),
				Parameters: []interface{}{ruleno, matchno, dupno},
			}))
		}

		analyzeUnreachableHTTPRules(virtualService.Http, warnUnused, warnIneffective)
		analyzeUnreachableTCPRules(virtualService.Tcp, warnUnused, warnIneffective)
		analyzeUnreachableTLSRules(virtualService.Tls, warnUnused, warnIneffective)

		return errs.Unwrap()
	})

func assignExactOrPrefix(exact, prefix string) string {
	if exact != "" {
		return matchExact + exact
	}
	if prefix != "" {
		return matchPrefix + prefix
	}
	return ""
}

// genMatchHTTPRoutes build the match rules into struct OverlappingMatchValidationForHTTPRoute
// based on particular HTTPMatchRequest, according to comments on https://github.com/istio/istio/pull/32701
// only support Match's port, method, authority, headers, query params and nonheaders for now.
func genMatchHTTPRoutes(route *networking.HTTPRoute, match *networking.HTTPMatchRequest,
	rulen, matchn int) (matchHTTPRoutes *OverlappingMatchValidationForHTTPRoute) {
	// skip current match if no match field for current route
	if match == nil {
		return nil
	}
	// skip current match if no URI field
	if match.Uri == nil {
		return nil
	}
	// store all httproute with prefix match uri
	tmpPrefix := match.Uri.GetPrefix()
	if tmpPrefix != "" {
		// set Method
		methodExact := match.Method.GetExact()
		methodPrefix := match.Method.GetPrefix()
		methodMatch := assignExactOrPrefix(methodExact, methodPrefix)
		// if no method information, it should be GET by default
		if methodMatch == "" {
			methodMatch = matchExact + "GET"
		}

		// set Authority
		authorityExact := match.Authority.GetExact()
		authorityPrefix := match.Authority.GetPrefix()
		authorityMatch := assignExactOrPrefix(authorityExact, authorityPrefix)

		// set Headers
		headerMap := make(map[string]string)
		for hkey, hvalue := range match.Headers {
			hvalueExact := hvalue.GetExact()
			hvaluePrefix := hvalue.GetPrefix()
			hvalueMatch := assignExactOrPrefix(hvalueExact, hvaluePrefix)
			headerMap[hkey] = hvalueMatch
		}

		// set QueryParams
		QPMap := make(map[string]string)
		for qpkey, qpvalue := range match.QueryParams {
			qpvalueExact := qpvalue.GetExact()
			qpvaluePrefix := qpvalue.GetPrefix()
			qpvalueMatch := assignExactOrPrefix(qpvalueExact, qpvaluePrefix)
			QPMap[qpkey] = qpvalueMatch
		}

		// set WithoutHeaders
		noHeaderMap := make(map[string]string)
		for nhkey, nhvalue := range match.WithoutHeaders {
			nhvalueExact := nhvalue.GetExact()
			nhvaluePrefix := nhvalue.GetPrefix()
			nhvalueMatch := assignExactOrPrefix(nhvalueExact, nhvaluePrefix)
			noHeaderMap[nhkey] = nhvalueMatch
		}

		matchHTTPRoutes = &OverlappingMatchValidationForHTTPRoute{
			routeName(route, rulen),
			requestName(match, matchn),
			tmpPrefix,
			match.Port,
			methodMatch,
			authorityMatch,
			headerMap,
			QPMap,
			noHeaderMap,
		}
		return
	}
	return nil
}

// coveredValidation validate the overlapping match between two instance of OverlappingMatchValidationForHTTPRoute
func coveredValidation(vA, vB *OverlappingMatchValidationForHTTPRoute) bool {
	// check the URI overlapping match, such as vB.Prefix is '/debugs' and vA.Prefix is '/debug'
	if strings.HasPrefix(vB.Prefix, vA.Prefix) {
		// check the port field
		if vB.MatchPort != vA.MatchPort {
			return false
		}

		// check the match method
		if vA.MatchMethod != vB.MatchMethod {
			if !strings.HasPrefix(vA.MatchMethod, vB.MatchMethod) {
				return false
			}
		}

		// check the match authority
		if vA.MatchAuthority != vB.MatchAuthority {
			if !strings.HasPrefix(vA.MatchAuthority, vB.MatchAuthority) {
				return false
			}
		}

		// check the match Headers
		vAHeaderLen := len(vA.MatchHeaders)
		vBHeaderLen := len(vB.MatchHeaders)
		if vAHeaderLen != vBHeaderLen {
			return false
		}
		for hdKey, hdValue := range vA.MatchHeaders {
			vBhdValue, ok := vB.MatchHeaders[hdKey]
			if !ok {
				return false
			} else if hdValue != vBhdValue {
				if !strings.HasPrefix(hdValue, vBhdValue) {
					return false
				}
			}
		}

		// check the match QueryParams
		vAQPLen := len(vA.MatchQueryParams)
		vBQPLen := len(vB.MatchQueryParams)
		if vAQPLen != vBQPLen {
			return false
		}
		for qpKey, qpValue := range vA.MatchQueryParams {
			vBqpValue, ok := vB.MatchQueryParams[qpKey]
			if !ok {
				return false
			} else if qpValue != vBqpValue {
				if !strings.HasPrefix(qpValue, vBqpValue) {
					return false
				}
			}
		}

		// check the match NonHeaders
		vANonHDLen := len(vA.MatchNonHeaders)
		vBNonHDLen := len(vB.MatchNonHeaders)
		if vANonHDLen != vBNonHDLen {
			return false
		}
		for nhKey, nhValue := range vA.MatchNonHeaders {
			vBnhValue, ok := vB.MatchNonHeaders[nhKey]
			if !ok {
				return false
			} else if nhValue != vBnhValue {
				if !strings.HasPrefix(nhValue, vBnhValue) {
					return false
				}
			}
		}
	} else {
		// no URI overlapping match
		return false
	}
	return true
}

func analyzeUnreachableHTTPRules(routes []*networking.HTTPRoute,
	reportUnreachable func(ruleno, reason string), reportIneffective func(ruleno, matchno, dupno string)) {
	matchesEncountered := make(map[string]int)
	emptyMatchEncountered := -1
	var matchHTTPRoutes []*OverlappingMatchValidationForHTTPRoute
	for rulen, route := range routes {
		if route == nil {
			continue
		}
		if len(route.Match) == 0 {
			if emptyMatchEncountered >= 0 {
				reportUnreachable(routeName(route, rulen), "only the last rule can have no matches")
			}
			emptyMatchEncountered = rulen
			continue
		}

		duplicateMatches := 0
		for matchn, match := range route.Match {
			dupn, ok := matchesEncountered[asJSON(match)]
			if ok {
				reportIneffective(routeName(route, rulen), requestName(match, matchn), routeName(routes[dupn], dupn))
				duplicateMatches++
				// no need to handle for totally duplicated match rules
				continue
			}
			matchesEncountered[asJSON(match)] = rulen
			// build the match rules into struct OverlappingMatchValidationForHTTPRoute based on current match
			matchHTTPRoute := genMatchHTTPRoutes(route, match, rulen, matchn)
			if matchHTTPRoute != nil {
				matchHTTPRoutes = append(matchHTTPRoutes, matchHTTPRoute)
			}
		}
		if duplicateMatches == len(route.Match) {
			reportUnreachable(routeName(route, rulen), "all matches used by prior rules")
		}
	}

	// at least 2 prefix matched routes for overlapping match validation
	if len(matchHTTPRoutes) > 1 {
		// check the overlapping match from the first prefix information
		for routeIndex, routePrefix := range matchHTTPRoutes {
			for rIndex := routeIndex + 1; rIndex < len(matchHTTPRoutes); rIndex++ {
				// exclude the duplicate-match cases which have been validated above
				if strings.Compare(matchHTTPRoutes[rIndex].Prefix, routePrefix.Prefix) == 0 {
					continue
				}
				// Validate former prefix match does not cover the latter one.
				if coveredValidation(routePrefix, matchHTTPRoutes[rIndex]) {
					prefixMatchA := matchHTTPRoutes[rIndex].MatchStr + " of prefix " + matchHTTPRoutes[rIndex].Prefix
					prefixMatchB := routePrefix.MatchStr + " of prefix " + routePrefix.Prefix + " on " + routePrefix.RouteStr
					reportIneffective(matchHTTPRoutes[rIndex].RouteStr, prefixMatchA, prefixMatchB)
				}
			}
		}
	}
}

// NOTE: This method identical to analyzeUnreachableHTTPRules.
func analyzeUnreachableTCPRules(routes []*networking.TCPRoute,
	reportUnreachable func(ruleno, reason string), reportIneffective func(ruleno, matchno, dupno string)) {
	matchesEncountered := make(map[string]int)
	emptyMatchEncountered := -1
	for rulen, route := range routes {
		if route == nil {
			continue
		}
		if len(route.Match) == 0 {
			if emptyMatchEncountered >= 0 {
				reportUnreachable(routeName(route, rulen), "only the last rule can have no matches")
			}
			emptyMatchEncountered = rulen
			continue
		}

		duplicateMatches := 0
		for matchn, match := range route.Match {
			dupn, ok := matchesEncountered[asJSON(match)]
			if ok {
				reportIneffective(routeName(route, rulen), requestName(match, matchn), routeName(routes[dupn], dupn))
				duplicateMatches++
			} else {
				matchesEncountered[asJSON(match)] = rulen
			}
		}
		if duplicateMatches == len(route.Match) {
			reportUnreachable(routeName(route, rulen), "all matches used by prior rules")
		}
	}
}

// NOTE: This method identical to analyzeUnreachableHTTPRules.
func analyzeUnreachableTLSRules(routes []*networking.TLSRoute,
	reportUnreachable func(ruleno, reason string), reportIneffective func(ruleno, matchno, dupno string)) {
	matchesEncountered := make(map[string]int)
	emptyMatchEncountered := -1
	for rulen, route := range routes {
		if route == nil {
			continue
		}
		if len(route.Match) == 0 {
			if emptyMatchEncountered >= 0 {
				reportUnreachable(routeName(route, rulen), "only the last rule can have no matches")
			}
			emptyMatchEncountered = rulen
			continue
		}

		duplicateMatches := 0
		for matchn, match := range route.Match {
			dupn, ok := matchesEncountered[asJSON(match)]
			if ok {
				reportIneffective(routeName(route, rulen), requestName(match, matchn), routeName(routes[dupn], dupn))
				duplicateMatches++
			} else {
				matchesEncountered[asJSON(match)] = rulen
			}
		}
		if duplicateMatches == len(route.Match) {
			reportUnreachable(routeName(route, rulen), "all matches used by prior rules")
		}
	}
}

// asJSON() creates a JSON serialization of a match, to use for match comparison.  We don't use the JSON itself.
func asJSON(data interface{}) string {
	// Remove the name, so we can create a serialization that only includes traffic routing config
	switch mr := data.(type) {
	case *networking.HTTPMatchRequest:
		if mr != nil && mr.Name != "" {
			cl := &networking.HTTPMatchRequest{}
			protomarshal.ShallowCopy(cl, mr)
			cl.Name = ""
			data = cl
		}
	}

	b, err := json.Marshal(data)
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func routeName(route interface{}, routen int) string {
	switch r := route.(type) {
	case *networking.HTTPRoute:
		if r.Name != "" {
			return fmt.Sprintf("%q", r.Name)
		}
		// TCP and TLS routes have no names
	}

	return fmt.Sprintf("#%d", routen)
}

func requestName(match interface{}, matchn int) string {
	switch mr := match.(type) {
	case *networking.HTTPMatchRequest:
		if mr != nil && mr.Name != "" {
			return fmt.Sprintf("%q", mr.Name)
		}
		// TCP and TLS matches have no names
	}

	return fmt.Sprintf("#%d", matchn)
}

func validateTLSRoute(tls *networking.TLSRoute, context *networking.VirtualService) (errs Validation) {
	if tls == nil {
		return
	}
	if len(tls.Match) == 0 {
		errs = appendValidation(errs, errors.New("TLS route must have at least one match condition"))
	}
	for _, match := range tls.Match {
		errs = appendValidation(errs, validateTLSMatch(match, context))
	}
	if len(tls.Route) == 0 {
		errs = appendValidation(errs, errors.New("TLS route is required"))
	}
	errs = appendValidation(errs, validateRouteDestinations(tls.Route))
	return errs
}

func validateTLSMatch(match *networking.TLSMatchAttributes, context *networking.VirtualService) (errs Validation) {
	if match == nil {
		errs = appendValidation(errs, errors.New("TLS match may not be null"))
		return
	}
	if len(match.SniHosts) == 0 {
		errs = appendValidation(errs, fmt.Errorf("TLS match must have at least one SNI host"))
	} else {
		for _, sniHost := range match.SniHosts {
			errs = appendValidation(errs, validateSniHost(sniHost, context))
		}
	}

	for _, destinationSubnet := range match.DestinationSubnets {
		errs = appendValidation(errs, ValidateIPSubnet(destinationSubnet))
	}

	if match.Port != 0 {
		errs = appendValidation(errs, ValidatePort(int(match.Port)))
	}
	errs = appendValidation(errs, labels.Instance(match.SourceLabels).Validate())
	errs = appendValidation(errs, validateGatewayNames(match.Gateways))
	return
}

func validateSniHost(sniHost string, context *networking.VirtualService) (errs Validation) {
	if err := ValidateWildcardDomain(sniHost); err != nil {
		ipAddr := net.ParseIP(sniHost) // Could also be an IP
		if ipAddr != nil {
			errs = appendValidation(errs, WrapWarning(fmt.Errorf("using an IP address (%q) goes against SNI spec and most clients do not support this", ipAddr)))
			return
		}
		return appendValidation(errs, err)
	}
	sniHostname := host.Name(sniHost)
	for _, hostname := range context.Hosts {
		if sniHostname.SubsetOf(host.Name(hostname)) {
			return
		}
	}
	return appendValidation(errs, fmt.Errorf("SNI host %q is not a compatible subset of any of the virtual service hosts: [%s]",
		sniHost, strings.Join(context.Hosts, ", ")))
}

func validateTCPRoute(tcp *networking.TCPRoute) (errs error) {
	if tcp == nil {
		return nil
	}
	for _, match := range tcp.Match {
		errs = appendErrors(errs, validateTCPMatch(match))
	}
	if len(tcp.Route) == 0 {
		errs = appendErrors(errs, errors.New("TCP route is required"))
	}
	errs = appendErrors(errs, validateRouteDestinations(tcp.Route))
	return
}

func validateTCPMatch(match *networking.L4MatchAttributes) (errs error) {
	if match == nil {
		errs = multierror.Append(errs, errors.New("tcp match may not be nil"))
		return
	}
	for _, destinationSubnet := range match.DestinationSubnets {
		errs = appendErrors(errs, ValidateIPSubnet(destinationSubnet))
	}
	if match.Port != 0 {
		errs = appendErrors(errs, ValidatePort(int(match.Port)))
	}
	errs = appendErrors(errs, labels.Instance(match.SourceLabels).Validate())
	errs = appendErrors(errs, validateGatewayNames(match.Gateways))
	return
}

func validateStringMatchRegexp(sm *networking.StringMatch, where string) error {
	switch sm.GetMatchType().(type) {
	case *networking.StringMatch_Regex:
	default:
		return nil
	}
	re := sm.GetRegex()
	if re == "" {
		return fmt.Errorf("%q: regex string match should not be empty", where)
	}

	// Envoy enforces a re2.max_program_size.error_level re2 program size is not the same as length,
	// but it is always *larger* than length. Because goland does not have a way to evaluate the
	// program size, we approximate by the length. To ensure that a program that is smaller than 1024
	// length but larger than 1024 size does not enter the system, we program Envoy to allow very large
	// regexs to avoid NACKs. See
	// https://github.com/jpeach/snippets/blob/889fda84cc8713af09205438b33553eb69dd5355/re2sz.cc to
	// evaluate program size.
	if len(re) > 1024 {
		return fmt.Errorf("%q: regex is too large, max length allowed is 1024", where)
	}

	_, err := regexp.Compile(re)
	if err == nil {
		return nil
	}

	return fmt.Errorf("%q: %w; Istio uses RE2 style regex-based match (https://github.com/google/re2/wiki/Syntax)", where, err)
}

func validateGatewayNames(gatewayNames []string) (errs Validation) {
	for _, gatewayName := range gatewayNames {
		parts := strings.SplitN(gatewayName, "/", 2)
		if len(parts) != 2 {
			if strings.Contains(gatewayName, ".") {
				// Legacy FQDN style
				parts := strings.Split(gatewayName, ".")
				recommended := fmt.Sprintf("%s/%s", parts[1], parts[0])
				errs = appendValidation(errs, WrapWarning(fmt.Errorf(
					"using legacy gatewayName format %q; prefer the <namespace>/<name> format: %q", gatewayName, recommended)))
			}
			errs = appendValidation(errs, ValidateFQDN(gatewayName))
			return
		}

		if len(parts[0]) == 0 || len(parts[1]) == 0 {
			errs = appendValidation(errs, fmt.Errorf("config namespace and gateway name cannot be empty"))
		}

		// namespace and name must be DNS labels
		if !labels.IsDNS1123Label(parts[0]) {
			errs = appendValidation(errs, fmt.Errorf("invalid value for namespace: %q", parts[0]))
		}

		if !labels.IsDNS1123Label(parts[1]) {
			errs = appendValidation(errs, fmt.Errorf("invalid value for gateway name: %q", parts[1]))
		}
	}
	return
}

func validateHTTPRouteDestinations(weights []*networking.HTTPRouteDestination) (errs error) {
	var totalWeight int32
	for _, weight := range weights {
		if weight == nil {
			errs = multierror.Append(errs, errors.New("weight may not be nil"))
			continue
		}
		if weight.Destination == nil {
			errs = multierror.Append(errs, errors.New("destination is required"))
		}

		// header manipulations
		for name, val := range weight.Headers.GetRequest().GetAdd() {
			errs = appendErrors(errs, ValidateHTTPHeaderWithHostOperationName(name))
			errs = appendErrors(errs, ValidateHTTPHeaderValue(val))
		}
		for name, val := range weight.Headers.GetRequest().GetSet() {
			errs = appendErrors(errs, ValidateHTTPHeaderWithHostOperationName(name))
			errs = appendErrors(errs, ValidateHTTPHeaderValue(val))
		}
		for _, name := range weight.Headers.GetRequest().GetRemove() {
			errs = appendErrors(errs, ValidateHTTPHeaderOperationName(name))
		}
		for name, val := range weight.Headers.GetResponse().GetAdd() {
			errs = appendErrors(errs, ValidateHTTPHeaderOperationName(name))
			errs = appendErrors(errs, ValidateHTTPHeaderValue(val))
		}
		for name, val := range weight.Headers.GetResponse().GetSet() {
			errs = appendErrors(errs, ValidateHTTPHeaderOperationName(name))
			errs = appendErrors(errs, ValidateHTTPHeaderValue(val))
		}
		for _, name := range weight.Headers.GetResponse().GetRemove() {
			errs = appendErrors(errs, ValidateHTTPHeaderOperationName(name))
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

func validateRouteDestinations(weights []*networking.RouteDestination) (errs error) {
	var totalWeight int32
	for _, weight := range weights {
		if weight == nil {
			errs = multierror.Append(errs, errors.New("weight may not be nil"))
			continue
		}
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

	for _, origin := range policy.AllowOrigins {
		errs = appendErrors(errs, validateAllowOrigins(origin))
	}

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

	return
}

func validateAllowOrigins(origin *networking.StringMatch) error {
	var match string
	switch origin.MatchType.(type) {
	case *networking.StringMatch_Exact:
		match = origin.GetExact()
	case *networking.StringMatch_Prefix:
		match = origin.GetPrefix()
	case *networking.StringMatch_Regex:
		match = origin.GetRegex()
	}
	if match == "" {
		return fmt.Errorf("'%v' is not a valid match type for CORS allow origins", match)
	}
	return validateStringMatchRegexp(origin, "corsPolicy.allowOrigins")
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

	errs = appendErrors(errs, validatePercentage(abort.Percentage))

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
	if status < 200 || status > 600 {
		return fmt.Errorf("HTTP status %d is not in range 200-599", status)
	}
	return nil
}

func validateHTTPFaultInjectionDelay(delay *networking.HTTPFaultInjection_Delay) (errs error) {
	if delay == nil {
		return
	}

	errs = appendErrors(errs, validatePercentage(delay.Percentage))

	switch v := delay.HttpDelayType.(type) {
	case *networking.HTTPFaultInjection_Delay_FixedDelay:
		errs = appendErrors(errs, ValidateDuration(v.FixedDelay))
	case *networking.HTTPFaultInjection_Delay_ExponentialDelay:
		errs = appendErrors(errs, ValidateDuration(v.ExponentialDelay))
		errs = multierror.Append(errs, fmt.Errorf("exponentialDelay not supported yet"))
	}

	return
}

func validateDestination(destination *networking.Destination) (errs error) {
	if destination == nil {
		return
	}

	hostname := destination.Host
	if hostname == "*" {
		errs = appendErrors(errs, fmt.Errorf("invalid destination host %s", hostname))
	} else {
		errs = appendErrors(errs, ValidateWildcardDomain(hostname))
	}
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
	if !labels.IsDNS1123Label(name) {
		return fmt.Errorf("subset name is invalid: %s", name)
	}
	return nil
}

func validatePortSelector(selector *networking.PortSelector) (errs error) {
	if selector == nil {
		return nil
	}

	// port must be a number
	number := int(selector.GetNumber())
	errs = appendErrors(errs, ValidatePort(number))
	return
}

func validateHTTPRetry(retries *networking.HTTPRetry) (errs error) {
	if retries == nil {
		return
	}

	if retries.Attempts < 0 {
		errs = multierror.Append(errs, errors.New("attempts cannot be negative"))
	}

	if retries.Attempts == 0 && (retries.PerTryTimeout != nil || retries.RetryOn != "" || retries.RetryRemoteLocalities != nil) {
		errs = appendErrors(errs, errors.New("http retry policy configured when attempts are set to 0 (disabled)"))
	}

	if retries.PerTryTimeout != nil {
		errs = appendErrors(errs, ValidateDuration(retries.PerTryTimeout))
	}
	if retries.RetryOn != "" {
		retryOnPolicies := strings.Split(retries.RetryOn, ",")
		for _, policy := range retryOnPolicies {
			// Try converting it to an integer to see if it's a valid HTTP status code.
			i, _ := strconv.Atoi(policy)

			if http.StatusText(i) == "" && !supportedRetryOnPolicies[policy] {
				errs = appendErrors(errs, fmt.Errorf("%q is not a valid retryOn policy", policy))
			}
		}
	}

	return
}

func validateHTTPRedirect(redirect *networking.HTTPRedirect) error {
	if redirect == nil {
		return nil
	}
	if redirect.Uri == "" && redirect.Authority == "" && redirect.RedirectPort == nil && redirect.Scheme == "" {
		return errors.New("redirect must specify URI, authority, scheme, or port")
	}

	if redirect.RedirectCode != 0 {
		if redirect.RedirectCode < 300 || redirect.RedirectCode > 399 {
			return fmt.Errorf("%d is not a valid redirect code, must be 3xx", redirect.RedirectCode)
		}
	}
	if redirect.Scheme != "" && redirect.Scheme != "http" && redirect.Scheme != "https" {
		return fmt.Errorf(`invalid redirect scheme, must be "http" or "https"`)
	}
	if redirect.GetPort() > 0 {
		if err := ValidatePort(int(redirect.GetPort())); err != nil {
			return err
		}
	}
	return nil
}

func validateHTTPRewrite(rewrite *networking.HTTPRewrite) error {
	if rewrite != nil && rewrite.Uri == "" && rewrite.Authority == "" {
		return errors.New("rewrite must specify URI, authority, or both")
	}
	return nil
}

// ValidateWorkloadEntry validates a workload entry.
var ValidateWorkloadEntry = registerValidateFunc("ValidateWorkloadEntry",
	func(cfg config.Config) (Warning, error) {
		we, ok := cfg.Spec.(*networking.WorkloadEntry)
		if !ok {
			return nil, fmt.Errorf("cannot cast to workload entry")
		}
		return validateWorkloadEntry(we)
	})

func validateWorkloadEntry(we *networking.WorkloadEntry) (Warning, error) {
	errs := Validation{}
	if we.Address == "" {
		return nil, fmt.Errorf("address must be set")
	}
	// Since we don't know if its meant to be DNS or STATIC type without association with a ServiceEntry,
	// check based on content and try validations.
	addr := we.Address
	// First check if it is a Unix endpoint - this will be specified for STATIC.
	if strings.HasPrefix(we.Address, UnixAddressPrefix) {
		errs = appendValidation(errs, ValidateUnixAddress(strings.TrimPrefix(addr, UnixAddressPrefix)))
		if len(we.Ports) != 0 {
			errs = appendValidation(errs, fmt.Errorf("unix endpoint %s must not include ports", we.Address))
		}
	} else {
		// This could be IP (in STATIC resolution) or DNS host name (for DNS).
		ipAddr := net.ParseIP(we.Address)
		if ipAddr == nil {
			if err := ValidateFQDN(we.Address); err != nil { // Otherwise could be an FQDN
				errs = appendValidation(errs,
					fmt.Errorf("endpoint address %q is not a valid FQDN or an IP address", we.Address))
			}
		}
	}

	errs = appendValidation(errs,
		labels.Instance(we.Labels).Validate())
	for name, port := range we.Ports {
		// TODO: Validate port is part of Service Port - which is tricky to validate with out service entry.
		errs = appendValidation(errs,
			ValidatePortName(name),
			ValidatePort(int(port)))
	}
	return errs.Unwrap()
}

// ValidateWorkloadGroup validates a workload group.
var ValidateWorkloadGroup = registerValidateFunc("ValidateWorkloadGroup",
	func(cfg config.Config) (warnings Warning, errs error) {
		wg, ok := cfg.Spec.(*networking.WorkloadGroup)
		if !ok {
			return nil, fmt.Errorf("cannot cast to workload entry")
		}

		if wg.Template == nil {
			return nil, fmt.Errorf("template is required")
		}
		// Do not call validateWorkloadEntry. Some fields, such as address, are required in WorkloadEntry
		// but not in the template since they are auto populated

		if wg.Metadata != nil {
			if err := labels.Instance(wg.Metadata.Labels).Validate(); err != nil {
				return nil, fmt.Errorf("invalid labels: %v", err)
			}
		}

		return nil, validateReadinessProbe(wg.Probe)
	})

func validateReadinessProbe(probe *networking.ReadinessProbe) (errs error) {
	if probe == nil {
		return nil
	}
	if probe.PeriodSeconds < 0 {
		errs = appendErrors(errs, fmt.Errorf("periodSeconds must be non-negative"))
	}
	if probe.InitialDelaySeconds < 0 {
		errs = appendErrors(errs, fmt.Errorf("initialDelaySeconds must be non-negative"))
	}
	if probe.TimeoutSeconds < 0 {
		errs = appendErrors(errs, fmt.Errorf("timeoutSeconds must be non-negative"))
	}
	if probe.SuccessThreshold < 0 {
		errs = appendErrors(errs, fmt.Errorf("successThreshold must be non-negative"))
	}
	if probe.FailureThreshold < 0 {
		errs = appendErrors(errs, fmt.Errorf("failureThreshold must be non-negative"))
	}
	switch m := probe.HealthCheckMethod.(type) {
	case *networking.ReadinessProbe_HttpGet:
		h := m.HttpGet
		if h == nil {
			errs = appendErrors(errs, fmt.Errorf("httpGet may not be nil"))
			break
		}
		errs = appendErrors(errs, ValidatePort(int(h.Port)))
		if h.Scheme != "" && h.Scheme != string(apimirror.URISchemeHTTPS) && h.Scheme != string(apimirror.URISchemeHTTP) {
			errs = appendErrors(errs, fmt.Errorf(`httpGet.scheme must be one of "http", "https"`))
		}
		for _, header := range h.HttpHeaders {
			if header == nil {
				errs = appendErrors(errs, fmt.Errorf("invalid nil header"))
				continue
			}
			errs = appendErrors(errs, ValidateHTTPHeaderName(header.Name))
		}
	case *networking.ReadinessProbe_TcpSocket:
		h := m.TcpSocket
		if h == nil {
			errs = appendErrors(errs, fmt.Errorf("tcpSocket may not be nil"))
			break
		}
		errs = appendErrors(errs, ValidatePort(int(h.Port)))
	case *networking.ReadinessProbe_Exec:
		h := m.Exec
		if h == nil {
			errs = appendErrors(errs, fmt.Errorf("exec may not be nil"))
			break
		}
		if len(h.Command) == 0 {
			errs = appendErrors(errs, fmt.Errorf("exec.command is required"))
		}
	default:
		errs = appendErrors(errs, fmt.Errorf("unknown health check method %T", m))
	}
	return errs
}

// ValidateServiceEntry validates a service entry.
var ValidateServiceEntry = registerValidateFunc("ValidateServiceEntry",
	func(cfg config.Config) (Warning, error) {
		serviceEntry, ok := cfg.Spec.(*networking.ServiceEntry)
		if !ok {
			return nil, fmt.Errorf("cannot cast to service entry")
		}

		if err := validateAlphaWorkloadSelector(serviceEntry.WorkloadSelector); err != nil {
			return nil, err
		}

		errs := Validation{}

		if serviceEntry.WorkloadSelector != nil && serviceEntry.Endpoints != nil {
			errs = appendValidation(errs, fmt.Errorf("only one of WorkloadSelector or Endpoints is allowed in Service Entry"))
		}

		if len(serviceEntry.Hosts) == 0 {
			errs = appendValidation(errs, fmt.Errorf("service entry must have at least one host"))
		}
		for _, hostname := range serviceEntry.Hosts {
			// Full wildcard is not allowed in the service entry.
			if hostname == "*" {
				errs = appendValidation(errs, fmt.Errorf("invalid host %s", hostname))
			} else {
				errs = appendValidation(errs, ValidateWildcardDomain(hostname))
			}
		}

		cidrFound := false
		for _, address := range serviceEntry.Addresses {
			cidrFound = cidrFound || strings.Contains(address, "/")
			errs = appendValidation(errs, ValidateIPSubnet(address))
		}

		if cidrFound {
			if serviceEntry.Resolution != networking.ServiceEntry_NONE && serviceEntry.Resolution != networking.ServiceEntry_STATIC {
				errs = appendValidation(errs, fmt.Errorf("CIDR addresses are allowed only for NONE/STATIC resolution types"))
			}
		}

		servicePortNumbers := make(map[uint32]bool)
		servicePorts := make(map[string]bool, len(serviceEntry.Ports))
		for _, port := range serviceEntry.Ports {
			if port == nil {
				errs = appendValidation(errs, fmt.Errorf("service entry port may not be null"))
				continue
			}
			if servicePorts[port.Name] {
				errs = appendValidation(errs, fmt.Errorf("service entry port name %q already defined", port.Name))
			}
			servicePorts[port.Name] = true
			if servicePortNumbers[port.Number] {
				errs = appendValidation(errs, fmt.Errorf("service entry port %d already defined", port.Number))
			}
			servicePortNumbers[port.Number] = true
			if port.TargetPort != 0 {
				errs = appendValidation(errs, ValidatePort(int(port.TargetPort)))
			}
			errs = appendValidation(errs,
				ValidatePortName(port.Name),
				ValidateProtocol(port.Protocol),
				ValidatePort(int(port.Number)))
		}

		switch serviceEntry.Resolution {
		case networking.ServiceEntry_NONE:
			if len(serviceEntry.Endpoints) != 0 {
				errs = appendValidation(errs, fmt.Errorf("no endpoints should be provided for resolution type none"))
			}
		case networking.ServiceEntry_STATIC:
			unixEndpoint := false
			for _, endpoint := range serviceEntry.Endpoints {
				addr := endpoint.GetAddress()
				if strings.HasPrefix(addr, UnixAddressPrefix) {
					unixEndpoint = true
					errs = appendValidation(errs, ValidateUnixAddress(strings.TrimPrefix(addr, UnixAddressPrefix)))
					if len(endpoint.Ports) != 0 {
						errs = appendValidation(errs, fmt.Errorf("unix endpoint %s must not include ports", addr))
					}
				} else {
					errs = appendValidation(errs, ValidateIPAddress(addr))

					for name, port := range endpoint.Ports {
						if !servicePorts[name] {
							errs = appendValidation(errs, fmt.Errorf("endpoint port %v is not defined by the service entry", port))
						}
					}
				}
				errs = appendValidation(errs, labels.Instance(endpoint.Labels).Validate())
			}
			if unixEndpoint && len(serviceEntry.Ports) != 1 {
				errs = appendValidation(errs, errors.New("exactly 1 service port required for unix endpoints"))
			}
		case networking.ServiceEntry_DNS, networking.ServiceEntry_DNS_ROUND_ROBIN:
			if len(serviceEntry.Endpoints) == 0 {
				for _, hostname := range serviceEntry.Hosts {
					if err := ValidateFQDN(hostname); err != nil {
						errs = appendValidation(errs,
							fmt.Errorf("hosts must be FQDN if no endpoints are provided for resolution mode %s", serviceEntry.Resolution))
					}
				}
			}

			for _, endpoint := range serviceEntry.Endpoints {
				ipAddr := net.ParseIP(endpoint.Address) // Typically it is an IP address
				if ipAddr == nil {
					if err := ValidateFQDN(endpoint.Address); err != nil { // Otherwise could be an FQDN
						errs = appendValidation(errs,
							fmt.Errorf("endpoint address %q is not a valid FQDN or an IP address", endpoint.Address))
					}
				}
				errs = appendValidation(errs,
					labels.Instance(endpoint.Labels).Validate())
				for name, port := range endpoint.Ports {
					if !servicePorts[name] {
						errs = appendValidation(errs, fmt.Errorf("endpoint port %v is not defined by the service entry", port))
					}
					errs = appendValidation(errs,
						ValidatePortName(name),
						ValidatePort(int(port)))
				}
			}
			if len(serviceEntry.Addresses) > 0 {
				for _, port := range serviceEntry.Ports {
					p := protocol.Parse(port.Protocol)
					if p.IsTCP() {
						if len(serviceEntry.Hosts) > 1 {
							// TODO: prevent this invalid setting, maybe in 1.11+
							errs = appendValidation(errs, WrapWarning(fmt.Errorf("service entry can not have more than one host specified "+
								"simultaneously with address and tcp port")))
						}
						break
					}
				}
			}
		default:
			errs = appendValidation(errs, fmt.Errorf("unsupported resolution type %s",
				networking.ServiceEntry_Resolution_name[int32(serviceEntry.Resolution)]))
		}

		// multiple hosts and TCP is invalid unless the resolution type is NONE.
		// depending on the protocol, we can differentiate between hosts when proxying:
		// - with HTTP, the authority header can be used
		// - with HTTPS/TLS with SNI, the ServerName can be used
		// however, for plain TCP there is no way to differentiate between the
		// hosts so we consider it invalid, unless the resolution type is NONE
		// (because the hosts are ignored).
		if serviceEntry.Resolution != networking.ServiceEntry_NONE && len(serviceEntry.Hosts) > 1 {
			for _, port := range serviceEntry.Ports {
				p := protocol.Parse(port.Protocol)
				if !p.IsHTTP() && !p.IsTLS() {
					errs = appendValidation(errs, fmt.Errorf("multiple hosts provided with non-HTTP, non-TLS ports"))
					break
				}
			}
		}

		errs = appendValidation(errs, validateExportTo(cfg.Namespace, serviceEntry.ExportTo, true, false))
		return errs.Unwrap()
	})

// ValidatePortName validates a port name to DNS-1123
func ValidatePortName(name string) error {
	if !labels.IsDNS1123Label(name) {
		return fmt.Errorf("invalid port name: %s", name)
	}
	return nil
}

// ValidateProtocol validates a portocol name is known
func ValidateProtocol(protocolStr string) error {
	// Empty string is used for protocol sniffing.
	if protocolStr != "" && protocol.Parse(protocolStr) == protocol.Unsupported {
		return fmt.Errorf("unsupported protocol: %s", protocolStr)
	}
	return nil
}

// wrapper around multierror.Append that enforces the invariant that if all input errors are nil, the output
// error is nil (allowing validation without branching).
func appendValidation(v Validation, vs ...error) Validation {
	appendError := func(err, err2 error) error {
		if err == nil {
			return err2
		} else if err2 == nil {
			return err
		}
		return multierror.Append(err, err2)
	}

	for _, nv := range vs {
		switch t := nv.(type) {
		case Validation:
			v.Err = appendError(v.Err, t.Err)
			v.Warning = appendError(v.Warning, t.Warning)
		default:
			v.Err = appendError(v.Err, t)
		}
	}
	return v
}

// appendErrorf appends a formatted error string
// nolint: unparam
func appendErrorf(v Validation, format string, a ...interface{}) Validation {
	return appendValidation(v, fmt.Errorf(format, a...))
}

// appendWarningf appends a formatted warning string
// nolint: unparam
func appendWarningf(v Validation, format string, a ...interface{}) Validation {
	return appendValidation(v, Warningf(format, a...))
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
		switch t := err2.(type) {
		case Validation:
			err = appendError(err, t.Err)
		default:
			err = appendError(err, err2)
		}
	}
	return err
}

// validateLocalityLbSetting checks the LocalityLbSetting of MeshConfig
func validateLocalityLbSetting(lb *networking.LocalityLoadBalancerSetting) error {
	if lb == nil {
		return nil
	}

	if len(lb.GetDistribute()) > 0 && len(lb.GetFailover()) > 0 {
		return fmt.Errorf("can not simultaneously specify 'distribute' and 'failover'")
	}

	srcLocalities := make([]string, 0, len(lb.GetDistribute()))
	for _, locality := range lb.GetDistribute() {
		srcLocalities = append(srcLocalities, locality.From)
		var totalWeight uint32
		destLocalities := make([]string, 0)
		for loc, weight := range locality.To {
			destLocalities = append(destLocalities, loc)
			if weight <= 0 || weight > 100 {
				return fmt.Errorf("locality weight must be in range [1, 100]")
			}
			totalWeight += weight
		}
		if totalWeight != 100 {
			return fmt.Errorf("total locality weight %v != 100", totalWeight)
		}
		if err := validateLocalities(destLocalities); err != nil {
			return err
		}
	}

	if err := validateLocalities(srcLocalities); err != nil {
		return err
	}

	for _, failover := range lb.GetFailover() {
		if failover.From == failover.To {
			return fmt.Errorf("locality lb failover settings must specify different regions")
		}
		if strings.Contains(failover.From, "/") || strings.Contains(failover.To, "/") {
			return fmt.Errorf("locality lb failover only specify region")
		}
		if strings.Contains(failover.To, "*") || strings.Contains(failover.From, "*") {
			return fmt.Errorf("locality lb failover region should not contain '*' wildcard")
		}
	}

	return nil
}

func validateLocalities(localities []string) error {
	regionZoneSubZoneMap := map[string]map[string]map[string]bool{}
	for _, locality := range localities {
		if n := strings.Count(locality, "*"); n > 0 {
			if n > 1 || !strings.HasSuffix(locality, "*") {
				return fmt.Errorf("locality %s wildcard '*' number can not exceed 1 and must be in the end", locality)
			}
		}
		if _, exist := regionZoneSubZoneMap["*"]; exist {
			return fmt.Errorf("locality %s overlap with previous specified ones", locality)
		}

		region, zone, subZone, localityIndex, err := getLocalityParam(locality)
		if err != nil {
			return fmt.Errorf("locality %s must not contain empty region/zone/subzone info", locality)
		}

		switch localityIndex {
		case regionIndex:
			if _, exist := regionZoneSubZoneMap[region]; exist {
				return fmt.Errorf("locality %s overlap with previous specified ones", locality)
			}
			regionZoneSubZoneMap[region] = map[string]map[string]bool{"*": {"*": true}}
		case zoneIndex:
			if _, exist := regionZoneSubZoneMap[region]; exist {
				if _, exist := regionZoneSubZoneMap[region]["*"]; exist {
					return fmt.Errorf("locality %s overlap with previous specified ones", locality)
				}
				if _, exist := regionZoneSubZoneMap[region][zone]; exist {
					return fmt.Errorf("locality %s overlap with previous specified ones", locality)
				}
				regionZoneSubZoneMap[region][zone] = map[string]bool{"*": true}
			} else {
				regionZoneSubZoneMap[region] = map[string]map[string]bool{zone: {"*": true}}
			}
		case subZoneIndex:
			if _, exist := regionZoneSubZoneMap[region]; exist {
				if _, exist := regionZoneSubZoneMap[region]["*"]; exist {
					return fmt.Errorf("locality %s overlap with previous specified ones", locality)
				}
				if _, exist := regionZoneSubZoneMap[region][zone]; exist {
					if regionZoneSubZoneMap[region][zone]["*"] {
						return fmt.Errorf("locality %s overlap with previous specified ones", locality)
					}
					if regionZoneSubZoneMap[region][zone][subZone] {
						return fmt.Errorf("locality %s overlap with previous specified ones", locality)
					}
					regionZoneSubZoneMap[region][zone][subZone] = true
				} else {
					regionZoneSubZoneMap[region][zone] = map[string]bool{subZone: true}
				}
			} else {
				regionZoneSubZoneMap[region] = map[string]map[string]bool{zone: {subZone: true}}
			}
		}
	}

	return nil
}

func getLocalityParam(locality string) (string, string, string, int, error) {
	var region, zone, subZone string
	items := strings.SplitN(locality, "/", 3)
	for i, item := range items {
		if item == "" {
			return "", "", "", -1, errors.New("item is nil")
		}
		switch i {
		case regionIndex:
			region = items[i]
		case zoneIndex:
			zone = items[i]
		case subZoneIndex:
			subZone = items[i]
		}
	}
	return region, zone, subZone, len(items) - 1, nil
}

// ValidateMeshNetworks validates meshnetworks.
func ValidateMeshNetworks(meshnetworks *meshconfig.MeshNetworks) (errs error) {
	// TODO validate using the same gateway on multiple networks?
	for name, network := range meshnetworks.Networks {
		if err := validateNetwork(network); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("invalid network %v:", name)))
		}
	}
	return
}

func validateNetwork(network *meshconfig.Network) (errs error) {
	for _, n := range network.Endpoints {
		switch e := n.Ne.(type) {
		case *meshconfig.Network_NetworkEndpoints_FromCidr:
			if err := ValidateIPSubnet(e.FromCidr); err != nil {
				errs = multierror.Append(errs, err)
			}
		case *meshconfig.Network_NetworkEndpoints_FromRegistry:
			if ok := labels.IsDNS1123Label(e.FromRegistry); !ok {
				errs = multierror.Append(errs, fmt.Errorf("invalid registry name: %v", e.FromRegistry))
			}
		}
	}
	for _, n := range network.Gateways {
		switch g := n.Gw.(type) {
		case *meshconfig.Network_IstioNetworkGateway_RegistryServiceName:
			if err := ValidateFQDN(g.RegistryServiceName); err != nil {
				errs = multierror.Append(errs, err)
			}
		case *meshconfig.Network_IstioNetworkGateway_Address:
			if ipErr := ValidateIPAddress(g.Address); ipErr != nil {
				if !features.ResolveHostnameGateways {
					err := fmt.Errorf("%v (hostname is allowed if RESOLVE_HOSTNAME_GATEWAYS is enabled)", ipErr)
					errs = multierror.Append(errs, err)
				} else if fqdnErr := ValidateFQDN(g.Address); fqdnErr != nil {
					errs = multierror.Append(fmt.Errorf("%v is not a valid IP address or DNS name", g.Address))
				}
			}
		}
		if err := ValidatePort(int(n.Port)); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	return
}

func (aae *AnalysisAwareError) Error() string {
	return aae.Msg
}

// ValidateProxyConfig validates a ProxyConfig CR (as opposed to the MeshConfig field).
var ValidateProxyConfig = registerValidateFunc("ValidateProxyConfig",
	func(cfg config.Config) (Warning, error) {
		spec, ok := cfg.Spec.(*networkingv1beta1.ProxyConfig)
		if !ok {
			return nil, fmt.Errorf("cannot cast to proxyconfig")
		}

		errs := Validation{}

		errs = appendValidation(errs,
			validateWorkloadSelector(spec.Selector),
			validateConcurrency(spec.Concurrency.GetValue()),
		)
		return errs.Unwrap()
	})

func validateConcurrency(concurrency int32) (v Validation) {
	if concurrency < 0 {
		v = appendErrorf(v, "concurrency must be greater than or equal to 0")
	}
	return
}

// ValidateTelemetry validates a Telemetry.
var ValidateTelemetry = registerValidateFunc("ValidateTelemetry",
	func(cfg config.Config) (Warning, error) {
		spec, ok := cfg.Spec.(*telemetry.Telemetry)
		if !ok {
			return nil, fmt.Errorf("cannot cast to telemetry")
		}

		errs := Validation{}

		errs = appendValidation(errs,
			validateWorkloadSelector(spec.Selector),
			validateTelemetryMetrics(spec.Metrics),
			validateTelemetryTracing(spec.Tracing),
			validateTelemetryAccessLogging(spec.AccessLogging),
		)
		return errs.Unwrap()
	})

func validateTelemetryAccessLogging(logging []*telemetry.AccessLogging) (v Validation) {
	if len(logging) > 1 {
		v = appendWarningf(v, "multiple accessLogging is not currently supported")
	}
	for idx, l := range logging {
		if l == nil {
			continue
		}
		if len(l.Providers) > 1 {
			v = appendValidation(v, Warningf("accessLogging[%d]: multiple providers is not currently supported", idx))
		}
		if l.Filter != nil {
			v = appendValidation(v, validateTelemetryFilter(l.Filter))
		}
		v = appendValidation(v, validateTelemetryProviders(l.Providers))
	}
	return
}

func validateTelemetryTracing(tracing []*telemetry.Tracing) (v Validation) {
	if len(tracing) > 1 {
		v = appendWarningf(v, "multiple tracing is not currently supported")
	}
	for _, l := range tracing {
		if l == nil {
			continue
		}
		if len(l.Providers) > 1 {
			v = appendWarningf(v, "multiple providers is not currently supported")
		}
		v = appendValidation(v, validateTelemetryProviders(l.Providers))
		if l.RandomSamplingPercentage.GetValue() < 0 || l.RandomSamplingPercentage.GetValue() > 100 {
			v = appendErrorf(v, "randomSamplingPercentage must be in range [0.0, 100.0]")
		}
		for name, tag := range l.CustomTags {
			if name == "" {
				v = appendErrorf(v, "tag name may not be empty")
			}
			if tag == nil {
				v = appendErrorf(v, "tag '%s' may not have a nil value", name)
				continue
			}
			switch t := tag.Type.(type) {
			case *telemetry.Tracing_CustomTag_Literal:
				if t.Literal.GetValue() == "" {
					v = appendErrorf(v, "literal tag value may not be empty")
				}
			case *telemetry.Tracing_CustomTag_Header:
				if t.Header.GetName() == "" {
					v = appendErrorf(v, "header tag name may not be empty")
				}
			case *telemetry.Tracing_CustomTag_Environment:
				if t.Environment.GetName() == "" {
					v = appendErrorf(v, "environment tag name may not be empty")
				}
			}
		}
	}
	return
}

func validateTelemetryMetrics(metrics []*telemetry.Metrics) (v Validation) {
	for _, l := range metrics {
		if l == nil {
			continue
		}
		v = appendValidation(v, validateTelemetryProviders(l.Providers))
		for _, o := range l.Overrides {
			if o == nil {
				v = appendErrorf(v, "tagOverrides may not be null")
				continue
			}
			for tagName, to := range o.TagOverrides {
				if tagName == "" {
					v = appendWarningf(v, "tagOverrides.name may not be empty")
				}
				if to == nil {
					v = appendErrorf(v, "tagOverrides may not be null")
					continue
				}
				switch to.Operation {
				case telemetry.MetricsOverrides_TagOverride_UPSERT:
					if to.Value == "" {
						v = appendErrorf(v, "tagOverrides.value must be set set when operation is UPSERT")
					}
				case telemetry.MetricsOverrides_TagOverride_REMOVE:
					if to.Value != "" {
						v = appendErrorf(v, "tagOverrides.value may only be set when operation is UPSERT")
					}
				}
			}
			if o.Match != nil {
				switch mm := o.Match.MetricMatch.(type) {
				case *telemetry.MetricSelector_CustomMetric:
					if mm.CustomMetric == "" {
						v = appendErrorf(v, "customMetric may not be empty")
					}
				}
			}
		}
	}
	return
}

func validateTelemetryProviders(providers []*telemetry.ProviderRef) error {
	for _, p := range providers {
		if p == nil || p.Name == "" {
			return fmt.Errorf("providers.name may not be empty")
		}
	}
	return nil
}

// ValidateWasmPlugin validates a WasmPlugin.
var ValidateWasmPlugin = registerValidateFunc("ValidateWasmPlugin",
	func(cfg config.Config) (Warning, error) {
		spec, ok := cfg.Spec.(*extensions.WasmPlugin)
		if !ok {
			return nil, fmt.Errorf("cannot cast to wasmplugin")
		}

		errs := Validation{}
		errs = appendValidation(errs,
			validateWorkloadSelector(spec.Selector),
			validateWasmPluginURL(spec.Url),
			validateWasmPluginSHA(spec),
			validateWasmPluginVMConfig(spec.VmConfig),
		)
		return errs.Unwrap()
	})

func validateWasmPluginURL(pluginURL string) error {
	if pluginURL == "" {
		return fmt.Errorf("url field needs to be set")
	}
	validSchemes := map[string]bool{
		"": true, "file": true, "http": true, "https": true, "oci": true,
	}

	u, err := url.Parse(pluginURL)
	if err != nil {
		return fmt.Errorf("failed to parse url: %s", err)
	}
	if _, found := validSchemes[u.Scheme]; !found {
		return fmt.Errorf("url contains unsupported scheme: %s", u.Scheme)
	}
	return nil
}

func validateWasmPluginSHA(plugin *extensions.WasmPlugin) error {
	if plugin.Sha256 == "" {
		return nil
	}
	if len(plugin.Sha256) != 64 {
		return fmt.Errorf("sha256 field must be 64 characters long")
	}
	for _, r := range plugin.Sha256 {
		if !('a' <= r && r <= 'f' || '0' <= r && r <= '9') {
			return fmt.Errorf("sha256 field must match [a-f0-9]{64} pattern")
		}
	}
	return nil
}

func validateWasmPluginVMConfig(vm *extensions.VmConfig) error {
	if vm == nil || len(vm.Env) == 0 {
		return nil
	}

	keys := sets.New()
	for _, env := range vm.Env {
		if env == nil {
			continue
		}

		if env.Name == "" {
			return fmt.Errorf("spec.vmConfig.env invalid")
		}

		if keys.Contains(env.Name) {
			return fmt.Errorf("duplicate env")
		}
		keys.Insert(env.Name)
	}

	return nil
}
