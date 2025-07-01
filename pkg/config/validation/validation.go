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
	"math"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/lestrrat-go/jwx/jwk"

	"istio.io/api/annotation"
	extensions "istio.io/api/extensions/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	networkingv1beta1 "istio.io/api/networking/v1beta1"
	security_beta "istio.io/api/security/v1beta1"
	telemetry "istio.io/api/telemetry/v1alpha1"
	type_beta "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/networking/serviceentry"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/gateway"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/security"
	"istio.io/istio/pkg/config/validation/agent"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/jwt"
	"istio.io/istio/pkg/kube/apimirror"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/grpc"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/util/smallset"
)

// Constants for duration fields
const (
	// UnixAddressPrefix is the prefix used to indicate an address is for a Unix Domain socket. It is used in
	// ServiceEntry.Endpoint.Address message.
	UnixAddressPrefix = "unix://"

	matchExact  = "exact:"
	matchPrefix = "prefix:"
)

// https://greenbytes.de/tech/webdav/rfc7230.html#header.fields
var (
	tchars               = "!#$%&'*+-.^_`|~" + "A-Z" + "a-z" + "0-9"
	validHeaderNameRegex = regexp.MustCompile("^[" + tchars + "]+$")

	validProbeHeaderNameRegex  = regexp.MustCompile("^[-A-Za-z0-9]+$")
	validStrictHeaderNameRegex = validProbeHeaderNameRegex
)

const (
	kb = 1024
	mb = 1024 * kb
)

var (
	// envoy supported retry on header values
	supportedRetryOnPolicies = sets.New(
		// 'x-envoy-retry-on' supported policies:
		// https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/router_filter.html#x-envoy-retry-on
		"5xx",
		"gateway-error",
		"reset",
		"reset-before-request",
		"connect-failure",
		"retriable-4xx",
		"refused-stream",
		"retriable-status-codes",
		"retriable-headers",
		"envoy-ratelimited",
		"http3-post-connect-failure",

		// 'x-envoy-retry-grpc-on' supported policies:
		// https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/router_filter#x-envoy-retry-grpc-on
		"cancelled",
		"deadline-exceeded",
		"internal",
		"resource-exhausted",
		"unavailable",
	)

	// golang supported methods: https://golang.org/src/net/http/method.go
	supportedCORSMethods = sets.New[string](
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace,
		"*",
	)

	scope = log.RegisterScope("validation", "CRD validation debugging")

	// EmptyValidate is a Validate that does nothing and returns no error.
	EmptyValidate = RegisterValidateFunc("EmptyValidate",
		func(config.Config) (Warning, error) {
			return nil, nil
		})

	validateFuncs = make(map[string]ValidateFunc)
)

type (
	Warning    = agent.Warning
	Validation = agent.Validation
)

// WrapWarning turns an error into a Validation as a warning
func WrapWarning(e error) Validation {
	return Validation{Warning: e}
}

func AppendValidation(v Validation, vs ...error) Validation {
	return agent.AppendValidation(v, vs...)
}

func appendErrors(err error, errs ...error) error {
	return agent.AppendErrors(err, errs...)
}

type AnalysisAwareError struct {
	Type       string
	Msg        string
	Parameters []any
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

func RegisterValidateFunc(name string, f ValidateFunc) ValidateFunc {
	// Wrap the original validate function with an extra validate function for object metadata
	validate := validateMetadata(f)
	validateFuncs[name] = validate
	return validate
}

func validateMetadata(f ValidateFunc) ValidateFunc {
	return func(config config.Config) (Warning, error) {
		// Check the annotation "istio.io/dry-run".
		_, isAuthz := config.Spec.(*security_beta.AuthorizationPolicy)
		// Only the AuthorizationPolicy supports the annotation "istio.io/dry-run".
		if err := checkDryRunAnnotation(config, isAuthz); err != nil {
			return nil, err
		}
		if _, f := config.Annotations[constants.AlwaysReject]; f {
			return nil, fmt.Errorf("%q annotation found, rejecting", constants.AlwaysReject)
		}
		if _, f := config.Annotations[constants.InternalParentNamespace]; f {
			// This internal annotation escalations privileges; ban it from use for external resources.
			return nil, fmt.Errorf("%q annotation found, this may not be set by users", constants.InternalParentNamespace)
		}
		if _, f := config.Annotations[constants.InternalParentNames]; f {
			// This internal annotation escalations privileges; ban it from use for external resources.
			return nil, fmt.Errorf("%q annotation found, this may not be set by users", constants.InternalParentNames)
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

// ValidateStrictHTTPHeaderName validates a header name. This uses stricter semantics than HTTP allows (ValidateHTTPHeaderName)
func ValidateStrictHTTPHeaderName(name string) error {
	if name == "" {
		return fmt.Errorf("header name cannot be empty")
	}
	if !validStrictHeaderNameRegex.MatchString(name) {
		return fmt.Errorf("header name %s is not a valid header name", name)
	}
	return nil
}

// ValidateProbeHeaderName validates a header name for a HTTP probe. This aligns with Kubernetes logic
func ValidateProbeHeaderName(name string) error {
	if name == "" {
		return fmt.Errorf("header name cannot be empty")
	}
	if !validProbeHeaderNameRegex.MatchString(name) {
		return fmt.Errorf("header name %s is not a valid header name", name)
	}
	return nil
}

// ValidateCORSHTTPHeaderName validates a headers for CORS.
// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Access-Control-Allow-Headers#directives
func ValidateCORSHTTPHeaderName(name string) error {
	if name == "" {
		return fmt.Errorf("header name cannot be empty")
	}
	if name == "*" {
		return nil
	}
	if !validHeaderNameRegex.MatchString(name) {
		return fmt.Errorf("header name %s is not a valid header name", name)
	}
	return nil
}

// https://httpwg.org/specs/rfc7540.html#PseudoHeaderFields
var pseudoHeaders = smallset.New(
	":method",
	":scheme",
	":authority",
	":path",
	":status",
)

// ValidateHTTPHeaderNameOrJwtClaimRoute validates a header name, allowing special @request.auth.claims syntax
func ValidateHTTPHeaderNameOrJwtClaimRoute(name string) error {
	if name == "" {
		return fmt.Errorf("header name cannot be empty")
	}
	if jwt.ToRoutingClaim(name).Match {
		// Jwt claim form
		return nil
	}
	if pseudoHeaders.Contains(name) {
		// Valid pseudo header
		return nil
	}
	// Else ensure its a valid header
	if !validHeaderNameRegex.MatchString(name) {
		return fmt.Errorf("header name %s is not a valid header name", name)
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

// Copy from https://github.com/bufbuild/protoc-gen-validate/blob/a65858624dd654f2fb306d6af60f737132986f44/module/checker.go#L18
var httpHeaderValueRegexp = regexp.MustCompile("^[^\u0000-\u0008\u000A-\u001F\u007F]*$")

// ValidateHTTPHeaderValue validates a header value for Envoy
// Valid: "foo", "%HOSTNAME%", "100%%", "prefix %HOSTNAME% suffix"
// Invalid: "abc%123", "%START_TIME%%"
// We don't try to check that what is inside the %% is one of Envoy recognized values, we just prevent invalid config.
// See: https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/headers.html#custom-request-response-headers
func ValidateHTTPHeaderValue(value string) error {
	if !httpHeaderValueRegexp.MatchString(value) {
		return fmt.Errorf("header value configuration %s is invalid", value)
	}

	if err := validateHeaderValue(value); err != nil {
		return fmt.Errorf("header value configuration: %w", err)
	}

	// TODO: find a better way to validate fields supported in custom header, e.g %ENVIRONMENT(X):Z%

	return nil
}

// validateWeight checks if weight is valid
func validateWeight(weight int32) error {
	if weight < 0 {
		return fmt.Errorf("weight %d < 0", weight)
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
var ValidateGateway = RegisterValidateFunc("ValidateGateway",
	func(cfg config.Config) (Warning, error) {
		name := cfg.Name

		// Check if this was converted from a k8s gateway-api resource
		gatewaySemantics := cfg.Annotations[constants.InternalGatewaySemantics] == constants.GatewaySemanticsGateway

		v := Validation{}

		// Gateway name must conform to the DNS label format (no dots)
		if !labels.IsDNS1123Label(name) && !gatewaySemantics {
			v = AppendValidation(v, fmt.Errorf("invalid gateway name: %q", name))
		}
		value, ok := cfg.Spec.(*networking.Gateway)
		if !ok {
			v = AppendValidation(v, fmt.Errorf("cannot cast to gateway: %#v", cfg.Spec))
			return v.Unwrap()
		}

		if len(value.Servers) == 0 {
			v = AppendValidation(v, fmt.Errorf("gateway must have at least one server"))
		} else {
			for _, server := range value.Servers {
				v = AppendValidation(v, validateServer(server, gatewaySemantics))
			}
		}

		// Ensure unique port names
		portNames := make(map[string]bool)

		for _, s := range value.Servers {
			if s == nil {
				v = AppendValidation(v, fmt.Errorf("server may not be nil"))
				continue
			}
			if s.Port != nil {
				if portNames[s.Port.Name] {
					v = AppendValidation(v, fmt.Errorf("port names in servers must be unique: duplicate name %s", s.Port.Name))
				}
				portNames[s.Port.Name] = true
				if !protocol.Parse(s.Port.Protocol).IsHTTP() && s.GetTls().GetHttpsRedirect() {
					v = AppendValidation(v, WrapWarning(fmt.Errorf("tls.httpsRedirect should only be used with http servers")))
				}
			}
		}

		return v.Unwrap()
	})

func validateServer(server *networking.Server, gatewaySemantics bool) (v Validation) {
	if server == nil {
		return WrapError(fmt.Errorf("cannot have nil server"))
	}
	if len(server.Hosts) == 0 {
		v = AppendValidation(v, fmt.Errorf("server config must contain at least one host"))
	} else {
		for _, hostname := range server.Hosts {
			v = AppendValidation(v, agent.ValidateNamespaceSlashWildcardHostname(hostname, true, gatewaySemantics))
		}
	}
	portErr := validateServerPort(server.Port, server.Bind)
	v = AppendValidation(v, portErr)

	v = AppendValidation(v, validateServerBind(server.Port, server.Bind))
	v = AppendValidation(v, validateTLSOptions(server.Tls))

	// If port is HTTPS or TLS, make sure that server has TLS options
	if _, err := portErr.Unwrap(); err == nil {
		p := protocol.Parse(server.Port.Protocol)
		if p.IsTLS() && server.Tls == nil {
			v = AppendValidation(v, fmt.Errorf("server must have TLS settings for HTTPS/TLS protocols"))
		} else if !p.IsTLS() && server.Tls != nil {
			// only tls redirect is allowed if this is a HTTP server
			if p.IsHTTP() {
				if !gateway.IsPassThroughServer(server) ||
					server.Tls.CaCertificates != "" || server.Tls.PrivateKey != "" || server.Tls.ServerCertificate != "" {
					v = AppendValidation(v, fmt.Errorf("server cannot have TLS settings for plain text HTTP ports"))
				}
			} else {
				v = AppendValidation(v, fmt.Errorf("server cannot have TLS settings for non HTTPS/TLS ports"))
			}
		}
	}
	return v
}

func validateServerPort(port *networking.Port, bind string) (errs Validation) {
	if port == nil {
		return AppendValidation(errs, fmt.Errorf("port is required"))
	}
	if protocol.Parse(port.Protocol) == protocol.Unsupported {
		errs = AppendValidation(errs, fmt.Errorf("invalid protocol %q, supported protocols are HTTP, HTTP2, GRPC, GRPC-WEB, MONGO, REDIS, MYSQL, TCP", port.Protocol))
	}
	if port.Number > 0 || !strings.HasPrefix(bind, UnixAddressPrefix) {
		errs = AppendValidation(errs, agent.ValidatePort(int(port.Number)))
	}
	// nolint: staticcheck
	if port.TargetPort > 0 {
		errs = AppendValidation(errs, fmt.Errorf("targetPort has no impact on Gateways"))
	}

	if port.Name == "" {
		errs = AppendValidation(errs, fmt.Errorf("port name must be set: %v", port))
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
		errs = appendErrors(errs, agent.ValidateIPAddress(bind))
	}
	return
}

func validateTLSOptions(tls *networking.ServerTLSSettings) (v Validation) {
	if tls == nil {
		// no tls config at all is valid
		return
	}
	if tls.MinProtocolVersion == networking.ServerTLSSettings_TLSV1_0 || tls.MinProtocolVersion == networking.ServerTLSSettings_TLSV1_1 {
		if len(tls.CipherSuites) == 0 {
			v = AppendWarningf(v, "TLS version below TLSV1_2 require setting compatible ciphers as by default they no longer include compatible ciphers.")
		}
	}

	invalidCiphers := sets.New[string]()
	validCiphers := sets.New[string]()
	duplicateCiphers := sets.New[string]()
	for _, cs := range tls.CipherSuites {
		if !security.IsValidCipherSuite(cs) {
			invalidCiphers.Insert(cs)
		} else if validCiphers.InsertContains(cs) {
			duplicateCiphers.Insert(cs)
		}
	}

	if len(invalidCiphers) > 0 {
		v = AppendWarningf(v, "ignoring invalid cipher suites: %v", sets.SortedList(invalidCiphers))
	}

	if len(duplicateCiphers) > 0 {
		v = AppendWarningf(v, "ignoring duplicate cipher suites: %v", sets.SortedList(duplicateCiphers))
	}

	if tls.Mode == networking.ServerTLSSettings_ISTIO_MUTUAL {
		// ISTIO_MUTUAL TLS mode uses either SDS or default certificate mount paths
		// therefore, we should fail validation if other TLS fields are set
		if tls.ServerCertificate != "" {
			v = AppendValidation(v, fmt.Errorf("ISTIO_MUTUAL TLS cannot have associated server certificate"))
		}
		if tls.PrivateKey != "" {
			v = AppendValidation(v, fmt.Errorf("ISTIO_MUTUAL TLS cannot have associated private key"))
		}
		if tls.CaCertificates != "" {
			v = AppendValidation(v, fmt.Errorf("ISTIO_MUTUAL TLS cannot have associated CA bundle"))
		}
		if len(tls.TlsCertificates) > 0 {
			v = AppendValidation(v, fmt.Errorf("ISTIO_MUTUAL TLS cannot have associated tlsCertificates"))
		}
		if tls.CredentialName != "" {
			v = AppendValidation(v, fmt.Errorf("ISTIO_MUTUAL TLS cannot have associated credentialName"))
		}
		if len(tls.CredentialNames) > 0 {
			v = AppendValidation(v, fmt.Errorf("ISTIO_MUTUAL TLS cannot have associated credentialNames"))
		}
		return
	}

	if tls.Mode == networking.ServerTLSSettings_PASSTHROUGH || tls.Mode == networking.ServerTLSSettings_AUTO_PASSTHROUGH {
		if tls.CaCrl != "" || tls.ServerCertificate != "" || tls.PrivateKey != "" ||
			tls.CaCertificates != "" || len(tls.TlsCertificates) > 0 ||
			tls.CredentialName != "" || len(tls.CredentialNames) > 0 {
			// Warn for backwards compatibility
			v = AppendWarningf(v, "%v mode does not use certificates, they will be ignored", tls.Mode)
		}
	}

	// Validate that only one certificate source is specified
	certSourceCount := 0
	if tls.ServerCertificate != "" && tls.PrivateKey != "" {
		certSourceCount++
	}
	if tls.CredentialName != "" {
		certSourceCount++
	}
	if len(tls.CredentialNames) > 0 {
		certSourceCount++
	}
	if len(tls.TlsCertificates) > 0 {
		certSourceCount++
	}

	if certSourceCount > 1 {
		v = AppendValidation(v, fmt.Errorf("only one of credential_name, credential_names, server_certificate/private_key, tls_certificates should be specified"))
	}

	if (tls.Mode == networking.ServerTLSSettings_SIMPLE || tls.Mode == networking.ServerTLSSettings_MUTUAL ||
		tls.Mode == networking.ServerTLSSettings_OPTIONAL_MUTUAL) && (tls.CredentialName != "" || len(tls.CredentialNames) > 0) {
		// If tls mode is SIMPLE or MUTUAL/OPTIONL_MUTUAL, and CredentialName is specified, credentials are fetched
		// remotely. ServerCertificate and CaCertificates fields are not required.
		return
	}
	var serverCertsToVerify []*networking.ServerTLSSettings_TLSCertificate
	if len(tls.TlsCertificates) > 0 {
		serverCertsToVerify = tls.TlsCertificates
	} else {
		serverCertsToVerify = []*networking.ServerTLSSettings_TLSCertificate{
			{
				ServerCertificate: tls.ServerCertificate,
				PrivateKey:        tls.PrivateKey,
			},
		}
	}
	if tls.Mode == networking.ServerTLSSettings_SIMPLE || tls.Mode == networking.ServerTLSSettings_MUTUAL ||
		tls.Mode == networking.ServerTLSSettings_OPTIONAL_MUTUAL {
		validationPrefix := "SIMPLE TLS"
		if tls.Mode == networking.ServerTLSSettings_MUTUAL || tls.Mode == networking.ServerTLSSettings_OPTIONAL_MUTUAL {
			validationPrefix = "MUTUAL TLS"
			if tls.CaCertificates == "" {
				v = AppendValidation(v, fmt.Errorf("%s requires a client CA bundle", validationPrefix))
			}
		}
		if len(tls.TlsCertificates) > 2 {
			v = AppendWarningf(v, "%s can support up to 2 server certificates", validationPrefix)
		}
		for _, cert := range serverCertsToVerify {
			if cert.ServerCertificate == "" {
				v = AppendValidation(v, fmt.Errorf("%s requires a server certificate", validationPrefix))
			}
			if cert.PrivateKey == "" {
				v = AppendValidation(v, fmt.Errorf("%s requires a private key", validationPrefix))
			}
		}
	}
	if tls.CaCrl != "" {
		if tls.CredentialName != "" {
			v = AppendValidation(v, fmt.Errorf("CRL is not supported with credentialName. CRL has to be specified in the credential"))
		}
		if tls.Mode == networking.ServerTLSSettings_SIMPLE {
			v = AppendValidation(v, fmt.Errorf("CRL is not supported with SIMPLE TLS"))
		}
	}
	return
}

// ValidateDestinationRule checks proxy policies
var ValidateDestinationRule = RegisterValidateFunc("ValidateDestinationRule",
	func(cfg config.Config) (Warning, error) {
		rule, ok := cfg.Spec.(*networking.DestinationRule)
		if !ok {
			return nil, fmt.Errorf("cannot cast to destination rule")
		}
		v := Validation{}
		v = AppendValidation(v,
			agent.ValidateWildcardDomain(rule.Host),
			validateTrafficPolicy(cfg.Namespace, rule.TrafficPolicy))

		subsets := sets.String{}
		for _, subset := range rule.Subsets {
			if subset == nil {
				v = AppendValidation(v, errors.New("subset may not be null"))
				continue
			}
			if subsets.InsertContains(subset.Name) {
				v = AppendValidation(v, fmt.Errorf("duplicate subset names: %s", subset.Name))
			}
			v = AppendValidation(v, validateSubset(cfg.Namespace, subset))
		}
		v = AppendValidation(v,
			validateExportTo(cfg.Namespace, rule.ExportTo, false, rule.GetWorkloadSelector() != nil))

		v = AppendValidation(v, validateWorkloadSelector(rule.GetWorkloadSelector()))

		return v.Unwrap()
	})

func validateExportTo(namespace string, exportTo []string, isServiceEntry bool, isDestinationRuleWithSelector bool) (errs error) {
	if len(exportTo) > 0 {
		// Make sure there are no duplicates
		exportToSet := sets.New[string]()
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
				if exportToSet.Len() > 1 {
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

func ValidateAlphaWorkloadSelector(selector *networking.WorkloadSelector) (Warning, error) {
	var errs error
	var warning Warning

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
		if len(selector.Labels) == 0 {
			warning = fmt.Errorf("workload selector specified without labels") // nolint: stylecheck
		}
	}

	return warning, errs
}

// ValidateSidecar checks sidecar config supplied by user
var ValidateSidecar = RegisterValidateFunc("ValidateSidecar",
	func(cfg config.Config) (Warning, error) {
		errs := Validation{}
		rule, ok := cfg.Spec.(*networking.Sidecar)
		if !ok {
			return nil, fmt.Errorf("cannot cast to Sidecar")
		}

		warning, err := ValidateAlphaWorkloadSelector(rule.WorkloadSelector)
		if err != nil {
			return nil, err
		}

		// If workloadSelector is defined and labels are not set, it is most likely
		// an user error. Marking it as a warning to keep it backwards compatible.
		if warning != nil {
			errs = AppendValidation(errs, WrapWarning(fmt.Errorf("sidecar: %s, will be applied to all services in namespace",
				warning))) // nolint: stylecheck
		}

		if len(rule.Egress) == 0 && len(rule.Ingress) == 0 && rule.OutboundTrafficPolicy == nil && rule.InboundConnectionPool == nil {
			return nil, fmt.Errorf("sidecar: empty configuration provided")
		}

		portMap := sets.Set[uint32]{}
		for _, i := range rule.Ingress {
			if i == nil {
				errs = AppendValidation(errs, fmt.Errorf("sidecar: ingress may not be null"))
				continue
			}
			if i.Port == nil {
				errs = AppendValidation(errs, fmt.Errorf("sidecar: port is required for ingress listeners"))
				continue
			}

			// nolint: staticcheck
			if i.Port.TargetPort > 0 {
				errs = AppendValidation(errs, fmt.Errorf("targetPort has no impact on Sidecars"))
			}

			bind := i.GetBind()
			errs = AppendValidation(errs, validateSidecarIngressPortAndBind(i.Port, bind))

			if portMap.Contains(i.Port.Number) {
				errs = AppendValidation(errs, fmt.Errorf("sidecar: ports on IP bound listeners must be unique"))
			}
			portMap.Insert(i.Port.Number)

			if len(i.DefaultEndpoint) != 0 {
				if strings.HasPrefix(i.DefaultEndpoint, UnixAddressPrefix) {
					errs = AppendValidation(errs, ValidateUnixAddress(strings.TrimPrefix(i.DefaultEndpoint, UnixAddressPrefix)))
				} else {
					// format should be 127.0.0.1:port, [::1]:port or :port
					sHost, sPort, sErr := net.SplitHostPort(i.DefaultEndpoint)
					if sErr != nil {
						errs = AppendValidation(errs, sErr)
					}
					if sHost != "" && sHost != "127.0.0.1" && sHost != "0.0.0.0" && sHost != "::1" && sHost != "::" {
						errMsg := "sidecar: defaultEndpoint must be of form 127.0.0.1:<port>,0.0.0.0:<port>,[::1]:port,[::]:port,unix://filepath or unset"
						errs = AppendValidation(errs, errors.New(errMsg))
					}
					port, err := strconv.Atoi(sPort)
					if err != nil {
						errs = AppendValidation(errs, fmt.Errorf("sidecar: defaultEndpoint port (%s) is not a number: %v", sPort, err))
					} else {
						errs = AppendValidation(errs, agent.ValidatePort(port))
					}
				}
			}

			if i.Tls != nil {
				if len(i.Tls.SubjectAltNames) > 0 {
					errs = AppendValidation(errs, fmt.Errorf("sidecar: subjectAltNames is not supported in ingress tls"))
				}
				if i.Tls.HttpsRedirect {
					errs = AppendValidation(errs, fmt.Errorf("sidecar: httpsRedirect is not supported"))
				}
				if i.Tls.CredentialName != "" {
					errs = AppendValidation(errs, fmt.Errorf("sidecar: credentialName is not currently supported"))
				}
				if len(i.Tls.CredentialNames) > 0 {
					errs = AppendValidation(errs, fmt.Errorf("sidecar: credentialNames is not currently supported"))
				}
				if len(i.Tls.TlsCertificates) > 0 {
					errs = AppendValidation(errs, fmt.Errorf("sidecar: tls_certificates is not currently supported"))
				}
				if i.Tls.Mode == networking.ServerTLSSettings_ISTIO_MUTUAL || i.Tls.Mode == networking.ServerTLSSettings_AUTO_PASSTHROUGH {
					errs = AppendValidation(errs, fmt.Errorf("configuration is invalid: cannot set mode to %s in sidecar ingress tls", i.Tls.Mode.String()))
				}
				protocol := protocol.Parse(i.Port.Protocol)
				if !protocol.IsTLS() {
					errs = AppendValidation(errs, fmt.Errorf("server cannot have TLS settings for non HTTPS/TLS ports"))
				}
				errs = AppendValidation(errs, validateTLSOptions(i.Tls))
			}

			// Validate per-port connection pool settings
			errs = AppendValidation(errs, validateConnectionPool(i.ConnectionPool))
			if i.ConnectionPool != nil && i.ConnectionPool.Http != nil && i.Port != nil && !protocol.Parse(i.Port.Protocol).IsHTTP() {
				errs = AppendWarningf(errs,
					"sidecar: HTTP connection pool settings are configured for port %d (%q) but its protocol is not HTTP (%s); only TCP settings will apply",
					i.Port.Number, i.Port.Name, i.Port.Protocol)
			}
		}

		// Validate top-level connection pool setting
		errs = AppendValidation(errs, validateConnectionPool(rule.InboundConnectionPool))

		portMap = sets.Set[uint32]{}
		udsMap := sets.String{}
		catchAllEgressListenerFound := false
		for index, egress := range rule.Egress {
			if egress == nil {
				errs = AppendValidation(errs, errors.New("egress listener may not be null"))
				continue
			}
			// there can be only one catch all egress listener with empty port, and it should be the last listener.
			if egress.Port == nil {
				if !catchAllEgressListenerFound {
					if index == len(rule.Egress)-1 {
						catchAllEgressListenerFound = true
					} else {
						errs = AppendValidation(errs, fmt.Errorf("sidecar: the egress listener with empty port should be the last listener in the list"))
					}
				} else {
					errs = AppendValidation(errs, fmt.Errorf("sidecar: egress can have only one listener with empty port"))
					continue
				}
			} else {
				// nolint: staticcheck
				if egress.Port.TargetPort > 0 {
					errs = AppendValidation(errs, fmt.Errorf("targetPort has no impact on Sidecars"))
				}
				bind := egress.GetBind()
				captureMode := egress.GetCaptureMode()
				errs = AppendValidation(errs, validateSidecarEgressPortBindAndCaptureMode(egress.Port, bind, captureMode))

				if egress.Port.Number == 0 {
					if _, found := udsMap[bind]; found {
						errs = AppendValidation(errs, fmt.Errorf("sidecar: unix domain socket values for listeners must be unique"))
					}
					udsMap[bind] = struct{}{}
				} else {
					if portMap.Contains(egress.Port.Number) {
						errs = AppendValidation(errs, fmt.Errorf("sidecar: ports on IP bound listeners must be unique"))
					}
					portMap.Insert(egress.Port.Number)
				}
			}

			// validate that the hosts field is a slash separated value
			// of form ns1/host, or */host, or */*, or ns1/*, or ns1/*.example.com
			if len(egress.Hosts) == 0 {
				errs = AppendValidation(errs, fmt.Errorf("sidecar: egress listener must contain at least one host"))
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
								errs = AppendValidation(errs, WrapWarning(fmt.Errorf("duplicated egress host: %s", hostname)))
							}
						} else {
							if len(nssSvcs[ns]) != 0 {
								errs = AppendValidation(errs, WrapWarning(fmt.Errorf("duplicated egress host: %s", hostname)))
							}
						}
						nssSvcs[ns][svc] = true
					}
					errs = AppendValidation(errs, agent.ValidateNamespaceSlashWildcardHostname(hostname, false, false))
				}
				// */*
				// test/a
				if nssSvcs["*"]["*"] && len(nssSvcs) != 1 {
					errs = AppendValidation(errs, WrapWarning(fmt.Errorf("`*/*` host select all resources, no other hosts can be added")))
				}
			}
		}

		errs = AppendValidation(errs, validateSidecarOutboundTrafficPolicy(rule.OutboundTrafficPolicy))

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

		errs = appendErrors(errs, agent.ValidateFQDN(tp.EgressProxy.GetHost()))

		if tp.EgressProxy.Port == nil {
			errs = appendErrors(errs, fmt.Errorf("sidecar: egress_proxy port must be non-nil"))
			return
		}
		errs = appendErrors(errs, validateDestination(tp.EgressProxy))
	}
	return
}

func validateSidecarEgressPortBindAndCaptureMode(port *networking.SidecarPort, bind string,
	captureMode networking.CaptureMode,
) (errs error) {
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
			agent.ValidatePort(int(port.Number)))

		if len(bind) != 0 {
			errs = appendErrors(errs, agent.ValidateIPAddress(bind))
		}
	}

	return
}

func validateSidecarIngressPortAndBind(port *networking.SidecarPort, bind string) (errs error) {
	// Port name is optional. Validate if exists.
	if len(port.Name) > 0 {
		errs = appendErrors(errs, ValidatePortName(port.Name))
	}

	errs = appendErrors(errs,
		ValidateProtocol(port.Protocol),
		agent.ValidatePort(int(port.Number)))

	if len(bind) != 0 {
		errs = appendErrors(errs, agent.ValidateIPAddress(bind))
	}

	return
}

func validateTrafficPolicy(configNamespace string, policy *networking.TrafficPolicy) Validation {
	if policy == nil {
		return Validation{}
	}
	if policy.OutlierDetection == nil && policy.ConnectionPool == nil &&
		policy.LoadBalancer == nil && policy.Tls == nil && policy.PortLevelSettings == nil && policy.Tunnel == nil && policy.ProxyProtocol == nil {
		return WrapError(fmt.Errorf("traffic policy must have at least one field"))
	}

	if policy.Tunnel != nil && policy.ProxyProtocol != nil {
		return WrapError(fmt.Errorf("tunnel and proxyProtocol must not be set together"))
	}

	return AppendValidation(validateOutlierDetection(policy.OutlierDetection),
		validateConnectionPool(policy.ConnectionPool),
		validateLoadBalancer(policy.LoadBalancer, policy.OutlierDetection),
		agent.ValidateTLS(configNamespace, policy.Tls),
		validatePortTrafficPolicies(configNamespace, policy.PortLevelSettings),
		validateTunnelSettings(policy.Tunnel),
		validateProxyProtocol(policy.ProxyProtocol))
}

func validateProxyProtocol(proxyProtocol *networking.TrafficPolicy_ProxyProtocol) (errs error) {
	if proxyProtocol == nil {
		return
	}
	if proxyProtocol.Version != 0 && proxyProtocol.Version != 1 {
		errs = appendErrors(errs, fmt.Errorf("proxy protocol version is invalid: %d", proxyProtocol.Version))
	}
	return
}

func validateTunnelSettings(tunnel *networking.TrafficPolicy_TunnelSettings) (errs error) {
	if tunnel == nil {
		return
	}
	if tunnel.Protocol != "" && tunnel.Protocol != "CONNECT" && tunnel.Protocol != "POST" {
		errs = appendErrors(errs, fmt.Errorf("tunnel protocol must be \"CONNECT\" or \"POST\""))
	}
	fqdnErr := agent.ValidateFQDN(tunnel.TargetHost)
	ipErr := agent.ValidateIPAddress(tunnel.TargetHost)
	if fqdnErr != nil && ipErr != nil {
		errs = appendErrors(errs, fmt.Errorf("tunnel target host must be valid FQDN or IP address: %s; %s", fqdnErr, ipErr))
	}
	if err := agent.ValidatePort(int(tunnel.TargetPort)); err != nil {
		errs = appendErrors(errs, fmt.Errorf("tunnel target port is invalid: %s", err))
	}
	return
}

func validateOutlierDetection(outlier *networking.OutlierDetection) (errs Validation) {
	if outlier == nil {
		return
	}

	if outlier.BaseEjectionTime != nil {
		errs = AppendValidation(errs, agent.ValidateDuration(outlier.BaseEjectionTime))
	}
	// nolint: staticcheck
	if outlier.ConsecutiveErrors != 0 {
		warn := "outlier detection consecutive errors is deprecated, use consecutiveGatewayErrors or consecutive5xxErrors instead"
		scope.Warnf(warn)
		errs = AppendValidation(errs, WrapWarning(errors.New(warn)))
	}
	if !outlier.SplitExternalLocalOriginErrors && outlier.ConsecutiveLocalOriginFailures.GetValue() > 0 {
		err := "outlier detection consecutive local origin failures is specified, but split external local origin errors is set to false"
		errs = AppendValidation(errs, errors.New(err))
	}
	if outlier.Interval != nil {
		errs = AppendValidation(errs, agent.ValidateDuration(outlier.Interval))
	}
	errs = AppendValidation(errs, ValidatePercent(outlier.MaxEjectionPercent), ValidatePercent(outlier.MinHealthPercent))

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
			errs = appendErrors(errs, agent.ValidateDuration(httpSettings.IdleTimeout))
		}
		if httpSettings.H2UpgradePolicy == networking.ConnectionPoolSettings_HTTPSettings_UPGRADE && httpSettings.UseClientProtocol {
			errs = appendErrors(errs, fmt.Errorf("use client protocol must not be true when H2UpgradePolicy is UPGRADE"))
		}
		if httpSettings.MaxConcurrentStreams < 0 {
			errs = appendErrors(errs, fmt.Errorf("max concurrent streams must be non-negative"))
		}
	}

	if tcp := settings.Tcp; tcp != nil {
		if tcp.MaxConnections < 0 {
			errs = appendErrors(errs, fmt.Errorf("max connections must be non-negative"))
		}
		if tcp.ConnectTimeout != nil {
			errs = appendErrors(errs, agent.ValidateDuration(tcp.ConnectTimeout))
		}
		if tcp.MaxConnectionDuration != nil {
			errs = appendErrors(errs, agent.ValidateDuration(tcp.MaxConnectionDuration))
		}
		if tcp.IdleTimeout != nil {
			// Skip validation if the value is 0s, as it indicates that the timeout is disabled entirely.
			if tcp.IdleTimeout.AsDuration().Milliseconds() != 0 {
				errs = appendErrors(errs, agent.ValidateDuration(tcp.IdleTimeout))
			}
		}
		if ka := tcp.TcpKeepalive; ka != nil {
			if ka.Time != nil {
				errs = appendErrors(errs, agent.ValidateDuration(ka.Time))
			}
			if ka.Interval != nil {
				errs = appendErrors(errs, agent.ValidateDuration(ka.Interval))
			}
		}
	}

	return
}

func validateLoadBalancer(settings *networking.LoadBalancerSettings, outlier *networking.OutlierDetection) (errs Validation) {
	if settings == nil {
		return
	}

	// simple load balancing is always valid
	consistentHash := settings.GetConsistentHash()
	if consistentHash != nil {
		httpCookie := consistentHash.GetHttpCookie()
		if httpCookie != nil && httpCookie.GetName() == "" {
			errs = AppendValidation(errs, fmt.Errorf("name required for HttpCookie"))
		}
		if consistentHash.MinimumRingSize != 0 { // nolint: staticcheck
			warn := "consistent hash MinimumRingSize is deprecated, use ConsistentHashLB's RingHash configuration instead"
			scope.Warnf(warn)
			errs = AppendValidation(errs, WrapWarning(errors.New(warn)))
		}
		// nolint: staticcheck
		if consistentHash.MinimumRingSize != 0 && consistentHash.GetHashAlgorithm() != nil {
			errs = AppendValidation(errs, fmt.Errorf("only one of MinimumRingSize or Maglev/Ringhash can be specified"))
		}
		if ml := consistentHash.GetMaglev(); ml != nil {
			if ml.TableSize == 0 {
				errs = AppendValidation(errs, fmt.Errorf("tableSize must be set for maglev"))
			}
			if ml.TableSize >= 5000011 {
				errs = AppendValidation(errs, fmt.Errorf("tableSize must be less than 5000011 for maglev"))
			}
			if !isPrime(ml.TableSize) {
				errs = AppendValidation(errs, fmt.Errorf("tableSize must be a prime number for maglev"))
			}
		}
	}

	errs = AppendValidation(errs, agent.ValidateLocalityLbSetting(settings.LocalityLbSetting, outlier))

	if warm := settings.Warmup; warm != nil {
		if warm.Duration == nil {
			errs = AppendValidation(errs, fmt.Errorf("duration is required"))
		} else {
			errs = AppendValidation(errs, agent.ValidateDuration(warm.Duration))
		}
		if warm.MinimumPercent.GetValue() > 100 {
			errs = AppendValidation(errs, fmt.Errorf("minimumPercent value should be less than or equal to 100"))
		}
		if warm.Aggression != nil && warm.Aggression.GetValue() < 1 {
			errs = AppendValidation(errs, fmt.Errorf("aggression should be greater than or equal to 1"))
		}
	}
	return
}

// Copied from https://github.com/envoyproxy/envoy/blob/5451efd9b8f8a444431197050e45ba974ed4e9d8/source/common/common/utility.cc#L601-L615
// to ensure we 100% match Envoy's implementation
func isPrime(x uint64) bool {
	if x != 0 && x < 4 {
		return true // eliminates special-casing 2.
	} else if (x & 1) == 0 {
		return false // eliminates even numbers >2.
	}

	limit := uint64(math.Sqrt(float64(x)))
	for factor := uint64(3); factor <= limit; factor += 2 {
		if (x % factor) == 0 {
			return false
		}
	}
	return true
}

func validateSubset(configNamespace string, subset *networking.Subset) error {
	return appendErrors(validateSubsetName(subset.Name),
		labels.Instance(subset.Labels).Validate(),
		validateTrafficPolicy(configNamespace, subset.TrafficPolicy))
}

func validatePortTrafficPolicies(configNamespace string, pls []*networking.TrafficPolicy_PortTrafficPolicy) (errs error) {
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
				validateLoadBalancer(t.LoadBalancer, t.OutlierDetection),
				agent.ValidateTLS(configNamespace, t.Tls))
		}
	}
	return
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

func validatePolicyTargetReferences(targetRefs []*type_beta.PolicyTargetReference) (v Validation) {
	for _, r := range targetRefs {
		v = AppendValidation(v, validatePolicyTargetReference(r))
	}
	return
}

// don't validate version, just group and kind
var allowedTargetRefs = []config.GroupVersionKind{
	gvk.Service,
	gvk.ServiceEntry,
	gvk.KubernetesGateway,
	gvk.GatewayClass,
}

func validatePolicyTargetReference(targetRef *type_beta.PolicyTargetReference) (v Validation) {
	if targetRef == nil {
		return
	}
	if targetRef.Name == "" {
		v = appendErrorf(v, "targetRef name must be set")
	}
	if targetRef.Namespace != "" {
		// cross namespace referencing is currently not supported
		v = appendErrorf(v, "targetRef namespace must not be set; cross namespace referencing is not supported")
	}

	canoncalGroup := targetRef.Group
	if canoncalGroup == "" {
		canoncalGroup = "core"
	}
	allowed := slices.FindFunc(allowedTargetRefs, func(gvk config.GroupVersionKind) bool {
		return gvk.Kind == targetRef.Kind && gvk.CanonicalGroup() == canoncalGroup
	}) != nil

	if !allowed {
		v = appendErrorf(v, "targetRef must be to one of %v but was %s/%s",
			allowedTargetRefs, targetRef.Group, targetRef.Kind)
	}
	return
}

func validateWorkloadSelector(selector *type_beta.WorkloadSelector) Validation {
	validation := Validation{}
	if selector != nil {
		for k, v := range selector.MatchLabels {
			if k == "" {
				err := fmt.Errorf("empty key is not supported in selector: %q", fmt.Sprintf("%s=%s", k, v))
				validation = AppendValidation(validation, err)
			}
			if strings.Contains(k, "*") || strings.Contains(v, "*") {
				err := fmt.Errorf("wildcard is not supported in selector: %q", fmt.Sprintf("%s=%s", k, v))
				validation = AppendValidation(validation, err)
			}
		}
		if len(selector.MatchLabels) == 0 {
			warning := fmt.Errorf("workload selector specified without labels") // nolint: stylecheck
			validation = AppendValidation(validation, WrapWarning(warning))
		}
	}

	return validation
}

func validateOneOfSelectorType(
	selector *type_beta.WorkloadSelector,
	targetRef *type_beta.PolicyTargetReference,
	targetRefs []*type_beta.PolicyTargetReference,
) (v Validation) {
	set := 0
	if selector != nil {
		set++
	}
	if targetRef != nil {
		set++
	}
	if len(targetRefs) > 0 {
		set++
	}
	if set > 1 {
		v = appendErrorf(v, "only one of targetRefs or workloadSelector can be set")
	}
	return
}

// ValidateAuthorizationPolicy checks that AuthorizationPolicy is well-formed.
var ValidateAuthorizationPolicy = RegisterValidateFunc("ValidateAuthorizationPolicy",
	func(cfg config.Config) (Warning, error) {
		in, ok := cfg.Spec.(*security_beta.AuthorizationPolicy)
		if !ok {
			return nil, fmt.Errorf("cannot cast to AuthorizationPolicy")
		}

		var errs error
		var warnings Warning
		selectorTypeValidation := validateOneOfSelectorType(in.GetSelector(), in.GetTargetRef(), in.GetTargetRefs())
		workloadSelectorValidation := validateWorkloadSelector(in.GetSelector())
		targetRefValidation := validatePolicyTargetReference(in.GetTargetRef())
		targetRefsValidation := validatePolicyTargetReferences(in.GetTargetRefs())
		errs = appendErrors(errs, selectorTypeValidation, workloadSelectorValidation, targetRefValidation, targetRefsValidation)
		warnings = appendErrors(warnings, workloadSelectorValidation.Warning)

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
						errs = appendErrors(errs, check(len(src.ServiceAccounts) != 0, "From.ServiceAccounts"))
						errs = appendErrors(errs, check(len(src.NotServiceAccounts) != 0, "From.NotServiceAccounts"))
						errs = appendErrors(errs, check(len(src.Principals) != 0, "From.Principals"))
						errs = appendErrors(errs, check(len(src.NotPrincipals) != 0, "From.NotPrincipals"))
						errs = appendErrors(errs, check(len(src.RequestPrincipals) != 0, "From.RequestPrincipals"))
						errs = appendErrors(errs, check(len(src.NotRequestPrincipals) != 0, "From.NotRequestPrincipals"))
					}
				}
				for _, when := range rule.GetWhen() {
					if when == nil {
						errs = appendErrors(errs, fmt.Errorf("when field cannot be nil"))
						continue
					}
					errs = appendErrors(errs, check(when.Key == "source.namespace", when.Key))
					errs = appendErrors(errs, check(when.Key == "source.serviceAccount", when.Key))
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
			tcpRulesInFrom := false
			tcpRulesInTo := false
			fromRuleExist := false
			toRuleExist := false
			for _, from := range rule.From {
				if from == nil {
					errs = appendErrors(errs, fmt.Errorf("`from` must not be nil, found at rule %d", i))
					continue
				}
				if from.Source == nil {
					errs = appendErrors(errs, fmt.Errorf("`from.source` must not be nil, found at rule %d", i))
				} else {
					fromRuleExist = true
					src := from.Source
					if (len(src.Principals)+len(src.NotPrincipals)+len(src.Namespaces)+len(src.NotNamespaces) > 0) &&
						(len(src.ServiceAccounts)+len(src.NotServiceAccounts) > 0) {
						errs = appendErrors(errs, fmt.Errorf("cannot set serviceAccounts with namespaces or principals, found at rule %d", i))
					}
					if len(src.Principals) == 0 && len(src.RequestPrincipals) == 0 && len(src.Namespaces) == 0 && len(src.IpBlocks) == 0 &&
						len(src.RemoteIpBlocks) == 0 && len(src.NotPrincipals) == 0 && len(src.NotRequestPrincipals) == 0 && len(src.NotNamespaces) == 0 &&
						len(src.NotIpBlocks) == 0 && len(src.NotRemoteIpBlocks) == 0 && len(src.ServiceAccounts) == 0 && len(src.NotServiceAccounts) == 0 {
						errs = appendErrors(errs, fmt.Errorf("`from.source` must not be empty, found at rule %d", i))
					}
					errs = appendErrors(errs, security.ValidateIPs(from.Source.GetIpBlocks()))
					errs = appendErrors(errs, security.ValidateIPs(from.Source.GetNotIpBlocks()))
					errs = appendErrors(errs, security.ValidateIPs(from.Source.GetRemoteIpBlocks()))
					errs = appendErrors(errs, security.ValidateIPs(from.Source.GetNotRemoteIpBlocks()))
					errs = appendErrors(errs, security.CheckEmptyValues("Principals", src.Principals))
					errs = appendErrors(errs, security.CheckEmptyValues("RequestPrincipals", src.RequestPrincipals))
					errs = appendErrors(errs, security.CheckEmptyValues("Namespaces", src.Namespaces))
					errs = appendErrors(errs, security.CheckServiceAccount("ServiceAccounts", src.ServiceAccounts))
					errs = appendErrors(errs, security.CheckEmptyValues("IpBlocks", src.IpBlocks))
					errs = appendErrors(errs, security.CheckEmptyValues("RemoteIpBlocks", src.RemoteIpBlocks))
					errs = appendErrors(errs, security.CheckEmptyValues("NotPrincipals", src.NotPrincipals))
					errs = appendErrors(errs, security.CheckEmptyValues("NotRequestPrincipals", src.NotRequestPrincipals))
					errs = appendErrors(errs, security.CheckEmptyValues("NotNamespaces", src.NotNamespaces))
					errs = appendErrors(errs, security.CheckServiceAccount("NotServiceAccounts", src.NotServiceAccounts))
					errs = appendErrors(errs, security.CheckEmptyValues("NotIpBlocks", src.NotIpBlocks))
					errs = appendErrors(errs, security.CheckEmptyValues("NotRemoteIpBlocks", src.NotRemoteIpBlocks))
					if src.NotPrincipals != nil || src.Principals != nil || src.IpBlocks != nil ||
						src.NotIpBlocks != nil || src.Namespaces != nil ||
						src.NotNamespaces != nil || src.RemoteIpBlocks != nil || src.NotRemoteIpBlocks != nil ||
						src.ServiceAccounts != nil || src.NotServiceAccounts != nil {
						tcpRulesInFrom = true
					}
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
					toRuleExist = true
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
					errs = appendErrors(errs, security.CheckValidPathTemplate("Paths", op.Paths))
					errs = appendErrors(errs, security.CheckValidPathTemplate("NotPaths", op.NotPaths))
					if op.Ports != nil || op.NotPorts != nil {
						tcpRulesInTo = true
					}
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

			if ((fromRuleExist && !toRuleExist && !tcpRulesInFrom) || (toRuleExist && !tcpRulesInTo)) &&
				in.Action == security_beta.AuthorizationPolicy_DENY {
				warning := fmt.Errorf("configured AuthorizationPolicy will deny all traffic " +
					"to TCP ports under its scope due to the use of only HTTP attributes in a DENY rule; " +
					"it is recommended to explicitly specify the port")
				warnings = appendErrors(warnings, warning)

			}
		}
		return warnings, multierror.Prefix(errs, fmt.Sprintf("invalid policy %s.%s:", cfg.Name, cfg.Namespace))
	})

// ValidateRequestAuthentication checks that request authentication spec is well-formed.
var ValidateRequestAuthentication = RegisterValidateFunc("ValidateRequestAuthentication",
	func(cfg config.Config) (Warning, error) {
		in, ok := cfg.Spec.(*security_beta.RequestAuthentication)
		if !ok {
			return nil, errors.New("cannot cast to RequestAuthentication")
		}

		errs := Validation{}
		errs = AppendValidation(errs,
			validateOneOfSelectorType(in.GetSelector(), in.GetTargetRef(), in.GetTargetRefs()),
			validateWorkloadSelector(in.GetSelector()),
			validatePolicyTargetReference(in.GetTargetRef()),
			validatePolicyTargetReferences(in.GetTargetRefs()),
		)

		for _, rule := range in.JwtRules {
			errs = AppendValidation(errs, validateJwtRule(rule))
		}
		return errs.Unwrap()
	})

func validateJwtRule(rule *security_beta.JWTRule) (errs error) {
	if rule == nil {
		return nil
	}
	for _, audience := range rule.Audiences {
		if len(audience) == 0 {
			errs = multierror.Append(errs, errors.New("audience must be non-empty string"))
		}
	}

	if len(rule.Issuer) == 0 && len(rule.JwksUri) == 0 {
		errs = multierror.Append(errs, errors.New("issuer or jwksUri must be non-empty string"))
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

	for _, location := range rule.FromCookies {
		if len(location) == 0 {
			errs = multierror.Append(errs, errors.New("cookie name must be non-empty string"))
		}
	}

	for _, claimAndHeaders := range rule.OutputClaimToHeaders {
		if claimAndHeaders == nil {
			errs = multierror.Append(errs, errors.New("outputClaimToHeaders must not be null"))
			continue
		}
		if claimAndHeaders.Claim == "" || claimAndHeaders.Header == "" {
			errs = multierror.Append(errs, errors.New("outputClaimToHeaders header and claim value must be non-empty string"))
			continue
		}
		if err := ValidateStrictHTTPHeaderName(claimAndHeaders.Header); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	if rule.Timeout != nil {
		if err := agent.ValidateDuration(rule.Timeout); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	return
}

// ValidatePeerAuthentication checks that peer authentication spec is well-formed.
var ValidatePeerAuthentication = RegisterValidateFunc("ValidatePeerAuthentication",
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
			if port <= 0 || port > 65535 {
				errs = appendErrors(errs, fmt.Errorf("port must be in range 1..65535"))
			}
		}

		validation := validateWorkloadSelector(in.Selector)
		errs = appendErrors(errs, validation)

		return validation.Warning, errs
	})

// ValidateVirtualService checks that a v1alpha3 route rule is well-formed.
var ValidateVirtualService = RegisterValidateFunc("ValidateVirtualService",
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
				errs = AppendValidation(errs, fmt.Errorf("delegate virtual service must have no gateways specified"))
			}
			if len(virtualService.Tls) != 0 {
				// meaningless to specify tls in delegate, we do not support tls delegate
				errs = AppendValidation(errs, fmt.Errorf("delegate virtual service must have no tls route specified"))
			}
			if len(virtualService.Tcp) != 0 {
				// meaningless to specify tls in delegate, we do not support tcp delegate
				errs = AppendValidation(errs, fmt.Errorf("delegate virtual service must have no tcp route specified"))
			}
		}
		gatewaySemantics := cfg.Annotations[constants.InternalRouteSemantics] == constants.RouteSemanticsGateway

		appliesToMesh := false
		appliesToGateway := false
		if len(virtualService.Gateways) == 0 {
			appliesToMesh = true
		} else {
			errs = AppendValidation(errs, validateGatewayNames(virtualService.Gateways, gatewaySemantics))
			appliesToGateway = isGateway(virtualService)
			appliesToMesh = !appliesToGateway
		}

		if !appliesToGateway {
			validateJWTClaimRoute := func(headers map[string]*networking.StringMatch) {
				for key := range headers {
					if jwt.ToRoutingClaim(key).Match {
						msg := fmt.Sprintf("JWT claim based routing (key: %s) is only supported for gateway, found no gateways: %v", key, virtualService.Gateways)
						errs = AppendValidation(errs, errors.New(msg))
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
			var err error
			if appliesToGateway {
				err = agent.ValidateWildcardDomainForVirtualServiceBoundToGateway(isSniHost(virtualService), virtualHost)
			} else {
				err = agent.ValidateWildcardDomain(virtualHost)
			}
			if err != nil {
				if !netutil.IsValidIPAddress(virtualHost) {
					errs = AppendValidation(errs, err)
					allHostsValid = false
				}
			} else if appliesToMesh && virtualHost == "*" {
				errs = AppendValidation(errs, fmt.Errorf("wildcard host * is not allowed for virtual services bound to the mesh gateway"))
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
						errs = AppendValidation(errs, fmt.Errorf("duplicate hosts in virtual service: %s & %s", hostI, hostJ))
					}
				}
			}
		}

		if len(virtualService.Http) == 0 && len(virtualService.Tcp) == 0 && len(virtualService.Tls) == 0 {
			errs = AppendValidation(errs, errors.New("http, tcp or tls must be provided in virtual service"))
		}
		for _, httpRoute := range virtualService.Http {
			if httpRoute == nil {
				errs = AppendValidation(errs, errors.New("http route may not be null"))
				continue
			}
			errs = AppendValidation(errs, validateHTTPRoute(httpRoute, len(virtualService.Hosts) == 0, gatewaySemantics))
		}
		for _, tlsRoute := range virtualService.Tls {
			errs = AppendValidation(errs, validateTLSRoute(tlsRoute, virtualService, gatewaySemantics))
		}
		for _, tcpRoute := range virtualService.Tcp {
			errs = AppendValidation(errs, validateTCPRoute(tcpRoute, gatewaySemantics))
		}

		errs = AppendValidation(errs, validateExportTo(cfg.Namespace, virtualService.ExportTo, false, false))

		warnUnused := func(ruleno, reason string) {
			errs = AppendValidation(errs, WrapWarning(&AnalysisAwareError{
				Type:       "VirtualServiceUnreachableRule",
				Msg:        fmt.Sprintf("virtualService rule %v not used (%s)", ruleno, reason),
				Parameters: []any{ruleno, reason},
			}))
		}
		warnIneffective := func(ruleno, matchno, dupno string) {
			errs = AppendValidation(errs, WrapWarning(&AnalysisAwareError{
				Type:       "VirtualServiceIneffectiveMatch",
				Msg:        fmt.Sprintf("virtualService rule %v match %v is not used (duplicate/overlapping match in rule %v)", ruleno, matchno, dupno),
				Parameters: []any{ruleno, matchno, dupno},
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

func isSniHost(context *networking.VirtualService) bool {
	for _, tls := range context.Tls {
		for _, match := range tls.Match {
			if len(match.SniHosts) > 0 {
				return true
			}
		}
	}
	return false
}

func isGateway(context *networking.VirtualService) bool {
	for _, gatewayName := range context.Gateways {
		if gatewayName == constants.IstioMeshGateway {
			return false
		}
	}
	return true
}

// genMatchHTTPRoutes build the match rules into struct OverlappingMatchValidationForHTTPRoute
// based on particular HTTPMatchRequest, according to comments on https://github.com/istio/istio/pull/32701
// only support Match's port, method, authority, headers, query params and nonheaders for now.
func genMatchHTTPRoutes(route *networking.HTTPRoute, match *networking.HTTPMatchRequest,
	rulen, matchn int,
) (matchHTTPRoutes *OverlappingMatchValidationForHTTPRoute) {
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
	reportUnreachable func(ruleno, reason string), reportIneffective func(ruleno, matchno, dupno string),
) {
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
			if emptyMatchEncountered >= 0 {
				reportUnreachable(routeName(route, rulen), "route without matches defined before")
				continue
			}
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
	reportUnreachable func(ruleno, reason string), reportIneffective func(ruleno, matchno, dupno string),
) {
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
			if emptyMatchEncountered >= 0 {
				reportUnreachable(routeName(route, rulen), "route without matches defined before")
				continue
			}
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
	reportUnreachable func(ruleno, reason string), reportIneffective func(ruleno, matchno, dupno string),
) {
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
			if emptyMatchEncountered >= 0 {
				reportUnreachable(routeName(route, rulen), "route without matches defined before")
				continue
			}
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
func asJSON(data any) string {
	// Remove the name, so we can create a serialization that only includes traffic routing config
	switch mr := data.(type) {
	case *networking.HTTPMatchRequest:
		if mr != nil && mr.Name != "" {
			cl := protomarshal.ShallowClone(mr)
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

func routeName(route any, routen int) string {
	switch r := route.(type) {
	case *networking.HTTPRoute:
		if r.Name != "" {
			return fmt.Sprintf("%q", r.Name)
		}
		// TCP and TLS routes have no names
	}

	return fmt.Sprintf("#%d", routen)
}

func requestName(match any, matchn int) string {
	switch mr := match.(type) {
	case *networking.HTTPMatchRequest:
		if mr != nil && mr.Name != "" {
			return fmt.Sprintf("%q", mr.Name)
		}
		// TCP and TLS matches have no names
	}

	return fmt.Sprintf("#%d", matchn)
}

func validateTLSRoute(tls *networking.TLSRoute, context *networking.VirtualService, gatewaySemantics bool) (errs Validation) {
	if tls == nil {
		return
	}
	if len(tls.Match) == 0 {
		errs = AppendValidation(errs, errors.New("TLS route must have at least one match condition"))
	}
	for _, match := range tls.Match {
		errs = AppendValidation(errs, validateTLSMatch(match, context))
	}
	if len(tls.Route) == 0 {
		errs = AppendValidation(errs, errors.New("TLS route is required"))
	}
	errs = AppendValidation(errs, validateRouteDestinations(tls.Route, gatewaySemantics))
	return errs
}

func validateTLSMatch(match *networking.TLSMatchAttributes, context *networking.VirtualService) (errs Validation) {
	if match == nil {
		errs = AppendValidation(errs, errors.New("TLS match may not be null"))
		return
	}
	if len(match.SniHosts) == 0 {
		errs = AppendValidation(errs, fmt.Errorf("TLS match must have at least one SNI host"))
	} else {
		for _, sniHost := range match.SniHosts {
			errs = AppendValidation(errs, validateSniHost(sniHost, context))
		}
	}

	for _, destinationSubnet := range match.DestinationSubnets {
		errs = AppendValidation(errs, agent.ValidateIPSubnet(destinationSubnet))
	}

	if match.Port != 0 {
		errs = AppendValidation(errs, agent.ValidatePort(int(match.Port)))
	}
	errs = AppendValidation(errs, labels.Instance(match.SourceLabels).Validate())
	errs = AppendValidation(errs, validateGatewayNames(match.Gateways, false))
	return
}

func validateSniHost(sniHost string, context *networking.VirtualService) (errs Validation) {
	var err error
	if isGateway(context) {
		err = agent.ValidateWildcardDomainForVirtualServiceBoundToGateway(true, sniHost)
	} else {
		err = agent.ValidateWildcardDomain(sniHost)
	}
	if err != nil {
		// Could also be an IP
		if netutil.IsValidIPAddress(sniHost) {
			errs = AppendValidation(errs, WrapWarning(fmt.Errorf("using an IP address (%q) goes against SNI spec and most clients do not support this", sniHost)))
			return
		}
		return AppendValidation(errs, err)
	}
	sniHostname := host.Name(sniHost)
	for _, hostname := range context.Hosts {
		if sniHostname.SubsetOf(host.Name(hostname)) {
			return
		}
	}
	return AppendValidation(errs, fmt.Errorf("SNI host %q is not a compatible subset of any of the virtual service hosts: [%s]",
		sniHost, strings.Join(context.Hosts, ", ")))
}

func validateTCPRoute(tcp *networking.TCPRoute, gatewaySemantics bool) (errs error) {
	if tcp == nil {
		return nil
	}
	for _, match := range tcp.Match {
		errs = appendErrors(errs, validateTCPMatch(match))
	}
	if len(tcp.Route) == 0 {
		errs = appendErrors(errs, errors.New("TCP route is required"))
	}
	errs = appendErrors(errs, validateRouteDestinations(tcp.Route, gatewaySemantics))
	return
}

func validateTCPMatch(match *networking.L4MatchAttributes) (errs error) {
	if match == nil {
		errs = multierror.Append(errs, errors.New("tcp match may not be nil"))
		return
	}
	for _, destinationSubnet := range match.DestinationSubnets {
		errs = appendErrors(errs, agent.ValidateIPSubnet(destinationSubnet))
	}
	if match.Port != 0 {
		errs = appendErrors(errs, agent.ValidatePort(int(match.Port)))
	}
	errs = appendErrors(errs, labels.Instance(match.SourceLabels).Validate())
	errs = appendErrors(errs, validateGatewayNames(match.Gateways, false))
	return
}

func validateStringMatch(sm *networking.StringMatch, where string) error {
	switch sm.GetMatchType().(type) {
	case *networking.StringMatch_Prefix:
		if sm.GetPrefix() == "" {
			return fmt.Errorf("%q: prefix string match should not be empty", where)
		}
	case *networking.StringMatch_Regex:
		return validateStringMatchRegexp(sm, where)
	}
	return nil
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
	return validateStringRegexp(re, where)
}

func validateStringRegexp(re string, where string) error {
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

func validateGatewayNames(gatewayNames []string, gatewaySemantics bool) (errs Validation) {
	for _, gatewayName := range gatewayNames {
		parts := strings.SplitN(gatewayName, "/", 2)
		if len(parts) != 2 {
			if strings.Contains(gatewayName, ".") {
				// Legacy FQDN style
				parts := strings.Split(gatewayName, ".")
				recommended := fmt.Sprintf("%s/%s", parts[1], parts[0])
				errs = AppendValidation(errs, WrapWarning(fmt.Errorf(
					"using legacy gatewayName format %q; prefer the <namespace>/<name> format: %q", gatewayName, recommended)))
			}
			errs = AppendValidation(errs, agent.ValidateFQDN(gatewayName))
			return
		}

		if len(parts[0]) == 0 || len(parts[1]) == 0 {
			errs = AppendValidation(errs, fmt.Errorf("config namespace and gateway name cannot be empty"))
		}

		// namespace and name must be DNS labels
		if !labels.IsDNS1123Label(parts[0]) {
			errs = AppendValidation(errs, fmt.Errorf("invalid value for namespace: %q", parts[0]))
		}

		if !labels.IsDNS1123Label(parts[1]) && !gatewaySemantics {
			errs = AppendValidation(errs, fmt.Errorf("invalid value for gateway name: %q", parts[1]))
		}
	}
	return
}

func validateHTTPRouteDestinations(weights []*networking.HTTPRouteDestination, gatewaySemantics bool) (errs error) {
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
			errs = appendErrors(errs, ValidateHTTPHeaderWithAuthorityOperationName(name))
			errs = appendErrors(errs, ValidateHTTPHeaderValue(val))
		}
		for name, val := range weight.Headers.GetRequest().GetSet() {
			errs = appendErrors(errs, ValidateHTTPHeaderWithAuthorityOperationName(name))
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

		if !gatewaySemantics {
			errs = appendErrors(errs, validateDestination(weight.Destination))
		}
		errs = appendErrors(errs, validateWeight(weight.Weight))
		totalWeight += weight.Weight
	}
	if len(weights) > 1 && totalWeight == 0 {
		errs = appendErrors(errs, fmt.Errorf("total destination weight = 0"))
	}
	return
}

func validateRouteDestinations(weights []*networking.RouteDestination, gatewaySemantics bool) (errs error) {
	var totalWeight int32
	for _, weight := range weights {
		if weight == nil {
			errs = multierror.Append(errs, errors.New("weight may not be nil"))
			continue
		}
		if weight.Destination == nil {
			errs = multierror.Append(errs, errors.New("destination is required"))
		}
		if !gatewaySemantics {
			errs = appendErrors(errs, validateDestination(weight.Destination))
		}
		errs = appendErrors(errs, validateWeight(weight.Weight))
		totalWeight += weight.Weight
	}
	if len(weights) > 1 && totalWeight == 0 {
		errs = appendErrors(errs, fmt.Errorf("total destination weight = 0"))
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
		errs = appendErrors(errs, validateCORSHTTPMethod(method))
	}

	for _, name := range policy.AllowHeaders {
		errs = appendErrors(errs, ValidateCORSHTTPHeaderName(name))
	}

	for _, name := range policy.ExposeHeaders {
		errs = appendErrors(errs, ValidateCORSHTTPHeaderName(name))
	}

	if policy.MaxAge != nil {
		errs = appendErrors(errs, agent.ValidateDuration(policy.MaxAge))
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

func validateCORSHTTPMethod(method string) error {
	if !supportedCORSMethods.Contains(method) {
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

func validateHTTPFaultInjectionAbort(abort *networking.HTTPFaultInjection_Abort) (errs Validation) {
	if abort == nil {
		return
	}

	errs = AppendValidation(errs, validatePercentage(abort.Percentage))

	switch abort.ErrorType.(type) {
	case *networking.HTTPFaultInjection_Abort_GrpcStatus:
		errs = AppendValidation(errs, validateGRPCStatus(abort.GetGrpcStatus()))
	case *networking.HTTPFaultInjection_Abort_Http2Error:
		// TODO: HTTP2 error validation
		errs = AppendValidation(errs, errors.New("HTTP/2 abort fault injection not supported yet"))
	case *networking.HTTPFaultInjection_Abort_HttpStatus:
		errs = AppendValidation(errs, validateHTTPStatus(abort.GetHttpStatus()))
	default:
		errs = AppendWarningf(errs, "abort configured, but error type not set")
	}

	return
}

func validateHTTPStatus(status int32) error {
	if status < 200 || status > 600 {
		return fmt.Errorf("HTTP status %d is not in range 200-599", status)
	}
	return nil
}

func validateGRPCStatus(status string) error {
	_, found := grpc.SupportedGRPCStatus[status]
	if !found {
		return fmt.Errorf("gRPC status %q is not supported. See https://github.com/grpc/grpc/blob/master/doc/statuscodes.md "+
			"for a list of supported codes, for example 'NOT_FOUND'", status)
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
		errs = appendErrors(errs, agent.ValidateDuration(v.FixedDelay))
	case *networking.HTTPFaultInjection_Delay_ExponentialDelay:
		errs = appendErrors(errs, agent.ValidateDuration(v.ExponentialDelay))
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
		errs = appendErrors(errs, agent.ValidateWildcardDomain(hostname))
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
	errs = appendErrors(errs, agent.ValidatePort(number))
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
		errs = appendErrors(errs, agent.ValidateDuration(retries.PerTryTimeout))
	}
	if retries.RetryOn != "" {
		retryOnPolicies := strings.Split(retries.RetryOn, ",")
		for _, policy := range retryOnPolicies {
			// Try converting it to an integer to see if it's a valid HTTP status code.
			i, _ := strconv.Atoi(policy)

			if http.StatusText(i) == "" && !supportedRetryOnPolicies.Contains(policy) {
				errs = appendErrors(errs, fmt.Errorf("%q is not a valid retryOn policy", policy))
			}
		}
	}
	if retries.Backoff != nil {
		errs = appendErrors(errs, agent.ValidateDuration(retries.Backoff))
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
		if err := agent.ValidatePort(int(redirect.GetPort())); err != nil {
			return err
		}
	}
	return nil
}

func validateHTTPDirectResponse(directResponse *networking.HTTPDirectResponse) (errs Validation) {
	if directResponse == nil {
		return
	}

	if directResponse.Body != nil {
		size := 0
		switch op := directResponse.Body.Specifier.(type) {
		case *networking.HTTPBody_String_:
			size = len(op.String_)
		case *networking.HTTPBody_Bytes:
			size = len(op.Bytes)
		}

		if size > 1*mb {
			errs = AppendValidation(errs, WrapError(fmt.Errorf("large direct_responses may impact control plane performance, must be less than 1MB")))
		} else if size > 100*kb {
			errs = AppendValidation(errs, WrapWarning(fmt.Errorf("large direct_responses may impact control plane performance")))
		}
	}

	errs = AppendValidation(errs, WrapError(validateHTTPStatus(int32(directResponse.Status))))
	return
}

func validateHTTPMirrors(mirrors []*networking.HTTPMirrorPolicy) error {
	errs := Validation{}
	for _, mirror := range mirrors {
		if mirror == nil {
			errs = AppendValidation(errs, errors.New("mirror cannot be null"))
			continue
		}
		if mirror.Destination == nil {
			errs = AppendValidation(errs, errors.New("destination is required for mirrors"))
			continue
		}
		errs = AppendValidation(errs, validateDestination(mirror.Destination))

		if mirror.Percentage != nil {
			value := mirror.Percentage.GetValue()
			if value > 100 {
				errs = AppendValidation(errs, fmt.Errorf("mirror percentage must have a max value of 100 (it has %f)", value))
			}
			if value < 0 {
				errs = AppendValidation(errs, fmt.Errorf("mirror percentage must have a min value of 0 (it has %f)", value))
			}
		}
	}
	return errs
}

func validateHTTPRewrite(rewrite *networking.HTTPRewrite) error {
	if rewrite == nil {
		return nil
	}
	if rewrite.Uri != "" && rewrite.UriRegexRewrite != nil {
		return errors.New("rewrite may only contain one of URI or UriRegexRewrite")
	}
	if rewrite.Uri == "" && rewrite.UriRegexRewrite == nil && rewrite.Authority == "" {
		return errors.New("rewrite must specify at least one of URI, UriRegexRewrite, or authority. Only one of URI or UriRegexRewrite may be specified")
	}
	if err := validateURIRegexRewrite(rewrite.UriRegexRewrite); err != nil {
		return errors.Join(errors.New("UriRegexRewrite has errors"), err)
	}
	return nil
}

func validateURIRegexRewrite(regexRewrite *networking.RegexRewrite) error {
	if regexRewrite == nil {
		return nil
	}
	if regexRewrite.Match == "" || regexRewrite.Rewrite == "" {
		return errors.New("UriRegexRewrite requires both Rewrite and Match fields to be specified")
	}
	return validateStringRegexp(regexRewrite.Match, "HTTPRewrite.UriRegexRewrite.Match")
}

// ValidateWorkloadEntry validates a workload entry.
var ValidateWorkloadEntry = RegisterValidateFunc("ValidateWorkloadEntry",
	func(cfg config.Config) (Warning, error) {
		we, ok := cfg.Spec.(*networking.WorkloadEntry)
		if !ok {
			return nil, fmt.Errorf("cannot cast to workload entry")
		}
		// TODO: We currently don't validate if we port is part of service port, as that is tricky to do without ServiceEntry
		return validateWorkloadEntry(we, nil, true).Unwrap()
	})

func validateWorkloadEntry(we *networking.WorkloadEntry, servicePorts sets.String, allowFQDNAddresses bool) Validation {
	errs := Validation{}
	unixEndpoint := false

	addr := we.GetAddress()
	if addr == "" {
		if we.Network == "" {
			return appendErrorf(errs, "address is required")
		}
		errs = AppendWarningf(errs, "address is unset with network %q", we.Network)
	}

	// Since we don't know if it's meant to be DNS or STATIC type without association with a ServiceEntry,
	// check based on content and try validations.
	// First check if it is a Unix endpoint - this will be specified for STATIC.
	if strings.HasPrefix(addr, UnixAddressPrefix) {
		unixEndpoint = true
		errs = AppendValidation(errs, ValidateUnixAddress(strings.TrimPrefix(addr, UnixAddressPrefix)))
		if len(we.Ports) != 0 {
			errs = AppendValidation(errs, fmt.Errorf("unix endpoint %s must not include ports", addr))
		}
	} else if addr != "" && !netutil.IsValidIPAddress(addr) { // This could be IP (in STATIC resolution) or DNS host name (for DNS).
		if !allowFQDNAddresses {
			errs = AppendValidation(errs, fmt.Errorf("endpoint address %q is not a valid IP address", addr))
		} else if err := agent.ValidateFQDN(addr); err != nil { // Otherwise could be an FQDN
			errs = AppendValidation(errs, fmt.Errorf("endpoint address %q is not a valid FQDN or an IP address", addr))
		}
	}

	for name, port := range we.Ports {
		if servicePorts != nil && !servicePorts.Contains(name) {
			errs = AppendValidation(errs, fmt.Errorf("endpoint port %v is not defined by the service entry", port))
		}
		errs = AppendValidation(errs,
			ValidatePortName(name),
			agent.ValidatePort(int(port)),
		)
	}
	errs = AppendValidation(errs, labels.Instance(we.Labels).Validate())
	if unixEndpoint && servicePorts != nil && len(servicePorts) != 1 {
		errs = AppendValidation(errs, errors.New("exactly 1 service port required for unix endpoints"))
	}
	return errs
}

// ValidateWorkloadGroup validates a workload group.
var ValidateWorkloadGroup = RegisterValidateFunc("ValidateWorkloadGroup",
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
		errs = appendErrors(errs, agent.ValidatePort(int(h.Port)))
		if h.Scheme != "" && h.Scheme != string(apimirror.URISchemeHTTPS) && h.Scheme != string(apimirror.URISchemeHTTP) {
			errs = appendErrors(errs, fmt.Errorf(`httpGet.scheme must be one of %q, %q`, apimirror.URISchemeHTTPS, apimirror.URISchemeHTTP))
		}
		for _, header := range h.HttpHeaders {
			if header == nil {
				errs = appendErrors(errs, fmt.Errorf("invalid nil header"))
				continue
			}
			errs = appendErrors(errs, ValidateProbeHeaderName(header.Name))
		}
	case *networking.ReadinessProbe_TcpSocket:
		h := m.TcpSocket
		if h == nil {
			errs = appendErrors(errs, fmt.Errorf("tcpSocket may not be nil"))
			break
		}
		errs = appendErrors(errs, agent.ValidatePort(int(h.Port)))
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
var ValidateServiceEntry = RegisterValidateFunc("ValidateServiceEntry",
	func(cfg config.Config) (Warning, error) {
		serviceEntry, ok := cfg.Spec.(*networking.ServiceEntry)
		if !ok {
			return nil, fmt.Errorf("cannot cast to service entry")
		}

		errs := Validation{}

		warning, err := ValidateAlphaWorkloadSelector(serviceEntry.WorkloadSelector)
		if err != nil {
			return nil, err
		}

		// If workloadSelector is defined and labels are not set, it is most likely
		// an user error. Marking it as a warning to keep it backwards compatible.
		if warning != nil {
			errs = AppendValidation(errs, WrapWarning(fmt.Errorf("service entry: %s, will be applied to all services in namespace",
				warning))) // nolint: stylecheck
		}

		if serviceEntry.WorkloadSelector != nil && serviceEntry.Endpoints != nil {
			errs = AppendValidation(errs, fmt.Errorf("only one of WorkloadSelector or Endpoints is allowed in Service Entry"))
		}

		if len(serviceEntry.Hosts) == 0 {
			errs = AppendValidation(errs, fmt.Errorf("service entry must have at least one host"))
		}
		for _, hostname := range serviceEntry.Hosts {
			// Full wildcard is not allowed in the service entry.
			if hostname == "*" {
				errs = AppendValidation(errs, fmt.Errorf("invalid host %s", hostname))
			} else {
				errs = AppendValidation(errs, agent.ValidateWildcardDomain(hostname))
				errs = AppendValidation(errs, WrapWarning(agent.ValidatePartialWildCard(hostname)))
			}
		}

		cidrFound := false
		for _, address := range serviceEntry.Addresses {
			cidrFound = cidrFound || strings.Contains(address, "/")
			errs = AppendValidation(errs, agent.ValidateIPSubnet(address))
		}

		if cidrFound {
			if serviceEntry.Resolution != networking.ServiceEntry_NONE && serviceEntry.Resolution != networking.ServiceEntry_STATIC {
				errs = AppendValidation(errs, fmt.Errorf("CIDR addresses are allowed only for NONE/STATIC resolution types"))
			}
		}

		// check for v2 auto IP allocation or opt-out by user
		autoAllocation := serviceentry.ShouldV2AutoAllocateIPFromConfig(cfg)
		servicePortNumbers := sets.New[uint32]()
		servicePorts := sets.NewWithLength[string](len(serviceEntry.Ports))
		for _, port := range serviceEntry.Ports {
			if port == nil {
				errs = AppendValidation(errs, fmt.Errorf("service entry port may not be null"))
				continue
			}
			if servicePorts.InsertContains(port.Name) {
				errs = AppendValidation(errs, fmt.Errorf("service entry port name %q already defined", port.Name))
			}
			if servicePortNumbers.InsertContains(port.Number) {
				errs = AppendValidation(errs, fmt.Errorf("service entry port %d already defined", port.Number))
			}
			if port.TargetPort != 0 {
				errs = AppendValidation(errs, agent.ValidatePort(int(port.TargetPort)))
			}
			if len(serviceEntry.Addresses) == 0 && !autoAllocation {
				if port.Protocol == "" || port.Protocol == "TCP" {
					errs = AppendValidation(errs, WrapWarning(fmt.Errorf("addresses are required for ports serving TCP (or unset) protocol "+
						"when IP Autoallocation is not enabled")))
				}
			}
			errs = AppendValidation(errs,
				ValidatePortName(port.Name),
				ValidateProtocol(port.Protocol),
				agent.ValidatePort(int(port.Number)))
		}

		switch serviceEntry.Resolution {
		case networking.ServiceEntry_NONE:
			if len(serviceEntry.Endpoints) != 0 {
				errs = AppendValidation(errs, fmt.Errorf("no endpoints should be provided for resolution type none"))
			}
			if serviceEntry.WorkloadSelector != nil {
				errs = AppendWarningf(errs, "workloadSelector should not be set when resolution mode is NONE")
			}
		case networking.ServiceEntry_STATIC:
			for _, endpoint := range serviceEntry.Endpoints {
				if endpoint == nil {
					errs = AppendValidation(errs, errors.New("endpoint cannot be nil"))
					continue
				}
				errs = AppendValidation(errs, validateWorkloadEntry(endpoint, servicePorts, false))
			}
		case networking.ServiceEntry_DNS, networking.ServiceEntry_DNS_ROUND_ROBIN:
			if len(serviceEntry.Endpoints) == 0 {
				for _, hostname := range serviceEntry.Hosts {
					if err := agent.ValidateFQDN(hostname); err != nil {
						errs = AppendValidation(errs,
							fmt.Errorf("hosts must be FQDN if no endpoints are provided for resolution mode %s", serviceEntry.Resolution))
					}
				}
			}
			if serviceEntry.Resolution == networking.ServiceEntry_DNS_ROUND_ROBIN && len(serviceEntry.Endpoints) > 1 {
				errs = AppendValidation(errs,
					fmt.Errorf("there must only be 0 or 1 endpoint for resolution mode %s", serviceEntry.Resolution))
			}

			for _, endpoint := range serviceEntry.Endpoints {
				if endpoint == nil {
					errs = AppendValidation(errs, errors.New("endpoint cannot be nil"))
					continue
				}
				if !netutil.IsValidIPAddress(endpoint.Address) {
					if err := agent.ValidateFQDN(endpoint.Address); err != nil { // Otherwise could be an FQDN
						errs = AppendValidation(errs,
							fmt.Errorf("endpoint address %q is not a valid FQDN or an IP address", endpoint.Address))
					}
				}
				errs = AppendValidation(errs,
					labels.Instance(endpoint.Labels).Validate())
				for name, port := range endpoint.Ports {
					if !servicePorts.Contains(name) {
						errs = AppendValidation(errs, fmt.Errorf("endpoint port %v is not defined by the service entry", port))
					}
					errs = AppendValidation(errs,
						ValidatePortName(name),
						agent.ValidatePort(int(port)))
				}
			}

			if len(serviceEntry.Addresses) > 0 {
				for _, port := range serviceEntry.Ports {
					if port == nil {
						errs = AppendValidation(errs, errors.New("ports cannot be nil"))
						continue
					}
					p := protocol.Parse(port.Protocol)
					if p.IsTCP() {
						if len(serviceEntry.Hosts) > 1 {
							// TODO: prevent this invalid setting, maybe in 1.11+
							errs = AppendValidation(errs, WrapWarning(fmt.Errorf("service entry can not have more than one host specified "+
								"simultaneously with address and tcp port")))
						}
						break
					}
				}
			}
		default:
			errs = AppendValidation(errs, fmt.Errorf("unsupported resolution type %s",
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
				if port == nil {
					errs = AppendValidation(errs, errors.New("ports cannot be nil"))
					continue
				}
				p := protocol.Parse(port.Protocol)
				if !p.IsHTTP() && !p.IsTLS() {
					errs = AppendValidation(errs, fmt.Errorf("multiple hosts provided with non-HTTP, non-TLS ports"))
					break
				}
			}
		}

		errs = AppendValidation(errs, validateExportTo(cfg.Namespace, serviceEntry.ExportTo, true, false))
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
		return fmt.Errorf("jaellio2 unsupported protocol: %s", protocolStr)
	}
	return nil
}

// appendErrorf appends a formatted error string
// nolint: unparam
func appendErrorf(v Validation, format string, a ...any) Validation {
	return AppendValidation(v, fmt.Errorf(format, a...))
}

// AppendWarningf appends a formatted warning string
// nolint: unparam
func AppendWarningf(v Validation, format string, a ...any) Validation {
	return AppendValidation(v, agent.Warningf(format, a...))
}

func (aae *AnalysisAwareError) Error() string {
	return aae.Msg
}

// ValidateProxyConfig validates a ProxyConfig CR (as opposed to the MeshConfig field).
var ValidateProxyConfig = RegisterValidateFunc("ValidateProxyConfig",
	func(cfg config.Config) (Warning, error) {
		spec, ok := cfg.Spec.(*networkingv1beta1.ProxyConfig)
		if !ok {
			return nil, fmt.Errorf("cannot cast to proxyconfig")
		}

		errs := Validation{}

		errs = AppendValidation(errs,
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
var ValidateTelemetry = RegisterValidateFunc("ValidateTelemetry",
	func(cfg config.Config) (Warning, error) {
		spec, ok := cfg.Spec.(*telemetry.Telemetry)
		if !ok {
			return nil, fmt.Errorf("cannot cast to telemetry")
		}

		errs := Validation{}

		errs = AppendValidation(errs,
			validateOneOfSelectorType(spec.GetSelector(), spec.GetTargetRef(), spec.GetTargetRefs()),
			validateWorkloadSelector(spec.GetSelector()),
			validatePolicyTargetReference(spec.GetTargetRef()),
			validatePolicyTargetReferences(spec.GetTargetRefs()),
			validateTelemetryMetrics(spec.Metrics),
			validateTelemetryTracing(spec.Tracing),
			validateTelemetryAccessLogging(spec.AccessLogging),
		)
		return errs.Unwrap()
	})

func validateTelemetryAccessLogging(logging []*telemetry.AccessLogging) (v Validation) {
	for _, l := range logging {
		if l == nil {
			continue
		}
		if l.Filter != nil {
			v = AppendValidation(v, validateTelemetryFilter(l.Filter))
		}
		v = AppendValidation(v, validateTelemetryProviders(l.Providers))
	}
	return
}

func validateTelemetryTracing(tracing []*telemetry.Tracing) (v Validation) {
	if len(tracing) > 1 {
		v = AppendWarningf(v, "multiple tracing is not currently supported")
	}
	for _, l := range tracing {
		if l == nil {
			continue
		}
		if len(l.Providers) > 1 {
			v = AppendWarningf(v, "multiple providers is not currently supported")
		}
		v = AppendValidation(v, validateTelemetryProviders(l.Providers))
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
		v = AppendValidation(v, validateTelemetryProviders(l.Providers))
		for _, o := range l.Overrides {
			if o == nil {
				v = appendErrorf(v, "tagOverrides may not be null")
				continue
			}
			for tagName, to := range o.TagOverrides {
				if tagName == "" {
					v = AppendWarningf(v, "tagOverrides.name may not be empty")
				}
				if to == nil {
					v = appendErrorf(v, "tagOverrides may not be null")
					continue
				}
				switch to.Operation {
				case telemetry.MetricsOverrides_TagOverride_UPSERT:
					if to.Value == "" {
						v = appendErrorf(v, "tagOverrides.value must be set when operation is UPSERT")
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
var ValidateWasmPlugin = RegisterValidateFunc("ValidateWasmPlugin",
	func(cfg config.Config) (Warning, error) {
		spec, ok := cfg.Spec.(*extensions.WasmPlugin)
		if !ok {
			return nil, fmt.Errorf("cannot cast to wasmplugin")
		}
		// figure out how to add check for targetRef and workload selector is not nil
		errs := Validation{}
		errs = AppendValidation(errs,
			validateOneOfSelectorType(spec.GetSelector(), spec.GetTargetRef(), spec.GetTargetRefs()),
			validateWorkloadSelector(spec.GetSelector()),
			validatePolicyTargetReference(spec.GetTargetRef()),
			validatePolicyTargetReferences(spec.GetTargetRefs()),
			validateWasmPluginURL(spec.Url),
			validateWasmPluginSHA(spec),
			validateWasmPluginImagePullSecret(spec),
			validateWasmPluginName(spec),
			validateWasmPluginVMConfig(spec.VmConfig),
			validateWasmPluginMatch(spec.Match),
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

	u, err := strictParseURL(pluginURL)
	if err != nil {
		return err
	}
	if _, found := validSchemes[u.Scheme]; !found {
		return fmt.Errorf("url contains unsupported scheme: %s", u.Scheme)
	}
	return nil
}

func strictParseURL(originalURL string) (*url.URL, error) {
	u := originalURL
	ur, err := url.ParseRequestURI(u)
	if err != nil {
		u = "http://" + originalURL
		nu, nerr := url.ParseRequestURI(u)
		if nerr != nil {
			return nil, fmt.Errorf("failed to parse url: %s", err) // return original err
		}
		if _, err := url.Parse(u); err != nil {
			return nil, fmt.Errorf("failed to strict parse url: %s", err)
		}
		return nu, nil
	}
	if _, err := url.Parse(u); err != nil {
		return nil, fmt.Errorf("failed to strict parse url: %s", err)
	}
	return ur, nil
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

func validateWasmPluginImagePullSecret(plugin *extensions.WasmPlugin) error {
	if len(plugin.ImagePullSecret) > 253 {
		return fmt.Errorf("imagePullSecret field must be less than 253 characters long")
	}
	return nil
}

func validateWasmPluginName(plugin *extensions.WasmPlugin) error {
	if len(plugin.PluginName) > 256 {
		return fmt.Errorf("pluginName field must be less than 255 characters long")
	}
	return nil
}

func validateWasmPluginVMConfig(vm *extensions.VmConfig) error {
	if vm == nil || len(vm.Env) == 0 {
		return nil
	}

	keys := sets.New[string]()
	for _, env := range vm.Env {
		if env == nil {
			continue
		}

		if env.Name == "" {
			return fmt.Errorf("spec.vmConfig.env invalid")
		}

		if len(env.Name) > 256 {
			return fmt.Errorf("env.name field must be less than 255 characters long")
		}

		if keys.InsertContains(env.Name) {
			return fmt.Errorf("duplicate env")
		}

		if env.ValueFrom != extensions.EnvValueSource_INLINE && env.Value != "" {
			return fmt.Errorf("value may only be set when valueFrom is INLINE")
		}
	}

	return nil
}

func validateWasmPluginMatch(selectors []*extensions.WasmPlugin_TrafficSelector) error {
	if len(selectors) == 0 {
		return nil
	}
	for selIdx, sel := range selectors {
		if sel == nil {
			return fmt.Errorf("spec.Match[%d] is nil", selIdx)
		}
		for portIdx, port := range sel.Ports {
			if port == nil {
				return fmt.Errorf("spec.Match[%d].Ports[%d] is nil", selIdx, portIdx)
			}
			if port.GetNumber() <= 0 || port.GetNumber() > 65535 {
				return fmt.Errorf("spec.Match[%d].Ports[%d] is out of range: %d", selIdx, portIdx, port.GetNumber())
			}
		}
	}
	return nil
}
