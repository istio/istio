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

package agent

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"google.golang.org/protobuf/types/known/durationpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/security"
	"istio.io/istio/pkg/log"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/sets"
)

// Constants for duration fields
const (
	// Set some high upper bound to avoid weird configurations
	// nolint: revive
	connectTimeoutMax = time.Hour
	// nolint: revive
	connectTimeoutMin      = time.Millisecond
	outboundHostPrefix     = "outbound"
	outboundHostNameFormat = "outbound_.<PORT>_._.<HOSTNAME>"
)

var scope = log.RegisterScope("validation", "CRD validation debugging")

type Warning error

// Validation holds errors and warnings. They can be joined with additional errors by called AppendValidation
type Validation struct {
	Err     error
	Warning Warning
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

// WrapWarning turns an error into a Validation as a warning
func WrapWarning(e error) Validation {
	return Validation{Warning: e}
}

// wrapper around multierror.Append that enforces the invariant that if all input errors are nil, the output
// error is nil (allowing validation without branching).
func AppendValidation(v Validation, vs ...error) Validation {
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

// AppendWarningf appends a formatted warning string
// nolint: unparam
func AppendWarningf(v Validation, format string, a ...any) Validation {
	return AppendValidation(v, Warningf(format, a...))
}

// Warningf formats according to a format specifier and returns the string as a
// value that satisfies error. Like Errorf, but for warnings.
func Warningf(format string, a ...any) Validation {
	return WrapWarning(fmt.Errorf(format, a...))
}

// wrapper around multierror.Append that enforces the invariant that if all input errors are nil, the output
// error is nil (allowing validation without branching).
func AppendErrors(err error, errs ...error) error {
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
func ValidateDurationRange(dur, minimum, maximum time.Duration) error {
	if dur > maximum || dur < minimum {
		return fmt.Errorf("time %v must be >%v and <%v", dur.String(), minimum.String(), maximum.String())
	}

	return nil
}

// ValidateDrainDuration checks that parent and drain durations are valid
func ValidateDrainDuration(drainTime *durationpb.Duration) (errs error) {
	if err := ValidateDuration(drainTime); err != nil {
		errs = multierror.Append(errs, multierror.Prefix(err, "invalid drain duration:"))
	}
	if errs != nil {
		return errs
	}

	drainDuration := drainTime.AsDuration()

	if drainDuration%time.Second != 0 {
		errs = multierror.Append(errs,
			errors.New("drain time only supports durations to seconds precision"))
	}
	return errs
}

// ValidatePort checks that the network port is in range
func ValidatePort(port int) error {
	if 1 <= port && port <= 65535 {
		return nil
	}
	return fmt.Errorf("port number %d must be in the range 1..65535", port)
}

// encapsulates DNS 1123 checks common to both wildcarded hosts and FQDNs
func CheckDNS1123Preconditions(name string) error {
	if len(name) > 255 {
		return fmt.Errorf("domain name %q too long (max 255)", name)
	}
	if len(name) == 0 {
		return fmt.Errorf("empty domain name not allowed")
	}
	return nil
}

func ValidateDNS1123Labels(domain string) error {
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

// validateCustomTags validates that tracing CustomTags map does not contain any nil items
func validateCustomTags(tags map[string]*meshconfig.Tracing_CustomTag) error {
	for tagName, tagVal := range tags {
		if tagVal == nil {
			return fmt.Errorf("encountered nil value for custom tag: %s", tagName)
		}
	}
	return nil
}

// ValidateFQDN checks a fully-qualified domain name
func ValidateFQDN(fqdn string) error {
	if err := CheckDNS1123Preconditions(fqdn); err != nil {
		return err
	}
	return ValidateDNS1123Labels(fqdn)
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
		if !netutil.IsValidIPAddress(hostname) {
			return fmt.Errorf("%q is not a valid hostname or an IP address", hostname)
		}
	}

	return nil
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
	return ValidateProxyAddress(strings.Replace(z.GetAddress(), "$(HOST_IP)", "127.0.0.1", 1))
}

// ValidateDatadogCollector validates the configuration for sending envoy spans to Datadog
func ValidateDatadogCollector(d *meshconfig.Tracing_Datadog) error {
	// If the address contains $(HOST_IP), replace it with a valid IP before validation.
	return ValidateProxyAddress(strings.Replace(d.GetAddress(), "$(HOST_IP)", "127.0.0.1", 1))
}

func ValidateTLS(configNamespace string, settings *networking.ClientTLSSettings) (errs error) {
	if settings == nil {
		return errs
	}

	if settings.CredentialName != "" && strings.HasPrefix(settings.CredentialName, credentials.KubernetesConfigMapTypeURI) {
		rn, err := credentials.ParseResourceName(settings.CredentialName, configNamespace, "", "")
		if err != nil {
			errs = AppendErrors(errs, fmt.Errorf("invalid configmap:// credentialName: %v", err))
		} else if rn.Namespace != configNamespace || configNamespace == "" {
			errs = AppendErrors(errs, fmt.Errorf("invalid configmap:// credentialName: namespace must match the configuration namespace %q", configNamespace))
		}
	}

	if settings.GetInsecureSkipVerify().GetValue() {
		if settings.Mode == networking.ClientTLSSettings_SIMPLE {
			// In tls simple mode, we can specify ca cert by CaCertificates or CredentialName.
			if settings.CaCertificates != "" || settings.CredentialName != "" || settings.SubjectAltNames != nil {
				errs = AppendErrors(errs, fmt.Errorf("cannot specify CaCertificates or CredentialName or SubjectAltNames when InsecureSkipVerify is set true"))
			}
		}

		if settings.Mode == networking.ClientTLSSettings_MUTUAL {
			// In tls mutual mode, we can specify both client cert and ca cert by CredentialName.
			// However, here we can not distinguish whether user specify ca cert by CredentialName or not.
			if settings.CaCertificates != "" || settings.SubjectAltNames != nil {
				errs = AppendErrors(errs, fmt.Errorf("cannot specify CaCertificates or SubjectAltNames when InsecureSkipVerify is set true"))
			}
		}
	}

	if (settings.Mode == networking.ClientTLSSettings_SIMPLE || settings.Mode == networking.ClientTLSSettings_MUTUAL) &&
		settings.CredentialName != "" {
		if settings.ClientCertificate != "" || settings.CaCertificates != "" || settings.PrivateKey != "" || settings.CaCrl != "" {
			errs = AppendErrors(errs,
				fmt.Errorf("cannot specify client certificates or CA certificate or CA CRL If credentialName is set"))
		}

		// If tls mode is SIMPLE or MUTUAL, and CredentialName is specified, credentials are fetched
		// remotely. ServerCertificate and CaCertificates fields are not required.
		return errs
	}

	if settings.Mode == networking.ClientTLSSettings_MUTUAL {
		if settings.ClientCertificate == "" {
			errs = AppendErrors(errs, fmt.Errorf("client certificate required for mutual tls"))
		}
		if settings.PrivateKey == "" {
			errs = AppendErrors(errs, fmt.Errorf("private key required for mutual tls"))
		}
	}

	return errs
}

// ValidateMeshConfigProxyConfig checks that the mesh config is well-formed
func ValidateMeshConfigProxyConfig(config *meshconfig.ProxyConfig) Validation {
	var errs, warnings error
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

	if err := ValidateDrainDuration(config.DrainDuration); err != nil {
		errs = multierror.Append(errs, err)
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
		if err := ValidateTLS("", tracer); err != nil {
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
			scope.Warnf("EnvoyMetricsServiceAddress is deprecated, use EnvoyMetricsService instead.") // nolint: stylecheck
		}
	}

	// use of "ISTIO_META_DNS_AUTO_ALLOCATE" is being deprecated, check and warn
	if _, autoAllocationV1Used := config.GetProxyMetadata()["ISTIO_META_DNS_AUTO_ALLOCATE"]; autoAllocationV1Used {
		warnings = multierror.Append(warnings, errors.New("'ISTIO_META_DNS_AUTO_ALLOCATE' is deprecated; review "+
			"https://istio.io/latest/docs/ops/configuration/traffic-management/dns-proxy/#dns-auto-allocation-v2 for information about replacement functionality"))
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
			errs = multierror.Append(errs, multierror.Prefix(err, "invalid private key provider configuration:"))
		}
	}

	return Validation{errs, warnings}
}

func ValidateControlPlaneAuthPolicy(policy meshconfig.AuthenticationPolicy) error {
	if policy == meshconfig.AuthenticationPolicy_NONE || policy == meshconfig.AuthenticationPolicy_MUTUAL_TLS {
		return nil
	}
	return fmt.Errorf("unrecognized control plane auth policy %q", policy)
}

func validatePrivateKeyProvider(pkpConf *meshconfig.PrivateKeyProvider) error {
	var errs error
	if pkpConf.GetProvider() == nil {
		errs = multierror.Append(errs, errors.New("private key provider configuration is required"))
	}

	switch pkpConf.GetProvider().(type) {
	case *meshconfig.PrivateKeyProvider_Cryptomb:
		cryptomb := pkpConf.GetCryptomb()
		if cryptomb == nil {
			errs = multierror.Append(errs, errors.New("cryptomb configuration is required"))
		} else {
			pollDelay := cryptomb.GetPollDelay()
			if pollDelay == nil {
				errs = multierror.Append(errs, errors.New("pollDelay is required"))
			} else if pollDelay.GetSeconds() == 0 && pollDelay.GetNanos() == 0 {
				errs = multierror.Append(errs, errors.New("pollDelay must be non zero"))
			}
		}
	case *meshconfig.PrivateKeyProvider_Qat:
		qatConf := pkpConf.GetQat()
		if qatConf == nil {
			errs = multierror.Append(errs, errors.New("qat configuration is required"))
		} else {
			pollDelay := qatConf.GetPollDelay()
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

// ValidateLocalityLbSetting checks the LocalityLbSetting of MeshConfig
func ValidateLocalityLbSetting(lb *networking.LocalityLoadBalancerSetting, outlier *networking.OutlierDetection) (errs Validation) {
	if lb == nil {
		return errs
	}

	if len(lb.GetDistribute()) > 0 && len(lb.GetFailover()) > 0 {
		errs = AppendValidation(errs, fmt.Errorf("can not simultaneously specify 'distribute' and 'failover'"))
		return errs
	}

	if len(lb.GetFailover()) > 0 && len(lb.GetFailoverPriority()) > 0 {
		for _, priorityLabel := range lb.GetFailoverPriority() {
			switch priorityLabel {
			case label.LabelTopologyRegion, label.LabelTopologyZone, label.LabelTopologySubzone:
				errs = AppendValidation(errs, fmt.Errorf("can not simultaneously set 'failover' and topology label '%s' in 'failover_priority'", priorityLabel))
				return errs
			}
		}
	}

	srcLocalities := make([]string, 0, len(lb.GetDistribute()))
	for _, locality := range lb.GetDistribute() {
		srcLocalities = append(srcLocalities, locality.From)
		var totalWeight uint32
		destLocalities := make([]string, 0)
		for loc, weight := range locality.To {
			destLocalities = append(destLocalities, loc)
			if weight <= 0 || weight > 100 {
				errs = AppendValidation(errs, fmt.Errorf("locality weight must be in range [1, 100]"))
				return errs
			}
			totalWeight += weight
		}
		if totalWeight != 100 {
			errs = AppendValidation(errs, fmt.Errorf("total locality weight %v != 100", totalWeight))
			return errs
		}
		errs = AppendValidation(errs, validateLocalities(destLocalities))
	}

	errs = AppendValidation(errs, validateLocalities(srcLocalities))

	if (len(lb.GetFailover()) != 0 || len(lb.GetFailoverPriority()) != 0) && outlier == nil {
		errs = AppendValidation(errs, WrapWarning(fmt.Errorf("outlier detection policy must be provided for failover")))
	}

	for _, failover := range lb.GetFailover() {
		if failover.From == failover.To {
			errs = AppendValidation(errs, fmt.Errorf("locality lb failover settings must specify different regions"))
		}
		if strings.Contains(failover.From, "/") || strings.Contains(failover.To, "/") {
			errs = AppendValidation(errs, fmt.Errorf("locality lb failover only specify region"))
		}
		if strings.Contains(failover.To, "*") || strings.Contains(failover.From, "*") {
			errs = AppendValidation(errs, fmt.Errorf("locality lb failover region should not contain '*' wildcard"))
		}
	}

	return errs
}

const (
	regionIndex int = iota
	zoneIndex
	subZoneIndex
)

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

func validateServiceSettings(config *meshconfig.MeshConfig) (errs error) {
	for sIndex, s := range config.ServiceSettings {
		for _, h := range s.Hosts {
			if err := ValidateWildcardDomain(h); err != nil {
				errs = multierror.Append(errs, fmt.Errorf("serviceSettings[%d], host `%s`: %v", sIndex, h, err))
			}
		}
	}
	return errs
}

// ValidateWildcardDomain checks that a domain is a valid FQDN, but also allows wildcard prefixes.
func ValidateWildcardDomain(domain string) error {
	if err := CheckDNS1123Preconditions(domain); err != nil {
		return err
	}
	// We only allow wildcards in the first label; split off the first label (parts[0]) from the rest of the host (parts[1])
	parts := strings.SplitN(domain, ".", 2)
	if !labels.IsWildcardDNS1123Label(parts[0]) {
		return fmt.Errorf("domain name %q invalid (label %q invalid)", domain, parts[0])
	} else if len(parts) > 1 {
		return ValidateDNS1123Labels(parts[1])
	}
	return nil
}

// ValidateWildcardDomainForVirtualServiceBoundToGateway checks that a domain is a valid FQDN, but also allows wildcard prefixes.
// If it is an SNI domain, then it does a special validation where it allows a
// which matches outbound_.<PORT>_._.<HOSTNAME> format
func ValidateWildcardDomainForVirtualServiceBoundToGateway(sni bool, domain string) error {
	if err := CheckDNS1123Preconditions(domain); err != nil {
		return err
	}

	// check if its an auto generated domain, with outbound_ as a prefix.
	if sni && strings.HasPrefix(domain, outboundHostPrefix+"_") {
		// example of a valid domain: outbound_.80_._.e2e.foobar.mesh
		trafficDirectionSuffix, port, hostname, err := parseAutoGeneratedSNIDomain(domain)
		if err != nil {
			return err
		}
		if trafficDirectionSuffix != outboundHostPrefix {
			return fmt.Errorf("domain name %q invalid (label %q invalid)", domain, trafficDirectionSuffix)
		}
		match, _ := regexp.MatchString("([0-9].*)", port)
		if !match {
			return fmt.Errorf("domain name %q invalid (label %q invalid). should follow %s format", domain, port, outboundHostNameFormat)
		}
		match, _ = regexp.MatchString("([a-zA-Z].*)", hostname)
		if !match {
			return fmt.Errorf("domain name %q invalid (label %q invalid). should follow %s format", domain, hostname, outboundHostNameFormat)
		}
		return nil
	}
	return ValidateWildcardDomain(domain)
}

// parseAutoGeneratedSNIDomain parses auto generated sni domains
// which are generated when using AUTO_PASSTHROUGH mode in envoy
func parseAutoGeneratedSNIDomain(domain string) (string, string, string, error) {
	parts := strings.Split(domain, "_")
	if len(parts) < 4 {
		return "", "", "", fmt.Errorf("domain name %s invalid, should follow '%s' format", domain, outboundHostNameFormat)
	}
	trafficDirectionPrefix := parts[0]
	port := strings.Trim(parts[1], ".")
	hostname := strings.Trim(parts[3], ".")
	return trafficDirectionPrefix, port, hostname, nil
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

func validateTrustDomainConfig(config *meshconfig.MeshConfig) (errs error) {
	if err := ValidateTrustDomain(config.TrustDomain); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("trustDomain: %v", err))
	}
	for i, tda := range config.TrustDomainAliases {
		if err := ValidateTrustDomain(tda); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("trustDomainAliases[%d], domain `%s` : %v", i, tda, err))
		}
	}
	return errs
}

func ValidateMeshTLSConfig(mesh *meshconfig.MeshConfig) (errs error) {
	if meshMTLS := mesh.MeshMTLS; meshMTLS != nil {
		if meshMTLS.EcdhCurves != nil {
			errs = multierror.Append(errs, errors.New("mesh TLS does not support ECDH curves configuration"))
		}
	}
	return errs
}

func ValidateMeshTLSDefaults(mesh *meshconfig.MeshConfig) (v Validation) {
	unrecognizedECDHCurves := sets.New[string]()
	validECDHCurves := sets.New[string]()
	duplicateECDHCurves := sets.New[string]()
	if tlsDefaults := mesh.TlsDefaults; tlsDefaults != nil {
		for _, cs := range tlsDefaults.EcdhCurves {
			if !security.IsValidECDHCurve(cs) {
				unrecognizedECDHCurves.Insert(cs)
			} else if validECDHCurves.InsertContains(cs) {
				duplicateECDHCurves.Insert(cs)
			}
		}
	}

	if len(unrecognizedECDHCurves) > 0 {
		v = AppendWarningf(v, "detected unrecognized ECDH curves: %v", sets.SortedList(unrecognizedECDHCurves))
	}

	if len(duplicateECDHCurves) > 0 {
		v = AppendWarningf(v, "detected duplicate ECDH curves: %v", sets.SortedList(duplicateECDHCurves))
	}
	return v
}

// ValidateMeshConfig checks that the mesh config is well-formed
func ValidateMeshConfig(mesh *meshconfig.MeshConfig) (Warning, error) {
	v := Validation{}
	if err := ValidatePort(int(mesh.ProxyListenPort)); err != nil {
		v = AppendValidation(v, multierror.Prefix(err, "invalid proxy listen port:"))
	}

	if err := ValidateConnectTimeout(mesh.ConnectTimeout); err != nil {
		v = AppendValidation(v, multierror.Prefix(err, "invalid connect timeout:"))
	}

	if err := ValidateProtocolDetectionTimeout(mesh.ProtocolDetectionTimeout); err != nil {
		v = AppendValidation(v, multierror.Prefix(err, "invalid protocol detection timeout:"))
	}

	if mesh.DefaultConfig == nil {
		v = AppendValidation(v, errors.New("missing default config"))
	} else {
		v = AppendValidation(v, ValidateMeshConfigProxyConfig(mesh.DefaultConfig))
	}

	v = AppendValidation(v, ValidateLocalityLbSetting(mesh.LocalityLbSetting, &networking.OutlierDetection{}))
	v = AppendValidation(v, validateServiceSettings(mesh))
	v = AppendValidation(v, validateTrustDomainConfig(mesh))

	if err := validateExtensionProvider(mesh); err != nil {
		scope.Warnf("found invalid extension provider (can be ignored if the given extension provider is not used): %v", err)
	}

	v = AppendValidation(v, ValidateMeshTLSConfig(mesh))

	v = AppendValidation(v, ValidateMeshTLSDefaults(mesh))

	return v.Unwrap()
}

// ValidateIPAddress validates that a string in "CIDR notation" or "Dot-decimal notation"
func ValidateIPAddress(addr string) error {
	if _, err := netip.ParseAddr(addr); err != nil {
		return fmt.Errorf("%v is not a valid IP", addr)
	}

	return nil
}

func ValidatePartialWildCard(host string) error {
	if strings.Contains(host, "*") && len(host) != 1 && !strings.HasPrefix(host, "*.") {
		return fmt.Errorf("partial wildcard %q not allowed", host)
	}
	return nil
}

// validates that hostname in ns/<hostname> is a valid hostname according to
// API specs
func validateSidecarOrGatewayHostnamePart(hostname string, isGateway bool) (errs error) {
	// short name hosts are not allowed
	if hostname != "*" && !strings.Contains(hostname, ".") {
		errs = AppendErrors(errs, fmt.Errorf("short names (non FQDN) are not allowed"))
	}

	if err := ValidateWildcardDomain(hostname); err != nil {
		if !isGateway {
			errs = AppendErrors(errs, err)
		}

		// Gateway allows IP as the host string, as well
		if !netutil.IsValidIPAddress(hostname) {
			errs = AppendErrors(errs, err)
		}
	}
	// partial wildcard is not allowed
	// More details please refer to:
	// Gateway: https://istio.io/latest/docs/reference/config/networking/gateway/
	// SideCar: https://istio.io/latest/docs/reference/config/networking/sidecar/#IstioEgressListener
	errs = AppendErrors(errs, ValidatePartialWildCard(hostname))
	return errs
}

func ValidateNamespaceSlashWildcardHostname(hostname string, isGateway bool, gatewaySemantics bool) (errs error) {
	parts := strings.SplitN(hostname, "/", 2)
	if len(parts) != 2 {
		if isGateway {
			// Old style host in the gateway
			return validateSidecarOrGatewayHostnamePart(hostname, true)
		}
		errs = AppendErrors(errs, fmt.Errorf("host must be of form namespace/dnsName"))
		return errs
	}

	if len(parts[0]) == 0 || len(parts[1]) == 0 {
		errs = AppendErrors(errs, fmt.Errorf("config namespace and dnsName in host entry cannot be empty"))
	}

	if !isGateway {
		// namespace can be * or . or ~ or a valid DNS label in sidecars
		if parts[0] != "*" && parts[0] != "." && parts[0] != "~" {
			if !labels.IsDNS1123Label(parts[0]) {
				errs = AppendErrors(errs, fmt.Errorf("invalid namespace value %q in sidecar", parts[0]))
			}
		}
	} else {
		// namespace can be * or . or a valid DNS label in gateways
		// namespace can be ~ in gateways converted from Gateway API when no routes match
		if parts[0] != "*" && parts[0] != "." && (parts[0] != "~" || !gatewaySemantics) {
			if !labels.IsDNS1123Label(parts[0]) {
				errs = AppendErrors(errs, fmt.Errorf("invalid namespace value %q in gateway", parts[0]))
			}
		}
	}
	errs = AppendErrors(errs, validateSidecarOrGatewayHostnamePart(parts[1], isGateway))
	return errs
}

// ValidateIPSubnet checks that a string is in "CIDR notation" or "Dot-decimal notation"
func ValidateIPSubnet(subnet string) error {
	// We expect a string in "CIDR notation" or "Dot-decimal notation"
	// E.g., a.b.c.d/xx form or just a.b.c.d or 2001:1::1/64
	if strings.Count(subnet, "/") == 1 {
		// We expect a string in "CIDR notation", i.e. a.b.c.d/xx or 2001:1::1/64 form
		if _, err := netip.ParsePrefix(subnet); err != nil {
			return fmt.Errorf("%v is not a valid CIDR block", subnet)
		}

		return nil
	}
	return ValidateIPAddress(subnet)
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
	return errs
}

// ValidateMeshNetworks validates meshnetworks.
func ValidateMeshNetworks(meshnetworks *meshconfig.MeshNetworks) (errs error) {
	// TODO validate using the same gateway on multiple networks?
	for name, network := range meshnetworks.Networks {
		if err := validateNetwork(network); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("invalid network %v:", name)))
		}
	}
	return errs
}
