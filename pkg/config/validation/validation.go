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
	"errors"
	"fmt"
	"net"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	wellknown "github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"
	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"
	networking "istio.io/api/networking/v1alpha3"
	security_beta "istio.io/api/security/v1beta1"
	type_beta "istio.io/api/type/v1beta1"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/gateway"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/security"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/config/xds"
)

// Constants for duration fields
const (
	connectTimeoutMax = time.Second * 30
	connectTimeoutMin = time.Millisecond

	drainTimeMax          = time.Hour
	parentShutdownTimeMax = time.Hour

	// UnixAddressPrefix is the prefix used to indicate an address is for a Unix Domain socket. It is used in
	// ServiceEntry.Endpoint.Address message.
	UnixAddressPrefix = "unix://"
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

	_ ValidateFunc = EmptyValidate

	// EmptyValidate is a Validate that does nothing and returns no error.
	EmptyValidate = registerValidateFunc("EmptyValidate",
		func(string, string, proto.Message) error {
			return nil
		})

	validateFuncs = make(map[string]ValidateFunc)
)

// Validate defines a validation func for an API proto.
type ValidateFunc func(name, namespace string, config proto.Message) error

// IsValidateFunc indicates whether there is a validation function with the given name.
func IsValidateFunc(name string) bool {
	return GetValidateFunc(name) != nil
}

// GetValidateFunc returns the validation function with the given name, or null if it does not exist.
func GetValidateFunc(name string) ValidateFunc {
	return validateFuncs[name]
}

func registerValidateFunc(name string, f ValidateFunc) ValidateFunc {
	validateFuncs[name] = f
	return f
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
		if !labels.IsDNS1123Label(svc.Name) {
			errs = multierror.Append(errs, fmt.Errorf("name %q must be a valid label", svc.Name))
		}
	}

	if svc.Namespace != "" && !labels.IsDNS1123Label(svc.Namespace) {
		errs = multierror.Append(errs, fmt.Errorf("namespace %q must be a valid label", svc.Namespace))
	}

	if svc.Domain != "" {
		if err := ValidateFQDN(svc.Domain); err != nil {
			errs = multierror.Append(errs, err)
		}
	}

	if err := labels.Instance(svc.Labels).Validate(); err != nil {
		errs = multierror.Append(errs, err)
	}

	return
}

// ValidateHTTPHeaderName validates a header name
func ValidateHTTPHeaderName(name string) error {
	if name == "" {
		return fmt.Errorf("header name cannot be empty")
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
	func(name, _ string, msg proto.Message) (errs error) {
		// Gateway name must conform to the DNS label format (no dots)
		if !labels.IsDNS1123Label(name) {
			errs = appendErrors(errs, fmt.Errorf("invalid gateway name: %q", name))
		}
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
			if s.Port != nil {
				if portNames[s.Port.Name] {
					errs = appendErrors(errs, fmt.Errorf("port names in servers must be unique: duplicate name %s", s.Port.Name))
				}
				portNames[s.Port.Name] = true
			}
		}

		return errs
	})

func validateServer(server *networking.Server) (errs error) {
	if len(server.Hosts) == 0 {
		errs = appendErrors(errs, fmt.Errorf("server config must contain at least one host"))
	} else {
		for _, hostname := range server.Hosts {
			errs = appendErrors(errs, validateNamespaceSlashWildcardHostname(hostname, true))
		}
	}
	portErr := validateServerPort(server.Port)
	if portErr != nil {
		errs = appendErrors(errs, portErr)
	}
	errs = appendErrors(errs, validateTLSOptions(server.Tls))

	// If port is HTTPS or TLS, make sure that server has TLS options
	if portErr == nil {
		p := protocol.Parse(server.Port.Protocol)
		if p.IsTLS() && server.Tls == nil {
			errs = appendErrors(errs, fmt.Errorf("server must have TLS settings for HTTPS/TLS protocols"))
		} else if !p.IsTLS() && server.Tls != nil {
			// only tls redirect is allowed if this is a HTTP server
			if p.IsHTTP() {
				if !gateway.IsPassThroughServer(server) ||
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

func validateTLSOptions(tls *networking.ServerTLSSettings) (errs error) {
	if tls == nil {
		// no tls config at all is valid
		return
	}

	if tls.Mode == networking.ServerTLSSettings_ISTIO_MUTUAL {
		// ISTIO_MUTUAL TLS mode uses either SDS or default certificate mount paths
		// therefore, we should fail validation if other TLS fields are set
		if tls.ServerCertificate != "" {
			errs = appendErrors(errs, fmt.Errorf("ISTIO_MUTUAL TLS cannot have associated server certificate"))
		}
		if tls.PrivateKey != "" {
			errs = appendErrors(errs, fmt.Errorf("ISTIO_MUTUAL TLS cannot have associated private key"))
		}
		if tls.CaCertificates != "" {
			errs = appendErrors(errs, fmt.Errorf("ISTIO_MUTUAL TLS cannot have associated CA bundle"))
		}

		return
	}

	if (tls.Mode == networking.ServerTLSSettings_SIMPLE || tls.Mode == networking.ServerTLSSettings_MUTUAL) && tls.CredentialName != "" {
		// If tls mode is SIMPLE or MUTUAL, and CredentialName is specified, credentials are fetched
		// remotely. ServerCertificate and CaCertificates fields are not required.
		return
	}
	if tls.Mode == networking.ServerTLSSettings_SIMPLE {
		if tls.ServerCertificate == "" {
			errs = appendErrors(errs, fmt.Errorf("SIMPLE TLS requires a server certificate"))
		}
		if tls.PrivateKey == "" {
			errs = appendErrors(errs, fmt.Errorf("SIMPLE TLS requires a private key"))
		}
	} else if tls.Mode == networking.ServerTLSSettings_MUTUAL {
		if tls.ServerCertificate == "" {
			errs = appendErrors(errs, fmt.Errorf("MUTUAL TLS requires a server certificate"))
		}
		if tls.PrivateKey == "" {
			errs = appendErrors(errs, fmt.Errorf("MUTUAL TLS requires a private key"))
		}
		if tls.CaCertificates == "" {
			errs = appendErrors(errs, fmt.Errorf("MUTUAL TLS requires a client CA bundle"))
		}
	}
	return
}

// ValidateDestinationRule checks proxy policies
var ValidateDestinationRule = registerValidateFunc("ValidateDestinationRule",
	func(_, namespace string, msg proto.Message) (errs error) {
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

		errs = appendErrors(errs, validateExportTo(namespace, rule.ExportTo, false))
		return
	})

func validateExportTo(namespace string, exportTo []string, isServiceEntry bool) (errs error) {
	if len(exportTo) > 0 {
		// Make sure there are no duplicates
		exportToMap := make(map[string]struct{})
		for _, e := range exportTo {
			key := e
			if visibility.Instance(e) == visibility.Private {
				// substitute this with the current namespace so that we
				// can check for duplicates like ., namespace
				key = namespace
			}
			if _, exists := exportToMap[key]; exists {
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
					exportToMap[key] = struct{}{}
				} else {
					if err := visibility.Instance(key).Validate(); err != nil {
						errs = appendErrors(errs, err)
					} else {
						exportToMap[key] = struct{}{}
					}
				}
			}
		}

		// Make sure we have only one of . or *
		if _, public := exportToMap[string(visibility.Public)]; public {
			// make sure that there are no other entries in the exportTo
			// i.e. no point in saying ns1,ns2,*. Might as well say *
			if len(exportTo) > 1 {
				errs = appendErrors(errs, fmt.Errorf("cannot have both public (*) and non-public exportTo values for a resource"))
			}
		}

		// if this is a service entry, then we need to disallow * and ~ together. Or ~ and other namespaces
		if _, none := exportToMap[string(visibility.None)]; none {
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
	func(_, _ string, msg proto.Message) (errs error) {
		rule, ok := msg.(*networking.EnvoyFilter)
		if !ok {
			return fmt.Errorf("cannot cast to Envoy filter")
		}

		if err := validateAlphaWorkloadSelector(rule.WorkloadSelector); err != nil {
			return err
		}

		for _, cp := range rule.ConfigPatches {
			if cp.ApplyTo == networking.EnvoyFilter_INVALID {
				errs = appendErrors(errs, fmt.Errorf("Envoy filter: missing applyTo")) // nolint: golint,stylecheck
				continue
			}
			if cp.Patch == nil {
				errs = appendErrors(errs, fmt.Errorf("Envoy filter: missing patch")) // nolint: golint,stylecheck
				continue
			}
			if cp.Patch.Operation == networking.EnvoyFilter_Patch_INVALID {
				errs = appendErrors(errs, fmt.Errorf("Envoy filter: missing patch operation")) // nolint: golint,stylecheck
				continue
			}
			if cp.Patch.Operation != networking.EnvoyFilter_Patch_REMOVE && cp.Patch.Value == nil {
				errs = appendErrors(errs, fmt.Errorf("Envoy filter: missing patch value for non-remove operation")) // nolint: golint,stylecheck
				continue
			}

			// ensure that the supplied regex for proxy version compiles
			if cp.Match != nil && cp.Match.Proxy != nil && cp.Match.Proxy.ProxyVersion != "" {
				if _, err := regexp.Compile(cp.Match.Proxy.ProxyVersion); err != nil {
					errs = appendErrors(errs, fmt.Errorf("Envoy filter: invalid regex for proxy version, [%v]", err)) // nolint: golint,stylecheck
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
						errs = appendErrors(errs, fmt.Errorf("Envoy filter: applyTo for listener class objects cannot have non listener match")) // nolint: golint,stylecheck
						continue
					}
					listenerMatch := cp.Match.GetListener()
					if listenerMatch.FilterChain != nil {
						if listenerMatch.FilterChain.Filter != nil {
							// filter names are required if network filter matches are being made
							if listenerMatch.FilterChain.Filter.Name == "" {
								errs = appendErrors(errs, fmt.Errorf("Envoy filter: filter match has no name to match on")) // nolint: golint,stylecheck
								continue
							} else if listenerMatch.FilterChain.Filter.SubFilter != nil {
								// sub filter match is supported only for applyTo HTTP_FILTER
								if cp.ApplyTo != networking.EnvoyFilter_HTTP_FILTER {
									errs = appendErrors(errs, fmt.Errorf("Envoy filter: subfilter match can be used with applyTo HTTP_FILTER only")) // nolint: golint,stylecheck
									continue
								}
								// sub filter match requires the network filter to match to envoy http connection manager
								if listenerMatch.FilterChain.Filter.Name != wellknown.HTTPConnectionManager &&
									listenerMatch.FilterChain.Filter.Name != "envoy.http_connection_manager" {
									errs = appendErrors(errs, fmt.Errorf("Envoy filter: subfilter match requires filter match with %s", // nolint: golint,stylecheck
										wellknown.HTTPConnectionManager))
									continue
								}
								if listenerMatch.FilterChain.Filter.SubFilter.Name == "" {
									errs = appendErrors(errs, fmt.Errorf("Envoy filter: subfilter match has no name to match on")) // nolint: golint,stylecheck
									continue
								}
							}
						}
					}
				}
			case networking.EnvoyFilter_ROUTE_CONFIGURATION, networking.EnvoyFilter_VIRTUAL_HOST, networking.EnvoyFilter_HTTP_ROUTE:
				if cp.Match != nil && cp.Match.ObjectTypes != nil {
					if cp.Match.GetRouteConfiguration() == nil {
						errs = appendErrors(errs,
							fmt.Errorf("Envoy filter: applyTo for http route class objects cannot have non route configuration match")) // nolint: golint,stylecheck
					}
				}

			case networking.EnvoyFilter_CLUSTER:
				if cp.Match != nil && cp.Match.ObjectTypes != nil {
					if cp.Match.GetCluster() == nil {
						errs = appendErrors(errs, fmt.Errorf("Envoy filter: applyTo for cluster class objects cannot have non cluster match")) // nolint: golint,stylecheck
					}
				}
			}
			// ensure that the struct is valid
			if _, err := xds.BuildXDSObjectFromStruct(cp.ApplyTo, cp.Patch.Value); err != nil {
				errs = appendErrors(errs, err)
			}
		}

		return
	})

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
	func(_, _ string, msg proto.Message) (errs error) {
		rule, ok := msg.(*networking.Sidecar)
		if !ok {
			return fmt.Errorf("cannot cast to Sidecar")
		}

		if err := validateAlphaWorkloadSelector(rule.WorkloadSelector); err != nil {
			return err
		}

		if len(rule.Egress) == 0 {
			return fmt.Errorf("sidecar: missing egress")
		}

		portMap := make(map[uint32]struct{})
		for _, i := range rule.Ingress {
			if i.Port == nil {
				errs = appendErrors(errs, fmt.Errorf("sidecar: port is required for ingress listeners"))
				continue
			}

			bind := i.GetBind()
			errs = appendErrors(errs, validateSidecarIngressPortAndBind(i.Port, bind))

			if _, found := portMap[i.Port.Number]; found {
				errs = appendErrors(errs, fmt.Errorf("sidecar: ports on IP bound listeners must be unique"))
			}
			portMap[i.Port.Number] = struct{}{}

			if len(i.DefaultEndpoint) == 0 {
				errs = appendErrors(errs, fmt.Errorf("sidecar: default endpoint must be set for all ingress listeners"))
			} else {
				if strings.HasPrefix(i.DefaultEndpoint, UnixAddressPrefix) {
					errs = appendErrors(errs, ValidateUnixAddress(strings.TrimPrefix(i.DefaultEndpoint, UnixAddressPrefix)))
				} else {
					// format should be 127.0.0.1:port or :port
					parts := strings.Split(i.DefaultEndpoint, ":")
					if len(parts) < 2 {
						errs = appendErrors(errs, fmt.Errorf("sidecar: defaultEndpoint must be of form 127.0.0.1:<port>"))
					} else {
						if len(parts[0]) > 0 && parts[0] != "127.0.0.1" {
							errs = appendErrors(errs, fmt.Errorf("sidecar: defaultEndpoint must be of form 127.0.0.1:<port>"))
						}

						port, err := strconv.Atoi(parts[1])
						if err != nil {
							errs = appendErrors(errs, fmt.Errorf("sidecar: defaultEndpoint port (%s) is not a number: %v", parts[1], err))
						} else {
							errs = appendErrors(errs, ValidatePort(port))
						}
					}
				}
			}
		}

		portMap = make(map[uint32]struct{})
		udsMap := make(map[string]struct{})
		catchAllEgressListenerFound := false
		for index, i := range rule.Egress {
			// there can be only one catch all egress listener with empty port, and it should be the last listener.
			if i.Port == nil {
				if !catchAllEgressListenerFound {
					if index == len(rule.Egress)-1 {
						catchAllEgressListenerFound = true
					} else {
						errs = appendErrors(errs, fmt.Errorf("sidecar: the egress listener with empty port should be the last listener in the list"))
					}
				} else {
					errs = appendErrors(errs, fmt.Errorf("sidecar: egress can have only one listener with empty port"))
					continue
				}
			} else {
				bind := i.GetBind()
				captureMode := i.GetCaptureMode()
				errs = appendErrors(errs, validateSidecarEgressPortBindAndCaptureMode(i.Port, bind, captureMode))

				if i.Port.Number == 0 {
					if _, found := udsMap[bind]; found {
						errs = appendErrors(errs, fmt.Errorf("sidecar: unix domain socket values for listeners must be unique"))
					}
					udsMap[bind] = struct{}{}
				} else {
					if _, found := portMap[i.Port.Number]; found {
						errs = appendErrors(errs, fmt.Errorf("sidecar: ports on IP bound listeners must be unique"))
					}
					portMap[i.Port.Number] = struct{}{}
				}
			}

			// validate that the hosts field is a slash separated value
			// of form ns1/host, or */host, or */*, or ns1/*, or ns1/*.example.com
			if len(i.Hosts) == 0 {
				errs = appendErrors(errs, fmt.Errorf("sidecar: egress listener must contain at least one host"))
			} else {
				for _, hostname := range i.Hosts {
					errs = appendErrors(errs, validateNamespaceSlashWildcardHostname(hostname, false))
				}
			}

		}

		errs = appendErrors(errs, validateSidecarOutboundTrafficPolicy(rule.OutboundTrafficPolicy))

		return
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
	if outlier.ConsecutiveErrors != 0 {
		scope.Warnf("outlier detection consecutive errors is deprecated, use consecutiveGatewayErrors or consecutive5xxErrors instead")
	}
	if outlier.Interval != nil {
		errs = appendErrors(errs, ValidateDurationGogo(outlier.Interval))
	}
	errs = appendErrors(errs, ValidatePercent(outlier.MaxEjectionPercent), ValidatePercent(outlier.MinHealthPercent))

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
			errs = appendErrors(errs, ValidateDurationGogo(httpSettings.IdleTimeout))
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
func ValidateDuration(pd *types.Duration) error {
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

// ValidateGogoDuration validates the variant of duration.
func ValidateGogoDuration(in *types.Duration) error {
	return ValidateDuration(&types.Duration{
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
func ValidateParentAndDrain(drainTime, parentShutdown *types.Duration) (errs error) {
	if err := ValidateDuration(drainTime); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid drain duration:"))
	}
	if err := ValidateDuration(parentShutdown); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid parent shutdown duration:"))
	}
	if errs != nil {
		return
	}

	drainDuration, _ := types.DurationFromProto(drainTime)
	parentShutdownDuration, _ := types.DurationFromProto(parentShutdown)

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

// ValidateZipkinCollector validates the configuration for sending envoy spans to Zipkin
func ValidateZipkinCollector(z *meshconfig.Tracing_Zipkin) error {
	return ValidateProxyAddress(z.GetAddress())
}

// ValidateDatadogCollector validates the configuration for sending envoy spans to Datadog
func ValidateDatadogCollector(d *meshconfig.Tracing_Datadog) error {
	// If the address contains $(HOST_IP), replace it with a valid IP before validation.
	return ValidateProxyAddress(strings.Replace(d.GetAddress(), "$(HOST_IP)", "127.0.0.1", 1))
}

// ValidateConnectTimeout validates the envoy conncection timeout
func ValidateConnectTimeout(timeout *types.Duration) error {
	if err := ValidateDuration(timeout); err != nil {
		return err
	}

	timeoutDuration, _ := types.DurationFromProto(timeout)
	err := ValidateDurationRange(timeoutDuration, connectTimeoutMin, connectTimeoutMax)
	return err
}

// ValidateProtocolDetectionTimeout validates the envoy protocol detection timeout
func ValidateProtocolDetectionTimeout(timeout *types.Duration) error {
	dur, err := types.DurationFromProto(timeout)
	if err != nil {
		return err
	}
	// 0s is a valid value if trying to disable protocol detection timeout
	if dur == time.Second*0 {
		return nil
	}
	if dur%time.Millisecond != 0 {
		return errors.New("only durations to ms precision are supported")
	}

	return nil
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

	if err := ValidateProtocolDetectionTimeout(mesh.ProtocolDetectionTimeout); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid protocol detection timeout:"))
	}

	if mesh.DefaultConfig == nil {
		errs = multierror.Append(errs, errors.New("missing default config"))
	} else if err := ValidateProxyConfig(mesh.DefaultConfig); err != nil {
		errs = multierror.Append(errs, err)
	}

	if err := validateLocalityLbSetting(mesh.LocalityLbSetting); err != nil {
		errs = multierror.Append(errs, err)
	}

	if err := validateServiceSettings(mesh); err != nil {
		errs = multierror.Append(errs, err)
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

	if config.StatsdUdpAddress != "" {
		if err := ValidateProxyAddress(config.StatsdUdpAddress); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("invalid statsd udp address %q:", config.StatsdUdpAddress)))
		}
	}

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
		return errors.New("cannot cast to attributes")
	}
	if in == nil || len(in.Attributes) == 0 {
		return errors.New("list of attributes is nil/empty")
	}
	var errs error
	for k, v := range in.Attributes {
		if v == nil {
			errs = multierror.Append(errs, errors.New("an attribute cannot be empty"))
			continue
		}
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
var ValidateHTTPAPISpec = registerValidateFunc("ValidateHTTPAPISpec",
	func(_, _ string, msg proto.Message) error {
		in, ok := msg.(*mccpb.HTTPAPISpec)
		if !ok {
			return errors.New("cannot cast to HTTPAPISpec")
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
	})

// ValidateHTTPAPISpecBinding checks that HTTPAPISpecBinding is well-formed.
var ValidateHTTPAPISpecBinding = registerValidateFunc("ValidateHTTPAPISpecBinding",
	func(_, _ string, msg proto.Message) error {
		in, ok := msg.(*mccpb.HTTPAPISpecBinding)
		if !ok {
			return errors.New("cannot cast to HTTPAPISpecBinding")
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
			if spec.Namespace != "" && !labels.IsDNS1123Label(spec.Namespace) {
				errs = multierror.Append(errs, fmt.Errorf("namespace %q must be a valid label", spec.Namespace))
			}
		}
		return errs
	})

// ValidateQuotaSpec checks that Quota is well-formed.
var ValidateQuotaSpec = registerValidateFunc("ValidateQuotaSpec",
	func(_, _ string, msg proto.Message) error {
		in, ok := msg.(*mccpb.QuotaSpec)
		if !ok {
			return errors.New("cannot cast to QuotaSpec")
		}
		var errs error
		if len(in.Rules) == 0 {
			errs = multierror.Append(errs, errors.New("a least one rule must be specified"))
		}
		for _, rule := range in.Rules {
			for _, match := range rule.Match {
				for name, clause := range match.Clause {
					if clause == nil {
						errs = multierror.Append(errs, errors.New("a clause cannot be empty"))
						continue
					}
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
	})

// ValidateQuotaSpecBinding checks that QuotaSpecBinding is well-formed.
var ValidateQuotaSpecBinding = registerValidateFunc("ValidateQuotaSpecBinding",
	func(_, _ string, msg proto.Message) error {
		in, ok := msg.(*mccpb.QuotaSpecBinding)
		if !ok {
			return errors.New("cannot cast to QuotaSpecBinding")
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
			if spec.Namespace != "" && !labels.IsDNS1123Label(spec.Namespace) {
				errs = multierror.Append(errs, fmt.Errorf("namespace %q must be a valid label", spec.Namespace))
			}
		}
		return errs
	})

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
	func(name, namespace string, msg proto.Message) error {
		in, ok := msg.(*security_beta.AuthorizationPolicy)
		if !ok {
			return fmt.Errorf("cannot cast to AuthorizationPolicy")
		}

		if err := validateWorkloadSelector(in.Selector); err != nil {
			return err
		}

		if in.Action == security_beta.AuthorizationPolicy_DENY && in.Rules == nil {
			return fmt.Errorf("a deny policy without `rules` is meaningless and has no effect, found in %s.%s", name, namespace)
		}

		var errs error
		for i, rule := range in.GetRules() {
			if rule.From != nil && len(rule.From) == 0 {
				errs = appendErrors(errs, fmt.Errorf("`from` must not be empty, found at rule %d in %s.%s", i, name, namespace))
			}
			for _, from := range rule.From {
				if from.Source == nil {
					errs = appendErrors(errs, fmt.Errorf("`from.source` must not be nil, found at rule %d in %s.%s", i, name, namespace))
				} else {
					src := from.Source
					if len(src.Principals) == 0 && len(src.RequestPrincipals) == 0 && len(src.Namespaces) == 0 && len(src.IpBlocks) == 0 &&
						len(src.NotPrincipals) == 0 && len(src.NotRequestPrincipals) == 0 && len(src.NotNamespaces) == 0 && len(src.NotIpBlocks) == 0 {
						errs = appendErrors(errs, fmt.Errorf("`from.source` must not be empty, found at rule %d in %s.%s", i, name, namespace))
					}
					errs = appendErrors(errs, security.ValidateIPs(from.Source.GetIpBlocks()))
					errs = appendErrors(errs, security.ValidateIPs(from.Source.GetNotIpBlocks()))
					errs = appendErrors(errs, security.CheckEmptyValues("Principals", src.Principals))
					errs = appendErrors(errs, security.CheckEmptyValues("RequestPrincipals", src.RequestPrincipals))
					errs = appendErrors(errs, security.CheckEmptyValues("Namespaces", src.Namespaces))
					errs = appendErrors(errs, security.CheckEmptyValues("IpBlocks", src.IpBlocks))
					errs = appendErrors(errs, security.CheckEmptyValues("NotPrincipals", src.NotPrincipals))
					errs = appendErrors(errs, security.CheckEmptyValues("NotRequestPrincipals", src.NotRequestPrincipals))
					errs = appendErrors(errs, security.CheckEmptyValues("NotNamespaces", src.NotNamespaces))
					errs = appendErrors(errs, security.CheckEmptyValues("NotIpBlocks", src.NotIpBlocks))
				}
			}
			if rule.To != nil && len(rule.To) == 0 {
				errs = appendErrors(errs, fmt.Errorf("`to` must not be empty, found at rule %d in %s.%s", i, name, namespace))
			}
			for _, to := range rule.To {
				if to.Operation == nil {
					errs = appendErrors(errs, fmt.Errorf("`to.operation` must not be nil, found at rule %d in %s.%s", i, name, namespace))
				} else {
					op := to.Operation
					if len(op.Ports) == 0 && len(op.Methods) == 0 && len(op.Paths) == 0 && len(op.Hosts) == 0 &&
						len(op.NotPorts) == 0 && len(op.NotMethods) == 0 && len(op.NotPaths) == 0 && len(op.NotHosts) == 0 {
						errs = appendErrors(errs, fmt.Errorf("`to.operation` must not be empty, found at rule %d in %s.%s", i, name, namespace))
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
					errs = appendErrors(errs, fmt.Errorf("`key` must not be empty, found in %s.%s", name, namespace))
				} else {
					if len(condition.GetValues()) == 0 && len(condition.GetNotValues()) == 0 {
						errs = appendErrors(errs, fmt.Errorf("at least one of `values` or `notValues` must be set for key %s, found in %s.%s",
							key, name, namespace))
					} else {
						if err := security.ValidateAttribute(key, condition.GetValues()); err != nil {
							errs = appendErrors(errs, fmt.Errorf("invalid `value` for `key` %s: %v, found in %s.%s", key, err, name, namespace))
						}
						if err := security.ValidateAttribute(key, condition.GetNotValues()); err != nil {
							errs = appendErrors(errs, fmt.Errorf("invalid `notValue` for `key` %s: %v, found in %s.%s", key, err, name, namespace))
						}
					}
				}
			}
		}
		return errs
	})

// ValidateRequestAuthentication checks that request authentication spec is well-formed.
var ValidateRequestAuthentication = registerValidateFunc("ValidateRequestAuthentication",
	func(name, namespace string, msg proto.Message) error {
		in, ok := msg.(*security_beta.RequestAuthentication)
		if !ok {
			return errors.New("cannot cast to RequestAuthentication")
		}

		var errs error
		errs = appendErrors(errs, validateWorkloadSelector(in.Selector))

		for _, rule := range in.JwtRules {
			errs = appendErrors(errs, validateJwtRule(rule))
		}
		return errs
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

	for _, location := range rule.FromHeaders {
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
	func(name, namespace string, msg proto.Message) error {
		in, ok := msg.(*security_beta.PeerAuthentication)
		if !ok {
			return errors.New("cannot cast to PeerAuthentication")
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

		return errs
	})

// ValidateVirtualService checks that a v1alpha3 route rule is well-formed.
var ValidateVirtualService = registerValidateFunc("ValidateVirtualService",
	func(_, namespace string, msg proto.Message) (errs error) {
		virtualService, ok := msg.(*networking.VirtualService)
		if !ok {
			return errors.New("cannot cast to virtual service")
		}

		isDelegate := false
		if len(virtualService.Hosts) == 0 {
			if features.EnableVirtualServiceDelegate {
				isDelegate = true
			} else {
				errs = appendErrors(errs, fmt.Errorf("virtual service must have at least one host"))
			}
		}

		if isDelegate {
			if len(virtualService.Gateways) != 0 {
				// meaningless to specify gateways in delegate
				errs = appendErrors(errs, fmt.Errorf("delegate virtual service must have no gateways specified"))
			}
			if len(virtualService.Tls) != 0 {
				// meaningless to specify tls in delegate, we donot support tls delegate
				errs = appendErrors(errs, fmt.Errorf("delegate virtual service must have no tls route specified"))
			}
			if len(virtualService.Tcp) != 0 {
				// meaningless to specify tls in delegate, we donot support tcp delegate
				errs = appendErrors(errs, fmt.Errorf("delegate virtual service must have no tcp route specified"))
			}
		}

		appliesToMesh := false
		appliesToGateway := false
		if len(virtualService.Gateways) == 0 {
			appliesToMesh = true
		}

		errs = appendErrors(errs, validateGatewayNames(virtualService.Gateways))
		for _, gatewayName := range virtualService.Gateways {
			if gatewayName == constants.IstioMeshGateway {
				appliesToMesh = true
			} else {
				appliesToGateway = true
			}
		}

		allHostsValid := true
		for _, virtualHost := range virtualService.Hosts {
			if err := ValidateWildcardDomain(virtualHost); err != nil {
				ipAddr := net.ParseIP(virtualHost) // Could also be an IP
				if ipAddr == nil {
					errs = appendErrors(errs, err)
					allHostsValid = false
				}
			} else if appliesToMesh && virtualHost == "*" {
				errs = appendErrors(errs, fmt.Errorf("wildcard host * is not allowed for virtual services bound to the mesh gateway"))
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
						errs = appendErrors(errs, fmt.Errorf("duplicate hosts in virtual service: %s & %s", hostI, hostJ))
					}
				}
			}
		}

		if len(virtualService.Http) == 0 && len(virtualService.Tcp) == 0 && len(virtualService.Tls) == 0 {
			errs = appendErrors(errs, errors.New("http, tcp or tls must be provided in virtual service"))
		}
		for _, httpRoute := range virtualService.Http {
			if !appliesToGateway && httpRoute.Delegate != nil {
				errs = appendErrors(errs, errors.New("http delegate only applies to gateway"))
			}
			errs = appendErrors(errs, validateHTTPRoute(httpRoute, isDelegate))
		}
		for _, tlsRoute := range virtualService.Tls {
			errs = appendErrors(errs, validateTLSRoute(tlsRoute, virtualService))
		}
		for _, tcpRoute := range virtualService.Tcp {
			errs = appendErrors(errs, validateTCPRoute(tcpRoute))
		}

		errs = appendErrors(errs, validateExportTo(namespace, virtualService.ExportTo, false))
		return
	})

func validateTLSRoute(tls *networking.TLSRoute, context *networking.VirtualService) (errs error) {
	if tls == nil {
		return nil
	}
	if len(tls.Match) == 0 {
		errs = appendErrors(errs, errors.New("TLS route must have at least one match condition"))
	}
	for _, match := range tls.Match {
		errs = appendErrors(errs, validateTLSMatch(match, context))
	}
	if len(tls.Route) == 0 {
		errs = appendErrors(errs, errors.New("TLS route is required"))
	}
	errs = appendErrors(errs, validateRouteDestinations(tls.Route))
	return
}

func validateTLSMatch(match *networking.TLSMatchAttributes, context *networking.VirtualService) (errs error) {
	if len(match.SniHosts) == 0 {
		errs = appendErrors(errs, fmt.Errorf("TLS match must have at least one SNI host"))
	} else {
		for _, sniHost := range match.SniHosts {
			err := validateSniHost(sniHost, context)
			if err != nil {
				errs = appendErrors(errs, err)
			}
		}
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

func validateSniHost(sniHost string, context *networking.VirtualService) error {
	if err := ValidateWildcardDomain(sniHost); err != nil {
		ipAddr := net.ParseIP(sniHost) // Could also be an IP
		if ipAddr == nil {
			return err
		}
	}
	sniHostname := host.Name(sniHost)
	for _, hostname := range context.Hosts {
		if sniHostname.SubsetOf(host.Name(hostname)) {
			return nil
		}
	}
	return fmt.Errorf("SNI host %q is not a compatible subset of any of the virtual service hosts: [%s]",
		sniHost, strings.Join(context.Hosts, ", "))
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

func validateHTTPRoute(http *networking.HTTPRoute, delegate bool) (errs error) {
	if features.EnableVirtualServiceDelegate {
		if delegate {
			return validateDelegateHTTPRoute(http)
		}
		if http.Delegate != nil {
			return validateRootHTTPRoute(http)
		}
	}

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
	} else if len(http.Route) == 0 {
		errs = appendErrors(errs, errors.New("HTTP route or redirect is required"))
	}

	// header manipulation
	for name := range http.Headers.GetRequest().GetAdd() {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}
	for name := range http.Headers.GetRequest().GetSet() {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}
	for _, name := range http.Headers.GetRequest().GetRemove() {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}
	for name := range http.Headers.GetResponse().GetAdd() {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}
	for name := range http.Headers.GetResponse().GetSet() {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}
	for _, name := range http.Headers.GetResponse().GetRemove() {
		errs = appendErrors(errs, ValidateHTTPHeaderName(name))
	}

	errs = appendErrors(errs, validateCORSPolicy(http.CorsPolicy))
	errs = appendErrors(errs, validateHTTPFaultInjection(http.Fault))

	for _, match := range http.Match {
		if match != nil {
			for name, header := range match.Headers {
				if header == nil {
					errs = appendErrors(errs, fmt.Errorf("header match %v cannot be null", name))
				}
				errs = appendErrors(errs, ValidateHTTPHeaderName(name))
				errs = appendErrors(errs, validateStringMatchRegexp(header, "headers"))
			}

			if match.Port != 0 {
				errs = appendErrors(errs, ValidatePort(int(match.Port)))
			}
			errs = appendErrors(errs, labels.Instance(match.SourceLabels).Validate())
			errs = appendErrors(errs, validateGatewayNames(match.Gateways))
			errs = appendErrors(errs, validateStringMatchRegexp(match.GetUri(), "uri"))
			errs = appendErrors(errs, validateStringMatchRegexp(match.GetScheme(), "scheme"))
			errs = appendErrors(errs, validateStringMatchRegexp(match.GetMethod(), "method"))
			errs = appendErrors(errs, validateStringMatchRegexp(match.GetAuthority(), "authority"))
			for _, qp := range match.GetQueryParams() {
				errs = appendErrors(errs, validateStringMatchRegexp(qp, "queryParams"))
			}
		}
	}

	if http.MirrorPercent != nil {
		if value := http.MirrorPercent.GetValue(); value > 100 {
			errs = appendErrors(errs, fmt.Errorf("mirror_percent must have a max value of 100 (it has %d)", value))
		}
	}

	if http.MirrorPercentage != nil {
		if value := http.MirrorPercentage.GetValue(); value > 100 {
			errs = appendErrors(errs, fmt.Errorf("mirror_percentage must have a max value of 100 (it has %f)", value))
		}
	}

	errs = appendErrors(errs, validateDestination(http.Mirror))
	errs = appendErrors(errs, validateHTTPRedirect(http.Redirect))
	errs = appendErrors(errs, validateHTTPRetry(http.Retries))
	errs = appendErrors(errs, validateHTTPRewrite(http.Rewrite))
	errs = appendErrors(errs, validateHTTPRouteDestinations(http.Route))
	if http.Timeout != nil {
		errs = appendErrors(errs, ValidateDurationGogo(http.Timeout))
	}

	return
}

func validateStringMatchRegexp(sm *networking.StringMatch, where string) error {
	re := sm.GetRegex()
	if re == "" {
		return nil
	}

	_, err := regexp.Compile(re)
	if err == nil {
		return nil
	}

	return fmt.Errorf("%q: %w; Istio uses RE2 style regex-based match (https://github.com/google/re2/wiki/Syntax)", where, err)
}

func validateGatewayNames(gatewayNames []string) (errs error) {
	for _, gatewayName := range gatewayNames {
		parts := strings.SplitN(gatewayName, "/", 2)
		if len(parts) != 2 {
			// deprecated
			// Old style spec with FQDN gateway name
			errs = appendErrors(errs, ValidateFQDN(gatewayName))
			return
		}

		if len(parts[0]) == 0 || len(parts[1]) == 0 {
			errs = appendErrors(errs, fmt.Errorf("config namespace and gateway name cannot be empty"))
		}

		// namespace and name must be DNS labels
		if !labels.IsDNS1123Label(parts[0]) {
			errs = appendErrors(errs, fmt.Errorf("invalid value for namespace: %q", parts[0]))
		}

		if !labels.IsDNS1123Label(parts[1]) {
			errs = appendErrors(errs, fmt.Errorf("invalid value for gateway name: %q", parts[1]))
		}
	}
	return
}

func validateHTTPRouteDestinations(weights []*networking.HTTPRouteDestination) (errs error) {
	var totalWeight int32
	for _, weight := range weights {
		if weight.Destination == nil {
			errs = multierror.Append(errs, errors.New("destination is required"))
		}

		// header manipulations
		for name := range weight.Headers.GetRequest().GetAdd() {
			errs = appendErrors(errs, ValidateHTTPHeaderName(name))
		}
		for name := range weight.Headers.GetRequest().GetSet() {
			errs = appendErrors(errs, ValidateHTTPHeaderName(name))
		}
		for _, name := range weight.Headers.GetRequest().GetRemove() {
			errs = appendErrors(errs, ValidateHTTPHeaderName(name))
		}
		for name := range weight.Headers.GetResponse().GetAdd() {
			errs = appendErrors(errs, ValidateHTTPHeaderName(name))
		}
		for name := range weight.Headers.GetResponse().GetSet() {
			errs = appendErrors(errs, ValidateHTTPHeaderName(name))
		}
		for _, name := range weight.Headers.GetResponse().GetRemove() {
			errs = appendErrors(errs, ValidateHTTPHeaderName(name))
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
		errs = appendErrors(errs, ValidateDurationGogo(policy.MaxAge))
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
		errs = appendErrors(errs, ValidateDurationGogo(retries.PerTryTimeout))
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
	if redirect != nil && redirect.Uri == "" && redirect.Authority == "" {
		return errors.New("redirect must specify URI, authority, or both")
	}

	if redirect != nil && redirect.RedirectCode != 0 {
		if redirect.RedirectCode < 300 || redirect.RedirectCode > 399 {
			return fmt.Errorf("%d is not a valid redirect code, must be 3xx", redirect.RedirectCode)
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
	func(_, _ string, config proto.Message) (errs error) {
		we, ok := config.(*networking.WorkloadEntry)
		if !ok {
			return fmt.Errorf("cannot cast to workload entry")
		}
		if we.Address == "" {
			return fmt.Errorf("address must be set")
		}
		// TODO: add better validation. The tricky thing is that we don't know if its meant to be
		// DNS or STATIC type without association with a ServiceEntry
		return nil
	})

// ValidateServiceEntry validates a service entry.
var ValidateServiceEntry = registerValidateFunc("ValidateServiceEntry",
	func(_, namespace string, config proto.Message) (errs error) {
		serviceEntry, ok := config.(*networking.ServiceEntry)
		if !ok {
			return fmt.Errorf("cannot cast to service entry")
		}

		if err := validateAlphaWorkloadSelector(serviceEntry.WorkloadSelector); err != nil {
			return err
		}

		if serviceEntry.WorkloadSelector != nil && serviceEntry.Endpoints != nil {
			errs = appendErrors(errs, fmt.Errorf("only one of WorkloadSelector or Endpoints is allowed in Service Entry"))
		}

		if len(serviceEntry.Hosts) == 0 {
			errs = appendErrors(errs, fmt.Errorf("service entry must have at least one host"))
		}
		for _, hostname := range serviceEntry.Hosts {
			// Full wildcard is not allowed in the service entry.
			if hostname == "*" {
				errs = appendErrors(errs, fmt.Errorf("invalid host %s", hostname))
			} else {
				errs = appendErrors(errs, ValidateWildcardDomain(hostname))
			}
		}

		cidrFound := false
		for _, address := range serviceEntry.Addresses {
			cidrFound = cidrFound || strings.Contains(address, "/")
			errs = appendErrors(errs, ValidateIPSubnet(address))
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
				errs = appendErrors(errs, fmt.Errorf("no endpoints should be provided for resolution type none"))
			}
		case networking.ServiceEntry_STATIC:
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
					errs = appendErrors(errs, ValidateIPAddress(addr))

					for name, port := range endpoint.Ports {
						if !servicePorts[name] {
							errs = appendErrors(errs, fmt.Errorf("endpoint port %v is not defined by the service entry", port))
						}
					}
				}
				errs = appendErrors(errs, labels.Instance(endpoint.Labels).Validate())

			}
			if unixEndpoint && len(serviceEntry.Ports) != 1 {
				errs = appendErrors(errs, errors.New("exactly 1 service port required for unix endpoints"))
			}
		case networking.ServiceEntry_DNS:
			if len(serviceEntry.Endpoints) == 0 {
				for _, hostname := range serviceEntry.Hosts {
					if err := ValidateFQDN(hostname); err != nil {
						errs = appendErrors(errs,
							fmt.Errorf("hosts must be FQDN if no endpoints are provided for resolution mode DNS"))
					}
				}
			}

			for _, endpoint := range serviceEntry.Endpoints {
				ipAddr := net.ParseIP(endpoint.Address) // Typically it is an IP address
				if ipAddr == nil {
					if err := ValidateFQDN(endpoint.Address); err != nil { // Otherwise could be an FQDN
						errs = appendErrors(errs,
							fmt.Errorf("endpoint address %q is not a valid FQDN or an IP address", endpoint.Address))
					}
				}
				errs = appendErrors(errs,
					labels.Instance(endpoint.Labels).Validate())
				for name, port := range endpoint.Ports {
					if !servicePorts[name] {
						errs = appendErrors(errs, fmt.Errorf("endpoint port %v is not defined by the service entry", port))
					}
					errs = appendErrors(errs,
						ValidatePortName(name),
						ValidatePort(int(port)))
				}
			}
		default:
			errs = appendErrors(errs, fmt.Errorf("unsupported resolution type %s",
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
			canDifferentiate := true
			for _, port := range serviceEntry.Ports {
				p := protocol.Parse(port.Protocol)
				if !p.IsHTTP() && !p.IsTLS() {
					canDifferentiate = false
					break
				}
			}

			if !canDifferentiate {
				errs = appendErrors(errs, fmt.Errorf("multiple hosts provided with non-HTTP, non-TLS ports"))
			}
		}

		for _, port := range serviceEntry.Ports {
			errs = appendErrors(errs,
				ValidatePortName(port.Name),
				ValidateProtocol(port.Protocol),
				ValidatePort(int(port.Number)))
		}

		errs = appendErrors(errs, validateExportTo(namespace, serviceEntry.ExportTo, true))
		return
	})

func ValidatePortName(name string) error {
	if !labels.IsDNS1123Label(name) {
		return fmt.Errorf("invalid port name: %s", name)
	}
	return nil
}

func ValidateProtocol(protocolStr string) error {
	// Empty string is used for protocol sniffing.
	if protocolStr != "" && protocol.Parse(protocolStr) == protocol.Unsupported {
		return fmt.Errorf("unsupported protocol: %s", protocolStr)
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

// validateLocalityLbSetting checks the LocalityLbSetting of MeshConfig
func validateLocalityLbSetting(lb *networking.LocalityLoadBalancerSetting) error {
	if lb == nil {
		return nil
	}

	if len(lb.GetDistribute()) > 0 && len(lb.GetFailover()) > 0 {
		return fmt.Errorf("can not simultaneously specify 'distribute' and 'failover'")
	}

	srcLocalities := make([]string, 0)
	for _, locality := range lb.GetDistribute() {
		srcLocalities = append(srcLocalities, locality.From)
		var totalWeight uint32
		destLocalities := make([]string, 0)
		for loc, weight := range locality.To {
			destLocalities = append(destLocalities, loc)
			if weight == 0 {
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
		if strings.Contains(failover.To, "*") {
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

		items := strings.SplitN(locality, "/", 3)
		for _, item := range items {
			if item == "" {
				return fmt.Errorf("locality %s must not contain empty region/zone/subzone info", locality)
			}
		}
		if _, ok := regionZoneSubZoneMap["*"]; ok {
			return fmt.Errorf("locality %s overlap with previous specified ones", locality)
		}
		switch len(items) {
		case 1:
			if _, ok := regionZoneSubZoneMap[items[0]]; ok {
				return fmt.Errorf("locality %s overlap with previous specified ones", locality)
			}
			regionZoneSubZoneMap[items[0]] = map[string]map[string]bool{"*": {"*": true}}
		case 2:
			if _, ok := regionZoneSubZoneMap[items[0]]; ok {
				if _, ok := regionZoneSubZoneMap[items[0]]["*"]; ok {
					return fmt.Errorf("locality %s overlap with previous specified ones", locality)
				}
				if _, ok := regionZoneSubZoneMap[items[0]][items[1]]; ok {
					return fmt.Errorf("locality %s overlap with previous specified ones", locality)
				}
				regionZoneSubZoneMap[items[0]][items[1]] = map[string]bool{"*": true}
			} else {
				regionZoneSubZoneMap[items[0]] = map[string]map[string]bool{items[1]: {"*": true}}
			}
		case 3:
			if _, ok := regionZoneSubZoneMap[items[0]]; ok {
				if _, ok := regionZoneSubZoneMap[items[0]]["*"]; ok {
					return fmt.Errorf("locality %s overlap with previous specified ones", locality)
				}
				if _, ok := regionZoneSubZoneMap[items[0]][items[1]]; ok {
					if regionZoneSubZoneMap[items[0]][items[1]]["*"] {
						return fmt.Errorf("locality %s overlap with previous specified ones", locality)
					}
					if regionZoneSubZoneMap[items[0]][items[1]][items[2]] {
						return fmt.Errorf("locality %s overlap with previous specified ones", locality)
					}
					regionZoneSubZoneMap[items[0]][items[1]][items[2]] = true
				} else {
					regionZoneSubZoneMap[items[0]][items[1]] = map[string]bool{items[2]: true}
				}
			} else {
				regionZoneSubZoneMap[items[0]] = map[string]map[string]bool{items[1]: {items[2]: true}}
			}
		}
	}

	return nil
}

// ValidateMeshNetworks validates meshnetworks.
func ValidateMeshNetworks(meshnetworks *meshconfig.MeshNetworks) (errs error) {
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
			if err := ValidateIPAddress(g.Address); err != nil {
				errs = multierror.Append(errs, err)
			}
		}
		if err := ValidatePort(int(n.Port)); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	return
}
