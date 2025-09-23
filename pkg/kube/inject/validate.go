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

package inject

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/validation/agent"
	"istio.io/istio/pkg/util/protomarshal"
)

type annotationValidationFunc func(value string) error

// per-sidecar policy and status
var (
	AnnotationValidation = map[string]annotationValidationFunc{
		annotation.SidecarInterceptionMode.Name:                   validateInterceptionMode,
		annotation.SidecarStatusPort.Name:                         validateStatusPort,
		annotation.SidecarStatusReadinessInitialDelaySeconds.Name: validateUInt32,
		annotation.SidecarStatusReadinessPeriodSeconds.Name:       validateUInt32,
		annotation.SidecarStatusReadinessFailureThreshold.Name:    validateUInt32,
		annotation.SidecarTrafficIncludeOutboundIPRanges.Name:     ValidateIncludeIPRanges,
		annotation.SidecarTrafficExcludeOutboundIPRanges.Name:     ValidateExcludeIPRanges,
		annotation.SidecarTrafficIncludeInboundPorts.Name:         ValidateIncludeInboundPorts,
		annotation.SidecarTrafficExcludeInboundPorts.Name:         ValidateExcludeInboundPorts,
		annotation.SidecarTrafficExcludeOutboundPorts.Name:        ValidateExcludeOutboundPorts,
		annotation.PrometheusMergeMetrics.Name:                    validateBool,
		annotation.ProxyConfig.Name:                               validateProxyConfig,
	}
)

func validateProxyConfig(value string) error {
	config := mesh.DefaultProxyConfig()
	if err := protomarshal.ApplyYAML(value, config); err != nil {
		return fmt.Errorf("failed to convert to apply proxy config: %v", err)
	}
	v := agent.ValidateMeshConfigProxyConfig(config)
	return v.Err
}

func validateAnnotations(annotations map[string]string) (err error) {
	for name, value := range annotations {
		if v, ok := AnnotationValidation[name]; ok {
			if e := v(value); e != nil {
				err = multierror.Append(err, fmt.Errorf("invalid value '%s' for annotation '%s': %v", value, name, e))
			}
		}
	}
	return err
}

func validatePortList(parameterName, ports string) error {
	if _, err := parsePorts(ports); err != nil {
		return fmt.Errorf("%s invalid: %v", parameterName, err)
	}
	return nil
}

// validateInterceptionMode validates the interceptionMode annotation
func validateInterceptionMode(mode string) error {
	switch mode {
	case meshconfig.ProxyConfig_REDIRECT.String():
	case meshconfig.ProxyConfig_TPROXY.String():
	case string(model.InterceptionNone): // not a global mesh config - must be enabled for each sidecar
	default:
		return fmt.Errorf("interceptionMode invalid, use REDIRECT,TPROXY,NONE: %v", mode)
	}
	return nil
}

// ValidateIncludeIPRanges validates the includeIPRanges parameter
func ValidateIncludeIPRanges(ipRanges string) error {
	if ipRanges != "*" {
		if e := validateCIDRList(ipRanges); e != nil {
			return fmt.Errorf("includeIPRanges invalid: %v", e)
		}
	}
	return nil
}

// ValidateExcludeIPRanges validates the excludeIPRanges parameter
func ValidateExcludeIPRanges(ipRanges string) error {
	if e := validateCIDRList(ipRanges); e != nil {
		return fmt.Errorf("excludeIPRanges invalid: %v", e)
	}
	return nil
}

// ValidateIncludeInboundPorts validates the includeInboundPorts parameter
func ValidateIncludeInboundPorts(ports string) error {
	if ports != "*" {
		return validatePortList("includeInboundPorts", ports)
	}
	return nil
}

// ValidateExcludeInboundPorts validates the excludeInboundPorts parameter
func ValidateExcludeInboundPorts(ports string) error {
	return validatePortList("excludeInboundPorts", ports)
}

// ValidateExcludeOutboundPorts validates the excludeOutboundPorts parameter
func ValidateExcludeOutboundPorts(ports string) error {
	return validatePortList("excludeOutboundPorts", ports)
}

// validateStatusPort validates the statusPort parameter
func validateStatusPort(port string) error {
	if _, e := parsePort(port); e != nil {
		return fmt.Errorf("excludeInboundPorts invalid: %v", e)
	}
	return nil
}

// validateUInt32 validates that the given annotation value is a positive integer.
func validateUInt32(value string) error {
	_, err := strconv.ParseUint(value, 10, 32)
	return err
}

// validateBool validates that the given annotation value is a boolean.
func validateBool(value string) error {
	_, err := strconv.ParseBool(value)
	return err
}

func validateCIDRList(cidrs string) error {
	if len(cidrs) > 0 {
		for _, cidr := range strings.Split(cidrs, ",") {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				return fmt.Errorf("failed parsing cidr '%s': %v", cidr, err)
			}
		}
	}
	return nil
}

func splitPorts(portsString string) []string {
	return strings.Split(portsString, ",")
}

func parsePort(portStr string) (int, error) {
	port, err := strconv.ParseUint(strings.TrimSpace(portStr), 10, 16)
	if err != nil {
		return 0, fmt.Errorf("failed parsing port '%s': %v", portStr, err)
	}
	return int(port), nil
}

func parsePorts(portsString string) ([]int, error) {
	portsString = strings.TrimSpace(portsString)
	ports := make([]int, 0)
	if len(portsString) > 0 {
		for _, portStr := range splitPorts(portsString) {
			port, err := parsePort(portStr)
			if err != nil {
				return nil, fmt.Errorf("failed parsing port '%s': %v", portStr, err)
			}
			ports = append(ports, port)
		}
	}
	return ports, nil
}
